# Diagnostics System — Design Document

> **Status:** Draft
> **Date:** 2026-06-28
> **Purpose:** Specify the diagnostics subsystem — a centralized engine for
> collecting, storing, and querying real-time state and metrics from the
> dotfiles runtime.

> **⚠️ Full rewrite — no backwards compatibility.** This document describes
> a ground-up redesign of the diagnostics system. The existing
> `SystemService.Diagnostics` RPC, the ad-hoc tree builder in
> `daemon/system.go`, and the old `DiagNode`-based snapshot model are all
> **removed and replaced**. There is no migration path, no compatibility
> shim, and no hybrid mode. Old CLI workflows that consume the previous
> diagnostics output will break until updated to use the new
> `DiagnosticsQueryService` / `DiagnosticsPostService` RPCs.

---

## 1. Architecture Overview

```
┌──────────────────────────────────────────────────────────────────┐
│                        Daemon Components                          │
│                                                                   │
│  Manager      Executor     IO Service     Session Store           │
│  Plugin Mgr   Exec Server  Log/Stdin     Shell Sessions           │
│       │           │             │              │                  │
│       ▼           ▼             ▼              ▼                  │
│  ┌───────────────────────────────────────────────────────────┐   │
│  │              DiagnosticsPostService (RPC)                  │   │
│  │  PostEvent(), PostMetric(), PostSnapshot()                 │   │
│  └───────────────────────┬───────────────────────────────────┘   │
└──────────────────────────┼────────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│              Diagnostics Engine (internal/pkg/diagnostics/)        │
│                                                                   │
│  ┌──────────────────────┐  ┌──────────────────────────────────┐  │
│  │  Current State Cache │  │  History Store (ring buffer)     │  │
│  │  (latest snapshot)   │  │  - Per-resource-type retention   │  │
│  │                      │  │  - Configurable TTLs             │  │
│  │  daemon node         │  │  - Compact in-memory DB          │  │
│  │  plugin nodes        │  │  - Automatic eviction            │  │
│  │  client nodes        │  │                                  │  │
│  │  executor streams    │  │  Event log:                      │  │
│  │  shell sessions      │  │  - plugin_spawn                  │  │
│  │  bg tasks            │  │  - plugin_crash                  │  │
│  │  metrics             │  │  - exec_start/stop               │  │
│  └──────────────────────┘  │  - client_connect/disconnect     │  │
│                             │  - executor_open/close          │  │
│                             └──────────────────────────────────┘  │
└───────────────────────┬──────────────────────────────────────────┘
                        │
                        ▼
┌──────────────────────────────────────────────────────────────────┐
│              DiagnosticsQueryService (RPC)                        │
│  QueryTree(filter) → DiagNode tree                                │
│  QueryHistory(filter) → []Event                                    │
│  QueryMetrics(filter) → []MetricPoint                              │
└───────┬───────────────────────────────────────┬──────────────────┘
        │                                       │
        ▼                                       ▼
┌───────────────┐              ┌──────────────────────────────┐
│ dotfilesctl   │              │  tui-diagnostics plugin       │
│ system diag   │              │  (htop-like, live tree,       │
│ system events │              │   filters, search, history)   │
│ system stats  │              └──────────────────────────────┘
└───────────────┘
```

### Principles

1. **Engine is independent.** The `internal/pkg/diagnostics/` package has zero
   dependencies on daemon internals. It knows about `DiagNode`, events, and
   metrics only. Daemon components post to it via RPC or direct Go calls.

2. **Real-time push, not poll.** Daemon subsystems push events and metrics as
   they happen. The engine never polls.

3. **Current state + history.** The engine maintains both a latest-state cache
   (for instant tree reconstruction) and a ring-buffer history (for forensics).

4. **Configurable retention.** Each resource type (plugin_start, exec_cmd,
   client_connect, etc.) has its own TTL and max count in the history store.

5. **Filterable queries.** The query service supports filters on type, status,
   label regex, attribute key/value, time range, and resource ID.

---

## 2. Engine Package — `internal/pkg/diagnostics/`

### 2.1 Core types

