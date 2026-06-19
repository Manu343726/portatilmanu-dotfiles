# Architecture

dotfilesd is a Go daemon that exposes dotfiles management via two protocols:

```
┌─────────────┐     Connect RPC (gRPC/HTTP)     ┌──────────────┐
│ dotfilesctl ├───── port 9105 ─────────────────▶│              │
│ (CLI)       │                                  │  dotfilesd   │
└─────────────┘                                  │  (daemon)    │
                                                 │              │
┌─────────────┐     MCP SSE (JSON-RPC)           │              │
│ opencode    ├───── port 9106 ─────────────────▶│              │
│ (AI agent)  │     /sse + /message              └──────┬───────┘
└─────────────┘                                        │
                                                ┌───────┴───────┐
                                                │  System calls │
                                                │  (shell, git, │
                                                │   i3, tmux,   │
                                                │   pkexec)     │
                                                └───────────────┘
```

## Ports

| Port | Protocol | Endpoint | Purpose |
|------|----------|----------|---------|
| 9105 | Connect RPC | `http://127.0.0.1:9105/` | Tool/service API (gRPC-compatible) |
| 9106 | MCP SSE | `http://127.0.0.1:9106/sse` | AI agent integration |

Both bind to `127.0.0.1` only (no remote access).

## Components

### Daemon (`cmd/dotfilesd/`)

- **`main.go`** — Entry point. Starts both the Connect RPC HTTP server and the MCP SSE server. Sets up slog logging (JSON to stdout + rotated file).
- **`server.go`** — Connect RPC handler implementations (`dotfilesServer` struct). Implements the `DotfilesService` protobuf interface.
- **`mcp.go`** — MCP server. Accepts SSE connections, dispatches JSON-RPC 2.0 messages, and wraps Connect RPC client calls in-process.

### CLI (`cmd/dotfilesctl/`)

- **`main.go`** — Command-line client that makes Connect RPC calls to the daemon. Outputs to stdout only (no log noise). Supports `--verbose` for debugging.

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

The MCP server exposes these tools:

| Tool | Description |
|------|-------------|
| `dotfiles_status` | Dotfiles repo status + system info |
| `dotfiles_reload` | Reload dotfiles configs (tmux, i3, kitty) |
| `dotfiles_git` | Git operations on the dotfiles repo |
| `system_exec` | Execute shell commands |
| `system_info` | Detailed system information |

## Data flow

```
dotfilesctl  ──Connect RPC──▶  dotfilesd  ──exec.Command()──▶  git/i3/tmux/kitty/shell
opencode     ────MCP SSE────▶  dotfilesd  ──in-process RPC──▶  dotfilesServer methods
```

## Directory layout

```
~/dotfilesd/
├── cmd/
│   ├── dotfilesd/           # Daemon entry point + server + MCP
│   └── dotfilesctl/         # CLI client
├── docs/                    # Documentation
├── proto/                   # Protobuf definitions + generated code
│   └── dotfilesd/v1/dotfilesdv1/
├── service/                 # Systemd service template
├── logs/                    # Runtime logs (gitignored)
├── Makefile                 # Build, install, service management
├── go.mod / go.sum
└── README.md
```
