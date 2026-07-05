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
| `type` | string |  |
| `label` | string | "session", "plugin", "bg_task", "shell" |
| `status` | string |  |
| `attrs` | map<...> |  |
| `children` | repeated dotfilesd.v1.DiagNode |  |

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
| `id` | string |  |
| `timestamp_ns` | int64 |  |
| `type` | string |  |
| `resource` | string |  |
| `parent` | string |  |
| `labels` | map<...> |  |
| `message` | string |  |
| `attrs` | map<...> |  |

### MetricPoint


#### LabelsEntry

| Field | Type | Description |
|-------|------|-------------|
| `key` | string |  |
| `value` | string |  |

| Field | Type | Description |
|-------|------|-------------|
| `timestamp_ns` | int64 |  |
| `name` | string |  |
| `value` | double |  |
| `labels` | map<...> |  |

### PostEventResponse

### PostMetricResponse

### PostSnapshotResponse

### QueryTreeRequest


#### AttrFiltersEntry

| Field | Type | Description |
|-------|------|-------------|
| `key` | string |  |
| `value` | string |  |

| Field | Type | Description |
|-------|------|-------------|
| `include_types` | repeated string | Include only nodes matching these filters (AND). |
| `label_regex` | string |  |
| `status_filter` | string |  |
| `attr_filters` | map<...> |  |
| `show_idle` | bool | Show idle/inactive nodes too (default: active only). |
| `time_window` | google.protobuf.Duration | Time window for finished/crashed nodes (takes precedence over show_idle). 0 or unset        → active-only (default) positive duration → include finished nodes within this window -1 / "inf"        → no pruning (like show_idle=true) |

### QueryTreeResponse

| Field | Type | Description |
|-------|------|-------------|
| `root` | dotfilesd.v1.DiagNode |  |

### QueryHistoryRequest

| Field | Type | Description |
|-------|------|-------------|
| `types` | repeated string |  |
| `resource_regex` | string |  |
| `since_ns` | int64 |  |
| `until_ns` | int64 |  |
| `limit` | int32 |  |

### QueryHistoryResponse

| Field | Type | Description |
|-------|------|-------------|
| `events` | repeated dotfilesd.v1.DiagEvent |  |

### QueryMetricsRequest


#### LabelsEntry

| Field | Type | Description |
|-------|------|-------------|
| `key` | string |  |
| `value` | string |  |

| Field | Type | Description |
|-------|------|-------------|
| `name_regex` | string |  |
| `labels` | map<...> |  |
| `since_ns` | int64 |  |
| `until_ns` | int64 |  |
| `limit` | int32 |  |

### QueryMetricsResponse

| Field | Type | Description |
|-------|------|-------------|
| `points` | repeated dotfilesd.v1.MetricPoint |  |

### StreamEventsRequest

| Field | Type | Description |
|-------|------|-------------|
| `types` | repeated string |  |

### QueryResourcesRequest


#### AttrFiltersEntry

| Field | Type | Description |
|-------|------|-------------|
| `key` | string |  |
| `value` | string |  |

| Field | Type | Description |
|-------|------|-------------|
| `include_types` | repeated string | All filter fields are identical to QueryTreeRequest. |
| `label_regex` | string |  |
| `status_filter` | string |  |
| `attr_filters` | map<...> |  |
| `time_window` | google.protobuf.Duration | Time window for finished/crashed nodes. 0 or unset        → active-only positive duration → include finished nodes within this window -1 / "inf"        → no pruning |
| `sort_by` | string | Sort order for results. |
| `sort_desc` | bool |  |
| `limit` | int32 |  |

### QueryResourcesResponse

| Field | Type | Description |
|-------|------|-------------|
| `resources` | repeated dotfilesd.v1.ResourceState |  |
| `total_count` | int32 |  |

### ResourceState


#### AttrsEntry

| Field | Type | Description |
|-------|------|-------------|
| `key` | string |  |
| `value` | string |  |

| Field | Type | Description |
|-------|------|-------------|
| `id` | string |  |
| `type` | string |  |
| `label` | string |  |
| `parent_id` | string |  |
| `status` | string |  |
| `created_at_ns` | int64 |  |
| `started_at_ns` | int64 |  |
| `finished_at_ns` | int64 |  |
| `duration_ns` | int64 |  |
| `attrs` | map<...> |  |
| `exit_code` | int32 |  |

