# Architecture

dotfilesd is a daemon + CLI that manages dotfiles and hosts a plugin ecosystem, exposed to AI agents via MCP.

```
┌─────────────────────────────────────────────────────────────────────┐
│                         AI Agent (MCP client)                       │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │  MCP Apps Webview (sudo password form, interactive UI)       │   │
│  └──────────────────────────────────────────────────────────────┘   │
└──────────────────────────┬──────────────────────────────────────────┘
                           │ MCP stdio (JSON-RPC 2.0)
                           ▼
┌──────────────────────────────────────────────────────────────────────┐
│  dotfilesctl mcp  (CLI ↔ MCP bridge)                                 │
│                                                                      │
│  Core tools:       system_ping, system_runtime, system_sudo         │
│                    dotfiles_status, exec_run                         │
│                    config_reconfigure, config_restart                │
│                    script_run, script_list                           │
│                    config_reload (→ scripts/reload/<target>)         │
│                    dotfiles_git (→ scripts/git/<action>)             │
│  Plugin tools:     <plugin>_<method> (auto-discovered, per-method)  │
│  App tool:         _sudo_submit_password (MCP Apps only)            │
│  Resources:        ui://dotfilesd/sudo-prompt (MCP Apps HTML)       │
└──────────────────────────┬───────────────────────────────────────────┘
                           │ Connect RPC over HTTP
                           │ port 9105 · 127.0.0.1 only
                           ▼
┌──────────────────────────────────────────────────────────────────────┐
│  dotfilesd  (daemon — RPC server + plugin supervisor)                │
│                                                                      │
│  PUBLIC services (no auth required):                                 │
│  ┌─────────────────┐  ┌──────────────────────┐                      │
│  │  SystemService   │  │  SessionService      │                      │
│  │  · Ping          │  │  · CreateSession     │                      │
│  │  · RuntimeInfo   │  │  · Connect (callback)│                      │
│  │  · SudoMethods   │  │  · FinalizeSession   │                      │
│  └─────────────────┘  │  · GetSession         │                      │
│                        │  · ListSessions       │                      │
│  ┌─────────────────┐  └──────────────────────┘                      │
│  │  ConfigService   │                                                │
│  │  · Reconfigure   │  ┌───────────────────────────┐                │
│  │  · Restart       │  │  DiagnosticsPostService    │                │
│  └─────────────────┘  │  · PostEvent               │                │
│                        │  · PostMetric              │                │
│  ┌──────────────────┐  │  · PostSnapshot            │                │
│  │  DotfilesService  │  └───────────────────────────┘                │
│  │  · Status         │                                              │
│  └──────────────────┘  ┌───────────────────────────┐                │
│                        │  DiagnosticsQueryService   │                │
│  ┌──────────────────┐  │  · QueryTree               │                │
│  │  ScriptService    │  │  · QueryResources         │                │
│  │  · RunScript      │  │  · QueryHistory           │                │
│  │  · ListScripts    │  │  · QueryMetrics           │                │
│  └──────────────────┘  │  · StreamEvents            │                │
│                        └───────────────────────────┘                │
│  ┌───────────────────┐                                              │
│  │  ExecService       │  ┌───────────────────────────┐              │
│  │  · Exec            │  │  PluginRegistryService     │              │
│  │  · ExecStream      │  │  · GetPlugin               │              │
│  │  · BackgroundExec  │  │  · ListPlugins             │              │
│  │  · SudoExec        │  │  · LoadPlugin              │              │
│  └───────────────────┘  │  · UnloadPlugin             │              │
│                          │  · ReloadPlugins            │              │
│  ┌───────────────────┐  └───────────────────────────┘              │
│  │  PluginExecutorSvc  │                                            │
│  │  · CallPlugin       │                                            │
│  └───────────────────┘                                              │
│                                                                      │
│  AUTH-REQUIRED services (token-authenticated, plugins + CLI):       │
│  ┌──────────────────┐  ┌──────────────────┐                        │
│  │  FeedbackService  │  │  IOService        │                        │
│  │  · RequestInput   │  │  · Log            │                        │
│  │  · RequestConfirm │  │                   │                        │
│  │  · RequestChoose  │  │                   │                        │
│  └──────────────────┘  └──────────────────┘                        │
│                                                                      │
│  CALLBACK services (daemon → client):                                │
│  ┌───────────────────────────────────────────────┐                  │
│  │  InputService / ConfirmService / ChooseService │                  │
│  │  (feedback forwarded to session callback URL)  │                  │
│  └───────────────────────────────────────────────┘                  │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Diagnostics Engine                                          │    │
│  │  · State cache (resource lifecycle tracking)                 │    │
│  │  · History ring buffers (per event type)                    │    │
│  │  · Metrics store                                            │    │
│  │  · Real-time event subscribers                              │    │
│  │  · Configurable retention policies                          │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Plugin Manager                                              │    │
│  │  ┌───────────────┐  ┌───────────────┐  ┌──────────────────┐ │    │
│  │  │  Builder       │  │  Registry      │  │  Supervisor       │ │    │
│  │  │  · source hash │  │  · plugin dir  │  │  · crash restart  │ │    │
│  │  │  · cache       │  │  · tool index  │  │  · exponential    │ │    │
│  │  │  · proto comp  │  │  · deps graph  │  │    backoff 1-30s  │ │    │
│  │  └───────┬───────┘  └───────┬───────┘  └────────┬─────────┘ │    │
│  │          │                  │                     │           │    │
│  │  ┌───────▼──────────────────▼─────────────────────▼────────┐ │    │
│  │  │  Runtime (grpcreflect discovery, handshake, shutdown)    │ │    │
│  │  └─────────────────────────────────────────────────────────┘ │    │
│  └──────────────────────────────────────────────────────────────┘    │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Session Store + Shell Sessions                               │    │
│  │  · Persistent bash processes per session                     │    │
│  │  · Shell env, session variables, CWD                         │    │
│  │  · Callback URL for daemon → client feedback                 │    │
│  │  · Background task manager (bidi-stream command execution)    │    │
│  └─────────────────────────────────────────────────────────────┘    │
└──────────────────────────┬───────────────────────────────────────────┘
                           │ launches plugin subprocesses
                           ▼
┌──────────────────────────────────────────────────────────────────────┐
│  Plugin Ecosystem  (standalone Go binaries, separate processes)      │
│                                                                      │
│  Plugins serve Connect RPC services discovered via grpcreflect.     │
│  The daemon auto-discovers all services, generates CLI commands     │
│  with typed flags from proto schemas, and exposes per-method MCP    │
│  tools for AI agents.                                               │
│                                                                      │
│  ┌──────────────────────┐  ┌──────────────────────────────────────┐  │
│  │  Weather Plugin       │  │  Resources Plugin                     │  │
│  │  · forecast RPC       │  │  · current (RAM/CPU/disk/I/O)        │  │
│  │  · ctx.Exec("curl…")  │  │  · top (N processes by CPU/mem)      │  │
│  │  · wttr.in            │  │  · ps (PID detail)                   │  │
│  │                       │  │  · history (sparkline graphs)         │  │
│  │                       │  │  · Background collector goroutine     │  │
│  └──────────────────────┘  └──────────────────────────────────────┘  │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │  SDK (dotfilesd/plugin/)                                      │   │
│  │  · Serve(cfg Config) — entry point with grpcreflect          │   │
│  │  · Context interface — Exec, ExecStream, BackgroundExec      │   │
│  │  · Plugin-to-plugin calls via generated Connect clients      │   │
│  │  · RunScript — invoke registered scripts                      │   │
│  │  · BackgroundTask — Stdin/Stdout/Tee/Cancel/Wait              │   │
│  │  · RequestInput / RequestConfirm / RequestChoose              │   │
│  │  · RenderOutput() — format control flag                       │   │
│  │  · Log() — structured logging via daemon                      │   │
│  └──────────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────────┘
```

