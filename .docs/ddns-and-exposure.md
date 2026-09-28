# Dynamic DNS and external exposure — DockerHost / new home

How `manu343726.ddns.net` is kept up to date, and what is actually reachable from
the internet as of 2026-09-28.

## TL;DR

- **DDNS works.** ddclient updates the record every 5 minutes; verified against
  No-IP's authoritative nameservers.
- **The name points at `47.58.172.246`** (Vodafone España, AS12430).
- **Port forwarding is gated by the router** and only works for DHCP-lease
  devices — see `dhcp-migration.md`.
- **ZeroTier is the reliable path** to everything. No router involvement, no
  exposure, already proven between both homes.

## Addresses

| | |
|---|---|
| Public IPv4 | `47.58.172.246` (Vodafone España, dynamic) |
| Old home (`servergordo`) public IPv4 | `79.117.18.238` |
| DockerHost LAN | `192.168.0.225` (DHCP — was `192.168.0.251` static) |
| DockerHost ZeroTier | `172.25.10.159` |
| Router | `192.168.0.1` (Sercomm, ports 80/443/8443) |

## The DDNS setup

**Service:** `manu343726.ddns.net` on No-IP free DDNS.

**Client:** `ddclient` (Arch package), running as the packaged system service.
It polls every **5 minutes** as a daemon — there is no timer involved.

```
service:  ddclient.service
config:   /etc/ddclient/ddclient.conf   (0640 root:ddclient)
```

**Behaviour, verified:**

```
SUCCESS: [dyndns2][all.ddnskey.com]> IPv4 address set to 47.58.172.246
```

`nochg` on every cycle afterwards is the healthy steady state, not an error.

**TTL is 1 second** on No-IP's records, so updates propagate almost immediately.

### Files

| Path (on DockerHost) | Mode | Role |
|---|---|---|
| `~/.config/ddns/ddns.conf` | 600 | **holds the credentials — edit this** |
| `~/.config/ddns/setup-system.sh` | 700 | installs `/etc` copy + enables the service |

The credential lives in `~/.config` and is copied to `/etc` root-only. To change
it, edit the file in `~/.config` then re-run the installer — editing `/etc`
directly works but gets overwritten on the next install.

### Applying changes

```sh
nano ~/.config/ddns/ddns.conf      # set login= and password=
sudo ~/.config/ddns/setup-system.sh --force
```

The installer is idempotent and refuses to run if `login=` or `password=` is
empty, so a half-filled config cannot be installed.

**Note:** run it with `sudo` from your own account. The script resolves the
invoking user's home via `SUDO_USER`, not `$HOME` — under sudo `$HOME` is
`/root`, which is an easy trap (an earlier version had this bug).

### Credentials

A **DDNS Key** is used, not the No-IP account password. A key can only update its
own hostnames; the account password can change DNS, mail and everything else.

- `host=all.ddnskey.com` — the sentinel No-IP expands to every hostname in the
  key's group. It is not a real DNS name.
- A DDNS Key password **cannot be recovered** if lost; generate a new one.
- Create one at <https://my.noip.com/dns/records> → *Enable Dynamic DNS*.

### ddclient 4.0.0 syntax gotchas

Worth remembering, because the old syntax still appears in most guides:

- `use=web` / `web=` are **deprecated** → use `usev4=webv4` / `webv4=`
- `include=` is **not supported** (this is why the secret lives in `~/.config`
  and is copied, not included)
- The config format is `directive=value`, **not** shell `KEY=value`, so a real
  `.env` would be silently ignored — hence `ddns.conf`, not `.env`
- Default public-IP discovery page is `ipify-ipv4`

### Verifying an update without waiting 5 minutes

ddclient throttles retries to once per 5 minutes. To force one, run it with a
throwaway cache:

```sh
ddclient --file=~/.config/ddns/ddns.conf --cache=/tmp/dd.cache --debug
```

The `--cache` path bypasses the throttle without touching the real cache.

**Check DNS against the authoritative servers**, not a local resolver — a
recursive resolver can serve a stale answer and make a correct setup look broken:

