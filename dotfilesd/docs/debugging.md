# Debugging

## Logs

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
```

### CLI verbose mode

```sh
dotfilesctl --verbose ping
```

This writes slog output to stderr alongside normal stdout.

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
