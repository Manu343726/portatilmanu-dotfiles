# htop

## Table of Contents

- [Services](#services)
  - [htop.HtopService](#htophtopservice)
    - [Open](#open)
- [Messages](#messages)
  - [TerminalSize](#terminalsize)
  - [OpenRequest](#openrequest)
  - [OpenResponse](#openresponse)

## Services

### htop.HtopService

#### Open

- **Request:** `htop.OpenRequest`
- **Response:** `htop.OpenResponse`


## Messages

### TerminalSize

| Field | Type | Description |
|-------|------|-------------|
| `width` | int32 |  |
| `height` | int32 |  |

### OpenRequest

| Field | Type | Description |
|-------|------|-------------|
| `terminal_size` | htop.TerminalSize |  |

### OpenResponse

