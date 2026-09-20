# TrueNAS / NAS recovery over smart plugs + Proxmox

The NASes (TrueNAS and the Synology) are **VMs on Proxmox**. Their hosts are on
smart plugs managed by Home Assistant, and the VMs themselves are managed via
the Proxmox MCP tools. This doc records the recovery path when a NAS VM goes
unresponsive — used when the TrueNAS VM hung in 2026-09.

## Topology (relevant bits)

- **Proxmox host `servergordo`** runs the TrueNAS VM; **`servernotangordo`**
  runs DockerHost, DockerHost2, and the Home Assistant VM.
- The TrueNAS VM is `vmid 100` on `servergordo`, with `onboot=1`,
  `startup=order=1` (auto-starts on host boot).
- Smart plugs in Home Assistant: `enchufe servergordo Socket 1` and
  `enchufe servernotangordo Socket 1` power the two Proxmox hosts. The
  `enchufe servergordo Socket 1` plug was found **off**, which is why the whole
  host (and the TrueNAS VM on it) was unreachable.

## Symptom

- NAS unreachable: no ping on either its LAN IP or the VPN IP; NFS mounts on the
  clients fail / return empty dirs.
- The Proxmox MCP target (`servergordo`) is also unreachable → the host itself
  is down, not just the VM.

## Recovery steps

### 1. Power the host back on via the smart plug

From Home Assistant, turn on the plug powering the Proxmox host:

```
HassTurnOn → "enchufe servergordo Socket 1"
```

Wait for the host to boot (SSH poll: `ssh ServerGordo hostname`). It can take a
few minutes.

### 2. Verify the VM is running on Proxmox

```
Proxmox → GetVMs          # find the NAS VM (vmid 100) and its node
Proxmox → GetNodeStatus / GetVmConfig   # check state
```

The VM may come back `RUNNING` but hung very early in boot (very low RAM
usage, no network response) — the guest OS stalled during boot.

### 3. Hard-reset the VM if it's hung

```
Proxmox → ResetVM(node=servergordo, vmid=100)
```

If the guest agent isn't available (no qemu-guest-agent configured), `ExecuteVM`
won't work; `ResetVM` (a hard reset) is the right tool. Poll the VM's IP until
it pings.

## Verify recovery

- NAS answers ping on its LAN IP (and VPN IP if applicable).
- NFS port reachable (`/dev/tcp/<ip>/2049`).
- On the clients, start the NFS mounts again if they failed at boot:
  `systemctl start mnt-media.mount` / `mnt-synology.mount` (see
  `dockerhost-nfs.md`).

## Notes

- The NAS VMs have **no QEMU guest agent**, so graceful `ShutdownVM` /
  `ExecuteVM` commands aren't available for them — hard reset is the option.
- Smart plug state is visible in Home Assistant live context; the plugs report
  `on`/`off` and live in the same area as the host (`Salon` for servergordo,
  `Despacho` for servernotangordo).
- The `pcgordo` skill (`~/.agents/skills/pcgordo-wol/`) covers power ops for the
  salon desktop PC via Home Assistant; the NAS/proxmox path is documented here.

## See also

- `dockerhost-nfs.md` — the client-side NFS mounts that depend on these NASes
- `nfs-media.md` / `synology-nfs.md` — laptop-side mounts over the VPN