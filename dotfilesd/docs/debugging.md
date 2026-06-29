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

### Diagnostics engine not showing expected resources

```sh
dotfilesctl system diag --show-idle          # show all resources
dotfilesctl system diag --include-types=plugin  # filter by type
dotfilesctl system diag --history            # show recent events
dotfilesctl system diag --time-window=10m    # include recent finished resources
```

If the diagnostics tree looks incomplete, the engine may have pruned finished
resources via retention policies. Use `--show-idle` or `--time-window` to
expand the view.

### Plugin command flags not working

Plugin CLI commands are generated dynamically from proto schemas cached in
the `PluginRegistryService`. If flags don't match expectations, reload plugins:
```sh
dotfilesctl plugin reload
```

### Plugin executor call fails

The `PluginExecutorService` proxies stdin/stdout/stderr between CLI and plugin
via bidi streams. If a plugin call hangs:
1. Check the plugin process is running: `dotfilesctl plugin list`
2. Check daemon logs: `grep "executor" ~/dotfilesd/logs/dotfilesd.log`

### Session warnings in logs

The daemon creates named sessions for plugins at load time. If you see "session not found" warnings, ensure plugins were loaded correctly (`dotfilesctl plugin list`).

### Plugin-to-plugin calls fail with 404

This means the calling plugin cannot find the target plugin's RPC service. Verify:
1. Both plugins are loaded: `dotfilesctl plugin list`
2. The target plugin exposes the expected service
3. The calling plugin's `go.mod` has the correct `replace` directive for the target plugin

Rebuild both:
```sh
make plugin-build-all
systemctl --user restart dotfilesd
```
