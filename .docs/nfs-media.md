# NFS mount — TrueNAS `/mnt/media/media`

Mounts the TrueNAS media library over ZeroTier at `/mnt/media`.

- **Server:** `TrueNAS` — `172.25.225.161` (ZeroTier, online)
- **Export:** `/mnt/media/media` (share name `media`, visible to everyone)
- **Local mount point:** `/mnt/media`
- **fs type:** `nfs` (NFSv3)

Mounted subdirs on the share: `DescargasTorrent`, `dropbox`, `Juegos`, `Libros`,
`Musica`, `Peliculas`, `Podcasts`, `Series`, `Services`, `UseNetDownloads`, `Youtube`.

## fstab entry

**File:** `/etc/fstab` (system file, outside this repo — recreate on reinstall)

```
172.25.225.161:/mnt/media/media  /mnt/media  nfs  rw,nolock,_netdev,nofail,x-systemd.automount,x-systemd.mount-timeout=60,x-systemd.idle-timeout=60  0  0
```

## Why these options

Boot-safe (the point of this setup):
- `_netdev` — network filesystem; ordered after the network is up.
- `nofail` — don't block fail the boot if the mount can't happen.
- `x-systemd.automount` — **the key option.** The share is *not* mounted at boot
  at all. systemd creates a placeholder **autofs** mount at `/mnt/media` and only
  performs the real NFS mount on first access. So an unreachable server can never
  stall startup — boot is completely independent of the TrueNAS being online.

Mount behavior:
- `rw` — read-write.
- `nolock` — skip NFS file locking (no rpcbind/lockd portmapper lookup). Avoids a
  common hang on `mount.nfs` and is fine for media.
- `x-systemd.mount-timeout=60` — cap each mount attempt at 60s. Over ZeroTier the
  server responds in ~10–15s, so a short timeout (e.g. 10s) fails spuriously.
- `x-systemd.idle-timeout=60` — auto-unmount after 60s of inactivity.

`findmnt --verify` passes (fstab is valid).

## How autofs works here

1. `x-systemd.automount` creates an `autofs` placeholder at `/mnt/media`
   (`systemd-1 ... type autofs`) with no network involvement.
2. When a process actually accesses `/mnt/media`, systemd fires the real NFS mount.
3. After `x-systemd.idle-timeout=60` of no access, it unmounts automatically.

`findmnt /mnt/media` therefore shows **two** lines after access:
- `systemd-1 /mnt/media autofs` — the placeholder (present by default)
- `172.25.225.161:/mnt/media/media /mnt/media nfs` — the real mount (after trigger)

### Failure scenarios

- **Server offline at boot** → placeholder mounts instantly, boot unaffected.
  `ls /mnt/media` attempts the real mount, times out after 60s (`nofail` swallows
  the error), shows an empty dir.
- **Server offline, later back** → next access retries and mounts fine.
- **Server online** → first access takes ~10–15s over ZeroTier, then works; unmounts
  after 60s idle.

**Caveat:** because it auto-unmounts on idle, long-running directory watches on
`/mnt/media` may briefly see it vanish/reappear. Fine for media playback; if it ever
misbehaves, drop `x-systemd.idle-timeout=60` to keep it mounted after first access.

## Verify

```sh
findmnt /mnt/media                 # autofs placeholder + real nfs mount
ls /mnt/media                      # share contents, triggers the automount
mount | grep media                 # real nfs mount line
```

## Files involved

| File | Purpose |
|------|---------|
| `/etc/fstab` | the NFS automount entry (system file, not in repo) |
| `/mnt/media` | local mount point (created by the fstab entry) |
| `/etc/udev/rules.d/90-zerotier-mtu.rules` | keeps ZeroTier MTU sane so NFS-over-VPN works (`ssh-zerotier-mtu.md`) |