## Service architecture

Services are split into three access tiers:

| Category | Access | Services |
|----------|--------|----------|
| **Public** | No auth required | `SystemService`, `ConfigService`, `SessionService`, `DotfilesService`, `ScriptService`, `ExecService`, `DiagnosticsPostService`, `DiagnosticsQueryService`, `PluginRegistryService`, `PluginExecutorService` |
| **Auth-required** | Token (`X-Dotfiles-Context-Token`) | `FeedbackService`, `IOService` |
| **Callback** | Daemon → client | `InputService`, `ConfirmService`, `ChooseService` |

Previously the system had an **Admin vs Usage** split. The current design uses a simpler model: most services are public (no auth), token-gated services are for sensitive operations (feedback, I/O), and callback services are daemon-initiated only.

### Public services

**SystemService** — Daemon health and runtime environment.
- `Ping` — version, PID, uptime
- `RuntimeInfo` — OS, kernel, shell, desktop, hostname, uptime, available tools (sudo, pkexec, tmux, i3, kitty)
- `SudoMethods` — available privilege escalation paths (graphical, nopass)

**ConfigService** — Daemon runtime reconfiguration.
- `Reconfigure` — change log level at runtime
- `Restart` — graceful daemon restart

**SessionService** — Session lifecycle management.
- `CreateSession` — allocate a new session
- `Connect` — register client callback URL for feedback
- `FinalizeSession` / `GetSession` / `ListSessions`

