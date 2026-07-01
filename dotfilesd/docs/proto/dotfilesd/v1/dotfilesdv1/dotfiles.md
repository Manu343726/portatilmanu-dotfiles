# dotfilesd.v1

## Table of Contents

- [Services](#services)
  - [DotfilesService](#dotfilesservice)
    - [Status](#status)
- [Messages](#messages)
  - [StatusRequest](#statusrequest)
  - [StatusResponse](#statusresponse)

## Services

### DotfilesService

DotfilesService — dotfiles repository status.
Git operations (commit, push, diff, add, log) are handled by scripts
in the scripts/git/ directory, not by this service.

#### Status

- **Request:** `dotfilesd.v1.StatusRequest`
- **Response:** `dotfilesd.v1.StatusResponse`


## Messages

### StatusRequest

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |

### StatusResponse

| Field | Type | Description |
|-------|------|-------------|
| `git_clean` | bool |  |
| `git_branch` | string |  |
| `last_commit` | string |  |

