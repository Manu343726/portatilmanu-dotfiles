# dotfilesd.v1

## Table of Contents

- [Services](#services)
  - [dotfilesd.v1.IOService](#dotfilesdv1ioservice)
    - [Log](#log)
    - [ReadStdin](#readstdin)
    - [TtySession](#ttysession)
- [Messages](#messages)
  - [TtyPacket](#ttypacket)
  - [StdinRequest](#stdinrequest)
  - [StdinResponse](#stdinresponse)
  - [LogEntry](#logentry)
  - [LogRequest](#logrequest)
  - [LogResponse](#logresponse)

## Services

### dotfilesd.v1.IOService

IOService — plugins submit structured log entries to the daemon's logging
system. The daemon also uses this to forward stdout/stderr from plugins
back to the active CallPlugin bidi stream for real-time output relay.

The TtySession RPC provides raw bidirectional byte streaming for plugins
that need full terminal control (e.g. tview/tcell). Unlike Log+ReadStdin
which are line-buffered, TtySession gives the plugin a real-time TTY
connection to the CLI's terminal through the executor bidi stream.

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

#### TtySession

TtySession opens a raw bidirectional TTY stream. The plugin sends an
initial TtyPacket with its client_id, then raw bytes flow in both
directions between the plugin and the CLI's terminal via the executor's
stdin buffer and stdout channel. Unlike Log (line-buffered), this stream
delivers every byte immediately — suitable for tview/tcell and other
full-screen terminal libraries.

- **Request:** `dotfilesd.v1.TtyPacket`
- **Response:** `dotfilesd.v1.TtyPacket`


## Messages

### TtyPacket

| Field | Type | Description |
|-------|------|-------------|
| `data` | bytes | Raw terminal bytes (stdin from CLI → plugin, stdout from plugin → CLI). |
| `eof` | bool | True when the sending side has closed its stream. |
| `client_id` | string | Client ID identifying the active executor stream. Set on the first packet sent from the plugin to the daemon, ignored thereafter. |
| `window_width` | int32 | Terminal window size (optional). When set, the daemon forwards a SIGWINCH-like resize notification to the plugin. |
| `window_height` | int32 |  |

### StdinRequest

| Field | Type | Description |
|-------|------|-------------|
| `client_id` | string | Client identifier from the active CallPlugin stream. |
| `max_bytes` | int32 | Maximum number of bytes to return. 0 means use the default (4096). |

### StdinResponse

| Field | Type | Description |
|-------|------|-------------|
| `data` | bytes | Bytes read from stdin. |
| `eof` | bool | True when the stdin stream is closed (no more data will arrive). |

### LogEntry

LogEntry represents a single log record from a plugin or tool.


#### AttributesEntry

| Field | Type | Description |
|-------|------|-------------|
| `key` | string |  |
| `value` | string |  |

| Field | Type | Description |
|-------|------|-------------|
| `level` | dotfilesd.v1.LogLevel | Severity level of the log entry. |
| `message` | string | Log message text. |
| `attributes` | map<...> | Structured key-value attributes for additional context. |

### LogRequest

LogRequest submits a log entry to the daemon.

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |
| `source` | string | Source identifier (e.g. plugin name, tool name). |
| `entry` | dotfilesd.v1.LogEntry | The structured log entry. |

### LogResponse

LogResponse acknowledges receipt of a log entry.

