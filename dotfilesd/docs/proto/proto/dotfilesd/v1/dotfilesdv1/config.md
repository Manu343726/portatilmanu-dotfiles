# dotfilesd.v1

## Table of Contents

- [Services](#services)
  - [dotfilesd.v1.ConfigService](#dotfilesdv1configservice)
    - [Reconfigure](#reconfigure)
    - [Restart](#restart)
- [Messages](#messages)
  - [ReconfigureRequest](#reconfigurerequest)
  - [ReconfigureResponse](#reconfigureresponse)
  - [RestartRequest](#restartrequest)
  - [RestartResponse](#restartresponse)
- [Enums](#enums)
  - [LogLevel](#loglevel)

## Services

### dotfilesd.v1.ConfigService

ConfigService — daemon runtime reconfiguration and restart.
Reloading user-facing dotfiles (tmux, i3, kitty) is handled by scripts
in the scripts/reload/ directory, not by this service.

#### Reconfigure

Reconfigure changes daemon runtime configuration (e.g. log level). Changes take effect immediately.

- **Request:** `dotfilesd.v1.ReconfigureRequest`
- **Response:** `dotfilesd.v1.ReconfigureResponse`

#### Restart

Restart gracefully restarts the daemon process.

- **Request:** `dotfilesd.v1.RestartRequest`
- **Response:** `dotfilesd.v1.RestartResponse`


## Messages

### ReconfigureRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |
| `log_level` | dotfilesd.v1.LogLevel | New log level to apply. The change takes effect immediately. |

### ReconfigureResponse

| Field | Type | Description |
|-------|------|-------------|
| `success` | bool | Whether the reconfiguration was applied successfully. |
| `message` | string | Human-readable status message or error description. |

### RestartRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |

### RestartResponse

| Field | Type | Description |
|-------|------|-------------|
| `message` | string | Human-readable status message (e.g. "restart initiated"). |


## Enums

### LogLevel

| Name | Number | Description |
|------|--------|-------------|
| `LOG_LEVEL_UNSPECIFIED` | 0 |  |
| `LOG_LEVEL_TRACE` | 1 | Verbose diagnostic information, includes all internal state changes. |
| `LOG_LEVEL_DEBUG` | 2 | Detailed debugging information useful for development. |
| `LOG_LEVEL_INFO` | 3 | Normal operational messages. |
| `LOG_LEVEL_WARN` | 4 | Warning conditions that should be reviewed. |
| `LOG_LEVEL_ERROR` | 5 | Error conditions that require attention. |