```go
// EventType categorizes a diagnostic event.
type EventType string

const (
    EventDaemonStart    EventType = "daemon_start"
    EventDaemonStop     EventType = "daemon_stop"
    EventPluginSpawn    EventType = "plugin_spawn"
    EventPluginCrash    EventType = "plugin_crash"
    EventPluginStop     EventType = "plugin_stop"
    EventClientConnect  EventType = "client_connect"
    EventClientDisconn  EventType = "client_disconnect"
    EventExecStart      EventType = "exec_start"
    EventExecStop       EventType = "exec_stop"
    EventExecutorOpen   EventType = "executor_open"
    EventExecutorClose  EventType = "executor_close"
    EventSessionCreate  EventType = "session_create"
    EventSessionEnd     EventType = "session_end"
    EventBgTaskStart    EventType = "bg_task_start"
    EventBgTaskStop     EventType = "bg_task_stop"
    EventMetricReport   EventType = "metric_report"
)

// Event is a timestamped diagnostic event with structured payload.
// The Resource and Parent fields form the parent/child links for tree reconstruction.
type Event struct {
    ID        string            `json:"id"`
    Timestamp time.Time         `json:"ts"`
    Type      EventType         `json:"type"`
    Resource  string            `json:"resource"`  // e.g. "plugin:weather", "session:ses_xxx"
    Parent    string            `json:"parent"`    // parent resource ID, empty = root-level
    Labels    map[string]string `json:"labels,omitempty"`
    Message   string            `json:"message,omitempty"`
    Attrs     map[string]string `json:"attrs,omitempty"`
}

// MetricPoint is a timestamped metric value.
type MetricPoint struct {
    Timestamp time.Time
    Name      string            // e.g. "exec_duration_ms", "plugin_mem_kb"
    Value     float64
    Labels    map[string]string // e.g. {"plugin": "weather"}
}

// RetentionPolicy defines how long events/metrics of a type are kept.
type RetentionPolicy struct {
    MaxCount  int           // max events to keep (0 = unlimited)
    MaxAge    time.Duration // max age before eviction
}

// Engine is the central diagnostics store.
type Engine struct {
    // state cache — resource lifecycles reconstructed from events
    state *StateCache  // map[string]*ResourceState with versioning

    // history — ring buffers per event type
    history map[EventType][]Event

    // metrics — ring buffers per metric name
    metrics map[string][]MetricPoint

    // retention config
    retention map[EventType]RetentionPolicy
}
```

### 2.2 Engine API

```go
func New() *Engine
func (e *Engine) PushEvent(evt Event)
func (e *Engine) PushMetric(m MetricPoint)
func (e *Engine) PushSnapshot(node *DiagNode) // replace current state for a resource

func (e *Engine) GetCurrentTree(filters ...FilterFunc) *DiagNode
func (e *Engine) GetHistory(filter EventFilter) []Event
func (e *Engine) GetMetrics(filter MetricFilter) []MetricPoint

func (e *Engine) SetRetention(typ EventType, policy RetentionPolicy)
```

---

## 3. Data Model (Proto)

The existing `DiagNode` message is extended with event and metric messages
for the post/query RPCs.

