# dotfilesd.v1

## Table of Contents

- [Services](#services)
  - [dotfilesd.v1.FeedbackService](#dotfilesdv1feedbackservice)
    - [RequestInput](#requestinput)
    - [RequestConfirm](#requestconfirm)
    - [RequestChoose](#requestchoose)
  - [dotfilesd.v1.InputService](#dotfilesdv1inputservice)
    - [RequestInput](#requestinput)
  - [dotfilesd.v1.ConfirmService](#dotfilesdv1confirmservice)
    - [RequestConfirm](#requestconfirm)
  - [dotfilesd.v1.ChooseService](#dotfilesdv1chooseservice)
    - [RequestChoose](#requestchoose)
- [Messages](#messages)
  - [InputRequest](#inputrequest)
  - [InputResponse](#inputresponse)
  - [ConfirmRequest](#confirmrequest)
  - [ConfirmResponse](#confirmresponse)
  - [ChooseRequest](#chooserequest)
  - [ChooseResponse](#chooseresponse)

## Services

### dotfilesd.v1.FeedbackService

FeedbackService — user interaction prompts (input, confirm, choose).
This is the usage-level service: both CLI tools and plugins use it to
request user feedback. The daemon routes the prompt through whatever
channel is available (MCP elicitation, terminal, graphical dialog).

#### RequestInput

RequestInput prompts the user for text input.

- **Request:** `dotfilesd.v1.InputRequest`
- **Response:** `dotfilesd.v1.InputResponse`

#### RequestConfirm

RequestConfirm prompts the user for a yes/no confirmation.

- **Request:** `dotfilesd.v1.ConfirmRequest`
- **Response:** `dotfilesd.v1.ConfirmResponse`

#### RequestChoose

RequestChoose prompts the user to pick from a list of options.

- **Request:** `dotfilesd.v1.ChooseRequest`
- **Response:** `dotfilesd.v1.ChooseResponse`

### dotfilesd.v1.InputService

InputService is called by the daemon when it needs arbitrary text input
from the user (e.g. a value for a shell variable, git identity config).

#### RequestInput

RequestInput prompts the user for text input and returns their response.

- **Request:** `dotfilesd.v1.InputRequest`
- **Response:** `dotfilesd.v1.InputResponse`

### dotfilesd.v1.ConfirmService

ConfirmService is called by the daemon when it needs a yes/no
confirmation before proceeding (e.g. destructive file operation).

#### RequestConfirm

RequestConfirm prompts the user for a yes/no confirmation and returns their choice.

- **Request:** `dotfilesd.v1.ConfirmRequest`
- **Response:** `dotfilesd.v1.ConfirmResponse`

### dotfilesd.v1.ChooseService

ChooseService is called by the daemon when it needs the user to pick
from a list of options (e.g. select a git branch, choose a target).

#### RequestChoose

RequestChoose prompts the user to pick from a list of options and returns their selection.

- **Request:** `dotfilesd.v1.ChooseRequest`
- **Response:** `dotfilesd.v1.ChooseResponse`


## Messages

### InputRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |
| `prompt` | string | Human-readable prompt describing what input is needed. |
| `default` | string | Optional default value if the user just presses enter. |
| `sensitive` | bool | If true, the value is sensitive (e.g. password) and should not be echoed. |

### InputResponse

| Field | Type | Description |
|-------|------|-------------|
| `session_id` | string | Session ID associated with this feedback request. |
| `value` | string | The value provided by the user (or the default). |

### ConfirmRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |
| `message` | string | Human-readable message describing what the user is confirming. |
| `default_confirm` | bool | Default choice if the user just presses enter (true = yes). |

### ConfirmResponse

| Field | Type | Description |
|-------|------|-------------|
| `session_id` | string | Session ID associated with this confirmation request. |
| `confirmed` | bool | Whether the user confirmed. |

### ChooseRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |
| `prompt` | string | Human-readable prompt describing what to choose. |
| `options` | repeated string | The list of options to choose from. |
| `default_index` | int32 | Index of the default option, -1 if no default. |

### ChooseResponse

| Field | Type | Description |
|-------|------|-------------|
| `session_id` | string | Session ID associated with this choice request. |
| `selected_index` | int32 | Index of the selected option, -1 if cancelled. |
| `selected_option` | string | The selected option text (empty if cancelled). |

