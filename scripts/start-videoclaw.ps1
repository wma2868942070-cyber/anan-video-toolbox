$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$source = Join-Path $root "third_party\videoclaw"
$backend = Join-Path $source "backend"
$frontend = Join-Path $source "frontend"
$state = Join-Path $env:LOCALAPPDATA "anan-video-toolbox\videoclaw"
$venv = Join-Path $state ".venv"
$python = Join-Path $venv "Scripts\python.exe"
$config = Join-Path $state "config.yaml"
$logs = Join-Path $state "logs"
$backendPort = 6101
$frontendPort = 6102

function Test-VideoClawBackend {
    try {
        $health = Invoke-RestMethod -Uri "http://127.0.0.1:$backendPort/api/health" -TimeoutSec 2
        return $null -ne $health -and $health.status -eq "ok"
    } catch {
        return $false
    }
}

function Test-VideoClawFrontend {
    try {
        $response = Invoke-WebRequest -Uri "http://127.0.0.1:$frontendPort" -UseBasicParsing -TimeoutSec 3
        return [int]$response.StatusCode -eq 200
    } catch {
        return $false
    }
}

function Test-PlaywrightChromium {
    $browserRoot = if (-not [string]::IsNullOrWhiteSpace($env:PLAYWRIGHT_BROWSERS_PATH)) {
        $env:PLAYWRIGHT_BROWSERS_PATH
    } else {
        Join-Path $env:LOCALAPPDATA "ms-playwright"
    }
    if (-not (Test-Path -LiteralPath $browserRoot)) { return $false }
    return $null -ne (Get-ChildItem -LiteralPath $browserRoot -Recurse -Filter "chrome.exe" -File -ErrorAction SilentlyContinue | Select-Object -First 1)
}

function Get-LatestFrontendWriteTicks {
    $paths = @(
        (Join-Path $frontend "app"),
        (Join-Path $frontend "components"),
        (Join-Path $frontend "config"),
        (Join-Path $frontend "lib"),
        (Join-Path $frontend "public")
    )
    $files = @()
    foreach ($path in $paths) {
        if (Test-Path -LiteralPath $path) {
            $files += Get-ChildItem -LiteralPath $path -Recurse -File
        }
    }
    foreach ($name in @("package.json", "package-lock.json", "next.config.ts", "tsconfig.json", "postcss.config.mjs")) {
        $path = Join-Path $frontend $name
        if (Test-Path -LiteralPath $path) { $files += Get-Item -LiteralPath $path }
    }
    if ($files.Count -eq 0) { return "0" }
    return (($files | Sort-Object LastWriteTimeUtc -Descending | Select-Object -First 1).LastWriteTimeUtc.Ticks).ToString()
}

if (-not (Test-Path -LiteralPath (Join-Path $backend "api_server.py"))) {
    throw "缺少 VideoClaw 后端源码：$backend"
}
if (-not (Test-Path -LiteralPath (Join-Path $frontend "package.json"))) {
    throw "缺少 VideoClaw 前端源码：$frontend"
}

New-Item -ItemType Directory -Force -Path $state, $logs | Out-Null
$env:VIDEOCLAW_SOURCE_DIR = $source
$env:VIDEOCLAW_STATE_DIR = $state
$env:VIDEOCLAW_CONFIG_PATH = $config
$env:VIDEOCLAW_HOST = "127.0.0.1"
$env:VIDEOCLAW_PORT = "$backendPort"
$env:VIDEOCLAW_BACKEND_URL = "http://127.0.0.1:$backendPort"
$env:NEXT_PUBLIC_API_URL = "http://127.0.0.1:$backendPort"
$env:NEXT_PUBLIC_API_BASE_URL = "http://127.0.0.1:$backendPort"
$env:PYTHONDONTWRITEBYTECODE = "1"
$env:VIDEOCLAW_LEONARDO_BASE_URL = "http://127.0.0.1:8001/v1"
$env:VIDEOCLAW_ADOBE_BASE_URL = "http://127.0.0.1:8001/adobe/v1"
$serverEnv = Join-Path $root ".env.server.local"
if (Test-Path -LiteralPath $serverEnv) {
    $apiKeyLine = Get-Content -LiteralPath $serverEnv | Where-Object { $_ -match '^\s*LEOSTUDIO_API_KEY\s*=' } | Select-Object -First 1
    if ($apiKeyLine) {
        $apiKey = ($apiKeyLine -split '=', 2)[1].Trim().Trim('"').Trim("'")
        if (-not [string]::IsNullOrWhiteSpace($apiKey)) {
            $env:VIDEOCLAW_LOCAL_API_KEY = $apiKey
        }
    }
}
$startupError = Join-Path $state "startup-error.txt"