**DotfilesService** — Dotfiles repository status.
- `Status` — git branch, clean/dirty, last commit

**ScriptService** — Multi-step scripts with feedback directives.
- `RunScript` — inline script, file path, or registered script name (e.g. `"git/status"`)
- `ListScripts` — tree of registered `.dsh` scripts

**ExecService** — Command execution (the only path to run shell commands).
- `Exec` — unary, returns complete stdout/stderr
- `ExecStream` — server-streaming, real-time output chunks
- `BackgroundExec` — bidi-stream, stdin/stdout/cancel, returns `BackgroundTask`
- `SudoExec` — challenge-response sudo protocol

**DiagnosticsPostService** — Push events, metrics, and snapshots into the diagnostics engine.
- `PostEvent` — record a timestamped event (daemon start, plugin spawn, exec start, etc.)
- `PostMetric` — record a metric data point
- `PostSnapshot` — replace current state for a resource subtree

**DiagnosticsQueryService** — Query the diagnostics engine.
- `QueryTree` — filtered state tree with parent/child reconstruction
- `QueryResources` — flat resource list (no tree)
- `QueryHistory` — historical events (ring buffer)
- `QueryMetrics` — metric data points
- `StreamEvents` — real-time event subscription (server-streaming)

**PluginRegistryService** — Plugin discovery and lifecycle.
- `GetPlugin` — connection info and schema for a named plugin
- `ListPlugins` — all registered plugins with full type introspection data
- `LoadPlugin` — load a plugin by name (including dependencies)
- `UnloadPlugin` — stop a plugin by name
- `ReloadPlugins` — rescan plugins directory

**PluginExecutorService** — Proxy RPC calls from CLI/MCP to plugins.
- `CallPlugin` — bidi-stream between client and plugin (stdin/stdout/stderr passthrough)

### Auth-required services

**FeedbackService** — User interaction prompts (input, confirm, choose) accessible to plugins and CLI via token auth.

**IOService** — Plugin I/O (logging + stdout/stderr) routed through the daemon, accessible via token auth.

### Callback services

`InputService`, `ConfirmService`, `ChooseService` — the daemon calls these on the client's callback URL to prompt the user. Not exposed to plugins.

## Key design principles

1. **Core vs. scripts** — The daemon provides primitive infrastructure (exec, sessions, plugin/script hosting). High-level features (git operations, config reloads) are scripts in `scripts/git/` and `scripts/reload/`. Adding a new reload target is creating a `.dsh` file, not recompiling the daemon.

2. **Deduplicated protocols** — Plugins and CLI use the same usage-level services. Plugins authenticate via `X-Dotfiles-Context-Token` header on standard usage service calls. No separate "execution context" proxy.

3. **Plugin isolation** — Plugins are separate processes communicating via Connect RPC. A crash never takes down the daemon. Server-type plugins are supervised with automatic restart.

