# Architecture

dotfilesd has two components: a daemon and a CLI client that also serves as the MCP gateway.

```
┌─────────────────────────────────────────────────────────────────────┐
│                         AI Agent (opencode)                         │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │  MCP Apps Webview (sudo password form, interactive UI)       │   │
│  └──────────────────────────────────────────────────────────────┘   │
└──────────────────────────┬──────────────────────────────────────────┘
                           │ MCP stdio (JSON-RPC 2.0)
                           │ Content-Length framing
                           ▼
┌──────────────────────────────────────────────────────────────────────┐
│  dotfilesctl mcp  (CLI ↔ MCP bridge)                                 │
│                                                                      │
│  Static tools:      system_ping, system_info, system_sudo            │
│                     dotfiles_status, dotfiles_git, exec_run          │
│                     config_reload, config_reconfigure, config_restart│
│                     script_run, script_list                          │
│  Plugin tools:      <plugin>_<tool>  (dynamically registered)       │
│  App tool:          _sudo_submit_password  (MCP Apps webview only)  │
│  Resources:         ui://dotfilesd/sudo-prompt  (MCP Apps HTML)     │
└──────────────────────────┬───────────────────────────────────────────┘
                           │ Connect RPC (gRPC/HTTP)
                           │ port 9105 · 127.0.0.1 only
                           ▼
┌──────────────────────────────────────────────────────────────────────┐
│  dotfilesd  (daemon — RPC server + plugin supervisor)                │
│                                                                      │
│  ┌─────────────────┐  ┌─────────────────┐  ┌──────────────────────┐ │
│  │  SystemService   │  │  DotfilesService │  │  ExecService         │ │
│  │  · Ping          │  │  · Status        │  │  · Exec              │ │
│  │  · SystemInfo    │  │  · Git           │  │  · SudoExec          │ │
│  │  · SudoMethods   │  │                  │  │                      │ │
│  │  · ListPlugins   │  │                  │  │  (pkexec, elicitation│ │
│  │  · ListPluginTree│  │                  │  │   terminal callback, │ │
│  │  · CallPluginTool│  │                  │  │   MCP Apps webview)  │ │
│  └─────────────────┘  └─────────────────┘  └──────────────────────┘ │
│                                                                      │
│  ┌─────────────────┐  ┌─────────────────┐  ┌──────────────────────┐ │
│  │  ConfigService   │  │  SessionService  │  │  ScriptService       │ │
│  │  · Reload        │  │  · CreateSession │  │  · RunScript         │ │
│  │  · Reconfigure   │  │  · Connect       │  │  · ListScripts       │ │
│  │  · Restart       │  │  · Finalize      │  │                      │ │
│  │                  │  │  · GetSession    │  │  (feedback directives│ │
│  │  (tmux, i3,      │  │  · ListSessions  │  │   @confirm, @input,  │ │
│  │   kitty reload)  │  │                  │  │   @choose)           │ │
│  └─────────────────┘  └─────────────────┘  └──────────────────────┘ │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐     │
│  │  Plugin Manager                                              │     │
│  │  ┌───────────────┐  ┌───────────────┐  ┌──────────────────┐ │     │
│  │  │  Builder       │  │  Registry      │  │  Supervisor       │ │     │
│  │  │  · source hash │  │  · plugin dir  │  │  · crash restart  │ │     │
│  │  │  · cache       │  │  · tool index  │  │  · exponential    │ │     │
│  │  │  · rebuild     │  │  · MCP export  │  │    backoff 1-30s  │ │     │
│  │  └───────┬───────┘  └───────┬───────┘  └────────┬─────────┘ │     │
│  │          │                  │                     │           │     │
│  │  ┌───────▼──────────────────▼─────────────────────▼────────┐  │     │
│  │  │  Runtime (process launcher, handshake, shutdown)         │  │     │
│  │  └─────────────────────────────────────────────────────────┘  │     │
│  └──────────────────────────────────────────────────────────────┘     │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐     │
│  │  Execution Context (plugin ↔ daemon bridge)                  │     │
│  │  · Exec / SudoExec — shell commands                          │     │
│  │  · RequestInput / Confirm / Choose — user interaction        │     │
│  │  · Token-authenticated, session-scoped                       │     │
│  └─────────────────────────────────────────────────────────────┘     │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐     │
│  │  Session Store + Shell Sessions                               │     │
│  │  · Persistent bash processes per session                      │     │
│  │  · Shell env: DOTFILESD_PORT, session variables, CWD          │     │
│  │  · Callback URL for agent ↔ daemon feedback                   │     │
│  └─────────────────────────────────────────────────────────────┘     │
└──────────────────────────┬───────────────────────────────────────────┘
                           │ daemon starts plugin subprocesses
                           ▼
┌──────────────────────────────────────────────────────────────────────┐
│  Plugin Ecosystem  (standalone Go binaries, separate processes)      │
│                                                                      │
│  ┌──────────────────────┐  ┌──────────────────────────────────────┐  │
│  │  Weather Plugin       │  │  Resources Plugin                     │  │
│  │  · forecast tool      │  │  · current (RAM/CPU/disk/I/O)        │  │
│  │  · ctx.Exec("curl…")  │  │  · top (N processes by CPU/mem)      │  │
│  │  · wttr.in            │  │  · ps (detail + sparkline bars)      │  │
│  │                       │  │  · history (sparkline graphs)        │  │
│  │                       │  │  · Background collector goroutine     │  │
│  └──────────────────────┘  └──────────────────────────────────────┘  │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │  SDK (dotfilesd/plugin/)                                      │   │
│  │  · Serve() / ServeWithBackground() — plugin entry point       │   │
│  │  · Tool interface + NewTool() helper                          │   │
│  │  · Context interface (Exec, SudoExec, RequestInput/Confirm…)  │   │
│  │  · Handshake protocol (stdout JSON → daemon)                  │   │
│  └──────────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────────┘
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

The daemon exposes the following Connect RPC services on port 9105:

```protobuf
service SystemService {
  rpc Ping(PingRequest) returns (PingResponse);
  rpc SystemInfo(SystemInfoRequest) returns (SystemInfoResponse);
  rpc SudoMethods(SudoMethodsRequest) returns (SudoMethodsResponse);
  rpc ListPlugins(ListPluginsRequest) returns (ListPluginsResponse);
  rpc ListPluginTree(ListPluginTreeRequest) returns (ListPluginTreeResponse);
  rpc CallPluginTool(CallPluginToolRequest) returns (stream CallPluginToolResponse);
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
service SessionService {
  rpc CreateSession(CreateSessionRequest) returns (CreateSessionResponse);
  rpc Connect(ConnectRequest) returns (ConnectResponse);
  rpc FinalizeSession(FinalizeSessionRequest) returns (FinalizeSessionResponse);
  rpc GetSession(GetSessionRequest) returns (GetSessionResponse);
  rpc ListSessions(ListSessionsRequest) returns (ListSessionsResponse);
}
service ScriptService {
  rpc RunScript(RunScriptRequest) returns (RunScriptResponse);
  rpc ListScripts(ListScriptsRequest) returns (ListScriptsResponse);
}
service InputService {
  rpc RequestInput(InputRequest) returns (InputResponse);
}
service ConfirmService {
  rpc RequestConfirm(ConfirmRequest) returns (ConfirmResponse);
}
service ChooseService {
  rpc RequestChoose(ChooseRequest) returns (ChooseResponse);
}
```

### Execution Context (plugin only, not exposed on daemon port)

The daemon mounts an additional `ExecutionContext` handler on its HTTP mux during
plugin initialization. This is the **reverse** RPC — plugins call back into the
daemon to execute commands or interact with the user:

```protobuf
service ExecutionContext {
  rpc Exec(ExecRequest) returns (ExecResponse);
  rpc SudoExec(SudoExecRequest) returns (SudoExecResponse);
  rpc RequestInput(InputRequest) returns (InputResponse);
  rpc RequestConfirm(ConfirmRequest) returns (ConfirmResponse);
  rpc RequestChoose(ChooseRequest) returns (ChooseResponse);
}
```

### Extension API (plugin ↔ daemon)

Each plugin exposes an `ExtensionService` on its own random port:

```protobuf
service ExtensionService {
  rpc GetDescriptor(GetDescriptorRequest) returns (GetDescriptorResponse);
  rpc CallTool(CallToolRequest) returns (stream CallToolResponse);
}

## MCP tools

The MCP stdio server (launched via `dotfilesctl mcp`) exposes these tools:

### Static tools (always available)

| Tool | Service | Description |
|------|---------|-------------|
| `system_ping` | SystemService | Daemon health check |
| `system_info` | SystemService | Detailed system information |
| `system_sudo` | SystemService | Available sudo methods |
| `dotfiles_status` | DotfilesService | Dotfiles repo status |
| `dotfiles_git` | DotfilesService | Git operations on the dotfiles repo |
| `exec_run` | ExecService | Execute shell commands (supports `sudo=true` with elicitation/MCP Apps) |
| `config_reload` | ConfigService | Reload dotfiles configs |
| `config_reconfigure` | ConfigService | Change daemon runtime config |
| `config_restart` | ConfigService | Gracefully restart the daemon |
| `script_run` | ScriptService | Run a multi-step script with feedback directives |
| `script_list` | ScriptService | List registered scripts |
| `_sudo_submit_password` | (internal) | MCP Apps-only: submit sudo password from webview (visibility: app) |

### Plugin tools (dynamic)

When plugins are loaded, their tools are automatically available as MCP tools
qualified with the plugin name: `<plugin>_<tool>`.

| MCP Tool | Plugin | Description |
|----------|--------|-------------|
| `weather_forecast` | weather | Get weather forecast for a location |
| `resources_current` | resources | Show system resource snapshot |
| `resources_top` | resources | Top processes by CPU or memory |
| `resources_ps` | resources | Detailed process list with sparklines |
| `resources_history` | resources | Historical sparkline graphs |

### MCP Apps resources

| URI | Purpose |
|-----|---------|
| `ui://dotfilesd/sudo-prompt` | HTML form for sudo password input (MCP Apps webview) |

## Sessions

Sessions allow clients to group related requests that share state. Each session
has an ID, creation/last-active timestamps, request counter, and a key-value
data map. Sessions also maintain a **persistent shell process** (bash) so
commands executed within a session can share shell state (working directory,
variables, aliases).

### Session lifecycle

1. **Connect** — The client calls `SessionService.Connect` with its callback URL
   (the URL of the CLI's feedback HTTP server). The daemon creates a new session
   or attaches to an existing one. The session stores the callback URL so the
   daemon can reach back to the client for user interaction (input prompts,
   confirmations).
2. **Use** — The client passes the session ID as a `Session-Id` HTTP header (or
   via the `Session` message in the RPC body) on subsequent requests. All requests
   with the same session ID share the same persistent shell.
3. **Finalize** — The client calls `FinalizeSession` to close the shell and mark
   the session as complete. No further requests may use a finalized session.

Without a session (empty `session_id`), the daemon creates **ephemeral sessions**
that exist for a single request and are not stored in the registry. They have no
persistent shell and no feedback capability.

### Shell sessions

Each session with a non-empty ID gets a persistent `bash --norc --noprofile`
subprocess. The shell receives:
- **Environment**: inherits the CLI's env vars (PATH, HOME, etc.) plus
  `DOTFILESD_DAEMON=1`, `DOTFILESD_PORT=<port>`, and `DOTFILESD_SESSION=<id>`
- **Working directory**: set to the CLI's current working directory at session
  creation
- **Session variables**: arbitrary key-value pairs injected as environment
  variables on every command execution (keys prefixed with `_` are private
  and not exported)

### Feedback (callback URL)

The daemon can ask the client for user input via three RPC services:
- `InputService.RequestInput` — text prompt (supports `sensitive` for passwords)
- `ConfirmService.RequestConfirm` — yes/no confirmation
- `ChooseService.RequestChoose` — pick from a list of options

These are called by the daemon at the client's callback URL. The CLI starts an
HTTP server and registers its URL via `SessionService.Connect`. This enables:
- **Elicitation**: MCP agents that support it see native UI forms
- **Terminal prompts**: fallback for headless sessions
- **MCP Apps webview**: for password input with masking

### CLI commands

```bash
dotfilesctl session create                    # returns a session ID
dotfilesctl --session <id> system ping        # use session in a request
dotfilesctl --session <id> exec 'ls -la'      # same session, shared shell
dotfilesctl session finalize <id>             # mark session complete
dotfilesctl session list                      # show active sessions
```

### Sudo execution flow

When a command is executed with `sudo=true`, the daemon tries these methods in
order based on the session's capabilities:

1. **Elicitation** — If the MCP client supports elicitation and the session has
   a callback URL, the daemon sends a password prompt via `InputService`. The
   password is sent to `sudo -S` and never stored.
2. **pkexec** — If the DISPLAY is available and `pkexec` is installed, a desktop
   polkit dialog is shown. No daemon-prompted password input needed.
3. **Terminal callback** — Fallback to terminal elicitation for headless sessions.
4. **MCP Apps webview** — If the MCP client supports MCP Apps (has `_meta.ui`
   capabilities), the `exec_run` tool call returns `_meta.ui.resourceUri` pointing
   to `ui://dotfilesd/sudo-prompt`. The agent renders the HTML webview, the user
   types their password, and the webview calls `_sudo_submit_password`. The
   `exec_run` goroutine receives the password via a channel and completes the
   sudo execution. This is the only method that provides password masking in VS Code.

## Plugin system

Plugins are standalone Go programs that register tools (commands) which get
automatically exposed as both CLI subcommands and MCP tools. See `docs/plugins.md`
for the full documentation.

### Two RPC services

| Service | Direction | Purpose |
|---------|-----------|---------|
| **Extension API** | Daemon → Plugin | Daemon discovers tools and invokes them |
| **Execution Context** | Plugin → Daemon | Plugin interacts with the host (exec, sudo, user prompts) |

### Plugin lifecycle

1. **Discovery** — Daemon scans `plugins_dir` for directories with `go.mod` or `main.go`
2. **Build** — Daemon compiles sources and caches the binary by SHA-256 hash of all sources
3. **Launch** — Daemon starts the plugin subprocess, reads handshake JSON from stdout
4. **Registration** — Daemon calls `GetDescriptor` to discover tools and their schemas
5. **Service** — Plugin tools are available via CLI (`dotfilesctl plugin call <plugin> <tool>`)
   and MCP (qualified as `<plugin>_<tool>`)
6. **Supervision** — If the plugin process crashes, it is automatically restarted with
   exponential backoff (1s–30s). The daemon re-builds the binary if sources changed,
   re-launches the process, and re-registers the tools.
7. **Shutdown** — Daemon sends SIGTERM to all plugins on graceful shutdown

### Plugin types

| Type | Behavior | Use case |
|------|----------|----------|
| `server` (default) | Long-lived process, supervised (auto-restart on crash) | Most plugins — weather, resources |
| `command` | Ephemeral — run once per invocation | One-shot tasks, scripts |

Type is inferred from `DirFrontMatter.Type` in the plugin directory's README.md
or defaults to `server`.

### Key packages

| Path | Purpose |
|------|---------|
| `dotfilesd/plugin/` | Public SDK for writing plugins |
| `internal/pkg/plugin/` | Plugin manager, builder, runtime, registry, supervisor |
| `internal/pkg/daemon/plugin.go` | Daemon-side context backend + plugin init |
| `plugins/` | Example plugins (`weather/`, `resources/`)

## Data flow

```
  AI Agent                 dotfilesctl                    dotfilesd                      Host / Plugins
(opencode)                   (CLI)                        (Daemon)
    │                          │                              │
    │  ╔══════════════════╗    │                              │
    │  ║   MCP stdio      ║    │                              │
    │  ║  JSON-RPC 2.0    ║    │                              │
    │  ╚════╤═════════════╝    │                              │
    │       │                  │                              │
    │       │  ┌─ exec_run ─┐  │  Connect RPC (port 9105)     │
    │       ├──▶  dispatch  │──┼─────────────────────────────▶│
    │       │  └────────────┘  │                              │
    │       │                  │                              │  ┌──────────┐
    │       │                  │  ┌─ SudoExec(elicitation) ─┐ │──▶  Input   │
    │       │                  │  │ ◀──── callback URL ──────│ │  │Service   │
    │       │  ┌─ plugin call┐ │  │                          │ │  └──────────┘
    │       ├──▶  dispatch   │─┼─┼──────────────────────────▶│
    │       │  └─────────────┘ │  │  Extension API            │──▶ Plugin 1
    │       │                  │  │  (http://127.0.0.1:PORT)  │    (weather)
    │       │                  │  │                           │──▶ Plugin N
    │       │  ┌─ list plugins┐│  │                           │    (resources)
    │       ├──▶  dispatch   │─┼─┼──────────────────────────▶│
    │       │  └─────────────┘ │  │  SystemService.ListPlugins│
    │       │                  │  │                           │
    │       │  ┌─ script_run ─┐│  │  ScriptService.RunScript  │
    │       ├──▶  dispatch   │─┼─┼──────────────────────────▶│──▶ bash session
    │       │  └─────────────┘ │  │                           │
    │       │                  │  │                           │
    │  ┌────▼─────────────────▼──┴───────────────────────────▼──┐
    │  │  MCP Apps webview: ui://dotfilesd/sudo-prompt          │
    │  │  HTML form → _sudo_submit_password tool → passwordCh    │
    │  └────────────────────────────────────────────────────────┘
```

### Interaction patterns

| Pattern | Description |
|---------|-------------|
| **Direct CLI** | `dotfilesctl system ping` → Connect RPC → daemon → response |
| **MCP agent** | AI agent → `dotfilesctl mcp` (stdio) → JSON-RPC dispatch → Connect RPC → daemon |
| **Plugin call** | daemon → Extension API HTTP → plugin subprocess → daemon → response |
| **Sudo (elicitation)** | daemon → callback URL → CLI HTTP server → MCP elicitation → user → password → `sudo -S` |
| **Sudo (MCP Apps)** | daemon returns `_meta.ui.resourceUri` → agent renders webview → user types password → `_sudo_submit_password` → passwordCh → `sudo -S` |
| **Session feedback** | daemon → SessionService.Connect callback → CLI HTTP server → `RequestInput/Confirm/Choose` → user response |
| **Script execution** | daemon → ScriptService.RunScript → parse directives → exec commands + feedback + sudo |

## Directory layout

```
~/dotfilesd/
├── cmd/
│   ├── dotfilesd/           # Daemon CLI setup (Cobra + config)
│   │   ├── main.go
│   │   ├── root.go          # Flags, viper, daemon.New(cfg).Start()
│   │   └── plugin_cmd.go    # Plugin subcommands (list, call, tree)
│   └── dotfilesctl/         # CLI client CLI setup (Cobra)
│       ├── main.go
│       ├── root.go           # Persistent flags, client creation
│       ├── plugin_cmd.go     # Plugin subcommands (list, call, tree)
│       └── session_cmd.go    # Session subcommands
├── internal/
│   └── pkg/
│       ├── daemon/           # Connect RPC server implementations
│       │   ├── server.go     # Daemon struct, HTTP server, graceful restart
│       │   ├── system.go     # SystemService (Ping, Info, SudoMethods)
│       │   ├── dotfiles.go   # DotfilesService (Status, Git)
│       │   ├── exec.go       # ExecService (Exec, SudoExec)
│       │   ├── config.go     # ConfigService (Reload, Reconfigure, Restart)
│       │   ├── session.go    # Session store + SessionService server
│       │   ├── plugin.go     # Context backend + plugin init
│       │   ├── helpers.go    # runCmd, runCmdFull
│       │   └── logging.go    # Logging setup
│       ├── cli/              # CLI action logic + MCP server
│       │   ├── client.go     # ConnectClients struct
│       │   ├── system.go     # RunPing, RunInfo, RunSudoMethods
│       │   ├── dotfiles.go   # RunStatus, RunGit
│       │   ├── exec.go       # RunExec, RunSudoExec
│       │   ├── config.go     # RunReload, RunReconfigure, RunRestart
│       │   ├── session.go    # RunCreateSession, RunConnect, etc.
│       │   ├── plugin.go     # RunListPlugins, RunCallPlugin, etc.
│       │   ├── mcp.go        # MCP stdio server (JSON-RPC, tool dispatch)
│       │   ├── enums.go      # Protobuf enum string parsing
│       │   └── helpers.go    # Logging setup, Fatalf
│       ├── plugin/           # Plugin manager, builder, runtime, registry
│       │   ├── manager.go    # PluginManager orchestration
│       │   ├── builder.go    # Build + SHA-256 hash cache
│       │   ├── runtime.go    # Process launcher, handshake, shutdown
│       │   ├── registry.go   # Plugin/tool registry
│       │   ├── supervisor.go # Auto-restart with exponential backoff
│       │   └── types.go      # Plugin type (server/command)
│       └── shared/           # Shared utilities
│           └── buildhash.go  # CheckBuildHash
├── plugin/                   # Public plugin SDK
│   ├── serve.go              # Serve() / ServeWithBackground()
│   ├── serve_test.go
│   ├── tool.go               # Tool interface + SimpleFuncTool
│   ├── context.go            # Context interface + client
│   └── convert.go            # SDK ↔ proto type conversions
├── plugins/                  # Example plugins
│   ├── weather/              # Weather forecast plugin (wttr.in)
│   │   ├── main.go
│   │   ├── plugin.go         # Tool registration + handler
│   │   └── README.md         # Plugin metadata / front matter
│   └── resources/            # System resources monitor plugin
│       ├── main.go
│       ├── plugin.go         # Tool registration
│       ├── collector.go      # Background data collector goroutine
│       ├── collector_test.go
│       ├── cpu.go            # CPU stat parsing
│       ├── mem.go            # Memory stat parsing
│       ├── disk.go           # Disk stat parsing
│       ├── types.go          # Shared data types
│       └── README.md         # Plugin metadata / front matter
├── docs/                     # Documentation
│   ├── README.md             # (symlink to ../README.md or separate)
│   ├── architecture.md       # This document
│   ├── development.md        # Build/test workflow
│   ├── deploy.md             # Install/deployment
│   ├── debugging.md          # Troubleshooting
│   ├── features.md           # CLI feature reference
│   ├── plugins.md            # Plugin authoring guide
│   └── mcp-apps-research.md  # MCP Apps SEP research notes
├── proto/                    # Protobuf definitions + generated code
│   └── dotfilesd/v1/dotfilesdv1/
│       ├── system.proto
│       ├── dotfiles.proto
│       ├── exec.proto
│       ├── config.proto
│       ├── session.proto
│       ├── script.proto
│       ├── extension.proto    # Extension API (daemon ↔ plugin)
│       ├── execution_context.proto  # Execution Context (plugin ↔ daemon)
│       ├── *.pb.go            # Generated Go types
│       └── dotfilesdv1connect/*.connect.go  # Generated Connect RPC stubs
├── service/                  # Systemd user service template
│   └── dotfilesd.service
├── scripts/                  # Dotfiles scripts (.dsh files)
│   └── hello.dsh             # Example script
├── logs/                     # Runtime logs (gitignored)
├── Makefile                  # Build, install, proto, service, plugin, test
├── go.mod / go.sum
└── README.md
```
