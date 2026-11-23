# PAIHub

PAIHub lets you use the AI coding tools on your Windows PC from a phone or another computer over your local network or Tailscale. It runs as a single Go binary and supports Antigravity, Claude Code, ChatGPT Codex, and local Ollama models.

## What you can do

- Send prompts, edit project files, or run PowerShell commands in the browser. The CLI starts on demand and exits after each response. You can resume the session later.
- Pick a local folder in Windows File Explorer, clone a GitHub repo, or use GitHub Direct to work through `gh`.
- Open Creator Mode from the workspace picker to build a plugin, such as a Telegram bot. Each plugin has a manifest at `data/plugins/<id>/plugin.json` and can include its own scripts. You can add plugins without rebuilding `paihub.exe`.

## Quick Start

> Requires **Go 1.25+** to build from source. Set an optional **Access PIN** in **Settings** before exposing over LAN/Tailscale.

1. **(Optional) Install AI CLIs:**
   ```powershell
   .\scripts\install-clis.ps1
   ```
2. **Build & Run:**
   ```powershell
   go build -trimpath -ldflags="-s -w" -o paihub.exe .
   .\paihub.exe
   ```
   Or launch directly with `.\scripts\start.ps1`. Flags: `--port 67`, `--bind 0.0.0.0`, `--dir <path>`.
3. **Open in browser:** Visit `http://localhost:67` (or the LAN/Tailscale URL printed on startup).
