# Diagnostics System — Design Document

> **Status:** Draft
> **Date:** 2026-06-28
> **Purpose:** Specify the daemon diagnostics subsystem — a real-time tree showing
> everything happening in the dotfiles runtime (plugins, clients, sessions, exec
> commands, background tasks, I/O streams).

---

## 1. Overview

The diagnostics system produces a **live tree of active runtime entities**.
It is the `htop` of the dotfiles daemon — showing only things that are
currently running or have active work to do. Idle entities are hidden.

The tree is split into **multiple roots**, one per *owner*:

```
└── Diagnostics (dotfilesd runtime state)
   ├── Daemon (dotfilesd)
   │  └── Plugins with background workers
   │      └── Shell session (bash)
   │  └── Background tasks
   │
   └── Client (cli_xxx)
      └── Session (ses_xxx)
         └── Shell (bash) — with active exec command
      └── Executor stream — service.method calling plugin
```

### Principles

1. **Only active things.** If it's not doing work right now, it's hidden.
2. **Owner-based trees.** Each active client gets its own tree showing what it's doing.
3. **Parents contain children.** A session contains its shell. A shell contains its active exec command. A client contains its executor streams.
4. **No polling.** The data is a snapshot collected atomically from daemon subsystem state.

---

## 2. Data Model (Protobuf)

```protobuf
// DiagNode represents one node in the daemon state tree.
message DiagNode {
  string type = 1;                    // node type (see §3)
  string label = 2;                   // human-readable name
  string status = 3;                  // status string (see §3)
  map<string, string> attrs = 4;      // key-value metadata
  repeated DiagNode children = 5;     // child nodes
}

message DiagnosticsResponse {
  DiagNode root = 1;                  // synthetic root containing all trees
}
```

The response contains a single `DiagNode` tree. The immediate children of
the root are the *owner trees* (daemon, client-1, client-2, ...).

---

## 3. Node Types

### 3.1 `type: "daemon"` — The dotfilesd process itself

| Field | Value |
|-------|-------|
| `label` | `dotfilesd (pid %d, port %s, up %ds)` |
| `attrs.pid` | OS pid |
| `attrs.port` | RPC port |
| `attrs.uptime` | Seconds since start |
| `attrs.version` | Version string |
| `attrs.plugins` | "N loaded" |
| `attrs.sessions` | "N total" (all sessions count) |

**Parent:** root (always present)
**Children:** plugins with bg workers, background tasks

### 3.2 `type: "plugin"` — A running plugin

| Field | Value |
|-------|-------|
| `label` | `Name vX.X.X` |
| `status` | `"bg_worker"` if it has a background goroutine (i.e. its plugin session has a shell) |
| `attrs.pid` | Plugin process PID |
| `attrs.url` | Plugin HTTP URL |
| `attrs.services` | Count of services exposed (e.g. `"2"`) |

**Parent:** daemon
**Children:** shell (if bg worker has a shell session)

**Visibility:** Only shown if the plugin has a background worker (`Config.Background != nil`).

### 3.3 `type: "shell"` — A bash session

| Field | Value |
|-------|-------|
| `label` | `"bash"` |
| `status` | `"running"` |
| `attrs.cwd` | Current working directory |
| `attrs.active` | Currently executing command string, or `"(idle)"` |

**Parent:** session (or plugin with bg worker)
**Children:** none

**Visibility:** Only shown if the shell exists and is attached to an active parent.

### 3.4 `type: "session"` — A daemon session (grouping of requests)

| Field | Value |
|-------|-------|
| `label` | Session ID (e.g. `"ses_xxx"` or `"plugin-resources"`) |
| `attrs.created` | Creation timestamp (RFC3339) |
| `attrs.callback` | Callback URL (if set) |

**Parent:** client tree (implicitly via client → session → shell)
**Children:** shell (if any)

**Visibility:** Hidden in the current design (sessions without active work
are not shown). The session ID is shown as an attribute on the client node.

### 3.5 `type: "client"` — An active connected client

| Field | Value |
|-------|-------|
| `label` | Client ID (e.g. `"cli_xxx"`) |
| `attrs.session` | Associated session ID |
| `attrs.pid` | Client PID (if available) |

**Parent:** root (each client is a separate tree)
**Children:** shell (if session has one), executor calls

