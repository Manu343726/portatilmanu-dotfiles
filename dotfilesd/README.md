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
dotfilesctl ping              # health check
dotfilesctl status            # repo status
dotfilesctl info              # system info
dotfilesctl exec --sudo pacman -Syu  # run command
dotfilesctl reload tmux       # reload config
dotfilesctl git status        # git operations
dotfilesctl plugin list       # list loaded plugins
```

## Plugins

dotfilesd supports dynamic extensions called **plugins** — standalone Go programs
that register tools which get automatically exposed as both CLI subcommands and
MCP tools. See `docs/plugins.md` for documentation.

Example: a weather plugin that fetches forecasts via wttr.in:

```sh
dotfilesctl plugin call weather forecast location=Madrid
# → Weather for Madrid, Spain
#    ⛅  +22°C
```

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
cmd/dotfilesd/     # Daemon (Connect RPC server only)
cmd/dotfilesctl/   # CLI client
plugin/            # Public plugin SDK
plugins/           # Example plugins
proto/             # Protobuf definitions and generated code
internal/pkg/plugin/  # Plugin manager, builder, runtime, registry
service/           # Systemd user service template
docs/              # Documentation
Makefile           # Build, install, service management

## Tech stack

- **Go 1.26** — Standard library slog, net/http
- **Connect RPC** — gRPC-compatible HTTP API on port 9105
- **MCP** — Model Context Protocol stdio (via `dotfilesctl mcp`)
- **lumberjack** — Log file rotation
- **systemd** — User service for auto-start
