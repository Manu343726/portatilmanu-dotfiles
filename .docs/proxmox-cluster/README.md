# Pre-migration config backups — cluster `casa`

Snapshots of both Proxmox nodes taken **2026-09-27 16:09**, immediately before
migrating the cluster interconnect from the old-home LAN to ZeroTier. Taken as
part of `.docs/proxmox-cluster.md`; read that doc for the full topology and
migration plan.

| Directory | Node | Site | ZeroTier |
|---|---|---|---|
| `pre-migration-servergordo-20260927-160915/` | `servergordo` (nodeid 1) | old home | `172.25.53.62`, node `cb1503f631` |
| `pre-migration-servernotangordo-20260927-160917/` | `servernotangordo` (nodeid 2) | new home | `172.25.211.19`, node `76bf1895c4` |

## Contents (per node)

| File | Source path on the node | Why it matters |
|---|---|---|
| `corosync.conf` | `/etc/corosync/corosync.conf` | The ring config being migrated. Both nodes identical. |
| `hosts` | `/etc/hosts` | Node-name → IP maps. `192.168.100.x` on both; must become ZeroTier IPs. |
| `interfaces` | `/etc/network/interfaces` | `servergordo` is untouched original; `servernotangordo` already has the additive `192.168.0.240` + route hooks. |
| `storage.cfg` | `/etc/pve/storage.cfg` | Cluster-wide storage, incl. the NFS backup target and the `servergordo`-only `pve2`. |
| `networks.d/b6079f73c67c6a35.conf` | `/var/lib/zerotier-one/networks.d/` | **Redacted** — see below. |
| `pvecm-status-before.txt` | `pvecm status` | Evidence: `Quorate: No`, `Activity blocked`, 1 of 2 votes. |
| `qm-list-before.txt` | `qm list` | All 4 VMs `stopped` before migration. |
| `pvesm-status-before.txt` | `pvesm status` | `casa_nas_backups` **inactive**, `pve2` disabled on the new-home node. |

## ⚠ Redaction: ZeroTier private identity keys were removed

`networks.d/<nwid>.conf` originally contained the node's **private identity key**
in these fields:

```
C=…     node identity + private key (hex)
COO=…   cached controller-provided identity
RT=…    receive timestamp
I=…     …
```

Those are **secrets**. A ZeroTier node identity is a bearer credential: whoever
holds the private key can present that node's identity to the controller and
appear on the network as that host. Committing them to a repo mirrored to GitHub
would have leaked the identity of **both** Proxmox hosts.

Only non-secret metadata was kept:

```
v=…  nwid=…  id=…  n=home  mtu=…  allowManaged=…  allowGlobal=…  allowDefault=…  allowDNS=…
```

`id=` is safe to publish — it is the same node address the ZeroTier controller
already shows in its member list.

`/etc/corosync/authkey` was deliberately **not** copied; it stays on the nodes.

If a full-fidelity copy is ever needed, take it directly from the host
(`/var/lib/zerotier-one/networks.d/`) and keep it **out** of git.

## Rolling back

The live originals are still on both nodes at:

```
/root/cluster-migration-backup-20260927-160915/   # servergordo
/root/cluster-migration-backup-20260927-160917/   # servernotangordo
```

To restore, copy the relevant file back and restart the service:

```sh
cp /root/cluster-migration-backup-*/corosync.conf /etc/corosync/corosync.conf
cp /root/cluster-migration-backup-*/hosts        /etc/hosts
systemctl restart corosync        # NOT pve-cluster first; verify with pvecm status
```

Always keep ZeroTier SSH working while repairing corosync — it does not depend
on the cluster:

```sh
ssh ServerGordo        # root@172.25.53.62
ssh ServerNoTanGordo   # root@172.25.211.19
```
