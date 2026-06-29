# dotfilesd

Go daemon for managing dotfiles on `portatilmanu` (ASUS ROG Flow X13, Manjaro i3).

Exposes dotfiles management through a Connect RPC daemon (port 9105) and a CLI client with an MCP stdio server for AI agent integration.

The repository is mirrored to two remotes — **GitHub** (public) and an **internal LAN server** (private). A plain `git push` sends to both.

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
dotfilesctl sudo                    # available sudo methods
dotfilesctl system diag             # runtime diagnostics (state tree, events, metrics)
dotfilesctl status                  # repo status
dotfilesctl exec 'ls -la'           # run command
dotfilesctl exec --sudo pacman -Syu # run command with sudo
dotfilesctl config reload tmux      # reload config
dotfilesctl config reconfigure --log-level debug  # change log level at runtime
dotfilesctl config restart          # restart daemon
dotfilesctl mcp                     # start MCP stdio server (for AI agents)
dotfilesctl session create          # create a new session
dotfilesctl session list            # list active sessions
dotfilesctl session finalize <id>   # close a session
dotfilesctl plugin list             # list loaded plugins
dotfilesctl plugin load <name>      # load a plugin dynamically
dotfilesctl plugin unload <name>    # unload a plugin
dotfilesctl git status              # git operations (via scripts)
dotfilesctl script run hello.dsh    # run a script
dotfilesctl script list             # list available scripts

# Plugin commands (auto-discovered, typed flags from proto schemas):
dotfilesctl weather forecast --location=Madrid --days=5
dotfilesctl resources top --count=10 --sort=cpu
dotfilesctl resources current
```

## Plugins

dotfilesd supports dynamic extensions called **plugins** — standalone Go programs
that serve Connect RPC services. The daemon discovers all services via gRPC
reflection and auto-exposes them as CLI subcommands (with typed flags from proto
schemas) and per-method MCP tools. See `docs/plugins.md` for full documentation.

### Weather plugin (example)

Fetches forecasts via wttr.in:

```sh
dotfilesctl weather forecast --location=Madrid
# → Weather for Madrid, Spain
#    ⛅  +22°C
```

### Resources plugin (example)

Monitors system resources (RAM, CPU, disk, I/O) with background data collection:

```sh
dotfilesctl resources current               # resource snapshot
dotfilesctl resources top --count=10        # top processes by CPU/mem
dotfilesctl resources ps --pid=1234         # detailed process list
dotfilesctl resources history --count=30    # sparkline graphs
```

## MCP tools (for AI agents)

When running `dotfilesctl mcp`, the following tools are available via MCP stdio:

| Tool | Description |
|------|-------------|
| `system_ping` | Daemon health check |
| `system_runtime` | Detailed system information |
| `system_sudo` | Available sudo methods |
| `dotfiles_status` | Dotfiles repo status |
| `dotfiles_git` | Git operations on dotfiles repo (status, diff, add, commit, push, log) |
| `exec_run` | Execute shell commands (supports `sudo=true`) |
| `config_reload` | Reload dotfiles configs (tmux, i3, kitty) |
| `config_reconfigure` | Change daemon runtime config (log level) |
| `config_restart` | Gracefully restart the daemon |
| `script_run` | Run a multi-step script with feedback directives |
| `script_list` | List registered scripts |
| `<plugin>_<method>` | Auto-discovered plugin methods (e.g. `weather_forecast`, `resources_current`, `resources_top`, `resources_ps`, `resources_history`) |
| `_sudo_submit_password` | Internal MCP Apps webview tool (visibility: app) |

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
| Logging System | `docs/logging.md` |
| Plugin System | `docs/plugins.md` |
| Plugin RPC Architecture (design spec) | `docs/plugin-rpc-architecture.md` |
| Diagnostics System (design spec) | `docs/diagnostics.md` |
| MCP Apps Research | `docs/mcp-apps-research.md` |

## Project layout

```
cmd/dotfilesd/            # Daemon (Connect RPC server)
cmd/dotfilesctl/          # CLI client + MCP stdio server
internal/pkg/daemon/      # Daemon RPC server implementations
internal/pkg/cli/         # CLI action logic + MCP bridge
internal/pkg/plugin/      # Plugin manager (builder, runtime, registry, supervisor)
internal/pkg/diagnostics/ # Diagnostics engine (state cache, history, metrics)
internal/pkg/logging/     # Structured logging package
internal/pkg/shared/      # Shared utilities (build hash)
internal/pkg/rpcreflection/ # gRPC reflection utilities
plugin/                   # Public plugin SDK (Serve, Context, Extractions)
proto/                    # Protobuf definitions + generated code
scripts/                  # Dotfiles scripts (.dsh files)
service/                  # Systemd user service template
docs/                     # Documentation
test/                     # End-to-end tests
Makefile                  # Build, install, proto, service, plugin, test

## Tech stack

- **Go 1.26** — Standard library slog, net/http
- **Connect RPC** — gRPC-compatible HTTP API on port 9105
- **protobuf** — Service definitions and code generation
- **grpcreflect** — gRPC server reflection for plugin service discovery
- **Cobra + Viper** — CLI framework (commands, flags, config)
- **MCP** — Model Context Protocol stdio (via `dotfilesctl mcp`)
- **MCP Apps** — HTML webviews for interactive UI (sudo password form)
- **lumberjack** — Log file rotation
- **systemd** — User service for auto-start
