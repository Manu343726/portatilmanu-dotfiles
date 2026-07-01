# dotfilesd.v1

## Table of Contents

- [Services](#services)
  - [SessionService](#sessionservice)
    - [CreateSession](#createsession)
    - [Connect](#connect)
    - [FinalizeSession](#finalizesession)
    - [GetSession](#getsession)
    - [ListSessions](#listsessions)
- [Messages](#messages)
  - [Shell](#shell)
  - [Session](#session)
  - [CreateSessionRequest](#createsessionrequest)
  - [CreateSessionResponse](#createsessionresponse)
  - [FinalizeSessionRequest](#finalizesessionrequest)
  - [FinalizeSessionResponse](#finalizesessionresponse)
  - [GetSessionRequest](#getsessionrequest)
  - [GetSessionResponse](#getsessionresponse)
  - [ConnectRequest](#connectrequest)
  - [ConnectResponse](#connectresponse)
  - [ListSessionsRequest](#listsessionsrequest)
  - [ListSessionsResponse](#listsessionsresponse)

## Services

### SessionService

SessionService - session management for grouping related requests.

#### CreateSession

CreateSession creates a new session and returns its ID.

- **Request:** `dotfilesd.v1.CreateSessionRequest`
- **Response:** `dotfilesd.v1.CreateSessionResponse`

#### Connect

Connect registers the client's callback URL with a session and starts
the client↔daemon feedback channel. If session_id is empty a new
session is created automatically.

- **Request:** `dotfilesd.v1.ConnectRequest`
- **Response:** `dotfilesd.v1.ConnectResponse`

#### FinalizeSession

FinalizeSession marks a session as complete. No further requests
may use this session after finalization.

- **Request:** `dotfilesd.v1.FinalizeSessionRequest`
- **Response:** `dotfilesd.v1.FinalizeSessionResponse`

#### GetSession

GetSession returns session metadata and state.

- **Request:** `dotfilesd.v1.GetSessionRequest`
- **Response:** `dotfilesd.v1.GetSessionResponse`

#### ListSessions

ListSessions returns all active (non-finalized) sessions.

- **Request:** `dotfilesd.v1.ListSessionsRequest`
- **Response:** `dotfilesd.v1.ListSessionsResponse`


## Messages

### Shell

Shell context from the CLI — tells the daemon the exact terminal environment
so commands behave as if run directly in the user's terminal.


#### EnvEntry

| Field | Type | Description |
|-------|------|-------------|
| `key` | string |  |
| `value` | string |  |

| Field | Type | Description |
|-------|------|-------------|
| `current_shell` | string |  |
| `cwd` | string |  |
| `env` | map<...> |  |

### Session


#### DataEntry

| Field | Type | Description |
|-------|------|-------------|
| `key` | string |  |
| `value` | string |  |


#### VariablesEntry

| Field | Type | Description |
|-------|------|-------------|
| `key` | string |  |
| `value` | string |  |

| Field | Type | Description |
|-------|------|-------------|
| `id` | string |  |
| `created_at` | int64 |  |
| `last_active` | int64 |  |
| `request_count` | int32 |  |
| `finalized` | bool |  |
| `data` | map<...> |  |
| `variables` | map<...> |  |
| `shell` | dotfilesd.v1.Shell |  |

### CreateSessionRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |

### CreateSessionResponse

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |

### FinalizeSessionRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |

### FinalizeSessionResponse

| Field | Type | Description |
|-------|------|-------------|
| `success` | bool |  |
| `message` | string |  |

### GetSessionRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |

### GetSessionResponse

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |

### ConnectRequest

| Field | Type | Description |
|-------|------|-------------|
| `callback_url` | string | Base URL of the client's feedback server (e.g. "http://127.0.0.1:43291") where the daemon can call InputService, ConfirmService, etc. |
| `session` | dotfilesd.v1.Session | Session to register the callback with. If empty the daemon creates a new session. |

### ConnectResponse

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |

### ListSessionsRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |

### ListSessionsResponse

| Field | Type | Description |
|-------|------|-------------|
| `sessions` | repeated dotfilesd.v1.Session |  |

