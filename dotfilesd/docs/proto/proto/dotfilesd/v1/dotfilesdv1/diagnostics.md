# dotfilesd.v1

## Table of Contents

- [Services](#services)
  - [dotfilesd.v1.DiagnosticsPostService](#dotfilesdv1diagnosticspostservice)
    - [PostEvent](#postevent)
    - [PostMetric](#postmetric)
    - [PostSnapshot](#postsnapshot)
  - [dotfilesd.v1.DiagnosticsQueryService](#dotfilesdv1diagnosticsqueryservice)
    - [QueryTree](#querytree)
    - [QueryResources](#queryresources)
    - [QueryHistory](#queryhistory)
    - [QueryMetrics](#querymetrics)
    - [StreamEvents](#streamevents)
- [Messages](#messages)
  - [DiagNode](#diagnode)
  - [DiagnosticsFilter](#diagnosticsfilter)
  - [DiagEvent](#diagevent)
  - [MetricPoint](#metricpoint)
  - [PostEventResponse](#posteventresponse)
  - [PostMetricResponse](#postmetricresponse)
  - [PostSnapshotResponse](#postsnapshotresponse)
  - [QueryTreeRequest](#querytreerequest)
  - [QueryTreeResponse](#querytreeresponse)
  - [QueryHistoryRequest](#queryhistoryrequest)
  - [QueryHistoryResponse](#queryhistoryresponse)
  - [QueryMetricsRequest](#querymetricsrequest)
  - [QueryMetricsResponse](#querymetricsresponse)
  - [StreamEventsRequest](#streameventsrequest)
  - [QueryResourcesRequest](#queryresourcesrequest)
  - [QueryResourcesResponse](#queryresourcesresponse)
  - [ResourceState](#resourcestate)
- [Enums](#enums)
  - [DiagNodeType](#diagnodetype)
  - [DiagNodeStatus](#diagnodestatus)
  - [EventType](#eventtype)
  - [SortField](#sortfield)

## Services

### dotfilesd.v1.DiagnosticsPostService

#### PostEvent

PostEvent records an event.

- **Request:** `dotfilesd.v1.DiagEvent`
- **Response:** `dotfilesd.v1.PostEventResponse`

#### PostMetric

PostMetric records a metric data point.

- **Request:** `dotfilesd.v1.MetricPoint`
- **Response:** `dotfilesd.v1.PostMetricResponse`

#### PostSnapshot

PostSnapshot replaces the current state for a resource subtree.

- **Request:** `dotfilesd.v1.DiagNode`
- **Response:** `dotfilesd.v1.PostSnapshotResponse`

### dotfilesd.v1.DiagnosticsQueryService

#### QueryTree

QueryTree returns a filtered state tree.

- **Request:** `dotfilesd.v1.QueryTreeRequest`
- **Response:** `dotfilesd.v1.QueryTreeResponse`

#### QueryResources

QueryResources returns filtered resources as a flat list (no tree
reconstruction). Uses the same filter logic as QueryTree.

- **Request:** `dotfilesd.v1.QueryResourcesRequest`
- **Response:** `dotfilesd.v1.QueryResourcesResponse`

#### QueryHistory

QueryHistory returns historical events.

- **Request:** `dotfilesd.v1.QueryHistoryRequest`
- **Response:** `dotfilesd.v1.QueryHistoryResponse`

#### QueryMetrics

QueryMetrics returns metric data points.

- **Request:** `dotfilesd.v1.QueryMetricsRequest`
- **Response:** `dotfilesd.v1.QueryMetricsResponse`

#### StreamEvents

StreamEvents subscribes to real-time events.

- **Request:** `dotfilesd.v1.StreamEventsRequest`
- **Response:** `dotfilesd.v1.DiagEvent`


## Messages

### DiagNode

DiagNode represents one node in the daemon state tree.
Each node has a type label, a human-readable description, optional metadata,
and child nodes forming a parent-child hierarchy.


#### AttrsEntry

| Field | Type | Description |
|-------|------|-------------|
| `key` | string |  |
| `value` | string |  |

| Field | Type | Description |
|-------|------|-------------|
| `type` | dotfilesd.v1.DiagNodeType | Node type in the daemon state tree. |
| `label` | string | Human-readable name for this node. |
| `status` | dotfilesd.v1.DiagNodeStatus | Current lifecycle status of this node. |
| `attrs` | map<...> | Key-value metadata about this node. |
| `children` | repeated dotfilesd.v1.DiagNode | Child nodes forming the state hierarchy. |

### DiagnosticsFilter

Shared filter criteria for querying the diagnostics state tree.
All conditions are combined with AND logic.


#### AttrFiltersEntry

| Field | Type | Description |
|-------|------|-------------|
| `key` | string |  |
| `value` | string |  |

| Field | Type | Description |
|-------|------|-------------|
| `include_types` | repeated dotfilesd.v1.DiagNodeType | Only include nodes matching these types. Empty means include all types. |
| `label_regex` | string | Filter by label using a regex pattern. Empty means no filter. |
| `status_filter` | dotfilesd.v1.DiagNodeStatus | Only include nodes matching this status. Unset means no filter. |
| `attr_filters` | map<...> | Key=value filter on node attributes. Only exact matches are kept. |
| `time_window` | google.protobuf.Duration | Time window for finished/crashed nodes. Takes precedence over show_idle. 0 or unset        -> active-only (default) positive duration -> include finished nodes within this window -1 / "inf"        -> no pruning (like show_idle=true) |

### DiagEvent


#### LabelsEntry

| Field | Type | Description |
|-------|------|-------------|
| `key` | string |  |
| `value` | string |  |


#### AttrsEntry

| Field | Type | Description |
|-------|------|-------------|
| `key` | string |  |
| `value` | string |  |

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique event identifier. |
| `timestamp_ns` | int64 | Unix timestamp (nanoseconds since epoch). |
| `type` | dotfilesd.v1.EventType | Type of event. |
| `resource` | string | Resource this event belongs to (e.g. "plugin:weather"). |
| `parent` | string | Parent resource ID for tree reconstruction. Empty means root. |
| `labels` | map<...> | Key-value labels for filtering and grouping. |
| `message` | string | Human-readable event message. |
| `attrs` | map<...> | Key-value attributes with additional event details. |

### MetricPoint


#### LabelsEntry

| Field | Type | Description |
|-------|------|-------------|
| `key` | string |  |
| `value` | string |  |

| Field | Type | Description |
|-------|------|-------------|
| `timestamp_ns` | int64 | Unix timestamp (nanoseconds since epoch) when this point was recorded. |
| `name` | string | Metric name (e.g. "cpu_percent"). |
| `value` | double | Metric value as a double-precision float. |
| `labels` | map<...> | Key-value labels for filtering and grouping metrics. |

### PostEventResponse

Empty response acknowledging the event was recorded.

### PostMetricResponse

Empty response acknowledging the metric was recorded.

### PostSnapshotResponse

Empty response acknowledging the snapshot was recorded.

### QueryTreeRequest

| Field | Type | Description |
|-------|------|-------------|
| `filter` | dotfilesd.v1.DiagnosticsFilter | Shared filter criteria. All conditions are combined with AND logic. Unset filter means return all active nodes. |
| `show_idle` | bool | Show idle/inactive nodes too (default: active only). Deprecated: use filter.time_window instead. |

### QueryTreeResponse

| Field | Type | Description |
|-------|------|-------------|
| `root` | dotfilesd.v1.DiagNode | Root node of the filtered state tree. |

### QueryHistoryRequest

| Field | Type | Description |
|-------|------|-------------|
| `types` | repeated dotfilesd.v1.EventType | Event types to include. Empty means include all types. |
| `resource_regex` | string | Filter by resource ID pattern. Empty means no filter. |
| `since_ns` | int64 | Only return events after this timestamp (nanoseconds). 0 means no lower bound. |
| `until_ns` | int64 | Only return events before this timestamp (nanoseconds). 0 means no upper bound. |
| `limit` | int32 | Maximum number of events to return. 0 means use the default (100). |

### QueryHistoryResponse

| Field | Type | Description |
|-------|------|-------------|
| `events` | repeated dotfilesd.v1.DiagEvent | Events matching the query, in chronological order. |

### QueryMetricsRequest


#### LabelsEntry

| Field | Type | Description |
|-------|------|-------------|
| `key` | string |  |
| `value` | string |  |

| Field | Type | Description |
|-------|------|-------------|
| `name_regex` | string | Filter by metric name pattern. Empty means no filter. |
| `labels` | map<...> | Filter by key-value labels. Only exact matches are kept. |
| `since_ns` | int64 | Only return points after this timestamp (nanoseconds). 0 means no lower bound. |
| `until_ns` | int64 | Only return points before this timestamp (nanoseconds). 0 means no upper bound. |
| `limit` | int32 | Maximum number of points to return. 0 means use the default (100). |

### QueryMetricsResponse

| Field | Type | Description |
|-------|------|-------------|
| `points` | repeated dotfilesd.v1.MetricPoint | Metric data points matching the query. |

### StreamEventsRequest

Request to subscribe to a stream of real-time diagnostic events.

| Field | Type | Description |
|-------|------|-------------|
| `types` | repeated dotfilesd.v1.EventType | Event types to subscribe to. Empty means subscribe to all types. |

### QueryResourcesRequest

| Field | Type | Description |
|-------|------|-------------|
| `filter` | dotfilesd.v1.DiagnosticsFilter | Shared filter criteria. All conditions are combined with AND logic. Unset filter means return all active nodes. |
| `sort_by` | dotfilesd.v1.SortField | Field to sort results by. Unset means sort by started_at. |
| `sort_desc` | bool | Sort in descending order (default: ascending). |
| `limit` | int32 | Maximum number of resources to return. 0 means no limit. |

### QueryResourcesResponse

| Field | Type | Description |
|-------|------|-------------|
| `resources` | repeated dotfilesd.v1.ResourceState | Resources matching the query. |
| `total_count` | int32 | Total number of matching resources before applying the limit. |

### ResourceState


#### AttrsEntry

| Field | Type | Description |
|-------|------|-------------|
| `key` | string |  |
| `value` | string |  |

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique resource identifier. |
| `type` | dotfilesd.v1.DiagNodeType | Node type in the daemon state tree. |
| `label` | string | Human-readable resource name. |
| `parent_id` | string | Parent resource ID for tree reconstruction. |
| `status` | dotfilesd.v1.DiagNodeStatus | Current lifecycle status. |
| `created_at_ns` | int64 | Unix timestamp (nanoseconds) of resource creation. |
| `started_at_ns` | int64 | Unix timestamp (nanoseconds) when execution started. |
| `finished_at_ns` | int64 | Unix timestamp (nanoseconds) when execution finished. 0 if still active. |
| `duration_ns` | int64 | Execution duration in nanoseconds. 0 if still active. |
| `attrs` | map<...> | Key-value metadata attributes. |
| `exit_code` | int32 | Exit code of the process. Only meaningful for executor/shell node types. |


## Enums

### DiagNodeType

Type of node in the daemon state tree.

| Name | Number | Description |
|------|--------|-------------|
| `DIAG_NODE_TYPE_UNSPECIFIED` | 0 |  |
| `DIAG_NODE_TYPE_ROOT` | 1 | Root node — the top of the state tree. |
| `DIAG_NODE_TYPE_DAEMON` | 2 | Daemon component (core daemon). |
| `DIAG_NODE_TYPE_CLIENT` | 3 | CLI client connection. |
| `DIAG_NODE_TYPE_EXECUTOR` | 4 | Executor process (command execution). |
| `DIAG_NODE_TYPE_SESSION` | 5 | Active session (client↔daemon interaction). |
| `DIAG_NODE_TYPE_PLUGIN` | 6 | Loaded plugin instance. |
| `DIAG_NODE_TYPE_BG_TASK` | 7 | Background task. |
| `DIAG_NODE_TYPE_SHELL` | 8 | Interactive shell session. |

### DiagNodeStatus

Status of a node in the daemon state tree.

| Name | Number | Description |
|------|--------|-------------|
| `DIAG_NODE_STATUS_UNSPECIFIED` | 0 |  |
| `DIAG_NODE_STATUS_ACTIVE` | 1 |  |
| `DIAG_NODE_STATUS_PENDING` | 2 |  |
| `DIAG_NODE_STATUS_FINISHED` | 3 |  |
| `DIAG_NODE_STATUS_CRASHED` | 4 |  |

### EventType

Type of diagnostic event.

| Name | Number | Description |
|------|--------|-------------|
| `EVENT_TYPE_UNSPECIFIED` | 0 |  |
| `EVENT_TYPE_LIFECYCLE` | 1 | Resource lifecycle — created, started, finished, crashed. |
| `EVENT_TYPE_METRIC` | 2 | Metric data point recorded. |
| `EVENT_TYPE_LOG` | 3 | Human-readable log message. |
| `EVENT_TYPE_ERROR` | 4 | Error or warning condition. |
| `EVENT_TYPE_CUSTOM` | 5 | User-defined custom event. |

### SortField

Field to sort query results by.

| Name | Number | Description |
|------|--------|-------------|
| `SORT_FIELD_UNSPECIFIED` | 0 |  |
| `SORT_FIELD_STARTED_AT` | 1 | Sort by the time the resource started. |
| `SORT_FIELD_TYPE` | 2 | Sort by the resource type. |
| `SORT_FIELD_LABEL` | 3 | Sort by the human-readable label. |
| `SORT_FIELD_STATUS` | 4 | Sort by the node status. |