if (-not (Test-Path -LiteralPath $python)) {
    $pyLauncher = Get-Command py.exe -ErrorAction SilentlyContinue
    if ($pyLauncher) {
        & $pyLauncher.Source -3.11 -m venv $venv
    } else {
        $pythonCommand = Get-Command python.exe -ErrorAction SilentlyContinue
        if (-not $pythonCommand) { throw "未找到 Python 3.11，无法初始化 VideoClaw。" }
        $version = & $pythonCommand.Source -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')"
        if ($LASTEXITCODE -ne 0 -or $version.Trim() -ne "3.11") {
            throw "VideoClaw 需要 Python 3.11；当前 python.exe 为 $version。"
        }
        & $pythonCommand.Source -m venv $venv
    }
    if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $python)) {
        throw "VideoClaw Python 3.11 虚拟环境创建失败。"
    }
}

$requirements = Join-Path $backend "requirements.txt"
$requirementsHash = (Get-FileHash -LiteralPath $requirements -Algorithm SHA256).Hash
$requirementsStamp = Join-Path $state ".requirements.sha256"
$installedRequirementsHash = if (Test-Path -LiteralPath $requirementsStamp) {
    (Get-Content -LiteralPath $requirementsStamp -Raw).Trim()
} else { "" }
if ($installedRequirementsHash -ne $requirementsHash) {
    $uv = Get-Command uv.exe -ErrorAction SilentlyContinue
    if ($uv) {
        & $uv.Source pip install --python $python -r $requirements
    } else {
        & $python -m pip install --disable-pip-version-check --progress-bar off --timeout 90 -r $requirements
    }
    if ($LASTEXITCODE -ne 0) { throw "VideoClaw Python 依赖安装失败。" }
    Set-Content -LiteralPath $requirementsStamp -Value $requirementsHash -Encoding ASCII
}

if (-not (Test-Path -LiteralPath $config)) {
    Copy-Item -LiteralPath (Join-Path $backend "config.yaml.example") -Destination $config
    $raw = Get-Content -LiteralPath $config -Raw
    $raw = $raw -replace '(?m)^\s*port:\s*8000\s*$', "  port: $backendPort"
    Set-Content -LiteralPath $config -Value $raw -Encoding UTF8
}

$npm = Get-Command npm.cmd -ErrorAction SilentlyContinue
$node = Get-Command node.exe -ErrorAction SilentlyContinue
if (-not $npm -or -not $node) { throw "未找到 Node.js/npm，无法初始化 VideoClaw 前端。" }

$packageLock = Join-Path $frontend "package-lock.json"
$packageHash = (Get-FileHash -LiteralPath $packageLock -Algorithm SHA256).Hash
$packageStamp = Join-Path $state ".package-lock.sha256"
$installedPackageHash = if (Test-Path -LiteralPath $packageStamp) {
    (Get-Content -LiteralPath $packageStamp -Raw).Trim()
} else { "" }
if ($installedPackageHash -ne $packageHash -or -not (Test-Path -LiteralPath (Join-Path $frontend "node_modules\next"))) {
    & $npm.Source ci --no-audit --no-fund --progress=false --prefix $frontend
    if ($LASTEXITCODE -ne 0) { throw "VideoClaw 前端依赖安装失败。" }
    Set-Content -LiteralPath $packageStamp -Value $packageHash -Encoding ASCII
}

$sourceTicks = Get-LatestFrontendWriteTicks
$buildStamp = Join-Path $state ".frontend-build.stamp"
$builtTicks = if (Test-Path -LiteralPath $buildStamp) { (Get-Content -LiteralPath $buildStamp -Raw).Trim() } else { "" }
if ($builtTicks -ne $sourceTicks -or -not (Test-Path -LiteralPath (Join-Path $frontend ".next\BUILD_ID"))) {
    & $npm.Source run build --prefix $frontend
    if ($LASTEXITCODE -ne 0) { throw "VideoClaw 前端构建失败。" }
    Set-Content -LiteralPath $buildStamp -Value $sourceTicks -Encoding ASCII
}

