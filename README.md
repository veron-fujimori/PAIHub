# PAIHub

**PAIHub** is a lightweight personal web hub (`Go single binary`) for running your subscription AI coding CLIs (**Antigravity**, **Claude Code**, **ChatGPT Codex**) and local **Ollama** models from any phone or laptop browser over LAN/Tailscale.

## Why Use PAIHub?

- **Use PC AI CLIs from anywhere:** Send prompts, inspect/edit project files, and run PowerShell commands from your phone or second device without SSH or remote desktop.
- **Zero idle overhead:** Launches each CLI on demand and exits cleanly after every response (**0 MB AI RAM while idle**), with session resume support.
- **Workspace & GitHub integration:** Switch local folders via Windows File Explorer, clone GitHub repos, or work directly via `gh` CLI (`GitHub Direct` mode), with optional PIN protection for network access.
- **Creator Mode (Custom Plugins):** Switch to **Creator Mode** in the workspace picker to let your AI build standalone plugins inside `data/plugins/<id>/plugin.json` and supporting scripts without modifying `paihub.exe` (for example, connecting WhatsApp, Telegram/Discord bots, or custom automation workflows).

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
