$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$source = Join-Path $root "third_party\localminidrama"
$state = Join-Path $env:LOCALAPPDATA "anan-video-toolbox\localminidrama"
$runtime = Join-Path $state "app"
$backend = Join-Path $runtime "backend-node"
$frontend = Join-Path $runtime "frontweb"
$logs = Join-Path $state "logs"
$data = Join-Path $state "data"
$storage = Join-Path $data "storage"
$configDir = Join-Path $backend "configs"
$config = Join-Path $configDir "config.yaml"
$port = 6201

function Test-LocalMiniDramaHealth {
    try {
        $health = Invoke-RestMethod -Uri "http://127.0.0.1:$port/health" -TimeoutSec 2
        return $null -ne $health -and $health.status -eq "ok"
    } catch {
        return $false
    }
}

function Get-LatestSourceWriteTicks {
    $files = Get-ChildItem -LiteralPath $source -Recurse -File | Where-Object {
        $_.FullName -notmatch '[\\/](node_modules|dist|data|\.git)[\\/]'
    }
    if (-not $files) { return "0" }
    return (($files | Sort-Object LastWriteTimeUtc -Descending | Select-Object -First 1).LastWriteTimeUtc.Ticks).ToString()
}

function Get-CompatibleNodeRuntime {
    $systemNode = Get-Command node.exe -ErrorAction SilentlyContinue
    if ($systemNode) {
        $version = (& $systemNode.Source --version 2>$null).TrimStart('v')
        $major = 0
        if ([int]::TryParse(($version -split '\.', 2)[0], [ref]$major) -and $major -ge 18 -and $major -le 22) {
            $systemNpm = Get-Command npm.cmd -ErrorAction SilentlyContinue
            if ($systemNpm) {
                return @{ Node = $systemNode.Source; Npm = $systemNpm.Source }
            }
        }
    }

    $runtimeRoot = Join-Path $env:LOCALAPPDATA "anan-video-toolbox\runtimes"
    $marker = Join-Path $runtimeRoot "node22-current.txt"
    if (Test-Path -LiteralPath $marker) {
        $savedDir = Join-Path $runtimeRoot (Get-Content -LiteralPath $marker -Raw).Trim()
        $savedNode = Join-Path $savedDir "node.exe"
        $savedNpm = Join-Path $savedDir "npm.cmd"
        if ((Test-Path -LiteralPath $savedNode) -and (Test-Path -LiteralPath $savedNpm)) {
            return @{ Node = $savedNode; Npm = $savedNpm }
        }
    }

    New-Item -ItemType Directory -Force -Path $runtimeRoot | Out-Null
    try {
        $releases = Invoke-RestMethod -Uri "https://nodejs.org/dist/index.json" -TimeoutSec 60
        $release = $releases | Where-Object {
            $_.version -match '^v22\.' -and $_.files -contains 'win-x64-zip'
        } | Select-Object -First 1
        if (-not $release) { throw "Node.js 发布目录中没有可用的 Node 22 Windows x64 包。" }

        $archiveName = "node-$($release.version)-win-x64.zip"
        $runtimeName = "node-$($release.version)-win-x64"
        $archive = Join-Path $runtimeRoot $archiveName
        $runtimeDir = Join-Path $runtimeRoot $runtimeName
        if (-not (Test-Path -LiteralPath (Join-Path $runtimeDir "node.exe"))) {
            Invoke-WebRequest -UseBasicParsing -Uri "https://nodejs.org/dist/$($release.version)/$archiveName" -OutFile $archive -TimeoutSec 300
            Expand-Archive -LiteralPath $archive -DestinationPath $runtimeRoot -Force
        }
        $portableNode = Join-Path $runtimeDir "node.exe"
        $portableNpm = Join-Path $runtimeDir "npm.cmd"
        if (-not (Test-Path -LiteralPath $portableNode) -or -not (Test-Path -LiteralPath $portableNpm)) {
            throw "Node 22 便携运行时解压不完整。"
        }
        Set-Content -LiteralPath $marker -Value $runtimeName -Encoding ASCII
        return @{ Node = $portableNode; Npm = $portableNpm }
    } catch {
        throw "无法准备 LocalMiniDrama 所需的 Node 22 运行时：$($_.Exception.Message)"
    }
}