**Visibility:** Only shown if the client has at least one active executor
stream or an active shell.

### 3.6 `type: "executor"` — An active bidi stream proxying a plugin RPC call

| Field | Value |
|-------|-------|
| `label` | `"ServiceName.MethodName"` |
| `attrs.plugin` | Plugin name being called |
| `attrs.service` | Full service name |
| `attrs.method` | Method name |

**Parent:** client (the client that initiated the call)
**Children:** none

**Visibility:** Only shown while the bidi stream is active.

### 3.7 `type: "bg_task"` — A daemon-managed background task

| Field | Value |
|-------|-------|
| `label` | Task ID (e.g. `"bg_xxx"`) |
| `attrs.command` | Shell command being executed |
| `attrs.session` | Session ID that launched the task (future) |

**Parent:** daemon
**Children:** none

**Visibility:** Only shown while the task is running.

---

## 4. Tree Structure

### 4.1 Daemon tree

```
daemon
├── plugin (bg_worker)
│   └── shell (bash, active: "cat /proc/meminfo")
├── plugin (bg_worker)
│   └── shell (bash, active: "(idle)")
└── bg_task (bg_xxx, command: "pacman -Syu")
```

### 4.2 Client tree

```
client (cli_xxx, session: ses_yyy)
├── shell (bash, active: "dotfilesctl weather --location=Madrid")
├── executor (resources.ResourcesService.Current, plugin: resources)
└── executor (weather.WeatherService.Forecast, plugin: weather)
```

### 4.3 Full compound view

```
root
├── daemon (dotfilesd pid 1234, port 9105, up 42s)
│   ├── Resources v1.0.0 (bg_worker)
│   │   └── bash (running)
│   │      cwd: /home/user
│   │      active: cat /proc/meminfo
│   └── bg_task (bg_1)
│       command: pacman -Syu --noconfirm
│
└── client (cli_1782667918360771514, session: ses_abc)
    ├── bash (running)
    │   cwd: /home/user
    │   active: (idle)
    └── weather.WeatherService.Forecast
        plugin: weather
```

---

## 5. Data Sources

The diagnostics handler collects data from these daemon subsystems:

| Subsystem | Data | Accessor |
|-----------|------|----------|
| `plugin.Manager` | List loaded plugins with metadata | `manager.ListPlugins()` |
| `SessionStore` | List all sessions with shell/status | `store.List()` |
| `executor_svc.go` | Active bidi executor streams | `ListActiveCalls()` |
| `background_task.go` | Running background tasks | `bgTaskManager.ListTasks()` |

### 5.1 Active call tracking

The executor service (`executor_svc.go`) maintains two maps:

- `activeCallsByClient[clientID]` → `activePluginCall`
- `activeCallsByPlugin[pluginName]` → `activePluginCall`

Each `activePluginCall` stores:
- `clientID` — who initiated the call
- `pluginName` — which plugin is being called
- `service` / `method` — the specific RPC being invoked

### 5.2 Shell command tracking

Each `shellSession` stores a `lastCommand` field. It is set to the
command string when `Exec()` or `ExecStream()` starts, and cleared
when the command completes. The diagnostics snapshot reads this field
to show what command is currently executing.

---

## 6. CLI Rendering

The CLI client renders the `DiagNode` tree as Unicode box-drawing:

```
└── label (status)
   attr: value
   ├── child-1 (status)
   │  nested attr: value
   └── child-2 (status)
```

Implementation: `RunDiagnostics()` in `internal/pkg/cli/system.go`
calls a recursive `printTree()` function that walks `DiagNode.children`
and prints `type`/`label`/`status`/`attrs` at each level with proper
`├──`/`└──`/`│` prefixes.

---

## 7. Future Extensions

- **Stale detection:** Show if a plugin's process is dead but not yet removed.
- **CPU/memory:** Show resource usage per plugin process.
- **Exec command history:** Show last N completed exec commands per session.
- **Active MCP clients:** Track MCP bridge connections as client nodes.
- **Plugin-to-plugin calls:** Show executor streams where a plugin is the caller.
- **I/O rates:** Show bytes/second on active executor streams.
- **Filter by type:** CLI flags to show only certain node types.
- **Watch mode:** `dotfilesctl system diag --watch` to refresh every N seconds.
