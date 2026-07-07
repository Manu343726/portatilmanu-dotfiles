# dotfilesd.v1

## Table of Contents

- [Services](#services)
  - [dotfilesd.v1.DotfilesService](#dotfilesdv1dotfilesservice)
    - [Status](#status)
- [Messages](#messages)
  - [StatusRequest](#statusrequest)
  - [StatusResponse](#statusresponse)

## Services

### dotfilesd.v1.DotfilesService

DotfilesService — dotfiles repository status.
Git operations (commit, push, diff, add, log) are handled by scripts
in the scripts/git/ directory, not by this service.

#### Status

Status returns the dotfiles repository git status: clean/dirty state, current branch, and last commit.

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
| `git_clean` | bool | Whether the working tree is clean (no uncommitted changes). |
| `git_branch` | string | Current git branch name (e.g. "master"). |
| `last_commit` | string | Short hash and subject of the most recent commit (e.g. "a1b2c3d Fix typo"). |

