# DockerHost network: static IP → DHCP (2026-09-28)

Why DockerHost's `ens18` was switched from a static address to DHCP, what changed,
and how to roll it back.

## Why we did this

To expose Traefik through a port forward on the Vodafone router
(`192.168.0.1`), the router must accept the rule. It refused with:

> Esta regla de asignación de puertos no es válida debido a un cambio en la subred LAN.

Two hypotheses were tested:

1. **Stale IP binding** — the router invalidating rules after its WAN IP changed.
   **Rejected:** the error appeared even when creating a *first* rule, with no
   prior entries to invalidate.
2. **The router only accepts rules for hosts in its DHCP lease table.** A device
   with a manually configured static IP is absent from that table, so the router
   cannot resolve it and rejects the rule. **Confirmed:** selecting a
   DHCP-lease device (the Roborock vacuum, which gets its address from the
   router) produced no error, while selecting DockerHost did.

That is why the fix is to move DockerHost into the lease table.

## What changed

| | Before | After |
|---|---|---|
| `ipv4.method` | `manual` | `auto` |
| Address | `192.168.0.251/24` | **`192.168.0.225/24`** (from DHCP) |
| Gateway | `192.168.0.1` | from DHCP |
| DNS | `192.168.0.1,1.1.1.1` | from DHCP |

**The address changed from `.251` to `.225`.** This is the single most important
fact in this doc — see *The address is not stable* below.

Nothing else was touched: no service, no container, no VM config. The Proxmox VM
is unaffected (`net0` has no IP config; the guest configures itself).

## Commands used

Run on DockerHost (`ssh DockerHost`, or the LAN IP — the session drops when the
address changes, so prefer ZeroTier `172.25.10.159`):

```sh
sudo nmcli con modify "Wired connection 1" \
  ipv4.method auto ipv4.addresses "" ipv4.gateway "" ipv4.dns "" ipv4.never-default no

sudo nmcli con up "Wired connection 1"
```

`ipv4.never-default` was already `no` and was left that way.

## The DHCP reservation

The router has a static lease for DockerHost's MAC:

```
MAC: BC:24:11:FF:EE:4D
```

This was added **after** the first switch to DHCP, so the initial lease came back
as `192.168.0.225` rather than the intended `192.168.0.251`. A reservation only
takes effect for leases granted after it exists.

Reservations reserve an address *from inside the DHCP pool* — `192.168.0.225` is
a valid choice for that purpose. Current state: `192.168.0.225`.

**Verify the reservation holds** by forcing a renewal:

```sh
sudo nmcli con down "Wired connection 1" && sudo nmcli con up "Wired connection 1"
```

Then confirm:

```sh
ip -4 -br addr show ens18
```

Expect `192.168.0.225`. If it comes back as something else, the reservation is
not being honoured and the address must be treated as dynamic.

## The address is not stable

Unlike the old static address, a DHCP lease **can change**. Anything that
hardcodes the LAN IP will break silently.

Checked at the time of writing — **nothing** referenced `192.168.0.251`:

- Traefik labels route by **hostname** (`romm.manu343726.xyz`,
  `Host(manu343726.ddns.net)`), not IP
- `~/.ssh/config` on the laptop points `DockerHost` at the ZeroTier address
  `172.25.10.159`, not the LAN IP
- No `/etc/systemd` unit, NetworkManager file, or `/etc/hosts` entry on
  `servernotangordo` referenced it

The only residue is a pinned `known_hosts` entry for the old IP. Harmless; clear
it with `ssh-keygen -R 192.168.0.251`.

**Prefer ZeroTier addresses for anything durable.** The LAN IP is for local
play and for the router's port-forward table only.

## Rollback

To return to the static configuration:

```sh
sudo nmcli con modify "Wired connection 1" \
  ipv4.method manual \
  ipv4.addresses 192.168.0.251/24 \
  ipv4.gateway 192.168.0.1 \
  ipv4.dns "192.168.0.1,1.1.1.1" \
  ipv4.never-default no

sudo nmcli con up "Wired connection 1"
```

Caveat: rolling back **re-breaks port forwarding**, since the router will once
again refuse rules for a host outside its lease table. Do not roll back while a
forward is needed.

### Backup files

Original state captured before the change, at `~/dhcp-migration-backup/` on
DockerHost:

| File | Contents |
|---|---|
| `ipv4-settings-before.txt` | method, address, gateway, DNS, never-default |
| `nmcli-con-show-before.txt` | full `nmcli con show` output |
| `nmcli-con-show-all-before.txt` | `nmcli -f all con show` output |
| `ip-addr-before.txt` | `ip -4 -br addr show ens18` |
| `routes-before.txt` | full routing table |

The original values, verbatim from `ipv4-settings-before.txt`:

```
manual
192.168.0.251/24
192.168.0.1
192.168.0.1,1.1.1.1
no
```

## Identity

| | |
|---|---|
| Host | `DockerHost` (Proxmox VM 102, node `servernotangordo`) |
| Interface | `ens18`, MAC `BC:24:11:FF:EE:4D` |
| NetworkManager profile | `Wired connection 1` (UUID `3994cb5d-e790-3e43-a5df-a1c5aa28331e`) |
| LAN address | `192.168.0.225` (DHCP) — was `192.168.0.251` (static) |
| ZeroTier address | `172.25.10.159` — **unchanged, stable** |

## See also

- `ddns-and-exposure.md` — DDNS on DockerHost and what is actually reachable
- `dockerhost-nfs.md` — NFS mounts on this host (over ZeroTier, unaffected)
- `../proxmox-cluster.md` — VM inventory and addresses