```protobuf
// ─────────────────────────────────────────────
// Core state tree (unchanged from current)
// ─────────────────────────────────────────────

message DiagNode {
  string type = 1;
  string label = 2;
  string status = 3;
  map<string, string> attrs = 4;
  repeated DiagNode children = 5;
}

// ─────────────────────────────────────────────
// Events
// ─────────────────────────────────────────────

message DiagEvent {
  string id = 1;
  int64 timestamp_ns = 2;         // unix nanos
  string type = 3;                // EventType string
  string resource = 4;            // e.g. "plugin:weather"
  string parent = 5;              // parent resource ID for tree reconstruction, empty = root
  map<string, string> labels = 6;
  string message = 7;
  map<string, string> attrs = 8;
}

message MetricPoint {
  int64 timestamp_ns = 1;
  string name = 2;
  double value = 3;
  map<string, string> labels = 4;
}

// ─────────────────────────────────────────────
// Post service — daemon components push data here
// ─────────────────────────────────────────────

service DiagnosticsPostService {
  // PostEvent records an event.
  rpc PostEvent(DiagEvent) returns (PostEventResponse);
  // PostMetric records a metric data point.
  rpc PostMetric(MetricPoint) returns (PostMetricResponse);
  // PostSnapshot replaces the current state for a resource subtree.
  rpc PostSnapshot(DiagNode) returns (PostSnapshotResponse);
}

message PostEventResponse {}
message PostMetricResponse {}
message PostSnapshotResponse {}

// ─────────────────────────────────────────────
// Query service — CLI/TUI query the engine
// ─────────────────────────────────────────────

service DiagnosticsQueryService {
  // QueryTree returns a filtered state tree.
  rpc QueryTree(QueryTreeRequest) returns (QueryTreeResponse);
  // QueryResources returns filtered resources as a flat list (no tree
  // reconstruction). Uses the same filter Phase 2 as QueryTree (§4.4)
  // but returns the raw ResourceState list instead of building a tree.
  rpc QueryResources(QueryResourcesRequest) returns (QueryResourcesResponse);
  // QueryHistory returns historical events.
  rpc QueryHistory(QueryHistoryRequest) returns (QueryHistoryResponse);
  // QueryMetrics returns metric data points.
  rpc QueryMetrics(QueryMetricsRequest) returns (QueryMetricsResponse);
  // StreamEvents subscribes to real-time events.
  rpc StreamEvents(StreamEventsRequest) returns (stream DiagEvent);
}

message QueryTreeRequest {
  // Include only nodes matching these filters (AND).
  repeated string include_types = 1;     // e.g. ["daemon", "plugin"]
  string label_regex = 2;                // filter by label pattern
  string status_filter = 3;              // e.g. "running", "bg_worker"
  map<string, string> attr_filters = 4;  // key=value filter on attrs
  // Show idle/inactive nodes too (default: active only).
  bool show_idle = 5;
  // Time window for finished/crashed nodes (takes precedence over show_idle).
  //   0 or unset        → active-only (default)
  //   positive duration → include finished nodes within this window
  //   -1 / "inf"        → no pruning (like show_idle=true)
  google.protobuf.Duration time_window = 6;
}

message QueryTreeResponse {
  DiagNode root = 1;
}

message QueryHistoryRequest {
  repeated string types = 1;             // event types to include
  string resource_regex = 2;             // filter by resource ID pattern
  int64 since_ns = 3;                    // only events after this timestamp
  int64 until_ns = 4;                    // only events before this timestamp
  int32 limit = 5;                       // max results (0 = default 100)
}

message QueryHistoryResponse {
  repeated DiagEvent events = 1;
}

message QueryMetricsRequest {
  string name_regex = 1;                 // metric name pattern
  map<string, string> labels = 2;        // label filter
  int64 since_ns = 3;
  int64 until_ns = 4;
  int32 limit = 5;
}

message QueryMetricsResponse {
  repeated MetricPoint points = 1;
}

message StreamEventsRequest {
  repeated string types = 1;             // event types to subscribe to
}

// ─────────────────────────────────────────────
// Flat list queries
// ─────────────────────────────────────────────

message QueryResourcesRequest {
  // All filter fields are identical to QueryTreeRequest.
  repeated string include_types = 1;     // e.g. ["daemon", "plugin"]
  string label_regex = 2;                // filter by label pattern
  string status_filter = 3;              // e.g. "running", "bg_worker"
  map<string, string> attr_filters = 4;  // key=value filter on attrs
  // Time window for finished/crashed nodes.
  //   0 or unset        → active-only
  //   positive duration → include finished nodes within this window
  //   -1 / "inf"        → no pruning
  google.protobuf.Duration time_window = 6;
  // Sort order for results.
  string sort_by = 7;                    // "started_at", "type", "label", "status"
  bool sort_desc = 8;                    // descending order (default: ascending)
  int32 limit = 9;                       // max results (0 = no limit)
}

message QueryResourcesResponse {
  repeated ResourceState resources = 1;
  int32 total_count = 2;                 // total matching before limit
}

message ResourceState {
  string id = 1;
  string type = 2;
  string label = 3;
  string parent_id = 4;
  string status = 5;
  int64 created_at_ns = 6;
  int64 started_at_ns = 7;
  int64 finished_at_ns = 8;             // 0 if still active
  int64 duration_ns = 9;                // 0 if still active
  map<string, string> attrs = 10;
  int32 exit_code = 11;
}
```

---

## 4. Tree Reconstruction Algorithm

The engine reconstructs a hierarchical runtime tree from flat diagnostic events
by tracking resource lifecycles through parent/child metadata. Every component
of the daemon emits events with a `resource` field (the subject) and a `parent`
field (the caller/container), which is the single link needed to rebuild the
full tree.

### 4.1 Parent/Child Metadata Model

Each `DiagEvent` carries two identity fields that drive tree reconstruction:

| Field | Example | Meaning |
|-------|---------|---------|
| `resource` | `"executor:call_17"` | The subject of the event — a node in the tree |
| `parent` | `"client:cli_abc"` | The logical parent — links this node to its caller |

**Resource ID convention:** IDs are namespaced by type with a colon separator:

| Namespace | Examples | Created By |
|-----------|----------|-----------|
| `daemon` | `daemon` (singleton) | `daemon_start` |
| `plugin:` | `plugin:weather`, `plugin:resources` | `plugin_spawn` |
| `session:` | `session:ses_01`, `session:ses_02` | `session_create` |
| `client:` | `client:cli_abc`, `client:tui_xyz` | `client_connect` |
| `executor:` | `executor:call_17`, `executor:call_18` | `executor_open` |
| `shell:` | (same as session, 1:1) | `session_create` |
| `bg_task:` | `bg_task:job_05` | `bg_task_start` |

The parent link forms a forest rooted at the daemon:

```
daemon                           (resource="daemon", parent="")
├── plugin:weather               (resource="plugin:weather", parent="")
│   └── session:ses_01           (resource="session:ses_01", parent="plugin:weather")
│       └── executor:call_17     (resource="executor:call_17", parent="session:ses_01")
├── client:cli_abc               (resource="client:cli_abc", parent="")
│   └── executor:call_18         (resource="executor:call_18", parent="client:cli_abc")
│       └── executor:call_19     (resource="executor:call_19", parent="executor:call_18")
└── bg_task:job_05               (resource="bg_task:job_05", parent="")
```

