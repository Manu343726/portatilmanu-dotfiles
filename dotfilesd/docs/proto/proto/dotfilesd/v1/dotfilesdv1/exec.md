# dotfilesd.v1

## Table of Contents

- [Services](#services)
  - [dotfilesd.v1.ExecService](#dotfilesdv1execservice)
    - [Exec](#exec)
    - [ExecStream](#execstream)
    - [SudoExec](#sudoexec)
    - [BackgroundExec](#backgroundexec)
- [Messages](#messages)
  - [ExecRequest](#execrequest)
  - [ExecResponse](#execresponse)
  - [ExecStreamRequest](#execstreamrequest)
  - [ExecStreamResponse](#execstreamresponse)
  - [SudoExecRequest](#sudoexecrequest)
  - [SudoExecResponse](#sudoexecresponse)
  - [AuthChallenge](#authchallenge)
  - [SudoResult](#sudoresult)
  - [BackgroundExecRequest](#backgroundexecrequest)
  - [StartCommand](#startcommand)
  - [BackgroundExecResponse](#backgroundexecresponse)
  - [StartedEvent](#startedevent)
  - [ExitEvent](#exitevent)
- [Enums](#enums)
  - [SudoMethod](#sudomethod)

## Services

### dotfilesd.v1.ExecService

ExecService - command execution.

#### Exec

Exec runs a command and returns the complete output.

- **Request:** `dotfilesd.v1.ExecRequest`
- **Response:** `dotfilesd.v1.ExecResponse`

#### ExecStream

ExecStream runs a command and streams stdout/stderr chunks in real
time. The final message has done=true with the exit code. Use this
for long-running commands (e.g. package updates, builds) where the
caller wants to see output as it's produced.

- **Request:** `dotfilesd.v1.ExecStreamRequest`
- **Response:** `dotfilesd.v1.ExecStreamResponse`

#### SudoExec

SudoExec is a challenge-response protocol for sudo elevation.
The first call omits password; if the daemon needs auth it returns
AuthChallenge. The client retries with the password.

- **Request:** `dotfilesd.v1.SudoExecRequest`
- **Response:** `dotfilesd.v1.SudoExecResponse`

#### BackgroundExec

BackgroundExec starts a command and keeps it running in the background.
The bidirectional stream carries stdin from client→server, stdout/stderr
from server→client, and a cancel signal. The command runs until it exits
or the client cancels. Only one BackgroundExec per stream.

- **Request:** `dotfilesd.v1.BackgroundExecRequest`
- **Response:** `dotfilesd.v1.BackgroundExecResponse`


## Messages

### ExecRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |
| `command` | string |  |
| `sudo` | bool |  |
| `sudo_timeout_seconds` | int32 | Override the daemon's sudo credential cache timeout (seconds). 0 or unset means use the daemon default. |

### ExecResponse

| Field | Type | Description |
|-------|------|-------------|
| `exit_code` | int32 |  |
| `stdout` | string |  |
| `stderr` | string |  |

### ExecStreamRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |
| `command` | string |  |
| `sudo` | bool |  |
| `sudo_timeout_seconds` | int32 | Override the daemon's sudo credential cache timeout (seconds). 0 or unset means use the daemon default. |

### ExecStreamResponse

| Field | Type | Description |
|-------|------|-------------|
| `stdout_chunk` | bytes | A chunk of stdout output from the command. |
| `stderr_chunk` | bytes | A chunk of stderr output from the command. |
| `done` | bool | If true, this is the final message — the command has finished. |
| `exit_code` | int32 | The command's exit code (only meaningful when done=true). |
| `error_message` | string | Non-empty if the command could not be started (Go error). |

### SudoExecRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |
| `command` | string |  |
| `password` | string |  |
| `preferred_method` | dotfilesd.v1.SudoMethod |  |
| `sudo_timeout_seconds` | int32 | Override the daemon's sudo credential cache timeout (seconds). 0 or unset means use the daemon default. |

### SudoExecResponse

| Field | Type | Description |
|-------|------|-------------|
| `result` | dotfilesd.v1.SudoResult |  |
| `auth_challenge` | dotfilesd.v1.AuthChallenge |  |
| `outcome` | oneof |  |

### AuthChallenge

| Field | Type | Description |
|-------|------|-------------|
| `methods` | repeated dotfilesd.v1.SudoMethod |  |
| `prompt` | string |  |

### SudoResult

| Field | Type | Description |
|-------|------|-------------|
| `exit_code` | int32 |  |
| `stdout` | string |  |
| `stderr` | string |  |
| `auth_cancelled` | bool |  |

### BackgroundExecRequest

BackgroundExecRequest is a client→server message on a background exec
stream. The first message MUST be a start action; subsequent messages
carry stdin data or a cancel signal.

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |
| `start` | dotfilesd.v1.StartCommand |  |
| `stdin_chunk` | bytes |  |
| `cancel` | bool |  |
| `action` | oneof |  |

### StartCommand

StartCommand configures the background process.

| Field | Type | Description |
|-------|------|-------------|
| `command` | string |  |
| `sudo` | bool |  |
| `sudo_timeout_seconds` | int32 | Override the daemon's sudo credential cache timeout (seconds). 0 or unset means use the daemon default. |

### BackgroundExecResponse

BackgroundExecResponse is a server→client message on a background exec
stream. The first response is always a started event; subsequent responses
carry output chunks or a final exit event.

| Field | Type | Description |
|-------|------|-------------|
| `stdout_chunk` | bytes |  |
| `stderr_chunk` | bytes |  |
| `exit` | dotfilesd.v1.ExitEvent |  |
| `started` | dotfilesd.v1.StartedEvent |  |
| `event` | oneof |  |

### StartedEvent

| Field | Type | Description |
|-------|------|-------------|
| `task_id` | string | Opaque task ID for logging/diagnostics. |

### ExitEvent

| Field | Type | Description |
|-------|------|-------------|
| `exit_code` | int32 |  |
| `error_message` | string | Non-empty if the command could not be started (Go error). |


## Enums

### SudoMethod

| Name | Number | Description |
|------|--------|-------------|
| `SUDO_METHOD_UNSPECIFIED` | 0 |  |
| `SUDO_METHOD_GRAPHICAL` | 1 |  |
| `SUDO_METHOD_NOPASS` | 2 |  |

