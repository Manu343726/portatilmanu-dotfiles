# dotfilesd.v1

## Table of Contents

- [Services](#services)
  - [ConfigService](#configservice)
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

### ConfigService

ConfigService — daemon runtime reconfiguration and restart.
Reloading user-facing dotfiles (tmux, i3, kitty) is handled by scripts
in the scripts/reload/ directory, not by this service.

#### Reconfigure

- **Request:** `dotfilesd.v1.ReconfigureRequest`
- **Response:** `dotfilesd.v1.ReconfigureResponse`

#### Restart

- **Request:** `dotfilesd.v1.RestartRequest`
- **Response:** `dotfilesd.v1.RestartResponse`


## Messages

### ReconfigureRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |
| `log_level` | dotfilesd.v1.LogLevel |  |

### ReconfigureResponse

| Field | Type | Description |
|-------|------|-------------|
| `success` | bool |  |
| `message` | string |  |

### RestartRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |

### RestartResponse

| Field | Type | Description |
|-------|------|-------------|
| `message` | string |  |


## Enums

### LogLevel

| Name | Number | Description |
|------|--------|-------------|
| `LOG_LEVEL_UNSPECIFIED` | 0 |  |
| `LOG_LEVEL_TRACE` | 1 |  |
| `LOG_LEVEL_DEBUG` | 2 |  |
| `LOG_LEVEL_INFO` | 3 |  |
| `LOG_LEVEL_WARN` | 4 |  |
| `LOG_LEVEL_ERROR` | 5 |  |

