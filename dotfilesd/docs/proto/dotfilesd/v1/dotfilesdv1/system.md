# dotfilesd.v1

## Table of Contents

- [Services](#services)
  - [SystemService](#systemservice)
    - [Ping](#ping)
    - [RuntimeInfo](#runtimeinfo)
    - [SudoMethods](#sudomethods)
- [Messages](#messages)
  - [PingRequest](#pingrequest)
  - [PingResponse](#pingresponse)
  - [RuntimeInfoRequest](#runtimeinforequest)
  - [RuntimeInfoResponse](#runtimeinforesponse)
  - [SudoMethodsRequest](#sudomethodsrequest)
  - [SudoMethodsResponse](#sudomethodsresponse)

## Services

### SystemService

SystemService — daemon health and runtime environment (admin-only).
Ping returns daemon identity. RuntimeInfo describes the OS and host.
SudoMethods reports available privilege escalation paths.
These RPCs are not exposed to plugins — only CLI tools and internal
daemon components can query this information.

#### Ping

- **Request:** `dotfilesd.v1.PingRequest`
- **Response:** `dotfilesd.v1.PingResponse`

#### RuntimeInfo

- **Request:** `dotfilesd.v1.RuntimeInfoRequest`
- **Response:** `dotfilesd.v1.RuntimeInfoResponse`

#### SudoMethods

- **Request:** `dotfilesd.v1.SudoMethodsRequest`
- **Response:** `dotfilesd.v1.SudoMethodsResponse`


## Messages

### PingRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |

### PingResponse

| Field | Type | Description |
|-------|------|-------------|
| `version` | string |  |
| `pid` | int64 |  |
| `uptime_secs` | int64 |  |

### RuntimeInfoRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |

### RuntimeInfoResponse

| Field | Type | Description |
|-------|------|-------------|
| `os` | string | Operating system (always "linux"). |
| `kernel` | string | Kernel version (uname -r). |
| `shell` | string | Current shell binary path. |
| `desktop` | string | Desktop environment name (XDG_CURRENT_DESKTOP). |
| `hostname` | string | Machine hostname. |
| `uptime` | string | System uptime (uptime -p). |
| `available_tools` | repeated string | Tools found on PATH: sudo, pkexec, tmux, i3, kitty, etc. |

### SudoMethodsRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |

### SudoMethodsResponse

| Field | Type | Description |
|-------|------|-------------|
| `available_methods` | repeated string |  |
| `current_method` | string |  |
| `has_elevation` | bool |  |

