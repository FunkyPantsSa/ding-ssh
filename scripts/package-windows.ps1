$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
if (-not $env:APP_VERSION) { throw "APP_VERSION is required" }
$Version = $env:APP_VERSION
$BinDir = Join-Path $Root "build\bin"
$Dist = Join-Path $Root "dist"
New-Item -ItemType Directory -Force -Path $Dist | Out-Null

$Exe = Join-Path $BinDir "ding-ssh.exe"
if (-not (Test-Path $Exe)) { throw "missing $Exe" }

$Zip = Join-Path $Dist "ding-ssh-$Version-windows-amd64.zip"
if (Test-Path $Zip) { Remove-Item $Zip }
Compress-Archive -Path $Exe -DestinationPath $Zip

$Installer = Get-ChildItem $BinDir -Filter "*installer*.exe" | Select-Object -First 1
if ($Installer) {
  $Dest = Join-Path $Dist "ding-ssh-$Version-windows-amd64-setup.exe"
  Copy-Item $Installer.FullName $Dest -Force
}

Get-ChildItem $Dist | Format-Table Name, Length
