# Proxmox cluster `casa` — two sites over ZeroTier

Two standalone Proxmox VE 8.2.0 hosts form a **2-node cluster named `casa`**.
They live in **two different physical homes** and are joined only by **ZeroTier**.

`servergordo` stayed in the old home. `servernotangordo` was moved to the new
home. The cluster interconnect was never re-IP'd after the move, so the ring
(`192.168.100.0/24`) no longer spans the two sites and **quorum has been lost**.
That loss is why every VM on both hosts is stopped.

This doc records the current topology, the pre-migration config, the changes
applied on 2026-09-27, and the migration that is still outstanding.

## Topology

```
  OLD HOME                          NEW HOME
  ┌──────────────────┐              ┌──────────────────────────┐
  │ servergordo      │              │ servernotangordo         │
  │ nodeid 1         │              │ nodeid 2                 │
  │ 192.168.100.2    │  ✗ dead L2   │ 192.168.100.5 (stale)    │
  │ ZT 172.25.53.62  │◄────ZT──────►│ ZT 172.25.211.19         │
  │ VM 100 TrueNAS   │              │ VM 102 DockerHost        │
  └──────────────────┘              │ VM 104 homeassistant     │
                                    │ VM 106 DockerHost2       │
                                    └──────────────────────────┘
```

The `192.168.100.0/24` subnet and the NAS at `192.168.100.112` belong to the
**old home**. They are not routable from the new home.

## Node inventory

### `servergordo` — old home ("Salon")

| | |
|---|---|
| Cluster nodeid | 1 |
| Legacy LAN IP | `192.168.100.2/24` (vmbr0, `eno1`) — **unreachable, wrong site** |
| ZeroTier IP | `172.25.53.62` (node `cb1503f631`) |
| CPU | Intel Xeon E5-1650 v2 @ 3.50GHz, 12 threads |
| RAM | 62 GB |
| Disks | 3× 7.3 TB `ST8000DM004` (sda/sdb/sdd), 2× 894 GB Kingston SA400 (sdc/sde) |
| Extra NIC | `wlx9ca2f45e6f77` (USB WiFi, `inet manual`) |
| SSH | `ssh ServerGordo` → `root@172.25.53.62` |

### `servernotangordo` — new home ("Despacho")

| | |
|---|---|
| Cluster nodeid | 2 |
| Legacy LAN IP | `192.168.100.5/24` (vmbr0, `eno1`) — kept, corosync still binds it |
| Real LAN IP | `192.168.0.240/24` — added 2026-09-27 |
| ZeroTier IP | `172.25.211.19` (node `3d3064796f`) |
| CPU | AMD Ryzen 9 6900HX, 16 threads |
| RAM | 29 GB |
| Disk | 500 GB NVMe (Samsung SSD 970 EVO Plus) |
| Extra NIC | `wlp3s0` MediaTek MT7921K WiFi 6E, **DOWN / unconfigured** |
| SSH | `ssh ServerNoTanGordo` → `root@172.25.211.19`, or `root@192.168.0.240` |

## VMs

| VMID | Name | Node | Mem | Disk | Net MAC | onboot |
|---|---|---|---|---|---|---|
| 100 | **TrueNAS** | servergordo | 2 GB | 32 GB boot + 3× 8 TB raw passthrough | `BC:24:11:EA:1A:90` | ✅ |
| 102 | **DockerHost** | servernotangordo | 20 GB | 272 GB | `BC:24:11:FF:EE:4D` | ✅ |
| 104 | **homeassistant** | servernotangordo | 4 GB | 32 GB | `02:FF:F5:50:70:6C` | ✅ |
| 106 | **DockerHost2** | servernotangordo | 4 GB | 128 GB | `BC:24:11:7B:65:99` | ✅ |

No LXC containers on either host. All VMs are attached to `vmbr0`.

TrueNAS passes through three bare 8 TB disks (`scsi1-3`,
`/dev/disk/by-id/ata-ST8000DM004-*ZR15KEL2/…M28B/…M453`) — do not renumber them.

## Storage

Identical `storage.cfg` on both nodes (it is cluster-wide config):

| ID | Type | Status on both | Notes |
|---|---|---|---|
| `local` | dir | active | `/var/lib/vz` |
| `local-lvm` | lvmthin | active | thinpool `data` on vg `pve` |
| `casa_nas_backups` | nfs | **inactive** | server `192.168.100.112`, export `/volume1/Backups` — that's the **TrueNAS VM** on `servergordo` |
| `pve2` | lvm | disabled on `servernotangordo`, active on `servergordo` | `nodes servergordo`, `shared 0` |