Resources with an empty parent become top-level children of the synthetic root
node returned by `QueryTree`.

### 4.2 Resource State Machine

Each resource ID progresses through a lifecycle defined by the event pairs it
receives:

```
                ┌─────────────────┐
                │    PENDING      │  (referenced in metadata but no start yet)
                └────────┬────────┘
                         │ start event
                         ▼
                ┌─────────────────┐
         ┌─────│     ACTIVE      │─────┐
         │     │  (running/idle) │     │
         │     └────────┬────────┘     │
         │              │              │
         │   end event  │              │  crash / timeout
         ▼              ▼              ▼
  ┌──────────┐  ┌────────────┐  ┌──────────┐
  │ FINISHED │  │ TERMINATED │  │ CRASHED  │
  │ (normal) │  │ (signal)   │  │ (error)  │
  └──────────┘  └────────────┘  └──────────┘
```

**Lifecycle mapping per resource type:**

| Type | Start Event | End Event | Terminal Status |
|------|------------|-----------|----------------|
| `daemon` | `daemon_start` | `daemon_stop` | `finished` |
| `plugin` | `plugin_spawn` | `plugin_stop` / `plugin_crash` | `finished` / `crashed` |
| `session` | `session_create` | `session_end` | `finished` |
| `client` | `client_connect` | `client_disconnect` | `finished` |
| `executor` | `executor_open` | `executor_close` | `finished` |
| `shell` | `session_create` | `session_end` | `finished` |
| `bg_task` | `bg_task_start` | `bg_task_stop` | `finished` |

The engine derives status from the events it has seen for each resource:

- **No events yet:** `"pending"` — resource ID referenced but not started.
- **Start received, no end:** `"active"` — resource is live in the runtime.
- **End received normally:** `"finished"` — completed successfully.
- **Error/crash end:** `"crashed"` — terminated unexpectedly.

Transitions are idempotent — if the same resource receives duplicate events,
the second is a no-op (the state machine checks `StartedAt`/`FinishedAt` being
nil before updating).

### 4.3 Core Data Structures

```go
// ResourceStatus represents the lifecycle phase of a resource.
type ResourceStatus string

const (
    StatusPending   ResourceStatus = "pending"
    StatusActive    ResourceStatus = "active"
    StatusFinished  ResourceStatus = "finished"
    StatusCrashed   ResourceStatus = "crashed"
)

// ResourceState tracks the full lifecycle of a single resource.
// Instances are created by the engine as events arrive and mutated in place
// under the StateCache lock. Finished resources remain in the cache until
// pruned by the retention policy or time-window filter.
type ResourceState struct {
    ID        string            `json:"id"`
    Type      string            `json:"type"`      // "daemon", "plugin", "client", etc.
    Label     string            `json:"label"`     // Human-readable name
    ParentID  string            `json:"parent"`    // Empty = root-level (daemon child)

    // Lifecycle phase
    Status    ResourceStatus    `json:"status"`

    // Timing — always set after start event
    CreatedAt time.Time         `json:"created_at"` // First event for this resource
    StartedAt time.Time         `json:"started_at"` // Start event timestamp

    // Timing — nil while resource is still active
    FinishedAt *time.Time       `json:"finished_at,omitempty"`
    Duration   *time.Duration   `json:"duration,omitempty"`

    // Metadata
    Attrs     map[string]string `json:"attrs,omitempty"`
    ExitCode  *int              `json:"exit_code,omitempty"`
    Error     string            `json:"error,omitempty"`
}

// StateCache is the engine's internal resource store with versioning for
// change detection.
type StateCache struct {
    mu        sync.RWMutex
    resources map[string]*ResourceState  // keyed by resource ID
    version   uint64                     // increments on every mutation
}
```

### 4.4 Reconstruction Algorithm

The algorithm transforms the flat `StateCache.resources` map into a rooted
`DiagNode` tree in four phases.

