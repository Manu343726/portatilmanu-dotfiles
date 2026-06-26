# Features

## CLI commands (`dotfilesctl`)

### `ping`

Check the daemon is alive.

```sh
dotfilesctl ping
# → dotfilesd v0.1.0 (pid 12345, up 5s)
```

### `status`

Show dotfiles repo status (branch, clean/dirty, last commit, hostname, uptime).

```sh
dotfilesctl status
# → branch: master (clean)
# → last:   abc1234 feat: add dotfilesd
# → host:   portatilmanu
# → uptime: up 2 hours, 15 minutes
```

### `info`

Detailed system information: OS, kernel, shell, desktop environment, memory, CPU load, tmux/kitty/i3 versions.

```sh
dotfilesctl info
# → OS:      linux
# → Kernel:  6.12.x
# → Shell:   /usr/bin/zsh
# → Desktop: i3
# → Memory:  15821 MB total / 8231 MB avail
# → CPU:     0.45 load
# → Tmux:    tmux 3.5a
# → Kitty:   kitty 0.38.1
# → I3:      i3 version 4.24
```

### `exec`

Run arbitrary shell commands. Supports `--sudo` for privileged operations via `pkexec`.

```sh
dotfilesctl exec "ls -la ~"
dotfilesctl exec --sudo "pacman -Syu"
```

### `reload`

Reload configuration files. Targets: `tmux`, `i3`, `kitty`, or `all` (default).

```sh
dotfilesctl reload         # reload all
dotfilesctl reload tmux    # reload tmux config only
dotfilesctl reload i3      # reload i3 config only
dotfilesctl reload kitty   # reload kitty config only
```

### `git`

Git operations on the dotfiles repo. Actions: `status`, `diff`, `add`, `commit`, `push`, `log`.

```sh
dotfilesctl git status
dotfilesctl git diff
dotfilesctl git add -m "feat: update colors"
dotfilesctl git commit .zshrc
dotfilesctl git push
dotfilesctl git log
```

### `sudo`

Show available privilege escalation methods.

```sh
dotfilesctl sudo
# → current:  pkexec
# → has sudo: true
# → available: pkexec, sudo
```

### `mcp`

Start the MCP stdio server for AI agent integration. Reads JSON-RPC 2.0 messages from stdin and writes responses to stdout (Content-Length framing). Logs to stderr.

```sh
dotfilesctl mcp
```

Not meant to be invoked directly by a human — opencode launches it as a subprocess, configured in `opencode.jsonc` as a local MCP server.

### `script`

Run a script (builtin registered, file, inline, or from stdin) with shell commands
interleaved with feedback directives. Scripts execute in a persistent session shell —
variables set in one step are available in subsequent steps.

**Syntax (for file, inline, and stdin scripts):**

| Line type | Description |
|-----------|-------------|
| `# comment` | Ignored |
| `shell command` | Executed in the session shell |
| `@confirm "message"` | Yes/no confirmation prompt |
| `@input "prompt" [as VARNAME]` | Text input (default var: `$_input`) |
| `@choose "prompt" "opt1" "opt2" ... [as VARNAME]` | Pick from options (default var: `$_choose`) |

**Flags (mutually exclusive):**

| Flag | Description |
|------|-------------|
| `--file FILE` / `-f` | Run a script from FILE on the daemon host |
| `--inline STR` | Run STR as an inline script |
| `--stdin` | Read script text from stdin |

Without any flag, positional arguments denote a **registered script path**
(e.g., `git status` runs the `git/status` registered script).
If no flags and no arguments are given, lists all registered scripts.

**Examples:**

```sh
# List registered scripts
dotfilesctl script
# → git/
# →   commit    Stage all changes and create a commit
# →   status    Show working tree status
# → system/
# →   update    Update system packages via pacman

# Run a registered script by path (args are joined with "/")
dotfilesctl script git status          # runs git/status
dotfilesctl script git commit          # runs git/commit
dotfilesctl script system update       # runs system/update

# Tab completion works, so dotfilesctl script git <TAB>
# shows status, commit, etc.

# Run a script from a file on the daemon host
dotfilesctl script --file ~/myscript.dsh

# Run an inline script
dotfilesctl script --inline '
  echo "=== Setup ==="
  @confirm "Ready?"
  @input "Project name:" as PROJECT
  @choose "Type:" "lib" "bin" "test" as TYPE
  mkdir -p "$PROJECT/$TYPE"
  echo "Created $PROJECT/$TYPE"
'

# Read script from stdin
echo 'echo "hello"' | dotfilesctl script --stdin
```

**Scripts directory layout:**

