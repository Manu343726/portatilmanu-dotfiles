# tuidiag

## Table of Contents

- [Services](#services)
  - [tuidiag.TuiDiagService](#tuidiagtuidiagservice)
    - [Watch](#watch)
- [Messages](#messages)
  - [WatchRequest](#watchrequest)
  - [WatchResponse](#watchresponse)

## Services

### tuidiag.TuiDiagService

TuiDiagService provides an interactive htop-like diagnostic browser
for the daemon runtime state tree. Supports tree view, table view,
real-time filtering, and live updates via StreamEvents.

#### Watch

Watch opens an interactive terminal TUI that shows the daemon
diagnostics tree in real-time. The session is fully interactive:
Tab/arrow keys to navigate, / to search, F2/F3/F4 to switch views,
q to quit.

- **Request:** `tuidiag.WatchRequest`
- **Response:** `tuidiag.WatchResponse`


## Messages

### WatchRequest

| Field | Type | Description |
|-------|------|-------------|
| `initial_type_filter` | string | Optional initial type filter (e.g. "plugin", "executor"). |
| `initial_status_filter` | string | Optional initial status filter (e.g. "active", "finished"). |
| `show_idle` | bool | Show idle/inactive nodes too. |

### WatchResponse

