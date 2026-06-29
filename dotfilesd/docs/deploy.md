# Deploy & Install

## Quick install

```sh
cd ~/dotfilesd
make setup
```

This builds binaries, installs them to `~/.local/bin/`, registers the systemd user service, and starts the daemon.

## Manual steps

### 1. Build (includes proto generation)

```sh
cd ~/dotfilesd
make build
```

### 2. Install binaries and restart daemon

```sh
make install
```

This builds (if needed), replaces the running daemon binary, and restarts the service.

### 3. Start/stop the service

```sh
make service-start    # enable and start
make service-stop     # stop and disable
```

### 4. Verify

```sh
dotfilesctl system ping
# → dotfilesd v0.1.0 (pid 12345, up 5s)
```

## Upgrade

```sh
cd ~/dotfilesd
git pull
make install    # auto-rebuilds if hash changed, restarts daemon
```

Or in one step:
```sh
cd ~/dotfilesd && git pull && make install
```

## Service management

```sh
systemctl --user status dotfilesd
systemctl --user restart dotfilesd
journalctl --user -u dotfilesd -f
```

## Configuration

The daemon reads from `~/.config/dotfilesd/config.yaml`:

```yaml
port: 9105
log_dir: ~/dotfilesd/logs
log_level: info
log_max_mb: 10
log_backup: 5
log_age: 30
plugins_dir: ~/.config/dotfilesd/plugins
plugin_cache_dir: ~/.cache/dotfilesd/plugins
scripts_dir: ~/.config/dotfilesd/scripts
```

Environment variables override config values: `DOTFILESD_PORT`, `DOTFILESD_LOG_LEVEL`, `DOTFILESD_LOG_DIR`.

### Configuration reload

The daemon supports runtime reconfiguration of the log level without restart:

```sh
dotfilesctl config reconfigure --log-level debug
dotfilesctl config reconfigure --log-level trace
dotfilesctl config reconfigure --log-level warn
```

## Post-install

After the daemon is running, build the plugins:

```sh
make plugin-build-all
systemctl --user restart dotfilesd
```

Distribute the systemd service file to the target machine:

```sh
cp ~/dotfilesd/service/dotfilesd.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now dotfilesd
```