Registered scripts live in `~/.config/dotfilesd/scripts/` (configurable via `scripts_dir`
in the daemon config or `DOTFILESD_SCRIPTS_DIR` env var). The directory is organized hierarchically:

```
scripts/
├── git/
│   ├── README.md       # Directory front matter (description, enabled, exclude)
│   ├── commit.dsh      # Script with YAML front matter
│   └── status.dsh
└── system/
    ├── README.md
    └── update.dsh
```

Each `.dsh` file can include YAML front matter between `---` markers for metadata:

```yaml
---
description: Stage all changes and create a commit
params:
  - name: message
    description: Commit message
    required: true
---
```

Directory `README.md` files can also include front matter to enable/disable scripts and set descriptions:

```yaml
---
description: Git operations and workflows
enabled: true
exclude:
  - dangerous_script
---
```

### `plugin`

Manage dynamic plugin extensions. See `docs/plugins.md` for full documentation.

```sh
# List loaded plugins and their tools
dotfilesctl plugin list
dotfilesctl plugin list -v    # verbose: show input schemas

# Invoke a tool on a plugin
dotfilesctl plugin call <plugin> <tool> key=value...
```

Example:

```sh
dotfilesctl plugin call weather forecast location=Madrid
# → Weather for Madrid, Spain
#    ⛅  +22°C

dotfilesctl plugin list
# → Name:        weather
#    Display:     Weather
#    Version:     1.0.0
#    Description: Weather forecast plugin using wttr.in
#    Tools:       forecast
#
#    1 plugin(s) loaded.
```

## MCP tools (for AI agents)

Available when opencode launches `dotfilesctl mcp` as a stdio subprocess.

### Static tools

| Tool | Service | Description |
|------|---------|-------------|
| `system_ping` | SystemService | Daemon health check |
| `system_info` | SystemService | Detailed system information |
| `system_sudo` | SystemService | Available sudo methods |
| `dotfiles_status` | DotfilesService | Dotfiles repo status |
| `dotfiles_git` | DotfilesService | Git operations |
| `exec_run` | ExecService | Execute shell commands |
| `config_reload` | ConfigService | Reload dotfiles configs |
| `config_reconfigure` | ConfigService | Change daemon runtime config |
| `config_restart` | ConfigService | Gracefully restart the daemon |
| `script_run` | ScriptService | Run multi-step scripts |
| `script_list` | ScriptService | List registered scripts |

### Plugin tools (dynamic)

When plugins are loaded, their tools are automatically available as MCP tools
qualified with the plugin name: `<plugin>_<tool>`.

Example with the weather plugin loaded:

| Tool | Description |
|------|-------------|
| `weather_forecast` | Get weather forecast for a location |

### Detailed reference

### `system_ping`

Daemon health check. Maps to `dotfilesctl system ping`.

### `system_info`

Returns detailed system information. Maps to `dotfilesctl system info`.

### `system_sudo`

Shows available sudo methods. Maps to `dotfilesctl system sudo`.

### `dotfiles_status`

Returns repo status, branch, last commit, hostname, uptime. Maps to `dotfilesctl dotfiles status`.

### `dotfiles_git`

Git operations. Parameters: `action` (required), `message`, `paths`. Maps to `dotfilesctl dotfiles git`.

### `exec_run`

Execute shell commands. Parameters: `command` (required), `sudo`. Returns stdout, stderr, and exit code. Maps to `dotfilesctl exec`.

### `script_run`

Run a multi-step script with shell commands and feedback directives (@confirm, @input, @choose). Parameters: `script` (inline content), `script_path` (path on daemon host), or `registered_script` (path in scripts tree). Maps to `dotfilesctl script`.

### `script_list`

List all registered scripts from the daemon's scripts directory, organized hierarchically. Maps to `dotfilesctl script list`.

### `config_reload`

Reloads configuration files. Takes a `target` parameter (`tmux`, `i3`, `kitty`, `all`). Maps to `dotfilesctl config reload`.

## RPC API

The Connect RPC API (port 9105) supports gRPC-compatible HTTP/JSON clients. See the protobuf definition at `proto/dotfilesd/v1/dotfilesdv1/service.proto` for message schemas.

## Logging

- **Daemon**: JSON logs to stdout (captured by systemd) and rotated file (`~/dotfilesd/logs/dotfilesd.log`)
- **CLI**: Text logs to rotated file (`~/dotfilesd/logs/dotfilesctl.log`); `--verbose` also writes to stderr
- **Log rotation**: 10 MB max size, 5 backups, 30 day retention, gzip compressed
- **Configurable**: `$DOTFILESD_LOG_DIR` overrides the log directory