```
FUNCTION ReconstructTree(cache, timeWindow, filters) → DiagNode

  ── PHASE 1: Snapshot ──
  snapshot = copy(cache.resources)       // atomic read under RLock
  nodeCount = len(snapshot)

  ── PHASE 2: Filter ──
  keep = new Map
  FOR EACH (id, res) IN snapshot:

    // 2a. Time-window gating
    IF res.Status IS NOT active:
        IF timeWindow == 0 OR timeWindow is unset:
            SKIP                          // default: active-only
        ELSE IF timeWindow == -1 OR timeWindow == "inf":
            PASS                          // no pruning
        ELSE:
            age = now() - res.FinishedAt
            IF age > timeWindow:
                SKIP                      // past the configurable window

    // 2b. Type filter
    IF include_types is set AND res.Type NOT IN include_types:
        SKIP

    // 2c. Label regex
    IF label_regex is set AND res.Label does NOT match label_regex:
        SKIP

    // 2d. Status filter
    IF status_filter is set AND string(res.Status) != status_filter:
        SKIP

    // 2e. Attr key-value filters
    IF attr_filters is set:
        FOR EACH (k, v) IN attr_filters:
            IF res.Attrs[k] != v:
                SKIP

    ADD (id → res) TO keep

  // [NOTE: Phase 2 is shared with QueryResources (§4.9). The flat list
  //  RPC runs the identical filter loop but stops here — it returns the
  //  |keep| entries directly as a sorted flat list instead of proceeding
  //  to adjacency and tree assembly.]

  ── PHASE 3: Build adjacency ──
  childrenOf = new Map(string → List)
  roots = new List

  FOR EACH (id, res) IN keep:
    parentID = res.ParentID
    IF parentID == "":
        APPEND res TO roots
    ELSE IF keep[parentID] exists:
        APPEND res TO childrenOf[parentID]
    ELSE:
        APPEND res TO roots              // orphan → promote to root

  SORT roots BY StartedAt ASC
  FOR EACH list IN childrenOf:
      SORT list BY StartedAt ASC

  ── PHASE 4: DFS assembly ──
  FUNCTION BuildNode(res, childrenOf):
    node = DiagNode{
        type:   res.Type,
        label:  res.Label,
        status: string(res.Status),
        attrs:  buildAttrs(res),          // §4.5
    }
    FOR EACH child IN childrenOf[res.ID]:
        APPEND BuildNode(child, childrenOf) TO node.children
    RETURN node

  BuildNode does NOT recurse into children not in |keep| — orphan children
  are promoted to roots in Phase 3, not silently dropped.

  ── PHASE 5: Wrap synthetic root ──
  root = DiagNode{
      type:   "root",
      label:  "dotfilesd runtime",
      status: "active",
      attrs:  { "node_count": string(nodeCount) },
  }
  FOR EACH res IN roots:
      APPEND BuildNode(res, childrenOf) TO root.children

  RETURN root
```

#### Correctness invariants

1. **Deterministic:** Same snapshot + same filters → identical tree. Sibling
   order is by `StartedAt` ascending.

2. **Parent-first guarantee:** Events are processed in timestamp order within
   each resource, so the start event always arrives before the end event. The
   engine does NOT require that a parent resource's events arrive before its
   child's — orphans are promoted to roots, which is correct behaviour when a
   parent hasn't emitted its start event yet.

3. **Idempotent event processing:** Duplicate events (same `id`) are silently
   ignored — the engine tracks processed event IDs in a small bloom filter.

4. **No unbounded memory:** Finished resources are eventually evicted from the
   state cache by the retention policy, separate from the time-window filter
   (which only controls query output).

### 4.5 Computing Timing Metadata

Every node in the output tree carries complete timing information so that
CLI and TUI consumers can display age, duration, and running time without
additional computation.

```python
# Pseudocode — buildAttrs(res):
def buildAttrs(res):
    attrs = copy(res.Attrs) if res.Attrs else {}

    # Always present
    attrs["started"] = format_rfc3339(res.StartedAt)
    attrs["created"] = format_rfc3339(res.CreatedAt)
    attrs["started_ago"] = format_duration(now() - res.StartedAt)

    if res.IsActive():
        # Currently running — compute running time live
        attrs["running_for"] = format_duration(now() - res.StartedAt)
        attrs["running_for_ns"] = str((now() - res.StartedAt).nanoseconds())
    else:
        # Finished/crashed — emit final timing
        attrs["finished"] = format_rfc3339(res.FinishedAt)
        attrs["finished_ago"] = format_duration(now() - res.FinishedAt)
        attrs["finished_ago_ns"] = str((now() - res.FinishedAt).nanoseconds())
        attrs["duration"] = format_duration(res.Duration)
        attrs["duration_ns"] = str(res.Duration.nanoseconds())

    if res.ExitCode is not None:
        attrs["exit_code"] = str(res.ExitCode)

    return attrs
```

**Attribute keys summary:**

| Key | Present When | Meaning |
|-----|-------------|---------|
| `started` | Always | RFC 3339 timestamp of start event |
| `created` | Always | RFC 3339 timestamp of first event for this resource |
| `started_ago` | Always | Human-readable duration since start |
| `running_for` | Active only | Human-readable duration since start (alias) |
| `running_for_ns` | Active only | Nanoseconds since start (machine consumers) |
| `finished` | Finished only | RFC 3339 timestamp of end event |
| `finished_ago` | Finished only | Human-readable how-long-ago finished |
| `finished_ago_ns` | Finished only | Nanoseconds since finish |
| `duration` | Finished only | Human-readable wall-clock run time |
| `duration_ns` | Finished only | Duration in nanoseconds |
| `exit_code` | When set | Exit code of the process/command |

