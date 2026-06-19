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

## MCP tools (for AI agents)

Available through the MCP SSE endpoint on port 9106. Documented in the MCP server's `tools/list` response.

### `dotfiles_status`

Returns repo status, branch, last commit, hostname, uptime. Maps to `dotfilesctl status`.

### `dotfiles_reload`

Reloads configuration files. Takes a `target` parameter (`tmux`, `i3`, `kitty`, `all`).

### `dotfiles_git`

Git operations. Parameters: `action` (required), `message`, `paths`.

### `system_exec`

Execute shell commands. Parameters: `command` (required), `sudo`. Returns stdout, stderr, and exit code.

### `system_info`

Returns detailed system information. Maps to `dotfilesctl info`.

## RPC API

The Connect RPC API (port 9105) supports gRPC-compatible HTTP/JSON clients. See the protobuf definition at `proto/dotfilesd/v1/dotfilesdv1/service.proto` for message schemas.

## Logging

- **Daemon**: JSON logs to stdout (captured by systemd) and rotated file (`~/dotfilesd/logs/dotfilesd.log`)
- **CLI**: Text logs to rotated file (`~/dotfilesd/logs/dotfilesctl.log`); `--verbose` also writes to stderr
- **Log rotation**: 10 MB max size, 5 backups, 30 day retention, gzip compressed
- **Configurable**: `$DOTFILESD_LOG_DIR` overrides the log directory
