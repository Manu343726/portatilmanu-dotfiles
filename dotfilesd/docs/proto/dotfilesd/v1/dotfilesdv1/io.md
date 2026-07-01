# dotfilesd.v1

## Table of Contents

- [Services](#services)
  - [IOService](#ioservice)
    - [Log](#log)
    - [ReadStdin](#readstdin)
- [Messages](#messages)
  - [StdinRequest](#stdinrequest)
  - [StdinResponse](#stdinresponse)
  - [LogEntry](#logentry)
  - [LogRequest](#logrequest)
  - [LogResponse](#logresponse)

## Services

### IOService

IOService — plugins submit structured log entries to the daemon's logging
system. The daemon also uses this to forward stdout/stderr from plugins
back to the active CallPlugin bidi stream for real-time output relay.

#### Log

Log submits a log entry. The daemon routes it through its logging system.

- **Request:** `dotfilesd.v1.LogRequest`
- **Response:** `dotfilesd.v1.LogResponse`

#### ReadStdin

ReadStdin reads stdin data for a specific client call. The plugin calls
this from its Stdin() reader to get stdin forwarded from the CLI through
the executor's bidi stream.

- **Request:** `dotfilesd.v1.StdinRequest`
- **Response:** `dotfilesd.v1.StdinResponse`


## Messages

### StdinRequest

| Field | Type | Description |
|-------|------|-------------|
| `client_id` | string |  |
| `max_bytes` | int32 |  |

### StdinResponse

| Field | Type | Description |
|-------|------|-------------|
| `data` | bytes |  |
| `eof` | bool |  |

### LogEntry

LogEntry represents a single log record from a plugin or tool.


#### AttributesEntry

| Field | Type | Description |
|-------|------|-------------|
| `key` | string |  |
| `value` | string |  |

| Field | Type | Description |
|-------|------|-------------|
| `level` | dotfilesd.v1.LogLevel |  |
| `message` | string |  |
| `attributes` | map<...> |  |

### LogRequest

LogRequest submits a log entry to the daemon.

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |
| `source` | string | Source identifier (e.g. plugin name, tool name). |
| `entry` | dotfilesd.v1.LogEntry | The structured log entry. |

### LogResponse

LogResponse acknowledges receipt of a log entry.

