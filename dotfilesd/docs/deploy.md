# Deploy & Install

## Quick install

```sh
cd ~/dotfilesd
make setup
```

This builds binaries, installs them to `~/.local/bin/`, registers the systemd user service, and starts the daemon.

## Manual steps

### 1. Build

```sh
cd ~/dotfilesd
make build
```

### 2. Install binaries

```sh
make install
```

Binaries are installed to `~/.local/bin/dotfilesd` and `~/.local/bin/dotfilesctl`.

### 3. Install systemd service

```sh
make service-install
```

This generates `~/.config/systemd/user/dotfilesd.service` from the template and runs `systemctl --user daemon-reload`.

### 4. Start the service

```sh
make service-start
```

Enables and starts `dotfilesd.service` via `systemctl --user`.

### 5. Verify

```sh
dotfilesctl ping
# → dotfilesd v0.1.0 (pid 12345, up 5s)
```

## Upgrade

```sh
cd ~/dotfilesd
git pull
make build
make service-stop
make service-start
```

Or in one step:

```sh
cd ~/dotfilesd && git pull && make build && systemctl --user restart dotfilesd
```

## Service management

```sh
make service-stop    # stop and disable the service
make service-start   # enable and start
make service-logs    # follow journald logs (journalctl --user -u dotfilesd)
```

Manual:

```sh
systemctl --user status dotfilesd
systemctl --user restart dotfilesd
systemctl --user stop dotfilesd
journalctl --user -u dotfilesd -f
```

## Configuration

The daemon reads these environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `DOTFILESD_PORT` | `9105` | Connect RPC port |
| `DOTFILESD_LOG_DIR` | `~/dotfilesd/logs` | Log output directory |

## Running without systemd

```sh
~/.local/bin/dotfilesd &
```

Logs go to stdout. Stop with `kill %1` or `pkill dotfilesd`.

## Uninstall

```sh
systemctl --user stop dotfilesd
systemctl --user disable dotfilesd
rm ~/.config/systemd/user/dotfilesd.service
systemctl --user daemon-reload
rm ~/.local/bin/dotfilesd ~/.local/bin/dotfilesctl
```