if (-not (Test-Path -LiteralPath (Join-Path $source "backend-node\src\server.js"))) {
    throw "缺少 LocalMiniDrama 后端源码：$source"
}
if (-not (Test-Path -LiteralPath (Join-Path $source "frontweb\package.json"))) {
    throw "缺少 LocalMiniDrama 前端源码：$source"
}

New-Item -ItemType Directory -Force -Path $state, $runtime, $logs, $data, $storage | Out-Null

# Keep an executable runtime copy under LOCALAPPDATA. Runtime data and npm
# artifacts never modify the vendored third-party source tree.
& robocopy $source $runtime /E /R:1 /W:1 /XD node_modules dist data .git /XF *.log *.pid | Out-Null
if ($LASTEXITCODE -ge 8) { throw "LocalMiniDrama 运行副本同步失败（robocopy $LASTEXITCODE）。" }

$nodeRuntime = Get-CompatibleNodeRuntime
$node = $nodeRuntime.Node
$npm = $nodeRuntime.Npm
$nodeDir = Split-Path -Parent $node
$env:Path = "$nodeDir;$env:Path"

$serverEnv = Join-Path $root ".env.server.local"
$apiKey = ""
if (Test-Path -LiteralPath $serverEnv) {
    $apiKeyLine = Get-Content -LiteralPath $serverEnv | Where-Object { $_ -match '^\s*LEOSTUDIO_API_KEY\s*=' } | Select-Object -First 1
    if ($apiKeyLine) { $apiKey = ($apiKeyLine -split '=', 2)[1].Trim().Trim('"').Trim("'") }
}
if ([string]::IsNullOrWhiteSpace($apiKey)) {
    throw "未找到 LEOSTUDIO_API_KEY，LocalMiniDrama 无法接入本地 8001 模型。"
}

$env:LOCALMINIDRAMA_SOURCE_DIR = $source
$env:LOCALMINIDRAMA_STATE_DIR = $state
$env:LOCAL_GATEWAY_API_KEY = $apiKey
$env:LOCAL_GATEWAY_LEONARDO_URL = "http://127.0.0.1:8001/v1"
$env:LOCAL_GATEWAY_ADOBE_URL = "http://127.0.0.1:8001/adobe/v1"
$env:PORT = "$port"
$env:HOST = "127.0.0.1"
$env:WEB_DIST_PATH = Join-Path $frontend "dist"
$env:NODE_ENV = "production"
$env:VIDEO_POLL_LOG_MAX = "8192"

New-Item -ItemType Directory -Force -Path $configDir | Out-Null
$dbYaml = (Join-Path $data "localminidrama.db").Replace("'", "''")
$storageYaml = $storage.Replace("'", "''")
@"
app:
  name: LocalMiniDrama API
  version: 1.2.8
  debug: false
  language: zh
server:
  port: $port
  host: 127.0.0.1
  cors_origins:
    - http://127.0.0.1:$port
    - http://localhost:$port
  read_timeout: 900
  write_timeout: 900
  insecure_tls: true
database:
  type: sqlite
  path: '$dbYaml'
  max_idle: 10
  max_open: 100
storage:
  type: local
  local_path: '$storageYaml'
  base_url: http://127.0.0.1:$port/static
video:
  generation_timeout_minutes: 360
ai:
  default_text_provider: openai
  default_image_provider: openai
  default_video_provider: openai
style:
  default_style: ''
  default_role_style: full body and face clearly visible, character centered, consistent facial features, high detail
  default_scene_style: wide establishing shot, highly detailed environment, sharp focus
  default_prop_style: object centered, clean background, studio lighting, high detail
  default_image_ratio: '16:9'
  default_video_ratio: '16:9'
  default_prop_ratio: '1:1'
  default_image_size: 1024x1024
