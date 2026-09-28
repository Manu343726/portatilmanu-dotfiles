# Proxmox cluster `casa` — two sites over ZeroTier

Two standalone Proxmox VE 8.2.0 hosts form a **2-node cluster named `casa`**.
They live in **two different physical homes** and are joined only by **ZeroTier**.

> **Updated 2026-09-28.** This doc used to record quorum as **lost** and every VM as
> **stopped**. **Both are fixed.** The ring has been migrated to ZeroTier, quorum
> is healthy (`Quorate: Yes`, 2/2 votes), 3 of 4 VMs are running, and daily backups
> are landing on the NAS again. Sections below that described the outage are kept
> as history and clearly marked — don't follow them as current state.

This doc records the current topology, the pre-migration config, the changes
applied on 2026-09-27 and 2026-09-28, and what is still outstanding.

## Current state (verified 2026-09-28)

```
Node ID: 0x00000001  servergordo       Quorate: Yes  Total votes: 2
Node ID: 0x00000002  servernotangordo  Quorate: Yes  Total votes: 2

100 TrueNAS      servergordo       running
102 DockerHost   servernotangordo  running
104 homeassistant servernotangordo running
106 DockerHost2  servernotangordo  stopped   ← intentionally off
```

## Topology

```
  OLD HOME                          NEW HOME
  ┌──────────────────┐              ┌──────────────────────────┐
  │ servergordo      │              │ servernotangordo         │
  │ nodeid 1         │              │ nodeid 2                 │
  │ 192.168.100.2    │  ✗ dead L2   │ 192.168.100.5 (legacy)   │
  │                  │              │ 192.168.0.240 (real LAN) │
  │ ZT 172.25.53.62  │◄────ZT──────►│ ZT 172.25.211.19         │
  │ VM 100 TrueNAS   │              │ VM 102 DockerHost        │
  └──────────────────┘              │ VM 104 homeassistant     │
   192.168.100.0/24                  │ VM 106 DockerHost2       │
   is this LAN only                  │ 192.168.0.0/24 is this LAN│
                                    └──────────────────────────┘
```

`192.168.100.0/24` belongs to the **old home** and is not routable from the new
home. `192.168.0.0/24` is the **real** LAN (new home + the router at
`192.168.0.1`) and is the only one to use for anything you actually operate.
`servergordo` has **no** address on the real LAN — it is reachable only over
ZeroTier.

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

| VMID | Name | Node | Mem | Disk | Net MAC | Real LAN | ZeroTier | onboot |
|---|---|---|---|---|---|---|---|---|
| 100 | **TrueNAS** | servergordo | 2 GB | 32 GB boot + 3× 8 TB raw passthrough | `BC:24:11:EA:1A:90` | — (old-home only) | `172.25.225.161` | ✅ |
| 102 | **DockerHost** | servernotangordo | 20 GB | 272 GB | `BC:24:11:FF:EE:4D` | **`192.168.0.225`** (DHCP) | `172.25.10.159` | ✅ |
| 104 | **homeassistant** | servernotangordo | 4 GB | 32 GB | `02:FF:F5:50:70:6C` | `192.168.0.41` | `172.25.219.62` | ✅ |
| 106 | **DockerHost2** | servernotangordo | 4 GB | 128 GB | `BC:24:11:7B:65:99` | `192.168.0.252` (last seen) | `172.25.223.123` | ✅ |

No LXC containers on either host. All VMs are attached to `vmbr0`.

TrueNAS passes through three bare 8 TB disks (`scsi1-3`,
`/dev/disk/by-id/ata-ST8000DM004-*ZR15KEL2/…M28B/…M453`) — do not renumber them.

**Port forwarding:** point it at the VM's **real LAN** address — `192.168.0.225`
for DockerHost. Note `net0` carries `firewall=1` on every VM, so Proxmox's per-VM
firewall can drop packets before any host-level forward lands — check it first if
a forward "looks right" but nothing arrives.

DockerHost's `ens18` is **DHCP**, not static (changed 2026-09-28 from a static
`192.168.0.251` because the Vodafone router only issues forward rules to devices
in its lease table — see `dhcp-migration.md`). Its address is **not stable**;
`192.168.0.225` holds only as long as the DHCP reservation is honoured. Traefik
inside the guest listens on host port **444**, not 443. See
`ddns-and-exposure.md`.

### Host addresses on the old-home LAN

Kept for reference — **unreachable from the new home**:

| Host | Old-home LAN | Note |
|---|---|---|
| `servergordo` | `192.168.100.2` | |
| `servernotangordo` | `192.168.100.5` | primary on `vmbr0`; corosync binds it |
| TrueNAS (VM 100) | `192.168.100.3` | media NFS target |
| Synology | `192.168.100.112` | `volume1` — **the Synology, not TrueNAS** |

