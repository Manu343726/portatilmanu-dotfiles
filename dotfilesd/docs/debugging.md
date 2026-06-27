# Debugging

## Logs

The daemon uses a structured logging system with level filtering, color output,
and rotating file sinks. See `docs/logging.md` for full documentation.

### Daemon (systemd service)

```sh
journalctl --user -u dotfilesd -f          # follow logs
journalctl --user -u dotfilesd --no-pager  # full log dump
journalctl --user -u dotfilesd -n 50       # last 50 lines
```

### Rotated log files

```sh
tail -f ~/dotfilesd/logs/dotfilesd.log
tail -f ~/dotfilesd/logs/dotfilesctl.log
tail -f ~/dotfilesd/logs/plugins/weather.log   # plugin-specific (if configured)
```

### CLI verbose mode

```sh
dotfilesctl --verbose ping
```

This writes slog output to stderr alongside normal stdout.

### Log level control

Both daemon and CLI support setting the log level at runtime:

```sh
dotfilesd --log-level debug
dotfilesctl --log-level trace system ping
dotfilesctl config reconfigure --log-level debug   # change daemon log level live
```

## Common issues

### Daemon won't start

```sh
systemctl --user status dotfilesd
journalctl --user -u dotfilesd -n 50 --no-pager
```

Check for:
- Port conflict (`ss -tlnp | grep 9105`)
- Missing binary (`ls -l ~/.local/bin/dotfilesd`)
- Incorrect paths in the service file (`cat ~/.config/systemd/user/dotfilesd.service`)

### Permission denied on exec

The `exec` command uses `pkexec` for sudo operations. If `pkexec` is not available or the polkit dialog is dismissed, commands will fail. Check:

```sh
dotfilesctl sudo
# → current:  pkexec
# → has sudo: true
# → available: pkexec, sudo
```

If `pkexec` is problematic, edit `server.go` to prefer `sudo` instead.

### MCP not working

If the agent reports an MCP error, test the stdio server directly:

```sh
printf "Content-Length: 46\r\n\r\n{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}" | dotfilesctl mcp
```

Should return a JSON response with the tool list. If nothing is returned, rebuild the CLI.

### Daemon unreachable from client

```sh
dotfilesctl ping   # shows daemon pid and uptime
```

If you get a protocol error, rebuild both daemon and CLI after pulling changes:

```sh
cd ~/dotfilesd && git pull && make build && systemctl --user restart dotfilesd
```

## Plugin debugging

### Check loaded plugins

```sh
dotfilesctl plugin list         # list plugins and their tools
dotfilesctl plugin list -v      # verbose with input schemas
dotfilesctl plugin tree         # show plugin hierarchy
```

### Inspect plugin logs

Plugin logs are interleaved with daemon logs. They appear under the `[plugin.<name>]`
module hierarchy:

```sh
journalctl --user -u dotfilesd -f | grep -i '\[plugin\.'
# Or via the log file:
tail -f ~/dotfilesd/logs/dotfilesd.log | grep '\[plugin\.'

# Filter for a specific plugin:
journalctl --user -u dotfilesd -f | grep '\[plugin\.weather'
```

If plugin `debug`/`trace` messages are not appearing, the daemon's log level
may be filtering them. Change it at runtime:

```sh
dotfilesctl config reconfigure --log-level debug
```

For dedicated per-plugin log files, see `docs/logging.md#plugin-log-files`.

### Common plugin issues

- **Plugin not showing up**: check `plugins_dir` config. Default is `~/.config/dotfilesd/plugins/`.
- **Build failure**: test compilation manually — `cd <plugin_dir> && go build .`
- **Missing `replace` directive**: plugin `go.mod` must have `replace dotfilesd => /home/manu343726/dotfilesd`
- **Handshake timeout**: plugin must print JSON handshake to stdout within a few seconds of startup
- **Plugin crashes repeatedly**: the daemon auto-restarts with exponential backoff (1s–30s).
  Check daemon logs for crash details: `journalctl --user -u dotfilesd -n 50`
- **Token mismatch**: if the plugin's token doesn't match the daemon's expected token,
  the Execution Context calls will be rejected. Restart the daemon to regenerate tokens.
- **Function call returning wrong result**: try calling the tool directly:
  ```sh
  dotfilesctl plugin call resources current
  ```

## Session debugging

### List active sessions

```sh
dotfilesctl session list
# → ID       Age  Requests  Last Active
# → abc123   2m   5         2m ago
# → def456   30s  1         30s ago
```

### Get session details

```sh
dotfilesctl session get <id>
# → ID:        abc123
# → Created:   2m ago
# → Requests:  5
# → Last:      2m ago
```

### Session not working

If a session's shell is unresponsive:
- **Check if the session is finalized**: finalized sessions reject all requests
- **Check the callback URL**: the session's callback URL must be reachable from the daemon (localhost)
- **Check daemon logs** for shell errors: `journalctl --user -u dotfilesd -f`

## Script debugging

### Test a script directly

```sh
dotfilesctl script run --inline 'echo "hello world"'
```

### Check registered scripts

```sh
dotfilesctl script list
# → git/
# →   status    Show working tree status
# → system/
# →   update    Update system packages
```

### Common script issues

- **Script not found**: scripts live in `scripts_dir` (`~/.config/dotfilesd/scripts/`). Check the path.
- **Directive not working**: `@confirm`, `@input`, `@choose` must be on their own line. They are
  not shell commands — they are parsed by the daemon's script runner.
- **Shell state not persisting**: scripts run in a session shell. If no session is specified,
  an ephemeral session is created per script and destroyed after execution.

## MCP Apps debugging

The MCP Apps webview renders an HTML form for sudo password input. If the webview doesn't
appear or the password flow fails:

- **Check that MCP Apps capability is advertised**: the MCP server must declare
  `_meta.ui` capability in the `initialize` response. The `dotfilesctl mcp` server only
  does this when launched in MCP Apps mode.
- **Check the resource URI**: the `exec_run` tool returns `_meta.ui.resourceUri` pointing
  to `ui://dotfilesd/sudo-prompt`. The agent must render this resource.
- **Check the `_sudo_submit_password` tool**: this tool must be present when in MCP Apps mode.
  It has `visibility: "app"` so it's only visible to the MCP Apps runtime, not the agent.
- **Check the pending sudo request**: if the daemon's sudo session times out, the
  `_sudo_submit_password` call will fail with "no pending sudo request". Re-run `exec_run`.

## Health checks

```sh
# Quick check
dotfilesctl ping

# Full status
dotfilesctl status

# Detailed system info
dotfilesctl info

# Sudo method availability
dotfilesctl sudo

# Plugin status
dotfilesctl plugin list

# Session status
dotfilesctl session list
```

## Development debugging

Run the daemon in the foreground:

```sh
cd ~/dotfilesd
go run ./cmd/dotfilesd
```

In another terminal:

```sh
dotfilesctl ping
dotfilesctl info
dotfilesctl reload tmux
```

Test the MCP stdio server:

```sh
printf "Content-Length: 46\r\n\r\n{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}" | go run ./cmd/dotfilesctl mcp
```
