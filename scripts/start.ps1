param(
    [int]$Port = 67
)

$ErrorActionPreference = "Stop"
$RootDir = Split-Path -Parent $PSScriptRoot
Set-Location $RootDir

$GoExe = "C:\Program Files\Go\bin\go.exe"
if (-not (Test-Path $GoExe)) {
    $goCmd = Get-Command go -ErrorAction SilentlyContinue
    if ($goCmd) { $GoExe = $goCmd.Source }
}

$ExePath = Join-Path $RootDir "paihub.exe"
if (-not (Test-Path $ExePath)) {
    if (-not $GoExe) {
        Write-Error "paihub.exe not found and Go compiler is missing."
    }
    Write-Host "Building paihub.exe..." -ForegroundColor Cyan
    & $GoExe build -trimpath -ldflags="-s -w" -o $ExePath .
}

Write-Host "`nStarting PAIHub on port $Port..." -ForegroundColor Green
Write-Host "Open locally : http://localhost:$Port" -ForegroundColor Cyan
Write-Host "Press [Enter] or Ctrl+C to shut down the server.`n" -ForegroundColor DarkGray

& $ExePath --port $Port --dir $RootDir
