# Development

## Prerequisites

- Go 1.26+ (`go version`)
- Protocol Buffers compiler (`protoc --version`)
- `protoc-gen-go` + `protoc-gen-connect-go`

Install proto tools:

```sh
make proto-tools
```

## Building

```sh
make build          # builds both daemon and CLI
make build-dev      # same, but without stripping debug info
```

Binaries land at `~/.local/bin/dotfilesd` and `~/.local/bin/dotfilesctl`.

## Quick test

```sh
# Start the daemon (foreground, kill with Ctrl+C)
~/.local/bin/dotfilesd

# In another terminal:
~/.local/bin/dotfilesctl ping
~/.local/bin/dotfilesctl status
~/.local/bin/dotfilesctl info
~/.local/bin/dotfilesctl exec uname -a
~/.local/bin/dotfilesctl reload tmux
~/.local/bin/dotfilesctl git status
```

## Regenerating protobuf code

```sh
make proto
```

Requires `protoc`, `protoc-gen-go`, and `protoc-gen-connect-go` on `$PATH`.

## Managing dependencies

```sh
make deps           # go mod tidy + go mod download
```

## Code layout

| Path | Package | Purpose |
|------|---------|---------|
| `cmd/dotfilesd/main.go` | `main` | Daemon entry, logging setup, server start |
| `cmd/dotfilesd/server.go` | `main` | Connect RPC handler implementations |
| `cmd/dotfilesd/mcp.go` | `main` | MCP SSE server, JSON-RPC dispatch |
| `cmd/dotfilesctl/main.go` | `main` | CLI client, subcommands |
| `proto/dotfilesd/v1/dotfilesdv1/service.proto` | `dotfilesdv1` | Protobuf service definition |

## Adding a new RPC

1. Add the RPC + messages to `proto/dotfilesd/v1/dotfilesdv1/service.proto`
2. Run `make proto`
3. Implement the handler method on `dotfilesServer` in `server.go`
4. Add a CLI subcommand in `cmd/dotfilesctl/main.go`
5. Add an MCP tool mapping in `mcp.go`'s `callTool` method

## Logging

The daemon uses `log/slog` with a JSON handler writing to `io.MultiWriter(os.Stdout, lumberjack)`:

- **stdout** — captured by systemd-journald when running as a service
- **rotated file** — `~/dotfilesd/logs/dotfilesd.log` (10 MB, 5 backups, 30 day retention, gzip compressed)

The CLI logs to `~/dotfilesd/logs/dotfilesctl.log`. Pass `--verbose` to additionally log to stderr.

Override the log directory with `$DOTFILESD_LOG_DIR`.
