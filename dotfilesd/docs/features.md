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

### `system diag`

Query the diagnostics engine for runtime state tree, events, and metrics.

```sh
# Show the current runtime tree (daemon, plugins, sessions, executors)
dotfilesctl system diag

# Filter by type
dotfilesctl system diag --include-types=plugin,daemon

# Show historical events
dotfilesctl system diag --history

# Show metrics
dotfilesctl system diag --metrics

# Include finished/crashed resources within a time window
dotfilesctl system diag --time-window=5m

# Show all resources (including idle/finished)
dotfilesctl system diag --show-idle
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

Run registered scripts (.dsh files in `~/.config/dotfilesd/scripts/`). Script commands
are also auto-registered as top-level CLI subcommands in groups:

```sh
dotfilesctl git/status         # same as dotfilesctl script run git/status
dotfilesctl reload/tmux         # same as dotfilesctl script run reload/tmux

# Or use the generic script command:
dotfilesctl script run git/status
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
dotfilesctl plugin list              # all plugins and services
dotfilesctl plugin list -v           # verbose with input schemas
dotfilesctl plugin load <name>       # load a plugin by name
dotfilesctl plugin unload <name>     # unload a plugin by name
dotfilesctl plugin reload            # rescan plugins directory
```

### Plugin commands (auto-discovered)

Plugin services and RPCs are auto-discovered via grpcreflect and registered as
top-level CLI subcommands with typed flags generated from proto schemas.

```sh
# Single-service plugin (service level elided):
dotfilesctl weather forecast --location=Madrid --days=5

# Multi-service plugin:
dotfilesctl resources current
dotfilesctl resources top --count=10 --sort=cpu
dotfilesctl resources ps --pid=1234
dotfilesctl resources history --count=30

# Use --json flag for raw JSON output instead of formatted output:
dotfilesctl weather forecast --location=Madrid --json
```

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
| `<plugin>_<method>` | Auto-discovered via `PluginExecutorService.CallPlugin` (per-method, with typed input schema from proto) |
| `_sudo_submit_password` | Internal MCP Apps webview tool |