$backendListener = Get-NetTCPConnection -LocalPort $backendPort -State Listen -ErrorAction SilentlyContinue
if ($backendListener -and -not (Test-VideoClawBackend)) {
    $owners = ($backendListener | Select-Object -ExpandProperty OwningProcess -Unique) -join ", "
    throw "端口 $backendPort 已被占用（PID：$owners），但不是可用的 VideoClaw 后端。"
}
if (-not $backendListener) {
    $backendOut = Join-Path $logs "backend.out.log"
    $backendErr = Join-Path $logs "backend.err.log"
    $backendProcess = Start-Process -FilePath $python -ArgumentList @("api_server.py") -WorkingDirectory $backend -WindowStyle Hidden -RedirectStandardOutput $backendOut -RedirectStandardError $backendErr -PassThru
    Set-Content -LiteralPath (Join-Path $state "backend.pid") -Value $backendProcess.Id -Encoding ASCII
    for ($i = 0; $i -lt 80; $i++) {
        Start-Sleep -Milliseconds 250
        if ($backendProcess.HasExited) {
            $tail = if (Test-Path -LiteralPath $backendErr) { (Get-Content -LiteralPath $backendErr -Tail 10) -join " | " } else { "" }
            throw "VideoClaw 后端启动后退出。$tail"
        }
        if (Test-VideoClawBackend) { break }
    }
    if (-not (Test-VideoClawBackend)) { throw "VideoClaw 后端健康检查超时。" }
}

$frontendListener = Get-NetTCPConnection -LocalPort $frontendPort -State Listen -ErrorAction SilentlyContinue
if ($frontendListener -and -not (Test-VideoClawFrontend)) {
    $owners = ($frontendListener | Select-Object -ExpandProperty OwningProcess -Unique) -join ", "
    throw "端口 $frontendPort 已被占用（PID：$owners），但不是可用的 VideoClaw 前端。"
}
if (-not $frontendListener) {
    $nextBin = Join-Path $frontend "node_modules\next\dist\bin\next"
    $frontendOut = Join-Path $logs "frontend.out.log"
    $frontendErr = Join-Path $logs "frontend.err.log"
    $frontendProcess = Start-Process -FilePath $node.Source -ArgumentList @($nextBin, "start", "--hostname", "127.0.0.1", "--port", "$frontendPort") -WorkingDirectory $frontend -WindowStyle Hidden -RedirectStandardOutput $frontendOut -RedirectStandardError $frontendErr -PassThru
    Set-Content -LiteralPath (Join-Path $state "frontend.pid") -Value $frontendProcess.Id -Encoding ASCII
    for ($i = 0; $i -lt 80; $i++) {
        Start-Sleep -Milliseconds 250
        if ($frontendProcess.HasExited) {
            $tail = if (Test-Path -LiteralPath $frontendErr) { (Get-Content -LiteralPath $frontendErr -Tail 10) -join " | " } else { "" }
            throw "VideoClaw 前端启动后退出。$tail"
        }
        if (Test-VideoClawFrontend) { break }
    }
    if (-not (Test-VideoClawFrontend)) { throw "VideoClaw 前端健康检查超时。" }
}

# Chromium is only needed by a subset of document/browser workflows. Do not
# block the whole desktop application on a large browser download: install it
# in the background after both Sidecar services are already healthy.
if (-not (Test-PlaywrightChromium)) {
    $playwrightPID = Join-Path $state "playwright-install.pid"
    $installing = $false
    if (Test-Path -LiteralPath $playwrightPID) {
        $existingPID = [int]((Get-Content -LiteralPath $playwrightPID -Raw).Trim())
        $installing = $null -ne (Get-Process -Id $existingPID -ErrorAction SilentlyContinue)
    }
    if (-not $installing) {
        $playwrightOut = Join-Path $logs "playwright-install.out.log"
        $playwrightErr = Join-Path $logs "playwright-install.err.log"
        $install = Start-Process -FilePath $python -ArgumentList @("-m", "playwright", "install", "chromium") -WorkingDirectory $backend -WindowStyle Hidden -RedirectStandardOutput $playwrightOut -RedirectStandardError $playwrightErr -PassThru
        Set-Content -LiteralPath $playwrightPID -Value $install.Id -Encoding ASCII
    }
}

Remove-Item -LiteralPath $startupError -Force -ErrorAction SilentlyContinue
Write-Output "VideoClaw ready: http://127.0.0.1:$frontendPort"