```sh
dig @nf1.no-ip.com manu343726.ddns.net A +short
```

## What is reachable

### Via ZeroTier — everything, already working

| Service | Address (from any ZeroTier member) |
|---|---|
| Organizr | `http://172.25.10.159:81` |
| Traefik (HTTPS) | `https://172.25.10.159:444` |
| Jellyfin | `http://172.25.10.159:8096` |
| Gitea | `http://172.25.10.159:3001` |
| ROMM | `http://172.25.10.159:8080` |
| SSH | `ssh DockerHost` → `172.25.10.159` |

### Via the public IP — gated

The Vodafone router only accepts port-forward rules for devices in its **DHCP
lease table**. See `dhcp-migration.md` for the full investigation.

As of 2026-09-28:

- Port **443 is open inbound**, but the connection is answered by the router's
  **own admin interface** — a Sercomm self-signed cert, `CN=192.168.0.1`,
  issued 2019. It is not passing through to Traefik.
- Ports 80, 444, 81, 8082 are all closed/filtered from outside.

**Outstanding:** set the forward's *internal* port to **444**, not 443. Traefik
publishes `444 → 443` inside the guest; nothing listens on 443 there. Once that
is right the chain is router → `192.168.0.225:444` → Traefik.

## Is this CGNAT?

**Probably not, and it no longer matters — but the check was never completed.**

Vodafone's own guidance is to compare the router's **WAN IP** against the public
IP: identical means no CGNAT, different means CGNAT. That number was never read.

What *was* established:

- All ports were closed/filtered from a genuinely external host
  (`servergordo`, a different site with a different public IP)
- After the DHCP change, port 443 became reachable — **which means the router can
  forward, so it is not a blanket carrier-NAT block**
- The public IP is a real Vodafone address, not `100.64.0.0/10`

The open 443 plus router-owned cert is consistent with either "forward exists but
targets the wrong internal port" or "router terminates it itself". Reading the
WAN IP settles it in one glance.

Worth knowing either way, because **IPv6 would make this moot**: neither home
has a global IPv6 address today.

## Exposure notes

The public-facing service is Traefik. Its routing rules:

| Container | Traefik rule |
|---|---|
| organizr | `Host(manu343726.ddns.net) && PathPrefix(/)` |
| jellyfin | `Host(manu343726.ddns.net) && PathPrefix(/jellyfin)` |
| romm | `Host(romm.manu343726.xyz)` |
| gitea | *(disabled — has its own domain)* |

Traefik matches longest prefix first, so `/jellyfin` wins for that path and
Organizr handles the rest. Both are reachable publicly once the forward works.

**Everything else is published on `0.0.0.0`**, so it is reachable from every
device on the LAN regardless of the proxy: Organizr `:81`, Traefik `:444` and
`:8082`, Gitea `:2222`/`:3001`, Jellyfin `:8096`, ROMM `:8080`.

Chosen deliberately: Traefik on `444` rather than `443` to filter uninteresting
internet scanners. Note this does nothing against a LAN-side attacker — rebind
those container ports to `127.0.0.1` if that is the actual concern.

## Verification commands

```sh
# external reachability, from a host outside your network
ssh ServerGordo 'for p in 443 444 80; do timeout 5 bash -c "echo > /dev/tcp/47.58.172.246/$p" 2>/dev/null && echo "$p open" || echo "$p closed"; done'

# what is listening inside the guest
ssh DockerHost 'ss -ltn | grep -E "444|81|8082"'

# DDNS health
ssh DockerHost 'systemctl is-active ddclient.service; journalctl -u ddclient.service --no-pager --lines=5'

# authoritative DNS (never trust a local resolver here)
dig @nf1.no-ip.com manu343726.ddns.net A +short
```

## See also

- `dhcp-migration.md` — static → DHCP change and rollback
- `dockerhost-nfs.md` — NFS mounts on DockerHost
- `../proxmox-cluster.md` — VM inventory, addresses, ZeroTier ring