### 4.6 Time Window Configuration

The `time_window` query parameter controls how far back finished nodes are
included in the tree output. It is exposed as a `google.protobuf.Duration` in
the proto and mapped to the following behaviour:

| Value | Behaviour |
|-------|-----------|
| `0` / unset (default) | **Active-only.** Finished and crashed nodes are excluded. Matches `show_idle=false`. |
| `"10s"` | Active nodes + finished/crashed nodes that ended in the last 10 seconds. |
| `"5m"` | Active nodes + finished/crashed nodes from the last 5 minutes. |
| `"-1"` / `"inf"` | **No pruning.** Every resource ever tracked is included. Matches `show_idle=true`. |

When `time_window` is set, it takes precedence over `show_idle`:

| `show_idle` | `time_window` | Effective behaviour |
|:-----------:|:-------------:|---------------------|
| `false` | unset | Active-only (default) |
| `true` | unset | No pruning |
| any | `"30s"` | Active + finished from last 30s |
| any | `"-1"` | No pruning |

### 4.7 Live Updates

The engine supports real-time tree updates through a three-tier mechanism:

**Tier 1 — Event-driven cache mutation (engine core).**
Each call to `PushEvent(evt)` performs:

```
PushEvent(evt):
  1. Look up ResourceState by evt.Resource in StateCache
  2. If not found: create new ResourceState with Status=Pending
  3. Apply state transition based on evt.Type:
     - Start type (spawn, create, connect, open):
         if state.StartedAt is zero:
             state.Status = Active
             state.StartedAt = evt.Timestamp
     - End type (stop, crash, disconnect, close):
         if state.FinishedAt is nil:
             state.Status = terminalStatus(evt.Type)
             state.FinishedAt = evt.Timestamp
             state.Duration = FinishedAt - StartedAt
     - Info type (metric, heartbeat):
         merge evt.Attrs into state.Attrs
  4. Increment stateCache.version
  5. Notify subscribed StreamTree streams
```

**Tier 2 — Full reconstruction on query.**
Every unary RPC (`QueryTree`, `QueryHistory`, `QueryMetrics`) runs the full
reconstruction algorithm at call time. This guarantees the response is always
consistent with the latest state cache.

**Tier 3 — Incremental client-side reconstruction (TUI/CLI --tail).**
When a client subscribes to real-time updates via `StreamEvents`, it builds
and maintains its own local copy of the `StateCache`:

```
On connect:
  1. QueryTree(time_window=5m)   → initial tree + all finished nodes in window
  2. StreamEvents()              → real-time event stream

On each DiagEvent received:
  1. Apply same state transition logic as engine (Tier 1) to local cache
  2. Run ReconstructTree(localCache, currentFilters)
  3. Re-render UI  (diff or full redraw)

On filter/time_window change:
  1. Run ReconstructTree(localCache, newFilters)
  2. Re-render UI  (zero network latency)
```

This approach gives:
- **Instant filter changes** — no network round-trip, the client has all data
- **Minimal bandwidth** — only compact `DiagEvent` messages travel the wire
- **Offline-capable UI** — the TUI remains fully interactive even if the
  connection drops momentarily (stale data until reconnect)
- **Consistent algorithm** — the same `ReconstructTree` logic runs on both
  engine and client; it should be provided as a shared library function

### 4.8 Query Integration

The `QueryTreeRequest` message carries the time window and filters as
described in §3. The `StreamEvents` RPC delivers raw events to clients
for local reconstruction:

```protobuf
service DiagnosticsQueryService {
  rpc QueryTree(QueryTreeRequest) returns (QueryTreeResponse);
  rpc QueryHistory(QueryHistoryRequest) returns (QueryHistoryResponse);
  rpc QueryMetrics(QueryMetricsRequest) returns (QueryMetricsResponse);
  rpc StreamEvents(StreamEventsRequest) returns (stream DiagEvent);
}
```

The `FilterFunc` type used in the engine Go API mirrors the proto filters:

```go
type FilterFunc func(*ResourceState) bool
```

And the `GetCurrentTree` method signature becomes:

```go
func (e *Engine) GetCurrentTree(timeWindow time.Duration, filters ...FilterFunc) *DiagNode
func (e *Engine) GetResources(filters ...FilterFunc) []*ResourceState
```

### 4.9 Flat List Queries

In addition to tree output, the query service provides a `QueryResources` RPC
that returns resources as a flat sorted list. This is useful for:

- **Table views** in CLI (`dotfilesctl system resources`) and TUI
- **Machine-friendly output** (JSON, YAML) for scripting
- **Pagination** — the response carries `total_count` for building paginated UIs
- **Sorting** — results can be ordered by any field (start time, type, status, label)

