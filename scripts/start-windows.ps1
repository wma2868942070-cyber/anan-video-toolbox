$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$server = Join-Path $root "dist\anan-video-toolbox-server.exe"
$desktop = Join-Path $root "desktop\build\bin\anan-video-toolbox.exe"
$adobeSource = Join-Path $root "third_party\adobe2api"
$adobeState = Join-Path $env:LOCALAPPDATA "anan-video-toolbox\adobe2api"
$adobeVenv = Join-Path $adobeState ".venv"
$adobePython = Join-Path $adobeVenv "Scripts\python.exe"
$adobeConfigDir = Join-Path $adobeState "config"
$adobeConfig = Join-Path $adobeConfigDir "config.json"
$adobeLogs = Join-Path $adobeState "logs"

function New-RandomHex([int]$Bytes = 24) {
    $buffer = New-Object byte[] $Bytes
    [System.Security.Cryptography.RandomNumberGenerator]::Fill($buffer)
    return [Convert]::ToHexString($buffer).ToLowerInvariant()
}

function Test-AdobeHealth {
    try {
        $health = Invoke-RestMethod -Uri "http://127.0.0.1:6001/api/v1/health" -Method Get -TimeoutSec 2
        return $null -ne $health -and $health.status -eq "ok"
    } catch {
        return $false
    }
}

if (-not (Test-Path -LiteralPath $server)) {
    throw "缺少本地 API 服务：$server，请先运行 go build -o dist\anan-video-toolbox-server.exe ./cmd/server"
}
if (-not (Test-Path -LiteralPath $desktop)) {
    throw "缺少桌面程序：$desktop，请先在 desktop 目录运行 wails build"
}
New-Item -ItemType Directory -Force -Path $adobeState, $adobeConfigDir, $adobeLogs | Out-Null
$env:ADOBE2API_STATE_DIR = $adobeState
$env:ADOBE2API_SOURCE_DIR = $adobeSource
$env:ADOBE2API_PYTHON = $adobePython
$env:ADOBE2API_BASE_URL = "http://127.0.0.1:6001"
$env:PYTHONDONTWRITEBYTECODE = "1"
$adobeStartupError = Join-Path $adobeState "startup-error.txt"

try {
    if (-not (Test-Path -LiteralPath (Join-Path $adobeSource "app.py"))) {
        throw "缺少 Adobe2API Sidecar：$adobeSource"
    }

    if (-not (Test-Path -LiteralPath $adobePython)) {
        $py311 = Get-Command py.exe -ErrorAction SilentlyContinue
        if ($py311) {
            & $py311.Source -3.11 -m venv $adobeVenv
        } else {
            $python311 = Get-Command python.exe -ErrorAction SilentlyContinue
            if (-not $python311) {
                throw "未找到 Python 3.11，无法初始化 Adobe2API。"
            }
            $pythonVersion = & $python311.Source -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')"
            if ($LASTEXITCODE -ne 0 -or $pythonVersion.Trim() -ne "3.11") {
                throw "未找到 Python 3.11；当前 python.exe 为 $pythonVersion。"
            }
            & $python311.Source -m venv $adobeVenv
        }
        if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $adobePython)) {
            throw "Adobe2API Python 3.11 虚拟环境创建失败。"
        }
    }

    $requirements = Join-Path $adobeSource "requirements.txt"
    $requirementsHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $requirements).Hash
    $requirementsStamp = Join-Path $adobeState ".requirements.sha256"
    $installedHash = if (Test-Path -LiteralPath $requirementsStamp) { (Get-Content -LiteralPath $requirementsStamp -Raw).Trim() } else { "" }
    if ($installedHash -ne $requirementsHash) {
        & $adobePython -m pip install --disable-pip-version-check --progress-bar off --timeout 60 -r $requirements
        if ($LASTEXITCODE -ne 0) {
            throw "Adobe2API Python 依赖安装失败。"
        }
        Set-Content -LiteralPath $requirementsStamp -Value $requirementsHash -Encoding UTF8
    }

    if (-not (Test-Path -LiteralPath $adobeConfig)) {
        $config = [ordered]@{
            api_key = New-RandomHex 24
            admin_username = "admin"
            admin_password = New-RandomHex 12
            admin_session_secret = New-RandomHex 32
            public_base_url = "http://127.0.0.1:6001/"
            proxy = ""
            use_proxy = $false
            generate_timeout = 900
            refresh_interval_hours = 15
            retry_enabled = $true
            retry_max_attempts = 5
            retry_backoff_seconds = 3.0
            retry_on_status_codes = @(408, 429, 451, 500, 502, 503, 504)
            retry_on_error_types = @("timeout", "connection", "proxy")
            token_rotation_strategy = "round_robin"
            batch_concurrency = 2
            generated_max_size_mb = 4096
            generated_prune_size_mb = 512
            gpt_image_quality = "low"
        }
        $config | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $adobeConfig -Encoding UTF8
    }

    $adobeListener = Get-NetTCPConnection -LocalPort 6001 -State Listen -ErrorAction SilentlyContinue
    if ($adobeListener) {
        if (-not (Test-AdobeHealth)) {
            $owners = ($adobeListener | Select-Object -ExpandProperty OwningProcess -Unique) -join ", "
            throw "端口 6001 已被占用（PID：$owners），但健康检查失败。"
        }
    } else {
        $adobeOut = Join-Path $adobeLogs "sidecar.out.log"
        $adobeErr = Join-Path $adobeLogs "sidecar.err.log"
        $adobeProcess = Start-Process -FilePath $adobePython -ArgumentList @("-m", "uvicorn", "app:app", "--host", "127.0.0.1", "--port", "6001") -WorkingDirectory $adobeSource -WindowStyle Hidden -RedirectStandardOutput $adobeOut -RedirectStandardError $adobeErr -PassThru
        Set-Content -LiteralPath (Join-Path $adobeState "sidecar.pid") -Value $adobeProcess.Id -Encoding ASCII
        for ($i = 0; $i -lt 60; $i++) {
            Start-Sleep -Milliseconds 500
            if ($adobeProcess.HasExited) {
                $tail = if (Test-Path -LiteralPath $adobeErr) { (Get-Content -LiteralPath $adobeErr -Tail 8) -join " | " } else { "" }
                throw "Adobe2API Sidecar 启动后退出。$tail"
            }
            if (Test-AdobeHealth) { break }
        }
        if (-not (Test-AdobeHealth)) {
            throw "Adobe2API Sidecar 健康检查超时，请查看 $adobeErr"
        }
    }
    Remove-Item -LiteralPath $adobeStartupError -Force -ErrorAction SilentlyContinue
} catch {
    $message = $_.Exception.Message
    Set-Content -LiteralPath $adobeStartupError -Value $message -Encoding UTF8
    Write-Warning "Adobe2API 未能启动：$message；桌面端仍会启动，可在 Adobe Firefly 页面查看并重试。"
}

