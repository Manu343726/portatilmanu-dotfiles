# Development

## Workflow

1. **Read the docs** — start with `README.md` and the files in `docs/`.
2. **Make your changes** — edit source code as needed.
3. **Build** — run `make build` to compile daemon + CLI.
4. **Install** — run `make install` to deploy binaries and restart the daemon.
6. **Test** — run `dotfilesctl ping` to verify, use other subcommands as needed.
7. **Commit and push** — all changes (dotfiles, daemon, CLI, docs) must be committed and pushed.

> Always use the Makefile for building and installing. Never run `go build` directly.

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
```

Binaries land at `~/.local/bin/dotfilesd` and `~/.local/bin/dotfilesctl`.

`make install` is a fast variant that skips the build if the git hash hasn't changed since the last install. Use it after editing to quickly redeploy.

## Daemon service management

The daemon runs as a systemd user service. All management goes through `make`:

```sh
make service-install   # install (or update) the systemd unit file
make service-start     # enable and start the daemon
make service-stop      # stop and disable the daemon
make service-restart   # restart the daemon (after code changes)
make service-logs      # tail daemon logs via journalctl
```

Or equivalently with `systemctl --user` directly:

```sh
systemctl --user enable --now  dotfilesd   # enable + start
systemctl --user disable --now dotfilesd   # stop + disable
systemctl --user restart dotfilesd         # restart
systemctl --user status dotfilesd          # check status
journalctl --user -u dotfilesd -f          # follow logs
```

After modifying daemon code, always run `make install` then `systemctl --user restart dotfilesd`.

## Quick test

```sh
# Start the daemon (foreground, kill with Ctrl+C)
dotfilesd

# In another terminal:
dotfilesctl ping
dotfilesctl status
dotfilesctl info
dotfilesctl exec uname -a
dotfilesctl reload tmux
dotfilesctl git status
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
| `cmd/dotfilesctl/main.go` | `main` | CLI client, subcommands |
| `cmd/dotfilesctl/root.go` | `main` | Root command, persistent flags, MCP entry |
| `internal/pkg/daemon/` | `daemon` | Daemon business logic (servers, session, exec) |
| `internal/pkg/cli/` | `cli` | CLI business logic (clients, MCP dispatch, feedback) |
| `proto/dotfilesd/v1/dotfilesdv1/*.proto` | `dotfilesdv1` | Protobuf service definitions (split by domain) |
| `proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect/` | `dotfilesdv1connect` | Generated Connect-RPC clients |

> **Convention:** `cmd/` directories contain only CLI setup (Cobra commands, flag/config wiring). All business logic lives in `internal/pkg/`.

## Adding a new RPC

1. Add the RPC + messages to the appropriate `.proto` file in `proto/dotfilesd/v1/dotfilesdv1/`
2. Run `make proto`
3. Implement the handler in `internal/pkg/daemon/`
4. Wire the handler in `internal/pkg/daemon/server.go`
5. Add a CLI subcommand or MCP tool call in `cmd/dotfilesctl/` or `internal/pkg/cli/`

## Logging

Both the daemon and CLI use Go's `log/slog` package with structured text output to rotated log files.
**Neither the daemon nor the CLI ever write log output to stdout or stderr** — program output
(e.g. `dotfilesctl exec` displaying command results) goes to stdout/stderr directly via `fmt.Print`,
but internal diagnostic logging goes exclusively to files.

| Component | Log file | Rotation | Retention |
|-----------|----------|----------|-----------|
| Daemon  | `~/dotfilesd/logs/dotfilesd.log` | 10 MB per file | 5 backups, 30 days, gzip |
| CLI     | `~/dotfilesd/logs/dotfilesctl.log` | 10 MB per file | 5 backups, 30 days, gzip |

The log directory can be overridden with the `$DOTFILESD_LOG_DIR` environment variable.

### Log levels

| Level | When to use |
|-------|-------------|
| `error` | Unrecoverable or unexpected failures that prevent an operation from completing |
| `warn`  | Recoverable issues, deprecated usage, non-critical failures |
| `info`  | General program workflow — operations starting/completing, state transitions |
| `debug` | Diagnostic details — request payloads, response summaries, internal state |
| `trace` | Verbose low-level details — individual function calls, loop iterations, wire bytes |

### Daemon

Configured via CLI flags, config file (`~/.config/dotfilesd/config.yaml`), or environment:

```sh
dotfilesd --log-level debug --log-dir ~/dotfilesd/logs
# or
DOTFILESD_LOG_LEVEL=debug dotfilesd
```

The daemon also writes logs to its own stdout (captured by systemd-journald when running as a service),
so you can follow live logs with:

```sh
journalctl --user -u dotfilesd -f
```

### CLI

Configured via `--log-level` / `-l` flag (`~/.config/dotfilesctl/config.yaml` or `DOTFILESCTL_LOG_LEVEL` env):

```sh
dotfilesctl --log-level debug exec 'echo hello'
dotfilesctl -l trace system ping
```

The `--verbose` / `-v` flag is a shorthand for `--log-level debug`.

### Adding logging to new code

- **info** — log when an operation starts and completes
- **debug** — log request/response summaries, key intermediate values
- **trace** — log high-frequency events, loop iterations, raw data dumps
- **error** — log failures before returning the error to the caller
- **warn** — log non-fatal edge cases, deprecation notices

Example pattern:

```go
slog.Debug("operation started", "key", value)
result, err := doSomething()
if err != nil {
    slog.Error("operation failed", "key", value, "error", err)
    return fmt.Errorf("operation failed: %w", err)
}
slog.Info("operation completed", "key", value, "result", result)
```

### Log file management

Log files are rotated automatically by [lumberjack](https://github.com/natefinch/lumberjack).
The log directory is created automatically on first use. Old compressed logs are cleaned
up based on the backup count and age settings.