**Implementation:** `QueryResources` reuses Phase 2 (filtering) of the tree
reconstruction algorithm (§4.4) verbatim. After filtering, instead of building
adjacency and DFS-assembling the tree, it:

1. Collects the matching `ResourceState` entries into a flat slice
2. Optionally sorts by `sort_by` / `sort_desc`
3. Applies `limit` if set
4. Returns the list with `total_count` (matching count before limit)

The filter code path is **identical** — there is a single `filterSnapshot()`
function called by both `ReconstructTree` and `QueryResources`:

```
filtered = filterSnapshot(snapshot, timeWindow, filters)

// Tree path:
adjacency = buildAdjacency(filtered)
root = assembleTree(filtered, adjacency)

// Flat list path:
sorted = sort(filtered, sortBy, sortDesc)
paginated = applyLimit(sorted, limit)
```

This ensures that `--type plugin --status crashed` returns exactly the same
set of resources whether displayed as a tree or as a flat table.

```bash
# Flat list examples
dotfilesctl system resources                              # all active resources
dotfilesctl system resources --type executor              # only executor calls
dotfilesctl system resources --status crashed --limit 10  # last 10 crashes
dotfilesctl system resources --sort-by started_at --desc  # newest first
```

---

## 5. Daemon Integration

Daemon components push data to the engine via `DiagnosticsPostService`:

| Component | When | Event/Metric |
|-----------|------|-------------|
| `plugin.Manager` | plugin launched | `EventPluginSpawn` with pid, url, services |
| `plugin.Manager` | plugin crashed/lost | `EventPluginCrash` with exit code |
| `plugin.Manager` | plugin stopped | `EventPluginStop` |
| `SessionStore` | session created | `EventSessionCreate` |
| `SessionStore` | session finalized | `EventSessionEnd` |
| `execServer` | Exec() starts | `EventExecStart` with command string |
| `execServer` | Exec() completes | `EventExecStop` with exit code, duration |
| `executorServer` | bidi stream opens | `EventExecutorOpen` with client, plugin, method |
| `executorServer` | bidi stream closes | `EventExecutorClose` with duration |
| `ioServer` | stdin read | metric `stdin_bytes` on resource |
| `ioServer` | log line forwarded | metric `stdout_bytes` / `stderr_bytes` |
| `backgroundTaskManager` | task starts | `EventBgTaskStart` |
| `backgroundTaskManager` | task ends | `EventBgTaskStop` |