`casa_nas_backups` is offline purely because VM 100 (TrueNAS) is stopped — and VM
100 is stopped because quorum is lost. Circular dependency.

## Cluster config (current — still points at the old home)

Identical on both nodes:

```
cluster_name: casa          config_version: 6
transport:    knet          secure auth: on
two_node:     1             link_mode: passive
mcastport:    5405
bindnetaddr:  192.168.100.0        ← old-home LAN, does not span the sites
ring0_addr:   servergordo      = 192.168.100.2
ring0_addr:   servernotangordo = 192.168.100.5
```

`/etc/hosts` on both nodes maps the two node names to those same `192.168.100.x`
addresses.

## Pre-migration config (the old home, as it was)

Before the move, both hosts sat on one LAN:

- Subnet `192.168.100.0/24`, gateway `192.168.100.1`
- `servergordo` = `.2`, `servernotangordo` = `.5`
- TrueNAS VM 100 = `.112` (the NFS target for `casa_nas_backups`)
- Corosync multicast over that LAN was fine — the nodes were on the same segment

**None of this is reachable from the new home.** `192.168.100.1` never resolves
there, which is why `servernotangordo` spent months ARP-looping and why daily
`aptupdate` failed with exit 100.

**Verbatim backups** of both nodes' `corosync.conf`, `/etc/hosts`,
`/etc/network/interfaces`, `storage.cfg` and pre-migration `pvecm`/`qm`/`pvesm`
output are committed under [`proxmox-cluster/`](proxmox-cluster/README.md),
captured 2026-09-27 16:09 just before the migration. That directory's README
also documents a **redaction**: the ZeroTier `networks.d/*.conf` files ship with
each node's **private identity key** (`C=`), which was stripped. A ZeroTier
identity is a bearer credential, so those must never enter git.

## What changed on 2026-09-27

### `servernotangordo` — added real-LAN reachability (additive)

`vmbr0` now carries a second address, and the live LAN gateway is preferred:

```
vmbr0  192.168.100.5/24    ← primary, untouched (corosync binds it)
       192.168.0.240/24    ← added

default via 192.168.0.1   metric 10    ← internet
default via 192.168.100.1 metric 200   ← legacy fallback (dead)
```

Implemented as `ifupdown2` `up`/`down` hooks in `/etc/network/interfaces`, so it
survives reboot. The primary address is never removed, so corosync and SSH stay up.
Backups: `/etc/network/interfaces.bak.20260927-144650` and later `.bak.HHMMSS`.

**Gotcha worth remembering:** the `gateway 192.168.100.1` directive installs a
**metric-0** default route that outranks any `metric 50` route added alongside it.
The first attempt silently left internet traffic pointing at the dead gateway. The
fix is to `ip route del` the kernel-owned `onlink` route in the `up` hook before
adding the real one. Symptom: `ip route get 1.1.1.1` still shows `192.168.100.1`
even though `ping 192.168.0.1` succeeds (the latter only proves the subnet is
directly connected).

### Both nodes — ZeroTier before corosync (boot ordering)

Installed on both hosts:

- `/usr/local/sbin/wait-for-zerotier.sh` (mode 0755) — polls until the ZeroTier
  interface has a global IPv4, then returns. Resolves the interface by hint
  (`ztyxa6ggpt`) and falls back to any `zty*` device with a global address.
- `/etc/systemd/system/corosync.service.d/10-zerotier.conf` —
  `Wants=`/`After=zerotier-one.service` **plus**
  `ExecStartPre=/usr/local/sbin/wait-for-zerotier.sh`
- `/etc/systemd/system/pve-cluster.service.d/10-zerotier.conf`
- `/etc/systemd/system/pve-ha-crm.service.d/10-zerotier.conf`

`systemctl daemon-reload` was run; nothing was restarted, so this takes effect on
next boot.

**Why the script is needed and `After=` alone is not enough:** `zerotier-one` is
`Type=simple`, so systemd marks it active the moment the process is forked —
which can be before the ZT interface exists or has an address. A plain
`After=zerotier-one.service` would not have guaranteed anything.

