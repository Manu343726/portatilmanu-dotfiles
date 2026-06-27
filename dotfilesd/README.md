# dotfilesd

Go daemon for managing dotfiles on `portatilmanu` (ASUS ROG Flow X13, Manjaro i3).

Exposes dotfiles management through a Connect RPC daemon (port 9105) and a CLI client with an MCP stdio server for AI agent integration.

## Quick start

```sh
cd ~/dotfilesd
make setup
dotfilesctl ping
```

## Usage

```sh
dotfilesctl ping                    # health check
dotfilesctl info                    # system info
dotfilesctl status                  # repo status
dotfilesctl git status              # git operations
dotfilesctl exec 'ls -la'           # run command
dotfilesctl exec --sudo pacman -Syu # run command with sudo
dotfilesctl config reload tmux      # reload config
dotfilesctl config restart          # restart daemon
dotfilesctl mcp                     # start MCP stdio server (for AI agents)
dotfilesctl session create          # create a new session
dotfilesctl session list            # list active sessions
dotfilesctl session finalize <id>   # close a session
dotfilesctl plugin list             # list loaded plugins
dotfilesctl plugin call weather forecast location=Madrid  # call plugin tool
dotfilesctl plugin tree             # show plugin hierarchy
dotfilesctl script run hello.dsh    # run a script
dotfilesctl script list             # list available scripts
```

## Plugins

dotfilesd supports dynamic extensions called **plugins** — standalone Go programs
that register tools which get automatically exposed as both CLI subcommands and
MCP tools. See `docs/plugins.md` for full documentation.

### Weather plugin (example)

Fetches forecasts via wttr.in:

```sh
dotfilesctl plugin call weather forecast location=Madrid
# → Weather for Madrid, Spain
#    ⛅  +22°C
```

### Resources plugin (example)

Monitors system resources (RAM, CPU, disk, I/O) with background data collection:

```sh
dotfilesctl plugin call resources current     # resource snapshot
dotfilesctl plugin call resources top         # top processes by CPU/mem
dotfilesctl plugin call resources ps          # detailed process list
dotfilesctl plugin call resources history     # sparkline graphs
```

## MCP tools (for AI agents)

When running `dotfilesctl mcp`, the following tools are available via MCP stdio:

| Tool | Description |
|------|-------------|
| `system_ping` | Daemon health check |
| `system_info` | Detailed system information |
| `system_sudo` | Available sudo methods |
| `dotfiles_status` | Dotfiles repo status |
| `dotfiles_git` | Git operations on dotfiles repo |
| `exec_run` | Execute shell commands (supports `sudo=true`) |
| `config_reload` | Reload dotfiles configs (tmux, i3, kitty) |
| `config_reconfigure` | Change daemon runtime config (log level) |
| `config_restart` | Gracefully restart the daemon |
| `script_run` | Run a multi-step script with feedback directives |
| `script_list` | List registered scripts |
| `weather_forecast` | Get weather forecast (plugin) |
| `resources_current` | System resource snapshot (plugin) |
| `resources_top` | Top processes by CPU/memory (plugin) |
| `resources_ps` | Detailed process list (plugin) |
| `resources_history` | Historical sparkline graphs (plugin) |

### Sudo password flow

For commands with `sudo=true`, the daemon supports multiple auth methods:
- **Elicitation** — native form prompts (VS Code Chat, Claude)
- **pkexec** — desktop polkit dialog
- **MCP Apps webview** — password masking in VS Code via HTML webview form

## Documentation

| Topic | File |
|-------|------|
| Architecture | `docs/architecture.md` |
| Development | `docs/development.md` |
| Deploy & Install | `docs/deploy.md` |
| Debugging | `docs/debugging.md` |
| Features | `docs/features.md` |
| Plugin System | `docs/plugins.md` |

## Project layout

```
cmd/dotfilesd/            # Daemon (Connect RPC server)
cmd/dotfilesctl/          # CLI client
internal/pkg/daemon/      # Daemon RPC server implementations
internal/pkg/cli/         # CLI action logic + MCP server
internal/pkg/plugin/      # Plugin manager, builder, runtime, registry
internal/pkg/shared/      # Shared utilities
plugin/                   # Public plugin SDK
plugins/                  # Example plugins (weather/, resources/)
proto/                    # Protobuf definitions + generated code
scripts/                  # Dotfiles scripts (.dsh files)
service/                  # Systemd user service template
docs/                     # Documentation
Makefile                  # Build, install, proto, service, plugin, test

## Tech stack

- **Go 1.26** — Standard library slog, net/http
- **Connect RPC** — gRPC-compatible HTTP API on port 9105
- **protobuf** — Service definitions and code generation
- **Cobra + Viper** — CLI framework (commands, flags, config)
- **MCP** — Model Context Protocol stdio (via `dotfilesctl mcp`)
- **MCP Apps** — HTML webviews for interactive UI (sudo password form)
- **lumberjack** — Log file rotation
- **systemd** — User service for auto-start