$listener = Get-NetTCPConnection -LocalPort 8001 -State Listen -ErrorAction SilentlyContinue
if ($listener) {
    $serverFullPath = [System.IO.Path]::GetFullPath($server)
    $serverWriteTime = (Get-Item -LiteralPath $server).LastWriteTimeUtc
    $restartOwnerIds = @()
    foreach ($ownerId in ($listener | Select-Object -ExpandProperty OwningProcess -Unique)) {
        $owner = Get-CimInstance Win32_Process -Filter "ProcessId = $ownerId" -ErrorAction SilentlyContinue
        if (-not $owner -or [string]::IsNullOrWhiteSpace($owner.ExecutablePath)) {
            continue
        }
        $ownerPath = [System.IO.Path]::GetFullPath($owner.ExecutablePath)
        if (-not [string]::Equals($ownerPath, $serverFullPath, [System.StringComparison]::OrdinalIgnoreCase)) {
            continue
        }
        $running = Get-Process -Id $ownerId -ErrorAction SilentlyContinue
        if ($running -and $serverWriteTime -gt $running.StartTime.ToUniversalTime().AddSeconds(1)) {
            $restartOwnerIds += $ownerId
        }
    }
    foreach ($ownerId in $restartOwnerIds) {
        Stop-Process -Id $ownerId -Force -ErrorAction SilentlyContinue
        Wait-Process -Id $ownerId -Timeout 10 -ErrorAction SilentlyContinue
    }
    if ($restartOwnerIds.Count -gt 0) {
        Start-Sleep -Milliseconds 500
        $listener = Get-NetTCPConnection -LocalPort 8001 -State Listen -ErrorAction SilentlyContinue
    }
}
if (-not $listener) {
    Start-Process -FilePath $server -WorkingDirectory $root -WindowStyle Hidden
    for ($i = 0; $i -lt 20; $i++) {
        Start-Sleep -Milliseconds 250
        if (Get-NetTCPConnection -LocalPort 8001 -State Listen -ErrorAction SilentlyContinue) {
            break
        }
    }
}

# VideoClaw synchronizes Leonardo and Adobe model catalogs through port 8001,
# so start it only after the local gateway is listening.
$videoClawState = Join-Path $env:LOCALAPPDATA "anan-video-toolbox\videoclaw"
$videoClawStartupError = Join-Path $videoClawState "startup-error.txt"
try {
    & (Join-Path $PSScriptRoot "start-videoclaw.ps1") | Out-Null
    Remove-Item -LiteralPath $videoClawStartupError -Force -ErrorAction SilentlyContinue
} catch {
    New-Item -ItemType Directory -Force -Path $videoClawState | Out-Null
    $message = $_.Exception.Message
    Set-Content -LiteralPath $videoClawStartupError -Value $message -Encoding UTF8
    Write-Warning "VideoClaw 未能启动：$message；Leonardo、Adobe 和原无限画布仍会正常启动。"
}

# LocalMiniDrama uses the same 8001 catalogs. It is optional and must never
# prevent the desktop, Leonardo, Adobe or the existing canvas from opening.
$localMiniDramaState = Join-Path $env:LOCALAPPDATA "anan-video-toolbox\localminidrama"
$localMiniDramaStartupError = Join-Path $localMiniDramaState "startup-error.txt"
try {
    & (Join-Path $PSScriptRoot "start-localminidrama.ps1") | Out-Null
    Remove-Item -LiteralPath $localMiniDramaStartupError -Force -ErrorAction SilentlyContinue
} catch {
    New-Item -ItemType Directory -Force -Path $localMiniDramaState | Out-Null
    $message = $_.Exception.Message
    Set-Content -LiteralPath $localMiniDramaStartupError -Value $message -Encoding UTF8
    Write-Warning "LocalMiniDrama 未能启动：$message；其他功能仍会正常启动。"
}

Start-Process -FilePath $desktop -WorkingDirectory (Split-Path -Parent $desktop)