The script **exits 0 on timeout** and only logs, so a node in the old home with no
console access still finishes booting. Set `ZTWAIT_STRICT=1` to make it hard-fail
instead (refuse to build a ring on a dead overlay).

## Why every VM is stopped

The chain, confirmed in the logs at the 14:37 boot on 2026-09-27:

```
servergordo (192.168.100.2) unreachable across sites
  → corosync cannot form the ring → Quorate: No, total votes 1 of 2
  → pmxcfs: quorum_initialize failed: 2
  → pve-ha-crm: status change startup => wait_for_quorum
  → pve-guests startall: "waiting for quorum ..."  (service hangs, `activating`)
  → all 4 VMs stay stopped
```

`pve-guests.service` sits in `activating` indefinitely. This is deliberate: with
no quorum PVE cannot know whether the peer is already running a given VMID, and
starting one risks two nodes running the same guest.

## Outstanding: migrate the ring to ZeroTier

The fix, still to be applied. On **both** nodes:

```
  servergordo       ring0_addr: 172.25.53.62
  servernotangordo  ring0_addr: 172.25.211.19
totem.interface.bindnetaddr: 172.25.0.0
```

Plus, on both nodes:

- `/etc/hosts` → node names to the ZeroTier IPs (pmxcfs resolves through it)
- boot ordering already in place (above), so ZT is up before the ring is built

Measured ZeroTier path between the two nodes: **14–16 ms avg, 0 % loss, ~3 ms
jitter** (50 probes) — comfortably within corosync's tolerance. So this is viable.

Open decisions:

- **Multicast vs unicast.** Current transport is `knet` with `mcastport 5405`.
  ZeroTier does forward multicast between members, so it may work unchanged, but
  `udpu` (unicast) is more predictable over a WAN-latency virtual link. Leaning
  `udpu`.
- **`casa_nas_backups` still points at `192.168.100.112`.** That is VM 100 on
  `servergordo`; once the ring is up and VM 100 starts, the NFS target resolves
  again on the old-home LAN. If the NAS should be reachable from the new home
  too, it needs its own ZeroTier membership or a new address.

Apply one node at a time and verify between. The safety net is that SSH to both
hosts runs over **ZeroTier**, which is independent of corosync — a broken ring
can still be repaired remotely.

## Backups are currently broken

`/var/lib/vz/dump/` on `servernotangordo` is **empty**, and `vzdump` has been
failing daily at 05:00 (09-25, 09-26, 09-27 all `job errors`) because
`casa_nas_backups` is offline. Retention policy is
`keep-daily=7, keep-weekly=8, keep-monthly=6`. Treat backup restoration as part
of the cluster migration, not a follow-up.

## Power / recovery

Both hosts are on **smart plugs** in Home Assistant, and both have BIOS
**auto power-on when AC power returns**:

- `enchufe servergordo Socket 1`
- `enchufe servernotangordo Socket 1`

Power-cycling a host this way is the recovery path when its SSH and PVE API stop
answering. `servergordo` was recovered exactly this way on 2026-09-27 after
port 22 accepted TCP then reset and port 8006 was dead.

`servergordo` is in the old home, so expect **no console access** — that is why
the ZeroTier gate is written to never hard-fail the boot.

## Diagnosing "is it up?"

```sh
# reachability over ZeroTier (works across both sites)
ping -c3 172.25.53.62      # servergordo
ping -c3 172.25.211.19     # servernotangordo

# ssh (ZT is the reliable path; the 192.168.100.x path is not)
ssh ServerGordo 'pvecm status; qm list'
ssh ServerNoTanGordo 'pvecm status; qm list'

# on either node
pvecm status | grep -E 'Quorate|Total votes'
qm list
systemctl status pve-guests     # `activating` == blocked on quorum
```

Port 8006 is the PVE web UI/API and is useful as a liveness probe
(`timeout 2 bash -c 'echo > /dev/tcp/<ip>/8006'`).

## See also

- `proxmox-cluster/README.md` — committed pre-migration config backups for both
  nodes, with the redaction note and rollback commands
- `nas-recovery.md` — TrueNAS VM recovery via the smart plugs
- `ssh-zerotier-mtu.md` — ZeroTier MTU handling (overlay MTU here is 2800)
- `dockerhost-nfs.md` / `nfs-media.md` / `synology-nfs.md` — clients that depend
  on the TrueNAS VM
- `power.md` — laptop power/lid behaviour