4. **Auto-discovery** — Plugins in `~/.config/dotfilesd/plugins/` are scanned, built, launched, and registered automatically. Tools appear as CLI subcommands and MCP tools without manual wiring.

5. **Streaming** — The daemon supports `ExecStream` for real-time output streaming. (Note: The old `CallPluginTool` streaming was removed; plugins now serve Connect RPC services directly.)

## Proto files

Service definitions are in `proto/dotfilesd/v1/dotfilesdv1/`:

| File | Services |
|------|----------|
| `system.proto` | `SystemService` (Ping, RuntimeInfo, SudoMethods) |
| `config.proto` | `ConfigService` (Reconfigure, Restart) + `LogLevel` enum |
| `session.proto` | `SessionService` (Create, Connect, Finalize, Get, List) |
| `exec.proto` | `ExecService` (Exec, ExecStream, SudoExec, BackgroundExec) + `SudoMethod` enum |
| `dotfiles.proto` | `DotfilesService` (Status) |
| `script.proto` | `ScriptService` (RunScript, ListScripts) |
| `feedback.proto` | `FeedbackService` + `InputService` + `ConfirmService` + `ChooseService` |
| `io.proto` | `IOService` |
| `diagnostics.proto` | `DiagnosticsPostService` (PostEvent, PostMetric, PostSnapshot) + `DiagnosticsQueryService` (QueryTree, QueryResources, QueryHistory, QueryMetrics, StreamEvents) + `DiagNode`, `DiagEvent`, `MetricPoint`, `ResourceState` messages |
| `plugin_registry.proto` | `PluginRegistryService` (GetPlugin, ListPlugins, LoadPlugin, UnloadPlugin, ReloadPlugins) + `PluginExecutorService` (CallPlugin) + `ServiceSchema`, `MethodSchema`, `FieldSchema`, `MessageSchema`, `EnumSchema` type introspection messages |

Generated `.pb.go` and `.connect.go` files are gitignored. Run `make proto` to regenerate them.

## Daemon source layout

```
internal/pkg/daemon/
├── server.go              # Daemon struct, Start(), InitPlugins(), RPC mux
├── system.go              # SystemService (Ping, RuntimeInfo, SudoMethods)
├── config.go              # ConfigService (Reconfigure, Restart)
├── exec.go                # ExecService (Exec, ExecStream, SudoExec, BackgroundExec)
├── dotfiles.go            # DotfilesService (Status)
├── session.go             # SessionStore, Session, shellSession
├── script.go              # Script parser + runner
├── scripts_registry.go    # Script discovery from filesystem
├── feedback.go            # FeedbackService handler
├── io.go                  # IOService handler
├── plugin.go              # InitPlugins, token generation, session creation
├── registry_svc.go        # PluginRegistryService + PluginExecutorService handler
├── diagnostics_svc.go     # DiagnosticsPostService + DiagnosticsQueryService handler
├── executor_svc.go        # Plugin executor (bidi-stream proxy to plugins)
├── background_task.go     # Background task manager
├── helpers.go             # runCmd, runCmdFull, runCmdStream
├── logging.go             # Logging setup, logLevelToSlog
└── *_test.go              # Tests

internal/pkg/diagnostics/   # Diagnostics engine (zero daemon dependencies)
├── engine.go              # Engine struct, PushEvent, PushMetric, PushSnapshot
├── state.go               # StateCache, ResourceState, lifecycle status
├── tree.go                # ReconstructTree, FilterResources, filter logic
└── engine_test.go         # Tests

internal/pkg/plugin/        # Daemon-side plugin manager
├── manager.go             # Manager (NewManager, LoadPlugins, ListPlugins, discovery)
└── supervisor.go          # Supervisor (auto-restart with exponential backoff)

plugin/                     # Plugin SDK (public API, compiled into each plugin)
├── serve.go               # Serve(cfg Config) — server, grpcreflect, handshake
├── context.go             # Context interface (Exec, RequestInput, Log, ...)
├── ctxkey.go              # Context key for ExtractContext
├── background_task.go     # BackgroundTask type
├── docs.go                # Default DocumentationService implementation
```