vendor_lock:
  enabled: false
image_proxy:
  expire_hours: 24
  use_for_video: false
  upload_timeout_seconds: 60
  upload_max_attempts: 1
"@ | Set-Content -LiteralPath $config -Encoding UTF8

foreach ($project in @($backend, $frontend)) {
    $lock = Join-Path $project "package-lock.json"
    $hash = (Get-FileHash -LiteralPath $lock -Algorithm SHA256).Hash
    $stampName = if ($project -eq $backend) { ".backend-package.sha256" } else { ".frontend-package.sha256" }
    $stamp = Join-Path $state $stampName
    $installed = if (Test-Path -LiteralPath $stamp) { (Get-Content -LiteralPath $stamp -Raw).Trim() } else { "" }
    $missingBuildTools = $project -eq $frontend -and -not (Test-Path -LiteralPath (Join-Path $project "node_modules\.bin\vite.cmd"))
    if ($installed -ne $hash -or -not (Test-Path -LiteralPath (Join-Path $project "node_modules")) -or $missingBuildTools) {
        & $npm ci --include=dev --no-audit --no-fund --progress=false --prefix $project
        if ($LASTEXITCODE -ne 0) { throw "LocalMiniDrama npm 依赖安装失败：$project" }
        Set-Content -LiteralPath $stamp -Value $hash -Encoding ASCII
    }
}

$sourceTicks = Get-LatestSourceWriteTicks
$buildStamp = Join-Path $state ".frontend-build.stamp"
$builtTicks = if (Test-Path -LiteralPath $buildStamp) { (Get-Content -LiteralPath $buildStamp -Raw).Trim() } else { "" }
if ($builtTicks -ne $sourceTicks -or -not (Test-Path -LiteralPath (Join-Path $frontend "dist\index.html"))) {
    & $npm run build --prefix $frontend
    if ($LASTEXITCODE -ne 0) { throw "LocalMiniDrama 前端构建失败。" }
    Set-Content -LiteralPath $buildStamp -Value $sourceTicks -Encoding ASCII
}

$listener = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
if ($listener -and -not (Test-LocalMiniDramaHealth)) {
    $owners = ($listener | Select-Object -ExpandProperty OwningProcess -Unique) -join ", "
    throw "端口 $port 已被占用（PID：$owners），但不是可用的 LocalMiniDrama。"
}
if (-not $listener) {
    $serverOut = Join-Path $logs "server.out.log"
    $serverErr = Join-Path $logs "server.err.log"
    $process = Start-Process -FilePath $node -ArgumentList @("src/server.js") -WorkingDirectory $backend -WindowStyle Hidden -RedirectStandardOutput $serverOut -RedirectStandardError $serverErr -PassThru
    Set-Content -LiteralPath (Join-Path $state "server.pid") -Value $process.Id -Encoding ASCII
    for ($i = 0; $i -lt 100; $i++) {
        Start-Sleep -Milliseconds 250
        if ($process.HasExited) {
            $tail = if (Test-Path -LiteralPath $serverErr) { (Get-Content -LiteralPath $serverErr -Tail 12) -join " | " } else { "" }
            throw "LocalMiniDrama 启动后退出。$tail"
        }
        if (Test-LocalMiniDramaHealth) { break }
    }
    if (-not (Test-LocalMiniDramaHealth)) { throw "LocalMiniDrama 健康检查超时。" }
}

try {
    Invoke-RestMethod -Uri "http://127.0.0.1:$port/api/v1/local-gateway/sync" -Method Post -TimeoutSec 45 | Out-Null
} catch {
    Write-Warning "LocalMiniDrama 已启动，但模型同步暂时失败；已保留上次目录：$($_.Exception.Message)"
}

Remove-Item -LiteralPath (Join-Path $state "startup-error.txt") -Force -ErrorAction SilentlyContinue
Write-Output "LocalMiniDrama ready: http://127.0.0.1:$port"
