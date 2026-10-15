---
name: homelab
description: The homelab estate — VM 108, the single-node Proxmox cluster, and every stack on it (Pangolin, Authentik, Traefik, Pi-hole, monitoring, backups, the Synology NAS). Use whenever the request mentions the homelab, a homelab service or stack, `docker-compose-services`, Homelable, Wiki.js, VM 108, the Proxmox host, the Synology NAS/NFS shares, or a change to the estate's configuration or documentation.
---

# The homelab estate

The estate lives on **VM 108** (`Homelab`) inside a Proxmox cluster, and it is the *other*
machine from this laptop: `/home/manu343726/AGENTS.md` **on the laptop** is `portatilmanu`'s
dotfiles contract, and `/home/manu343726/AGENTS.md` **on the VM** is the estate's. They are not
the same file, and the estate's is the one that governs homelab work.

## Read these two first, every time

```sh
ssh Homelab 'cat ~/AGENTS.md'   # the operating contract for the estate's repository
ssh Homelab 'cat ~/README.md'   # what the repository is, and where its documentation lives
```

Both are short, both are load-bearing, and both are the estate's own words — they are the
source, not this summary. If the VM is unreachable, the same files are in the estate's git:
[`AGENTS.md`](https://git.manu343726.com/homelab/Homelab/src/branch/main/AGENTS.md) and
[`README.md`](https://git.manu343726.com/homelab/Homelab/src/branch/main/README.md).

## The documentation is Homelable, not the repository

- **Homelable** (`https://topology.manu343726.com`) is the estate's written record and the place
  to read *before* proposing anything: [*Rules & plans → Rules*](https://topology.manu343726.com)
  is the acceptance test for any change, *Architecture* is the specification, and *Changelog*
  holds one row and one document per event. **A change that is not in the Changelog did not
  happen.**
- **Wiki.js** (`https://wiki.manu343726.com`) is only a viewer: `homelab/` there is a one-way
  mirror of Homelable, refreshed on a timer. Editing that subtree is pointless — the next run
  overwrites it.
- Read and write it as an agent through the **`homelable` MCP server** (`search_documentation`,
  `list_documentation`, `read_document`, `update_document`, `create_document`). Before writing
  any document, load the estate's own documentation skill, which carries the link and lint
  contract: `ssh Homelab 'cat ~/.opencode/skills/homelable-docs/SKILL.md'`.
  - **The MCP bridge truncates tool arguments at 4096 bytes**, which is smaller than most
    document bodies. Long bodies go through the API on the VM instead —
    `PATCH http://127.0.0.1:3009/api/v1/documents/<id>` with an `X-MCP-Service-Key` header (the
    value of `MCP_SERVICE_KEY` in `docker-compose-services/monitoring/homelable/.env`) — or
    create a stub through MCP and fill it in with the API.
- The mirror (Homelable → the wiki's git repo) is `tools/mirror-homelable-docs.py` on the VM. It
  **lints before it commits**: `0 finding(s), 0 error(s)` is the pass condition.

## Where things are

| | |
|---|---|
| VM 108 | `ssh Homelab` — `192.168.0.30` on the real LAN at home, `172.25.73.178` over ZeroTier. Its `~` **is** the repository root: `~/docker-compose-services/<stack>/`, `~/tools/`, `~/AGENTS.md`. |
| Proxmox host | `ssh ServerNoTanGordo` — the single surviving cluster node (`casa`, single vote). From there, `ssh homelab` reaches the VM. |
| Docker on the VM | as the VM's user, `docker` is the **rootless** daemon and `docker -c rootful` is the rootful one; as `root`, plain `docker` is the rootful one. Only Pi-hole, the `monitoring-rootful-*` pair and anything needing raw L2 live on the rootful side. |
| Laptop-side docs | this repo's `.docs/`: `homelab-vm.md`, `proxmox-cluster.md`, `ddns-and-exposure.md`, `dhcp-migration.md`, `pangolin-client.md`, `nas-recovery.md`, `synology-nfs.md`, `forgejo-homelab.md`, `vaultwarden-mcp.md`. |

## Traps this estate has already paid for

- **ZeroTier is the way in from anywhere, and it must not be broken.** Every remote repair path
  goes through it; a broken ring can still be fixed from there. The NAS is a ZeroTier peer
  (`172.25.106.32`) — its NFS mounts are `hard`, so a stalled write blocks forever and a clean
  reboot hangs on the same `sync()` the NFS blocks.
- **The backup destination is a ZeroTier NFS mount.** Dumps go to `casa_nas_backups`, never to
  the host's `/var/lib/vz/dump/`.
- **A service that stops on a missing dependency must be stopped, not left running.** Docker
  *creates* a missing bind source on the root filesystem: a container bound to an absent NFS
  share keeps running and writes to `/`, silently, with a healthy-looking dashboard.
- **Never `sudo` from the agent on the laptop** — use the `dotfilesd` MCP `exec_run` with
  `sudo: true`, which asks for the password through the secure channel.
- **Do not stage other people's work.** Both the estate's repo and this laptop's carry other
  agents' in-flight changes; stage the files this change actually touched.

## Before calling a homelab change done

The estate's *Rules* → §5 *Definition of done* is the checklist (Pangolin + Authentik wiring,
metrics scraped and verified, logs in Loki, a Grafana dashboard that links to *Homelab /
Containers* and *Homelab / Storage* with its stack preselected, a Homelable node, the wiki
`homelab` section, a Changelog row, a bounded Docker log, and a backup with a restore
exercised). A service is not finished because it answers on its port.
