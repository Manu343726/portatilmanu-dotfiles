# NFS mount — Synology NAS volumes at `/mnt/synology`

Mounts three Synology volume shares over ZeroTier at `/mnt/synology/`.

- **Server:** `NAS casa` (Synology DSM) — `172.25.106.32` (ZeroTier, online)
- **Exports:** `/volume1/Dropbox`, `/volume1/Backups`, `/volume1/Google Drive`
- **Local mount points:** `/mnt/synology/{Dropbox,Backups,Google Drive}`
- **fs type:** `nfs` (negotiated as NFSv4.1 here, `vers=4.1`)

Also exports `/volume1/Descargas` — not mounted (not requested).

## fstab entries

**File:** `/etc/fstab` (system file, outside this repo — recreate on reinstall)

```
172.25.106.32:/volume1/Dropbox       /mnt/synology/Dropbox        nfs  rw,nolock,_netdev,nofail,x-systemd.automount,x-systemd.mount-timeout=60,x-systemd.idle-timeout=60  0  0
172.25.106.32:/volume1/Backups       /mnt/synology/Backups        nfs  rw,nolock,_netdev,nofail,x-systemd.automount,x-systemd.mount-timeout=60,x-systemd.idle-timeout=60  0  0
172.25.106.32:/volume1/Google\040Drive  /mnt/synology/Google\040Drive  nfs  rw,nolock,_netdev,nofail,x-systemd.automount,x-systemd.mount-timeout=60,x-systemd.idle-timeout=60  0  0
```

The options are the same as the TrueNAS media mount (see `nfs-media.md`):
`_netdev` + `nofail` + `x-systemd.automount` → the shares are **not** mounted at
boot; each mounts lazily on first access (autofs placeholder), so an unreachable
NAS never stalls startup.

### Space escaping (`\040`)

The `Google Drive` share name contains a space. In fstab a space is written as
`\040` (octal for space) — in **both** the export path and the mount point:

```
/volume1/Google\040Drive  /mnt/synology/Google\040Drive
```

The resulting mountpoint on disk is still `/mnt/synology/Google Drive` (with a
literal space). `findmnt --verify` confirms the file is valid.

## Mount points

Created via:

```sh
sudo mkdir -p /mnt/synology/Dropbox /mnt/synology/Backups "/mnt/synology/Google Drive"
```

## Verify

```sh
systemctl list-units --type=automount | grep synology   # 3 active automounts
ls /mnt/synology/Dropbox          # triggers mount, shows contents
ls "/mnt/synology/Google Drive"   # space in path — quote it
findmnt /mnt/synology/Backups     # autofs placeholder + real nfs4 mount
```

## Files involved

| File | Purpose |
|------|---------|
| `/etc/fstab` | the three NFS automount entries (system file, not in repo) |
| `/mnt/synology/*` | local mount points (created above) |
| `/etc/udev/rules.d/90-zerotier-mtu.rules` | keeps ZeroTier MTU sane so NFS-over-VPN works (`ssh-zerotier-mtu.md`) |
