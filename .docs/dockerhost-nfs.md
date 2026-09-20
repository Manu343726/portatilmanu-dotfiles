# NFS mounts on the DockerHost machines — TrueNAS media + Synology

The two DockerHost VMs (`DockerHost` 172.25.10.159, `DockerHost2` 172.25.223.123)
sit on the **LAN** (`192.168.100.x`), so they mount the NAS shares over LAN IPs —
unlike the laptop (`portatilmanu`), which reaches the same NASes over ZeroTier
(see `nfs-media.md` / `synology-nfs.md`).

Mounts use **systemd `.mount` units** (not fstab/automount) so they can be
mounted/unmounted on demand with `systemctl`, and are enabled to mount at boot.

## Units

**Files:** `/etc/systemd/system/mnt-media.mount` and `mnt-synology.mount`

```
# /etc/systemd/system/mnt-media.mount
[Unit]
Description=TrueNAS media (NFS over LAN)
After=network-online.target
Wants=network-online.target
TimeoutSec=60

[Mount]
What=192.168.100.3:/mnt/media/media
Where=/mnt/media
Type=nfs
Options=rw,nolock
```

```
# /etc/systemd/system/mnt-synology.mount
[Unit]
Description=Synology volume1 (NFS over LAN)
After=network-online.target
Wants=network-online.target
TimeoutSec=60

[Mount]
What=192.168.100.112:/volume1
Where=/mnt/synology
Type=nfs
Options=rw,nolock
```

The Synology mount covers the whole `/volume1` export, so all shares (`Backups`,
`Descargas`, `Dropbox`, `Google Drive`, `Media`) appear under `/mnt/synology/`.

## Boot enable

`.mount` units are static — they have no `[Install]` section, so
`systemctl enable` refuses. They're enabled by symlinking into the target's
`.wants/` directory:

```sh
sudo mkdir -p /etc/systemd/system/multi-user.target.wants
sudo ln -sf /etc/systemd/system/mnt-media.mount     /etc/systemd/system/multi-user.target.wants/mnt-media.mount
sudo ln -sf /etc/systemd/system/mnt-synology.mount  /etc/systemd/system/multi-user.target.wants/mnt-synology.mount
sudo systemctl daemon-reload
```

## Usage

```sh
systemctl start  mnt-media.mount     # mount on demand
systemctl stop   mnt-media.mount     # unmount on demand
systemctl status mnt-media.mount
# same for mnt-synology.mount
```

## Boot behavior when a share is unavailable

- The `.wants` symlink is a **soft** `Wants=` dependency (not `Requires=`), so a
  failed mount does not block boot.
- Each unit has `TimeoutSec=60` + `After=network-online.target`: systemd waits up
  to 60s for the mount, marks the unit `failed`, and boot continues.
- The mountpoint is left as an empty local dir — safe to access, just empty.
- **No auto-retry** — once a unit fails it stays down; start it manually with
  `systemctl start mnt-media.mount` when the server is back.
- Mounts use NFS `hard` by default, so if a server dies *after* mounting,
  client I/O on that mount hangs until it returns (same tradeoff as the laptop).

## Setup script

`/tmp/setup-nfs-systemd.sh` on each DockerHost performs the whole setup
idempotently: unmounts existing mounts, comments out old LAN NFS fstab lines,
writes the two units, symlinks them, and starts them. (The LAN NFS fstab lines
were the previous setup — they're commented in `/etc/fstab`, not deleted.)

## Reference

- Laptop equivalents over ZeroTier: `nfs-media.md` (TrueNAS), `synology-nfs.md` (Synology)
- The rclone WebDAV stash mount on these hosts: `mac-stash.md`