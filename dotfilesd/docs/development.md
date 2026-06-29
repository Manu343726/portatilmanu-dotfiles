# Development

## Prerequisites

- Go 1.26+
- Protocol Buffers compiler (`protoc`)
- `protoc-gen-go` + `protoc-gen-connect-go`

Install proto tools: `make proto-tools`

## Building

```sh
make build          # builds daemon + CLI (runs make proto first)
make install        # fast install: skips build if git hash unchanged
```

Binaries land at `~/.local/bin/dotfilesd` and `~/.local/bin/dotfilesctl`.

## Daemon service management

```sh
make service-install   # install/update the systemd user unit
make service-start     # enable and start the daemon
make service-stop      # stop and disable
make service-restart   # restart after code changes
make service-logs      # tail daemon logs via journalctl
```

Or with `systemctl --user`:
```sh
systemctl --user restart dotfilesd
journalctl --user -u dotfilesd -f
```

After modifying daemon code, always run `make install` (which auto-restarts the daemon).

## Regenerating protobuf code

```sh
make proto
```

Generated `.pb.go` and `.connect.go` files are gitignored — each developer runs `make proto` locally before building.

Proto files are in `proto/dotfilesd/v1/dotfilesdv1/`:

| File | Services defined |
|------|-----------------|
| `system.proto` | `SystemService` (Ping, RuntimeInfo, SudoMethods) |
| `exec.proto` | `ExecService` (Exec, ExecStream, SudoExec, BackgroundExec) |
| `config.proto` | `ConfigService` (Reconfigure, Restart) + `LogLevel` enum |
| `dotfiles.proto` | `DotfilesService` (Status) |
| `session.proto` | `SessionService` (Create, Connect, Finalize, Get, List) |
| `script.proto` | `ScriptService` (RunScript, ListScripts) |
| `feedback.proto` | `FeedbackService` + `InputService`/`ConfirmService`/`ChooseService` |
| `io.proto` | `IOService` |
| `plugin_registry.proto` | `PluginRegistryService` (GetPlugin, ListPlugins) — replaces old PluginService |
| ~~`plugin.proto` / `extension.proto`~~ | ~~Old tool-dispatch protocol — removed~~ |

## Running tests

```sh
go test -count=1 ./internal/...
```

## Quick smoke test

```sh
dotfilesctl ping
dotfilesctl system runtime
dotfilesctl dotfiles status
dotfilesctl exec uname -a
dotfilesctl config reload tmux
dotfilesctl git log
dotfilesctl plugin list
dotfilesctl script list
```

## Project layout

```
dotfilesd/
├── cmd/
│   ├── dotfilesd/           # Daemon entry point
│   └── dotfilesctl/         # CLI entry point
├── internal/pkg/
│   ├── daemon/              # Daemon: RPC handlers, session store, plugin mgr
│   ├── cli/                 # CLI + MCP bridge
│   ├── diagnostics/         # Diagnostics engine (state cache, history, metrics)
│   ├── logging/             # Structured logging package
│   ├── plugin/              # Daemon-side plugin manager (builder, registry, supervisor)
│   ├── rpcreflection/       # gRPC reflection utilities
│   └── shared/              # Shared utilities (build hash)
├── plugin/                  # Plugin SDK (public API for plugin authors)
├── proto/                   # Protobuf definitions
├── docs/                    # Documentation
├── service/                 # systemd unit files
├── test/                    # End-to-end tests
└── Makefile
```

## Workflow

1. Read `docs/architecture.md` for the big picture.
2. Make changes (Go code, proto, scripts, plugins).
3. Run `make build` to compile.
4. Run `make install` to deploy and restart the daemon.
5. Run tests: `go test -count=1 ./internal/...`
6. Commit and push — `git push` pushes to **both** GitHub and the internal LAN server.

> Never run `go build` directly for the final build — use `make build` which regenerates protos first. Never use `sudo` for shell commands — the daemon handles sudo via pkexec or password elicitation.
