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

- **`main.go`** — Entry point. Starts the Connect RPC HTTP server. Sets up slog logging (JSON to stdout + rotated file).
- **`server.go`** — Connect RPC handler implementations (`dotfilesServer` struct). Implements the `DotfilesService` protobuf interface.

### CLI (`cmd/dotfilesctl/`)

- **`main.go`** — Command-line client that makes Connect RPC calls to the daemon. Outputs to stdout only (no log noise). Supports `--verbose` for debugging.
- **`mcp.go`** — MCP stdio server. Runs in-process when invoked as `dotfilesctl mcp`. Reads JSON-RPC 2.0 messages from stdin (Content-Length framing), dispatches to tool handlers that call the daemon via Connect RPC, and writes responses to stdout.

### Proto (`proto/dotfilesd/v1/dotfilesdv1/`)

- **`service.proto`** — Protobuf service definition.
- **`service.pb.go`** — Generated Go types (protoc-gen-go).
- **`dotfilesdv1connect/service.connect.go`** — Generated Connect RPC client/server stubs (protoc-gen-connect-go).

## RPC service

```protobuf
service DotfilesService {
  rpc Ping(PingRequest) returns (PingResponse);
  rpc Status(StatusRequest) returns (StatusResponse);
  rpc Exec(ExecRequest) returns (ExecResponse);
  rpc Reload(ReloadRequest) returns (ReloadResponse);
  rpc Git(GitRequest) returns (GitResponse);
  rpc SystemInfo(SystemInfoRequest) returns (SystemInfoResponse);
  rpc SudoMethods(SudoMethodsRequest) returns (SudoMethodsResponse);
}
```

## MCP tools

The MCP stdio server (launched via `dotfilesctl mcp`) exposes these tools:

| Tool | Description |
|------|-------------|
| `dotfiles_status` | Dotfiles repo status + system info |
| `dotfiles_reload` | Reload dotfiles configs (tmux, i3, kitty) |
| `dotfiles_git` | Git operations on the dotfiles repo |
| `system_exec` | Execute shell commands |
| `system_info` | Detailed system information |

## Data flow

```
dotfilesctl        ──Connect RPC──▶  dotfilesd  ──exec.Command()──▶  git/i3/tmux/kitty/shell
opencode ──stdio──▶  dotfilesctl mcp  ──Connect RPC──▶  dotfilesd
```

## Directory layout

```
~/dotfilesd/
├── cmd/
│   ├── dotfilesd/           # Daemon (Connect RPC server only)
│   └── dotfilesctl/         # CLI client + MCP stdio server
├── docs/                    # Documentation
├── proto/                   # Protobuf definitions + generated code
│   └── dotfilesd/v1/dotfilesdv1/
├── service/                 # Systemd service template
├── logs/                    # Runtime logs (gitignored)
├── Makefile                 # Build, install, service management
├── go.mod / go.sum
└── README.md
```
