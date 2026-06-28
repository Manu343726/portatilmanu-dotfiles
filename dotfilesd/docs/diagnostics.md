# Diagnostics — Daemon Runtime State Tree

> **Status:** Implemented
> **Date:** 2026-06-28
> **RPC:** `SystemService.Diagnostics`
> **CLI:** `dotfilesctl system diag`

## Purpose

Diagnostics provides a real-time hierarchical view of all active runtime
state in the dotfiles daemon. Think of it as `htop` for the dotfiles
runtime — it shows exactly what the daemon is doing, who invoked what,
and what is currently running.

## Architecture

```
CLI: dotfilesctl system diag
        │
        ▼ unary RPC
SystemService.Diagnostics()
        │
        ▼
Diagnostics handler (system.go)
  ├── session.go — sessions + shell sessions
  ├── executor_svc.go — active bidi executor streams
  ├── background_task.go — running background tasks
  ├── manager.go — loaded plugins
  └── scripts_registry.go — registered scripts
        │
        ▼
DiagnosticsResponse { DiagNode root }
  └── tree rendered by CLI (box-drawing)
```

## Data Model

```protobuf
message DiagNode {
  string type = 1;    // node type classifier
  string label = 2;   // human-readable name
  string status = 3;  // status: running, idle, active, bg_worker, etc.
  map<string, string> attrs = 4;  // key-value metadata
  repeated DiagNode children = 5; // child nodes
}
```

Every node in the tree uses the same recursive `DiagNode` message. The
`type` field lets consumers (CLI renderer, web UI, MCP tools) know what
kind of node they're looking at and how to display it.

### Node Types

| Type | Parent | Meaning | Attributes |
|------|--------|---------|------------|
| `daemon` | root | The dotfilesd process | pid, port, uptime, version |
| `plugin` | daemon, client | A loaded plugin | pid, url, services count |
| `shell` | session | A managed bash shell session | cwd, active command |
| `session` | client | A daemon session | created, callback URL |
| `executor` | client | An active bidi stream calling a plugin | plugin, service, method |
| `client` | root | An active CLI/MCP client | client ID, session ID |
| `bg_task` | daemon | A running background exec task | command |

### Node Type Rules

- **Only active things are shown.** Plugins without background workers
  and no active executor calls are hidden. Sessions without shells and
  no callback URL are hidden. Scripts (files on disk, not runtime) are
  never shown.
- **One tree per active root.** The response combines multiple trees:
  one for the daemon (plugins with bg workers, bg tasks) and one per
  active client (their session, shell, executor streams).
- **`bg_worker` status** means the plugin has a background shell session
  running (e.g. the resources plugin's collector goroutine).

## Tree Structure

```
root "dotfilesd diagnostics — N tree(s)"
│
├── daemon "dotfilesd (pid X, port 9105, up Xs)"
│   ├── attrs: version, plugins loaded count, sessions total
│   │
│   ├── plugin "Resources v1.0.0 (bg_worker)"   ← only plugins with bg
│   │   ├── attrs: pid, url, services count
│   │   └── shell "bash (running)"
│   │       └── attrs: cwd, active command (or idle)
│   │
│   └── bg_task "bg_xxxxx"                      ← running bg tasks
│       └── attrs: command
│
└── client "cli_xxxxxx"                          ← one per active client
    ├── attrs: session ID
    │
    ├── shell "bash"                             ← if session has shell
    │   └── attrs: cwd, active command
    │
    └── executor "resources.ResourcesService.Current"  ← active calls
        └── attrs: plugin name
```

## Data Sources

### Sessions (`session.go`)
- `SessionStore.List()` returns all sessions
- Each session may have:
  - A `shellSession` (bash process) for Exec/ExecStream
  - A `callbackURL` for interactive feedback
  - A `finalized` flag
- Shell sessions track their `lastCommand` (currently executing command)

### Executor Streams (`executor_svc.go`)
- `ListActiveCalls()` returns all active bidi `CallPlugin` streams
- Each call has: clientID, pluginName, service, method
- Stored in two maps: `activeCallsByClient` and `activeCallsByPlugin`

### Background Tasks (`background_task.go`)
- `backgroundTaskManager.ListTasks()` returns running tasks
- Each task has: id, command (exec.Cmd.String())

### Plugins (`manager.go`)
- `Manager.ListPlugins()` returns all loaded plugins
- Each PluginInfo has: Name, DisplayName, Version, URL, Process.Pid, Services, SourceDir, CacheDir

## CLI Rendering

```
dotfilesctl system diag
```

The CLI uses recursive box-drawing:

```go
func printTree(n *DiagNode, prefix string, isLast bool) {
    branch := "├── "
    if isLast { branch = "└──" }

    label := n.Label
    if n.Status != "" {
        label = fmt.Sprintf("%s (%s)", label, n.Status)
    }
    fmt.Printf("%s%s%s\n", prefix, branch, label)

    for k, v := range n.Attrs {
        // indented under the node
    }

    for i, child := range n.Children {
        printTree(child, childPrefix, i == len(n.Children)-1)
    }
}
```

## Extensions

### Future Node Types
- `exec_command` — currently running exec inside a shell session
  (already partially tracked via `lastCommand`)
- `script` — only when actively executing
- `mcp_client` — MCP-connected agent sessions
- `feedback_session` — interactive input/confirm/choose in progress

### Future Improvements
- Add exec command history per session
- Show plugin-to-plugin calls (not just client→plugin)
- Add resource usage per plugin (CPU, memory from `/proc`)
- Filter by tree type (`--daemon`, `--clients`, `--plugins`)
- Watch mode (`dotfilesctl system diag --watch`)
- JSON output (`--json`) for machine parsing
- WebSocket live updates

### Integration with daemon RPCs
- `SystemService.Diagnostics` is a read-only admin RPC (no auth required)
- MCP tools can call it and present the tree as structured data
- Third-party tools can consume the `/dotfilesd.v1.SystemService/Diagnostics` HTTP endpoint
