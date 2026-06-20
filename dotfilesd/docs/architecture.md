# Architecture

dotfilesd has two components: a daemon and a CLI client that also serves as the MCP gateway.

```
┌─────────────┐     Connect RPC (gRPC/HTTP)     ┌──────────────┐
│ dotfilesctl ├───── port 9105 ─────────────────▶│  dotfilesd   │
│ (CLI)       │                                  │  (daemon)    │
└──────┬──────┘                                  └──────────────┘
       │                                                    │
       │ MCP (stdio)                              ┌─────────┴─────────┐
┌──────┴──────┐                                   │  System calls     │
│ opencode    │                                   │  (shell, git,     │
│ (AI agent)  │                                   │   i3, tmux,       │
└─────────────┘                                   │   pkexec)         │
                                                    └───────────────────┘
```

## Port

| Port | Protocol | Purpose |
|------|----------|---------|
| 9105 | Connect RPC | Tool/service API (gRPC-compatible), `127.0.0.1` only |

## Components

### Daemon (`cmd/dotfilesd/`)

Thin CLI setup: Cobra command definition + flag/config wiring. Delegates to `internal/pkg/daemon/`.

- **`main.go`** — Entry point. Calls `newRootCmd().Execute()`.
- **`root.go`** — Cobra root command. Flag definitions, viper config loading, calls `daemon.New(cfg).Start()`.

### CLI (`cmd/dotfilesctl/`)

Thin CLI setup: Cobra command tree. Each `RunE` delegates to `internal/pkg/cli/`.

- **`main.go`** — Entry point. Calls `newRootCmd().Execute()`.
- **`root.go`** — Cobra root command. Persistent flags, client creation, subcommand registration.
- **`system.go`** — `system` subcommand tree (ping/info/sudo).
- **`dotfiles.go`** — `dotfiles` subcommand tree (status/git).
- **`exec.go`** — `exec` subcommand.
- **`config.go`** — `config` subcommand tree (reload/reconfigure/restart).
- **`mcp.go`** — `mcp` subcommand, starts MCP stdio server.

### Library (`internal/pkg/`)

Shared/internal packages containing all business logic.

- **`internal/pkg/daemon/`** — Connect RPC server implementations:
  - `server.go` — Daemon struct, HTTP server setup, graceful restart, signal handling.
  - `system.go` — System service (Ping, SystemInfo, SudoMethods).
  - `dotfiles.go` — Dotfiles service (Status, Git).
  - `exec.go` — Exec service (Exec, SudoExec).
  - `config.go` — Config service (Reload, Reconfigure, Restart).
  - `helpers.go` — Command execution helpers (runCmd, runCmdFull).
  - `logging.go` — Logging setup, level parsing.

- **`internal/pkg/cli/`** — CLI action logic:
  - `client.go` — Client creation (ConnectClients struct).
  - `system.go` — RunPing, RunInfo, RunSudoMethods.
  - `dotfiles.go` — RunStatus, RunGit.
  - `exec.go` — RunExec, RunSudoExec.
  - `config.go` — RunReload, RunReconfigure, RunRestart.
  - `mcp.go` — MCP stdio server (JSON-RPC framing, tool dispatch).
  - `enums.go` — Protobuf enum string parsing.
  - `helpers.go` — Logging setup, Fatalf.

- **`internal/pkg/shared/`** — Shared utilities:
  - `buildhash.go` — CheckBuildHash (binary version vs source staleness).

### Proto (`proto/dotfilesd/v1/dotfilesdv1/`)

- **`*.proto`** — Protobuf service definitions.
- **`*.pb.go`** — Generated Go types (protoc-gen-go).
- **`dotfilesdv1connect/*.connect.go`** — Generated Connect RPC client/server stubs.

## RPC services

```protobuf
service SystemService {
  rpc Ping(PingRequest) returns (PingResponse);
  rpc SystemInfo(SystemInfoRequest) returns (SystemInfoResponse);
  rpc SudoMethods(SudoMethodsRequest) returns (SudoMethodsResponse);
}
service DotfilesService {
  rpc Status(StatusRequest) returns (StatusResponse);
  rpc Git(GitRequest) returns (GitResponse);
}
service ExecService {
  rpc Exec(ExecRequest) returns (ExecResponse);
  rpc SudoExec(SudoExecRequest) returns (SudoExecResponse);
}
service ConfigService {
  rpc Reload(ReloadRequest) returns (ReloadResponse);
  rpc Reconfigure(ReconfigureRequest) returns (ReconfigureResponse);
  rpc Restart(RestartRequest) returns (RestartResponse);
}
```

## MCP tools

The MCP stdio server (launched via `dotfilesctl mcp`) exposes these tools:

| Tool | Service | Description |
|------|---------|-------------|
| `system_ping` | SystemService | Daemon health check |
| `system_info` | SystemService | Detailed system information |
| `system_sudo` | SystemService | Available sudo methods |
| `dotfiles_status` | DotfilesService | Dotfiles repo status |
| `dotfiles_git` | DotfilesService | Git operations on the dotfiles repo |
| `exec_run` | ExecService | Execute shell commands |
| `config_reload` | ConfigService | Reload dotfiles configs |
| `config_reconfigure` | ConfigService | Change daemon runtime config |
| `config_restart` | ConfigService | Gracefully restart the daemon |

## Sessions

Sessions allow clients to group related requests that share state. Each session
has an ID, request counter, last-active timestamp, and a key-value data map.

- **Explicit sessions** are created via `CreateSession` and finalized via
  `FinalizeSession`. The client passes the session ID as a `Session-Id` HTTP
  header on subsequent requests.
- **Ephemeral sessions** are created automatically by the daemon when a request
  arrives without a `Session-Id` header. They exist for the duration of that
  single request and are not stored in the session registry.
- If a request references a finalized or unknown session, the daemon falls back
  to an ephemeral session for that request.

```bash
dotfilesctl session create                    # returns a session ID
dotfilesctl --session <id> system ping        # use session in a request
dotfilesctl --session <id> exec 'ls -la'      # same session, shared state
dotfilesctl session finalize <id>             # mark session complete
dotfilesctl session list                      # show active sessions
```

## Data flow

```
dotfilesctl        ──Connect RPC──▶  dotfilesd  ──exec.Command()──▶  git/i3/tmux/kitty/shell
opencode ──stdio──▶  dotfilesctl mcp  ──Connect RPC──▶  dotfilesd
```

## Directory layout

```
~/dotfilesd/
├── cmd/
│   ├── dotfilesd/           # Daemon CLI setup (Cobra + config)
│   └── dotfilesctl/         # CLI client CLI setup (Cobra)
├── internal/
│   └── pkg/
│       ├── daemon/          # Connect RPC server implementations
│       │   └── session.go   # Session store + session service server
│       ├── cli/             # CLI action logic + MCP server
│       │   └── session.go   # CLI session actions (create, finalize, list)
│       └── shared/          # Shared utilities
├── docs/                    # Documentation
├── proto/                   # Protobuf definitions + generated code
│   └── dotfilesd/v1/dotfilesdv1/
├── service/                 # Systemd service template
├── logs/                    # Runtime logs (gitignored)
├── Makefile                 # Build, install, service management
├── go.mod / go.sum
└── README.md
```
