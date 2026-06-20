# Agent instructions

This is a dotfiles repo for the `portatilmanu` machine (ASUS ROG Flow X13 running Manjaro i3).

**Important: The workspace root `/home/manu343726/` is the real `$HOME` of this machine — not a cloned repo in a project directory. Git was init'd directly in `~` to track dotfiles, with `.gitignore` set to `/*` by default so only explicitly whitelisted files are tracked.**

## Rules

1. **Always commit and push changes.** After modifying any tracked file (dotfiles, daemon source, CLI source, docs, etc.), stage and commit with a descriptive message, then push.

2. **Read docs first.** Before making changes, read `~/dotfilesd/README.md` and `~/dotfilesd/docs/` for context. Follow the development guide in `docs/development.md`.

3. **Check `.agents/skills/`** for project-specific skills before working. Load any relevant skill with the skills tool.

4. Keep changes idempotent — reloading configs should work cleanly.

5. Follow the Monokai palette (`#272822` bg, `#A6E22E` accent, etc.) when adding visual configs.

6. **Use the Makefile for building and installing the daemon/CLI.** Run `make build` to compile, then `make install` to deploy binaries. After modifying daemon or client code, always run `make install` and restart the daemon if needed.

7. **Use the dotfilesd daemon for dotfiles operations.** The daemon runs as a systemd user service and exposes:
   - **MCP** via `dotfilesctl mcp` stdio — for AI agents (tools: `system_ping`, `system_info`, `system_sudo`, `dotfiles_status`, `dotfiles_git`, `exec_run`, `config_reload`).
   - **Connect RPC** at `http://127.0.0.1:9105` — accessible via the `dotfilesctl` CLI client (`~/dotfilesd/cmd/dotfilesctl/main.go`).
   - Prefer `dotfilesctl exec --sudo` for privileged commands instead of raw `pkexec`.
   - After modifying daemon/client code, run `make install` and restart the daemon if needed.
   - See `~/dotfilesd/docs/` for full daemon documentation.
