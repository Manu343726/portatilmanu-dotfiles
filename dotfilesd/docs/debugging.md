# Debugging

## Logs

### Systemd (daemon)

```sh
journalctl --user -u dotfilesd -f          # follow logs
journalctl --user -u dotfilesd --no-pager  # full dump
journalctl --user -u dotfilesd -n 50       # last 50 lines
```

### Rotated log files

```sh
tail -f ~/dotfilesd/logs/dotfilesd.log
```

### CLI verbose mode

```sh
dotfilesctl --verbose ping
dotfilesctl --log-level debug system runtime
```

### Change daemon log level at runtime

```sh
dotfilesctl config reconfigure --log-level trace
dotfilesctl config reconfigure --log-level warn
```

## Common issues

### Daemon won't start

```sh
systemctl --user status dotfilesd
journalctl --user -u dotfilesd -n 50 --no-pager
```

Check for:
- Port conflict: `ss -tlnp | grep 9105`
- Missing binary: `which dotfilesd`
- Service file: `cat ~/.config/systemd/user/dotfilesd.service`

### Permission denied on exec

The `exec` command uses `pkexec` for sudo. If `pkexec` is unavailable or the dialog is dismissed:

```sh
dotfilesctl system sudo
# → current:  pkexec
# → available: pkexec, sudo
```

### Daemon unreachable

```sh
dotfilesctl system ping
```

If it fails, rebuild and restart:
```sh
cd ~/dotfilesd && make install
```

### MCP not working

Test the MCP server directly:
```sh
printf '{"jsonrpc":"2.0","id":1,"method":"tools/list"}\n' | dotfilesctl mcp
```

### Plugin not loading

```sh
dotfilesctl plugin list
```

If a plugin is missing:
- Check binary exists in `~/.cache/dotfilesd/plugins/<name>/`
- Check daemon logs for build errors: `grep "plugin" ~/dotfilesd/logs/dotfilesd.log | tail -20`
- Force rebuild: `make plugin-clean && systemctl --user restart dotfilesd`

### Script not found

```sh
dotfilesctl script list
```

Scripts are discovered from `~/.config/dotfilesd/scripts/`. Files must end in `.dsh` and have valid front matter.

### Session warnings in logs

The daemon creates named sessions for plugins at load time. If you see "session not found" warnings, ensure plugins were loaded correctly (`dotfilesctl plugin list`).

### Plugin-to-plugin calls fail with 404

This means the plugin's SDK is outdated. Rebuild the calling plugin:
```sh
make plugin-build PLUGIN=<name>
systemctl --user restart dotfilesd
```
