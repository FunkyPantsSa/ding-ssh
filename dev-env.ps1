# ding-ssh development environment activator
#
# Usage (dot-source so PATH changes apply to the CURRENT session):
#     . .\dev-env.ps1
#     wails dev
#
# Why this exists:
#   * Go is installed globally (C:\Program Files\Go) and Node globally
#     (C:\Program Files\nodejs). Their installers edit the PATH, but a shell
#     that was already running keeps its old environment - this script fixes
#     that for the current session.
#
# Note: this file is intentionally ASCII-only. Windows PowerShell 5.1 on a
# Chinese-locale system reads .ps1 files as GBK, which corrupts UTF-8 text.

$ErrorActionPreference = 'Stop'

function Add-PathEntry {
    param([Parameter(Mandatory)][string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) { return $false }
    $parts = $env:PATH -split ';' | Where-Object { $_ -ne '' }
    if ($parts -notcontains $Path) { $env:PATH = ($Path, $env:PATH) -join ';' }
    return $true
}

$projectRoot = if ($PSScriptRoot) { $PSScriptRoot } else { (Get-Location).Path }

# 1) Go: global install first, then a project-local portable copy.
foreach ($candidate in @(
    'C:\Program Files\Go\bin',
    (Join-Path $env:LOCALAPPDATA 'Programs\Go\bin'),
    (Join-Path $projectRoot '.tools\go\bin')
)) {
    if (Add-PathEntry $candidate) { break }
}

# 1b) Go bin: where "go install" puts wails.exe.
if (Get-Command go -ErrorAction SilentlyContinue) {
    Add-PathEntry (Join-Path (go env GOPATH) 'bin') | Out-Null
} else {
    Add-PathEntry (Join-Path $env:USERPROFILE 'go\bin') | Out-Null
}

# 2) Node / npm: the global install (its installer edits the machine PATH, but an
#    already-running shell keeps its old environment, so add it explicitly).
Add-PathEntry 'C:\Program Files\nodejs' | Out-Null
Add-PathEntry (Join-Path $env:APPDATA 'npm') | Out-Null

# 3) Last resort: the DSH-bundled Node runtime, if nothing else provides Node.
if (-not (Get-Command node -ErrorAction SilentlyContinue)) {
    $dshNode = Join-Path $env:USERPROFILE '.dsh\dsh-runtimes\dsh-primary-runtime\dependencies\node\bin'
    if (-not (Add-PathEntry $dshNode)) {
        Write-Warning 'Node.js not found. Install Node 18+ (winget install OpenJS.NodeJS.LTS).'
    }
}

# 4) Go module proxy: proxy.golang.org is unreachable on this network.
if (Get-Command go -ErrorAction SilentlyContinue) {
    $proxy = go env GOPROXY 2>$null
    if ($proxy -notmatch 'goproxy\.cn') {
        go env -w GOPROXY='https://goproxy.cn,direct' GOSUMDB='sum.golang.google.cn'
    }
}

Write-Host '--- ding-ssh dev environment ---' -ForegroundColor Cyan
foreach ($tool in 'go', 'node', 'npm', 'wails') {
    $cmd = Get-Command $tool -ErrorAction SilentlyContinue
    if ($cmd) {
        $version = switch ($tool) {
            'go'    { (& go version) -replace 'go version ', '' }
            'wails' { (& wails version) 2>&1 | Select-Object -First 1 }
            default { (& $tool --version) 2>&1 | Select-Object -First 1 }
        }
        Write-Host ("  {0,-6} {1}" -f $tool, $version) -ForegroundColor Green
    } else {
        Write-Host ("  {0,-6} NOT FOUND" -f $tool) -ForegroundColor Yellow
    }
}
Write-Host 'Commands: wails dev | wails build | go test ./internal/...' -ForegroundColor Cyan
