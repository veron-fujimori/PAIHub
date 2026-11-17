$ErrorActionPreference = "Stop"

Write-Host "=== PAIHub: CLI Checker & Installer ===" -ForegroundColor Cyan

$nodeCmd = Get-Command node -ErrorAction SilentlyContinue
if (-not $nodeCmd) {
    $fallbackNodeDir = Join-Path $env:USERPROFILE ".cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin"
    $progNodeDir = "C:\Program Files\nodejs"
    if (Test-Path (Join-Path $progNodeDir "node.exe")) {
        $env:PATH = "$progNodeDir;$env:PATH"
        $nodeCmd = Get-Command node -ErrorAction SilentlyContinue
    } elseif (Test-Path (Join-Path $fallbackNodeDir "node.exe")) {
        Write-Host "Found local Node.js at $fallbackNodeDir" -ForegroundColor Yellow
        $env:PATH = "$fallbackNodeDir;$env:PATH"
        $nodeCmd = Get-Command node -ErrorAction SilentlyContinue
    }
}

if (-not $nodeCmd) {
    Write-Host "Node.js was not found on this PC." -ForegroundColor Yellow
    if (Get-Command winget -ErrorAction SilentlyContinue) {
        Write-Host "Installing Node.js LTS via winget..." -ForegroundColor Cyan
        winget install OpenJS.NodeJS.LTS --accept-package-agreements --accept-source-agreements
        $env:PATH = "C:\Program Files\nodejs;$env:APPDATA\npm;$env:PATH"
    } else {
        Write-Error "Please install Node.js LTS first from https://nodejs.org/"
    }
}

$npmCmd = Get-Command npm -ErrorAction SilentlyContinue
if (-not $npmCmd) {
    Write-Error "npm was not found in PATH. Restart PowerShell after installing Node.js."
}

Write-Host "Node version: $(node -v)" -ForegroundColor Green
Write-Host "npm version : $(npm -v)" -ForegroundColor Green

$packages = @(
    @{ Name = "Claude Code"; Pkg = "@anthropic-ai/claude-code"; Bin = "claude" },
    @{ Name = "ChatGPT Codex"; Pkg = "@openai/codex"; Bin = "codex" }
)

foreach ($item in $packages) {
    Write-Host "`nInstalling / updating $($item.Name) ($($item.Pkg))..." -ForegroundColor Cyan
    npm install -g $item.Pkg
}

Write-Host "`n=== Done! Next Steps ===" -ForegroundColor Green
Write-Host "1. Make sure each CLI is active or signed in to its respective subscription:"
Write-Host "   - agy           (Antigravity CLI)"
Write-Host "   - claude        (Sign in with Claude Pro/Max)"
Write-Host "   - codex         (Sign in with ChatGPT Plus/Pro)"
Write-Host "   - gh auth login (If you want to switch GitHub CLI accounts)"
Write-Host "2. Run .\scripts\start.ps1 to launch PAIHub."
