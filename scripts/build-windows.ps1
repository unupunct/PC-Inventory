# Builds the complete Windows release: binaries, zip and the Inno Setup installer.
#   powershell -NoProfile -ExecutionPolicy Bypass -File scripts\build-windows.ps1 [-Version 1.0.0]
param([string]$Version = "")

$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

if (-not $Version) {
  $Version = (Select-String -Path "internal\shared\version\version.go" -Pattern 'Version = "([^"]+)"').Matches[0].Groups[1].Value
}
Write-Host "Building PC Inventory Manager $Version for Windows"

# 1. Binaries (also cross-builds Linux/macOS for the other artefacts)
& (Join-Path $PSScriptRoot "build.sh") $Version 2>$null
if ($LASTEXITCODE -ne 0) {
  # build.sh needs bash; fall back to direct go builds
  New-Item -ItemType Directory -Force -Path dist | Out-Null
  $ld = "-s -w -X github.com/pcim/pc-inventory-manager/internal/shared/version.Version=$Version"
  go build -trimpath -ldflags $ld -o dist\pcim-server.exe .\cmd\server
  go build -trimpath -ldflags $ld -o dist\pcim-agent.exe  .\cmd\agent
}

# 2. Installer
$iscc = @(
  "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe",
  "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
  "$env:ProgramFiles\Inno Setup 6\ISCC.exe"
) | Where-Object { Test-Path $_ } | Select-Object -First 1

if (-not $iscc) {
  Write-Warning "Inno Setup not found. Install it with: winget install JRSoftware.InnoSetup"
  Write-Warning "Skipping installer; dist\pcim-$Version-windows.zip is still usable."
  exit 0
}

& $iscc "/DAppVersion=$Version" "packaging\windows\setup.iss"
if ($LASTEXITCODE -ne 0) { throw "Inno Setup failed" }

Write-Host ""
Write-Host "Artifacts:"
Get-ChildItem dist | Sort-Object Name | Format-Table Name, @{n="Size";e={"{0:N1} MB" -f ($_.Length/1MB)}}
