# htop

## Table of Contents

- [Services](#services)
  - [htop.HtopService](#htophtopservice)
    - [Open](#open)
- [Messages](#messages)
  - [OpenRequest](#openrequest)
  - [OpenResponse](#openresponse)

## Services

### htop.HtopService

#### Open

- **Request:** `htop.OpenRequest`
- **Response:** `htop.OpenResponse`


## Messages

### OpenRequest

| Field | Type | Description |
|-------|------|-------------|
| `terminal_width` | int32 |  |
| `terminal_height` | int32 |  |

### OpenResponse

