# rclone WebDAV mount — MacsStash at `/mnt/mac-stash`

Mounts the `MacsStash-dav:` rclone remote (WebDAV) at `/mnt/mac-stash` via a
systemd **user** unit.

## fstab mountpoint

The mountpoint `/mnt/mac-stash` is owned by `manu343726` so rclone (running as
that user) can mount over it:

```sh
sudo mkdir -p /mnt/mac-stash
sudo chown manu343726 /mnt/mac-stash
```

## rclone remote

Defined in `~/.config/rclone/rclone.conf`:

```
[MacsStash-dav]
type = webdav
url = ...
```

Only `MacsStash-dav` is defined. This is a **user** unit (not system) because
rclone needs `$HOME` to read the user's rclone.conf.

## systemd user unit

**File:** `~/.config/systemd/user/mac-stash.service`

```ini
[Unit]
Description=rclone mount of MacsStash-dav: WebDAV at /mnt/mac-stash
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
ExecStart=/usr/bin/rclone mount --vfs-cache-mode writes --dir-cache-time 5s MacsStash-dav: /mnt/mac-stash
ExecStop=/usr/bin/fusermount -u /mnt/mac-stash
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
```

- `Type=notify` — rclone supports systemd sd_notify; the unit reports `started`
  only once the mountpoint is actually ready, so dependent units see files immediately.
- `ExecStop` uses `fusermount -u` to cleanly unmount (the mount is
  `type fuse.rclone`).

## Usage

On demand (no enable) — mount / unmount anytime:

```sh
systemctl --user start mac-stash.service     # mount
systemctl --user stop mac-stash.service      # unmount
systemctl --user status mac-stash.service
```

Automount on boot — from now on it starts at login:

```sh
systemctl --user enable mac-stash.service
```

Disable boot automount (back to on-demand only):

```sh
systemctl --user disable mac-stash.service
```

## Verify

```sh
systemctl --user status mac-stash.service    # active (running)
mount | grep mac-stash                       # fuse.rclone on /mnt/mac-stash
ls /mnt/mac-stash                            # share contents (e.g. Dump)
```

## Notes

- Cache mode is `writes` and dir-cache is `5s` so the mount behaves like a local
  store for write-heavy access (rclone uploads in the background).
- This user unit is **not** versioned in the dotfiles repo (the
  `.config/systemd/user/` path is gitignored) — recreate it from this doc on
  reinstall.
