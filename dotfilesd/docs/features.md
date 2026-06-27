# Features

## CLI commands

### `system ping`

```sh
dotfilesctl system ping
# → dotfilesd v0.1.0 (pid 12345, up 5s)
```

### `system runtime`

OS-level identification of the machine the daemon runs on.

```sh
dotfilesctl system runtime
# → OS:      linux
# → Kernel:  6.12.91-1-MANJARO
# → Shell:   /usr/bin/zsh
# → Desktop: i3
# → Host:    portatilmanu
# → Uptime:  up 13 hours, 45 minutes
# → Tools:   sudo, pkexec, tmux, i3, kitty
```

Resource monitoring (RAM, CPU, disk) is handled by the `resources` plugin, not this command.

### `system sudo`

```sh
dotfilesctl system sudo
# → current:  pkexec
# → has sudo: true
# → available: pkexec, sudo
```

### `dotfiles status`

```sh
dotfilesctl dotfiles status
# → branch: master (clean)
# → last:   f054be8 feat: add new feature
# → host:   portatilmanu
# → uptime: up 13 hours, 45 minutes
```

Hostname and uptime come from `RuntimeInfo`; git info from `DotfilesService.Status`.

### `exec`

Run a shell command. Output streams in real time.

```sh
dotfilesctl exec "uname -a"
dotfilesctl exec --sudo "pacman -Syu"
```

### `script run` / `script list`

Run registered scripts (.dsh files in `~/.config/dotfilesd/scripts/`).

```sh
dotfilesctl script run registerd git/status
dotfilesctl script list
```

### `config reload`

Reload dotfiles configurations via scripts in `scripts/reload/`.

```sh
dotfilesctl config reload tmux     # → scripts/reload/tmux.dsh
dotfilesctl config reload i3       # → scripts/reload/i3.dsh
dotfilesctl config reload kitty    # → scripts/reload/kitty.dsh
dotfilesctl config reload all      # → scripts/reload/all.dsh
```

Adding a new reload target is creating a `.dsh` file — no recompilation needed.

### `config reconfigure`

Change the daemon's log level at runtime without restarting.

```sh
dotfilesctl config reconfigure --log-level debug
```

### `config restart`

Gracefully restart the daemon (starts a new process, exits old).

```sh
dotfilesctl config restart
```

### `git`

Git operations on the dotfiles repo via scripts in `scripts/git/`.

```sh
dotfilesctl git status    # → scripts/git/status.dsh
dotfilesctl git diff      # → scripts/git/diff.dsh
dotfilesctl git add       # → scripts/git/add.dsh
dotfilesctl git commit    # → scripts/git/commit.dsh
dotfilesctl git push      # → scripts/git/push.dsh
dotfilesctl git log       # → scripts/git/log.dsh
```

### `plugin`

```sh
dotfilesctl plugin list              # all plugins and tools
dotfilesctl plugin list -v           # verbose with input schemas
dotfilesctl plugin tree              # directory hierarchy
dotfilesctl plugin list-tools <name> # tools for one plugin
```

### `weather forecast`, `resources current`, ...

Plugin tools are auto-discovered and registered as CLI subcommands. Run `dotfilesctl --help` to see all available commands.

## Sessions

Sessions group requests that share a persistent shell process.

```sh
# Create a session
dotfilesctl session create

# Use it with any command
dotfilesctl --session <id> exec 'export FOO=bar'
dotfilesctl --session <id> exec 'echo $FOO'   # same shell, FOO is set

# List active sessions
dotfilesctl session list

# Finalize (close shell, mark complete)
dotfilesctl session finalize <id>
```

## MCP tools

When running as `dotfilesctl mcp`, the following MCP tools are exposed:

| Tool name | Maps to |
|-----------|---------|
| `system_ping` | `Ping` RPC |
| `system_runtime` | `RuntimeInfo` RPC |
| `system_sudo` | `SudoMethods` RPC |
| `dotfiles_status` | `Status` RPC |
| `exec_run` | `Exec` RPC (streaming via MCP Apps for sudo) |
| `config_reconfigure` | `Reconfigure` RPC |
| `config_restart` | `Restart` RPC |
| `config_reload` | `scripts/reload/<target>` via `RunScript` |
| `dotfiles_git` | `scripts/git/<action>` via `RunScript` |
| `script_run` | `RunScript` RPC |
| `script_list` | `ListScripts` RPC |
| `<plugin>_<tool>` | `CallPluginTool` RPC (auto-discovered) |
| `_sudo_submit_password` | Internal MCP Apps webview tool |
