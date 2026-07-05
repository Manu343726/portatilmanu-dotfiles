# dotfilesd.v1

## Table of Contents

- [Services](#services)
  - [dotfilesd.v1.ScriptService](#dotfilesdv1scriptservice)
    - [RunScript](#runscript)
    - [ListScripts](#listscripts)
- [Messages](#messages)
  - [RunScriptRequest](#runscriptrequest)
  - [StepResult](#stepresult)
  - [RunScriptResponse](#runscriptresponse)
  - [ListScriptsRequest](#listscriptsrequest)
  - [ListScriptsResponse](#listscriptsresponse)
  - [ScriptEntry](#scriptentry)
  - [ScriptParam](#scriptparam)

## Services

### dotfilesd.v1.ScriptService

ScriptService — run a multi-step script with interleaved shell commands
and client feedback (input, confirm, choose). Scripts execute in a
session with a persistent shell so variables set in one step are
available in subsequent steps.

#### RunScript

RunScript parses and executes a script. Feedback steps (input/confirm/
choose) are handled through the session's callback URL mechanism.

- **Request:** `dotfilesd.v1.RunScriptRequest`
- **Response:** `dotfilesd.v1.RunScriptResponse`

#### ListScripts

ListScripts returns the tree of registered scripts from the daemon's
scripts directory, with front-matter metadata.

- **Request:** `dotfilesd.v1.ListScriptsRequest`
- **Response:** `dotfilesd.v1.ListScriptsResponse`


## Messages

### RunScriptRequest

---------------------------------------------------------------------------
Script source
---------------------------------------------------------------------------

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |
| `script` | string |  |
| `script_path` | string |  |
| `registered_script` | string |  |
| `source` | oneof |  |

### StepResult

---------------------------------------------------------------------------
Per-step result
---------------------------------------------------------------------------

| Field | Type | Description |
|-------|------|-------------|
| `step_number` | int32 |  |
| `source_line` | string |  |
| `step_kind` | string |  |
| `exit_code` | int32 |  |
| `stdout` | string |  |
| `stderr` | string |  |
| `feedback_value` | string |  |

### RunScriptResponse

| Field | Type | Description |
|-------|------|-------------|
| `steps` | repeated dotfilesd.v1.StepResult |  |
| `all_succeeded` | bool |  |
| `error` | string |  |

### ListScriptsRequest

---------------------------------------------------------------------------
Script registry (ListScripts)
---------------------------------------------------------------------------

| Field | Type | Description |
|-------|------|-------------|
| `session` | dotfilesd.v1.Session |  |

### ListScriptsResponse

| Field | Type | Description |
|-------|------|-------------|
| `entries` | map<...> | Root-level tree of registered scripts. |

### ScriptEntry

ScriptEntry represents a node in the scripts tree.

| Field | Type | Description |
|-------|------|-------------|
| `path` | string | Relative path from the scripts root (e.g. "git/commit", "system"). |
| `name` | string | Basename without .dsh (e.g. "commit", "system"). |
| `is_directory` | bool | True if this is a directory with children. |
| `description` | string | Description from README.md front matter or script front matter. |
| `enabled` | bool | Whether the entry is enabled (can be disabled via README.md exclude list). |
| `children` | map<...> | Child entries (only for directories). |
| `params` | repeated dotfilesd.v1.ScriptParam | Parameter definitions from script front matter. |

### ScriptParam

| Field | Type | Description |
|-------|------|-------------|
| `name` | string |  |
| `description` | string |  |
| `required` | bool |  |
| `default_value` | string |  |