The engine reconstructs the tree from the event stream using the algorithm
in [§4](#4-tree-reconstruction-algorithm) — every `QueryTree` call runs the
full reconstruction at call time against the current `StateCache`.

---

## 6. Node Types

> **Timing metadata:** Every node also includes the timing attributes defined
> in [§4.5](#45-computing-timing-metadata) (`started`, `started_ago`,
> `running_for` / `running_for_ns` while active, `finished` / `finished_ago` /
> `duration` / `duration_ns` when finished). These are computed automatically
> by the reconstruction algorithm and are not listed per type below.

### 6.1 `type: "daemon"` — The dotfilesd process itself

| Field | Value |
|-------|-------|
| `label` | `dotfilesd (pid %d, port %s, up %ds)` |
| `attrs.pid` | OS pid |
| `attrs.port` | RPC port |
| `attrs.uptime` | Seconds since start |
| `attrs.version` | Version string |

**Children:** plugins with bg workers, background tasks

### 6.2 `type: "plugin"` — A running plugin

| Field | Value |
|-------|-------|
| `label` | `Name vX.X.X` |
| `status` | `"bg_worker"`, `"idle"`, `"crashed"` |
| `attrs.pid` | Plugin process PID |
| `attrs.url` | Plugin HTTP URL |
| `attrs.uptime` | Seconds since launch |
| `attrs.services` | Count of services exposed |

### 6.3 `type: "shell"` — A bash session

| Field | Value |
|-------|-------|
| `label` | `"bash"` |
| `status` | `"running"`, `"idle"` |
| `attrs.cwd` | Current working directory |
| `attrs.active` | Currently executing command, or `"(idle)"` |

**Hidden when:** idle and `show_idle=false`

### 6.4 `type: "session"` — A daemon session

| Field | Value |
|-------|-------|
| `label` | Session ID |
| `attrs.created` | Creation timestamp |
| `attrs.callback` | Callback URL (if set) |
| `attrs.exec_count` | Number of exec commands issued |

**Children:** shell (if any)
**Hidden when:** idle and `show_idle=false`

### 6.5 `type: "client"` — An active connected client

| Field | Value |
|-------|-------|
| `label` | Client ID |
| `attrs.session` | Associated session ID |
| `attrs.executor_count` | Number of active executor streams |

**Children:** shell (if session has one), executor calls
**Hidden when:** no active executor streams and `show_idle=false`

### 6.6 `type: "executor"` — An active bidi stream

| Field | Value |
|-------|-------|
| `label` | `"ServiceName.MethodName"` |
| `attrs.plugin` | Plugin name |
| `attrs.duration` | How long the call has been running |
| `attrs.method` | Full method name |

**Children:** (none)

### 6.7 `type: "bg_task"` — A daemon-managed background task

| Field | Value |
|-------|-------|
| `label` | Task ID |
| `attrs.command` | Shell command |
| `attrs.duration` | How long it's been running |

---

## 7. Query Examples

```bash
# Default: show only active things (no idle plugins, no idle sessions)
dotfilesctl system diag

# Show everything including idle
dotfilesctl system diag --all

# Show only plugins
dotfilesctl system diag --type plugin

# Show recent crash events
dotfilesctl system events --type plugin_crash --limit 20

# Show exec commands in the last 5 minutes
dotfilesctl system events --type exec_start --since 5m

# Show metrics as a table
dotfilesctl system stats --metric exec_duration_ms --avg

# Subscribe to real-time events
dotfilesctl system events --tail

# TUI interactive browser (plugin)
dotfilesctl tui-diag

# With time window (show finished nodes from last 30s)
dotfilesctl system diag --time-window 30s

# With explicit time window as duration
dotfilesctl system diag --time-window 5m --type executor

# Flat list: all active resources as a table
dotfilesctl system resources

# Flat list: only executor resources, sorted by start time
dotfilesctl system resources --type executor --sort-by started_at --desc

# Flat list: last 10 crashes
dotfilesctl system resources --status crashed --limit 10
```

---

## 8. TUI Plugin

A plugin named `tui-diag` implements an interactive htop-like browser.

### 8.1 Views

The TUI has two primary views for browsing the runtime state, switchable via
`Tab` or `F2`:

| View | Data Source | RPC | Description |
|------|-------------|-----|-------------|
| **Tree view** | `QueryTree` + `StreamEvents` | Hierarchical `DiagNode` | Expand/collapse tree of daemon → plugin → session → executor relationships. Best for understanding parent/child ancestry and runtime structure. |
| **Table view** | `QueryResources` + `StreamEvents` | Flat `ResourceState` list | Sortable table with columns (type, label, status, started, duration). Best for filtering, searching, and sorting specific resources. Built-in support for `--type`, `--status`, `--sort-by` toggles. |

Switching views preserves the active filter — both views share the same filter
bar, so a filter configured in tree view applies immediately when switching to
table view and vice versa.

### 8.2 Shared Features (both views)

- **Filter bar** — filter by type, status, label, text search, time window
- **History view** (`F3`) — browse events with time-based grouping
- **Metrics view** (`F4`) — sparklines for numeric metrics
- **Live updates** — both views subscribe via `StreamEvents` and apply the
  client-side reconstruction algorithm (§4.7, Tier 3) so the display updates
  in real time as events arrive
- **Keybindings:** `F5` refresh, `/` search, `Tab` / `F2` switch view,
  `F3` history, `F4` metrics, `q` quit

### 8.3 Client-Side Reconstruction

Both views maintain a local `StateCache` that is kept in sync via the
three-tier mechanism described in §4.7:

```
On connect:
  QueryTree(time_window=5m)   → initial tree state (or QueryResources for table)
  StreamEvents()              → real-time event stream

On each DiagEvent received:
  Apply state transition to local cache
  Re-run local ReconstructTree()   (tree view)
  Re-run local filter + sort       (table view)
  Re-render

On filter/time_window change:
  Re-run local ReconstructTree() / filter + sort
  Re-render (zero network latency)
```

When the user switches from tree view to table view, the local cache is already
populated — the switch is instant with no network round-trip.

### 8.4 Data Flow Between Views

```
StreamEvents ──→ Local StateCache ──→ ReconstructTree() ──→ Tree view
                                      ──→ filter + sort  ──→ Table view
```

---

## 9. Implementation Order

## 9. Implementation Order

0. **Remove old diagnostics:** Delete `SystemService.Diagnostics` RPC handler,
   the ad-hoc tree builder in `daemon/system.go`, and any references to the
   old `DiagNode`-based snapshot model. This is a clean-slate replacement.
1. **Proto:** Add `DiagnosticsPostService`, `DiagnosticsQueryService`, events,
   metrics messages to `diagnostics.proto`
2. **Engine package:** `internal/pkg/diagnostics/` with `Engine`, current state
   cache, ring-buffer history, retention policies
3. **Post service:** Mount `DiagnosticsPostService` in daemon, wire components
   to push events
4. **Query service:** Mount `DiagnosticsQueryService` in daemon, implement tree
   reconstruction with filters
5. **CLI commands:** `system diag --flags`, `system events`, `system stats`
6. **TUI plugin:** `plugins/tui-diag/` with htop-like interactive browser
7. **Streaming:** `StreamEvents` RPC for real-time event subscriptions