> The old version of this doc labelled `192.168.100.112` as "the TrueNAS VM" and
> claimed `casa_nas_backups` pointed at it. Both wrong: `.112` is the **Synology**,
> and TrueNAS is `.3`. The Proxmox storage has never been a TrueNAS target.

## Storage

Identical `storage.cfg` on both nodes (it is cluster-wide config):

| ID | Type | Status on both | Notes |
|---|---|---|---|
| `local` | dir | active | `/var/lib/vz` |
| `local-lvm` | lvmthin | active | thinpool `data` on vg `pve` |
| `casa_nas_backups` | nfs | **active** | server `172.25.106.32` (Synology over ZeroTier), export `/volume1/Backups` |
| `pve2` | lvm | active on both | `nodes servergordo`, `shared 0` |

`casa_nas_backups` was repointed from the dead `192.168.100.112` to the Synology's
ZeroTier address `172.25.106.32`. It now reports **active**, 67.76 % used
(2.6 TB of 3.84 TB). Note it is stored on `servergordo`, so `/var/lib/vz/dump/`
being empty locally is expected — dumps never land there.

## Cluster config (current — ring migrated to ZeroTier)

Identical on both nodes:

```
cluster_name: casa          config_version: 6
transport:    knet          secure auth: on
two_node:     1             link_mode: passive
mcastport:    5405
bindnetaddr:  172.25.0.0            ← ZeroTier overlay
ring0_addr:   servergordo      = 172.25.53.62
ring0_addr:   servernotangordo = 172.25.211.19
```

`/etc/hosts` on both nodes maps the two node names to those same ZeroTier IPs:

```
172.25.53.62     servergordo.local servergordo
172.25.211.19    servernotangordo.local servernotangordo
```

This migration **was completed** (it was the outstanding item in earlier revisions
of this doc). `knet` + multicast over ZeroTier works in practice — the ring forms
and holds quorum. `udpu` was considered but not needed.

## Pre-migration config (the old home, as it was)

Before the move, both hosts sat on one LAN:

- Subnet `192.168.100.0/24`, gateway `192.168.100.1`
- `servergordo` = `.2`, `servernotangordo` = `.5`
- TrueNAS VM 100 = `.3` (media NFS target), Synology = `.112` (volume1)
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

## Why every VM was stopped (historical — resolved 2026-09-28)

> **Historical.** This outage is over. Quorum is restored and the VMs are running;
> see *Current state* at the top. Kept because the failure chain explains the
> boot-ordering work in *ZeroTier before corosync*, which is still load-bearing.

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

## Migrating the ring to ZeroTier — DONE 2026-09-28

Applied on **both** nodes:

```
  servergordo       ring0_addr: 172.25.53.62
  servernotangordo  ring0_addr: 172.25.211.19
totem.interface.bindnetaddr: 172.25.0.0
```

Plus, on both nodes:

- `/etc/hosts` → node names to the ZeroTier IPs (pmxcfs resolves through it)
- boot ordering already in place (above), so ZT is up before the ring is built

Measured ZeroTier path between the two nodes: **14–16 ms avg, 0 % loss, ~3 ms
jitter** (50 probes) — comfortably within corosync's tolerance, and in practice it
holds quorum steadily.

**Decision taken:** `knet` with `mcastport 5405` was kept — ZeroTier does forward
multicast between members, so no switch to `udpu` was needed. If quorum ever turns
flaky, `udpu` (unicast) is the first thing to try.

**Never do this one-sided.** A half-migrated ring is how you lock out both nodes.
The safety net is that SSH to both hosts runs over **ZeroTier**, which is
independent of corosync — a broken ring can still be repaired remotely.

## Backups are working again

The old "backups are broken" state is resolved. `casa_nas_backups` is **active**
and daily 05:00 dumps are landing — VM 102 has a run for every day through
2026-09-28 (~65–69 GiB each, `vma.zst`). Retention is
`keep-daily=7, keep-weekly=8, keep-monthly=6`.

`/var/lib/vz/dump/` on `servernotangordo` is still empty and that is **not** a
fault: the storage resolves to `servergordo`, so dumps never touch the local
scratch dir. Check `list_backups` (or `pvesm list`) rather than that folder.

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
pvesm status                 # casa_nas_backups should read active
```

A VM's own address (needs `qemu-guest-agent`, installed and running):

```sh
ssh ServerNoTanGordo 'qm agent 102 network-get-interfaces'
```

`servergordo` has no real-LAN address, so it is reachable only via
`172.25.53.62` — never `192.168.100.2`.

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
