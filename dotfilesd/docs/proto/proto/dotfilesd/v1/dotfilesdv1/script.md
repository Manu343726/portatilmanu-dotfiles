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
- [Enums](#enums)
  - [StepKind](#stepkind)

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

| Field | Type | Description |
|-------|------|-------------|
| `step_number` | int32 | Index of this step within the script (1-based). |
| `source_line` | string | The raw source line that generated this step. |
| `step_kind` | dotfilesd.v1.StepKind |  |
| `exit_code` | int32 | Exit code of the shell command. 0 on success. Only meaningful for exec steps. |
| `stdout` | string | Standard output captured from the step's execution. |
| `stderr` | string | Standard error captured from the step's execution. |
| `feedback_value` | string | Value returned by the user for input/confirm/choose steps. |

### RunScriptResponse

| Field | Type | Description |
|-------|------|-------------|
| `steps` | repeated dotfilesd.v1.StepResult | Results for each step in execution order. |
| `all_succeeded` | bool | True if every step completed without error. |
| `error` | string | Overall script error message (empty on success). |

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
| `name` | string | Parameter name (used as the variable name in scripts). |
| `description` | string | Human-readable description of what this parameter controls. |
| `required` | bool | Whether this parameter must be provided. |
| `default_value` | string | Default value used when the parameter is not explicitly set. |


## Enums

### StepKind

Kind of script step — determines how the step is executed.

| Name | Number | Description |
|------|--------|-------------|
| `STEP_KIND_UNSPECIFIED` | 0 | Unspecified step kind (should not occur in practice). |
| `STEP_KIND_EXEC` | 1 | Execute a shell command and capture its output. |
| `STEP_KIND_CONFIRM` | 2 | Prompt the user for a yes/no confirmation. |
| `STEP_KIND_INPUT` | 3 | Prompt the user for text input. |
| `STEP_KIND_CHOOSE` | 4 | Prompt the user to choose from a list of options. |

