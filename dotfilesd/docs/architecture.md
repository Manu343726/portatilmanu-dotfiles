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
│  Plugin tools:     <plugin>_<tool> (auto-discovered)                │
│  App tool:         _sudo_submit_password (MCP Apps only)            │
│  Resources:        ui://dotfilesd/sudo-prompt (MCP Apps HTML)       │
└──────────────────────────┬───────────────────────────────────────────┘
                           │ Connect RPC over HTTP
                           │ port 9105 · 127.0.0.1 only
                           ▼
┌──────────────────────────────────────────────────────────────────────┐
│  dotfilesd  (daemon — RPC server + plugin supervisor)                │
│                                                                      │
│  ADMIN services (CLI only):                                          │
│  ┌─────────────────┐  ┌─────────────────────┐                       │
│  │  SystemService   │  │  SessionService      │                       │
│  │  · Ping          │  │  · CreateSession     │                       │
│  │  · RuntimeInfo   │  │  · Connect (callback)│                       │
│  │  · SudoMethods   │  │  · FinalizeSession   │                       │
│  └─────────────────┘  │  · GetSession         │                       │
│                        │  · ListSessions       │                       │
│  ┌─────────────────┐  └─────────────────────┘                       │
│  │  ConfigService   │                                                │
│  │  · Reconfigure   │                                                │
│  │  · Restart       │                                                │
│  └─────────────────┘                                                │
│                                                                      │
│  USAGE services (CLI + plugins, token-authenticated):                │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐  │
│  │  ExecService      │  │  FeedbackService  │  │  LogService       │  │
│  │  · Exec           │  │  · RequestInput   │  │  · Log            │  │
│  │  · ExecStream     │  │  · RequestConfirm │  │                   │  │
│  │  · BackgroundExec │  │  · RequestChoose  │  │                   │  │
│  │  · SudoExec       │  └──────────────────┘  └──────────────────┘  │
│  └──────────────────┘                                                │
│                                                                      │
│  ┌──────────────────┐  ┌──────────────────┐                         │
│  │  PluginService    │  │  ScriptService    │                         │
│  │  · ListPlugins    │  │  · RunScript      │                         │
│  │  · ListPluginTree │  │  · ListScripts    │                         │
│  │  · CallPluginTool │  │                   │                         │
│  └──────────────────┘  └──────────────────┘                         │
│                                                                      │
│  CALLBACK services (daemon → client):                                │
│  ┌───────────────────────────────────────────────┐                  │
│  │  InputService / ConfirmService / ChooseService │                  │
│  │  (feedback forwarded to session callback URL)  │                  │
│  └───────────────────────────────────────────────┘                  │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Plugin Manager                                              │    │
│  │  ┌───────────────┐  ┌───────────────┐  ┌──────────────────┐ │    │
│  │  │  Builder       │  │  Registry      │  │  Supervisor       │ │    │
│  │  │  · source hash │  │  · plugin dir  │  │  · crash restart  │ │    │
│  │  │  · cache       │  │  · tool index  │  │  · exponential    │ │    │
│  │  │  · rebuild     │  │               │  │    backoff 1-30s  │ │    │
│  │  └───────┬───────┘  └───────┬───────┘  └────────┬─────────┘ │    │
│  │          │                  │                     │           │    │
│  │  ┌───────▼──────────────────▼─────────────────────▼────────┐ │    │
│  │  │  Runtime (process launcher, handshake, shutdown)         │ │    │
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
│  ┌──────────────────────┐  ┌──────────────────────────────────────┐  │
│  │  Weather Plugin       │  │  Resources Plugin                     │  │
│  │  · forecast tool      │  │  · current (RAM/CPU/disk/I/O)        │  │
│  │  · ctx.Exec("curl…")  │  │  · top (N processes by CPU/mem)      │  │
│  │  · wttr.in            │  │  · ps (PID detail)                   │  │
│  │                       │  │  · history (sparkline graphs)         │  │
│  │                       │  │  · Background collector goroutine     │  │
│  └──────────────────────┘  └──────────────────────────────────────┘  │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │  SDK (dotfilesd/plugin/)                                      │   │
│  │  · Serve() / ServeWithBackground() — plugin entry point       │   │
│  │  · Context interface — Exec, ExecStream, BackgroundExec       │   │
│  │  · CallPlugin / CallPluginStream — plugin-to-plugin calls     │   │
│  │  · RunScript — invoke registered scripts                      │   │
│  │  · BackgroundTask — Stdin/Stdout/Tee/Cancel/Wait              │   │
│  │  · RequestInput / RequestConfirm / RequestChoose              │   │
│  │  · Log() — structured logging via daemon                      │   │
│  └──────────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────────┘
```

## Service architecture

Services are split into three categories:

| Category | Access | Services |
|----------|--------|----------|
| **Admin** | CLI only (via session) | `SystemService`, `ConfigService`, `SessionService`, `DotfilesService` |
| **Usage** | CLI + plugins (token or session) | `ExecService`, `ScriptService`, `FeedbackService`, `LogService`, `PluginService` |
| **Callback** | Daemon → client | `InputService`, `ConfirmService`, `ChooseService` |

This separation prevents plugins from accessing admin-only features (reconfigure, restart, session listing).

### Admin services

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

### Usage services

**ExecService** — Command execution (the only path to run shell commands).
- `Exec` — unary, returns complete stdout/stderr
- `ExecStream` — server-streaming, real-time output chunks
- `BackgroundExec` — bidi-stream, stdin/stdout/cancel, returns `BackgroundTask`
- `SudoExec` — challenge-response sudo protocol

**ScriptService** — Multi-step scripts with feedback directives.
- `RunScript` — inline script, file path, or registered script name (e.g. `"git/status"`)
- `ListScripts` — tree of registered `.dsh` scripts

**FeedbackService** — User interaction prompts (input, confirm, choose).

**LogService** — Plugin logging routed through the daemon's logging system.

**PluginService** — Plugin discovery and invocation.
- `ListPlugins` / `ListPluginTree` — discover loaded plugins
- `CallPluginTool` — invoke a plugin tool, streaming output

### Callback services

`InputService`, `ConfirmService`, `ChooseService` — the daemon calls these on the client's callback URL to prompt the user. Not exposed to plugins.

## Key design principles

1. **Core vs. scripts** — The daemon provides primitive infrastructure (exec, sessions, plugin/script hosting). High-level features (git operations, config reloads) are scripts in `scripts/git/` and `scripts/reload/`. Adding a new reload target is creating a `.dsh` file, not recompiling the daemon.

2. **Deduplicated protocols** — Plugins and CLI use the same usage-level services. Plugins authenticate via `X-Dotfiles-Context-Token` header on standard usage service calls. No separate "execution context" proxy.

3. **Plugin isolation** — Plugins are separate processes communicating via Connect RPC. A crash never takes down the daemon. Server-type plugins are supervised with automatic restart.

4. **Auto-discovery** — Plugins in `~/.config/dotfilesd/plugins/` are scanned, built, launched, and registered automatically. Tools appear as CLI subcommands and MCP tools without manual wiring.

5. **Streaming by default** — `ExecStream` and `CallPluginTool` stream output in real time. Plugins can use `BackgroundExec` for interactive commands with stdin control and `BackgroundTask.Tee()` for dual output/processing.

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
| `log.proto` | `LogService` |
| `plugin.proto` | `PluginService` (ListPlugins, ListPluginTree, CallPluginTool) |
| `extension.proto` | `ExtensionService` (daemon→plugin: GetDescriptor, CallTool) + `SchemaType`/`PropertyType` enums |

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
├── log.go                 # LogService handler
├── plugin.go              # InitPlugins, token generation, session creation
├── plugin_svc.go          # PluginService handler
├── background_task.go     # Background task manager
├── helpers.go             # runCmd, runCmdFull, runCmdStream
├── logging.go             # Logging setup, logLevelToSlog
└── *_test.go              # Tests
```
