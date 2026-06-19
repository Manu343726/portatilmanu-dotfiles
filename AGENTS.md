# Agent instructions

This is a dotfiles repo for the `portatilmanu` machine (ASUS ROG Flow X13 running Manjaro i3).

**Important: The workspace root `/home/manu343726/` is the real `$HOME` of this machine — not a cloned repo in a project directory. Git was init'd directly in `~` to track dotfiles, with `.gitignore` set to `/*` by default so only explicitly whitelisted files are tracked.**

## Rules

1. **Always commit changes to dotfiles.** After modifying any tracked dotfile (`.zshrc`, `.i3/config`, `.config/tmux/`, `.config/kitty/`, etc.), stage and commit with a descriptive message, then push.

2. **Check `.agents/skills/`** for project-specific skills before working. Load any relevant skill with the skills tool.

3. Keep changes idempotent — reloading configs should work cleanly.

4. Follow the Monokai palette (`#272822` bg, `#A6E22E` accent, etc.) when adding visual configs.

5. **Use the dotfilesd daemon for dotfiles operations.** The daemon runs as a systemd user service and exposes:
   - **MCP** via `dotfilesctl mcp` stdio — for AI agents (tools: `system_ping`, `system_info`, `system_sudo`, `dotfiles_status`, `dotfiles_git`, `exec_run`, `config_reload`).
   - **Connect RPC** at `http://127.0.0.1:9105` — accessible via the `dotfilesctl` CLI client (`~/dotfilesd/cmd/dotfilesctl/main.go`).
   - Prefer `dotfilesctl exec --sudo` for privileged commands instead of raw `pkexec`.
   - When modifying dotfilesd source code, rebuild with `make build` and restart with `systemctl --user restart dotfilesd`.
   - See `~/dotfilesd/docs/` for full daemon documentation.
