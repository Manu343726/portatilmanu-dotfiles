# dotfilesd.v1

## Table of Contents

- [Services](#services)
  - [dotfilesd.v1.SystemService](#dotfilesdv1systemservice)
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

### dotfilesd.v1.SystemService

SystemService — daemon health and runtime environment (admin-only).
Ping returns daemon identity. RuntimeInfo describes the OS and host.
SudoMethods reports available privilege escalation paths.
These RPCs are not exposed to plugins — only CLI tools and internal
daemon components can query this information.

#### Ping

Ping returns the daemon version, process ID, and uptime. Use this for health checks and liveness probes.

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
| `version` | string | Daemon version string. |
| `pid` | int64 | Process ID of the daemon. |
| `uptime_secs` | int64 | Daemon uptime in seconds. |

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
| `available_methods` | repeated string | Available sudo methods detected on the system (e.g. sudo, pkexec). |
| `current_method` | string | The currently active sudo method. |
| `has_elevation` | bool | Whether the daemon currently has sudo elevation (cached credentials). |

