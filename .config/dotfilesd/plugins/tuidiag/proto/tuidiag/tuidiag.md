# tuidiag

## Table of Contents

- [Services](#services)
  - [tuidiag.TuiDiagService](#tuidiagtuidiagservice)
    - [Watch](#watch)
- [Messages](#messages)
  - [TerminalSize](#terminalsize)
  - [WatchRequest](#watchrequest)
  - [WatchResponse](#watchresponse)
- [Enums](#enums)
  - [DiagTypeFilter](#diagtypefilter)
  - [DiagStatusFilter](#diagstatusfilter)

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

### TerminalSize

Terminal window dimensions from the CLI caller.

| Field | Type | Description |
|-------|------|-------------|
| `width` | int32 | Terminal width in characters (default: 132). |
| `height` | int32 | Terminal height in characters (default: 43). |

### WatchRequest

| Field | Type | Description |
|-------|------|-------------|
| `initial_type_filter` | tuidiag.DiagTypeFilter | Optional initial type filter. Unset means no type filter. |
| `initial_status_filter` | tuidiag.DiagStatusFilter | Optional initial status filter. Unset means no status filter. |
| `show_idle` | bool | Show idle/inactive nodes too. |
| `terminal_size` | tuidiag.TerminalSize | Terminal window size from the CLI caller. Used to size the PTY correctly before the TUI starts. When unset, defaults to 132x43. |

### WatchResponse

WatchResponse is returned when the TUI session ends. Currently empty.


## Enums

### DiagTypeFilter

Initial filter for diagnostic node types in the TUI.

| Name | Number | Description |
|------|--------|-------------|
| `DIAG_TYPE_FILTER_UNSPECIFIED` | 0 |  |
| `DIAG_TYPE_FILTER_PLUGIN` | 1 | Show plugin nodes. |
| `DIAG_TYPE_FILTER_EXECUTOR` | 2 | Show executor (command) nodes. |
| `DIAG_TYPE_FILTER_DAEMON` | 3 | Show daemon nodes. |
| `DIAG_TYPE_FILTER_SESSION` | 4 | Show session nodes. |
| `DIAG_TYPE_FILTER_BG_TASK` | 5 | Show background task nodes. |

### DiagStatusFilter

Initial filter for diagnostic node status in the TUI.

| Name | Number | Description |
|------|--------|-------------|
| `DIAG_STATUS_FILTER_UNSPECIFIED` | 0 |  |
| `DIAG_STATUS_FILTER_ACTIVE` | 1 | Show active nodes only. |
| `DIAG_STATUS_FILTER_FINISHED` | 2 | Show finished nodes only. |
| `DIAG_STATUS_FILTER_CRASHED` | 3 | Show crashed nodes only. |
| `DIAG_STATUS_FILTER_PENDING` | 4 | Show pending nodes only. |

