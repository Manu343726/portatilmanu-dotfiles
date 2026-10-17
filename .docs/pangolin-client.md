# Pangolin client on the portátil — the private-resource tunnel

`passwords.manu343726.com` (Vaultwarden) and every other **private** Pangolin resource are not
in public DNS: they resolve **only while this laptop has a Pangolin client connected**. The
client used to be started by hand (`sudo pangolin up`), so it vanished on every reboot and the
vault went dark ("no responde"). It now runs as the systemd unit `pangolin-client.service`,
`enabled` and `Restart=always`.

## It is a user device, not a machine client

It reuses the logged-in account in `~/.config/pangolin/accounts.json`, exactly like the phone
app — no `id`/`secret`. The CLI's own `pangolin service install client` is **not** what runs
here: it **requires** `--endpoint`/`--id`/`--secret` (machine credentials) and refuses to reuse
the user session. Those machine credentials also cannot be minted from here — the terraform API
key is scoped to resources/targets, so `GET /v1/org/manu343726/clients` answers `403` — and a
machine client would additionally need a manual access grant on the private Vaultwarden resource,
because machines cannot join roles. For a personal laptop the user-device client is the right
shape; the machine client is only for a *headless* connection that must work with nobody logged
in.

## The unit

`/etc/systemd/system/pangolin-client.service`:

```ini
[Unit]
Description=Pangolin client (user device) - private-resource tunnel
Documentation=https://docs.pangolin.net/manage/clients/platforms/cli
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=HOME=/home/manu343726
Environment=XDG_CONFIG_HOME=/home/manu343726/.config
# The client installs its own 192.168.0.30/32 through the tunnel and cannot reach its own
# WireGuard peer through it; put the LAN route in place first (see Gotchas).
ExecStartPre=-/etc/NetworkManager/dispatcher.d/50-homelab-lan-route
ExecStart=/home/manu343726/.local/bin/pangolin up --attach --prefer-local-routes --upstream-dns 192.168.0.30 --upstream-dns 1.1.1.1
Restart=always
RestartSec=5
User=root

[Install]
WantedBy=multi-user.target
```

Recreate it with `exec_run(sudo=true)` (never a bare `sudo` in the agent shell — see `AGENTS.md`
rule 3), then enable and start:

```sh
sudo tee /etc/systemd/system/pangolin-client.service >/dev/null <<'EOF'
[Unit]
Description=Pangolin client (user device) - private-resource tunnel
Documentation=https://docs.pangolin.net/manage/clients/platforms/cli
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=HOME=/home/manu343726
Environment=XDG_CONFIG_HOME=/home/manu343726/.config
# The client installs its own 192.168.0.30/32 through the tunnel and cannot reach its own
# WireGuard peer through it; put the LAN route in place first (see Gotchas).
ExecStartPre=-/etc/NetworkManager/dispatcher.d/50-homelab-lan-route
ExecStart=/home/manu343726/.local/bin/pangolin up --attach --prefer-local-routes --upstream-dns 192.168.0.30 --upstream-dns 1.1.1.1
Restart=always
RestartSec=5
User=root

[Install]
WantedBy=multi-user.target
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now pangolin-client
```

## Gotchas

- **The client overrides the system DNS** — it writes `nameserver 100.96.128.1` into
  `/etc/resolv.conf` and forwards everything that is not a Pangolin resource to its *own*
  upstream, so left alone it answers every estate FQDN with the public IP and **undoes the LAN
  split-brain**. The unit's `--upstream-dns 192.168.0.30 --upstream-dns 1.1.1.1` pins that
  upstream at Pi-hole, so `*.manu343726.com` resolves through Pi-hole to `192.168.0.30` and only
  the private resources (`passwords.manu343726.com` → `100.96.128.9`) come from the tunnel.
- **Without `--upstream-dns` the proxy inherits whatever the system resolver is at start, and
  that drift is the bug.** If it lands on the router (`192.168.0.1`) every estate FQDN answers the
  public IP (`47.58.172.246`) and the client cannot even reach its own API
  (`Get "https://pangolin.manu343726.com/api/v1": remote error: tls: handshake failure`), so
  `Restart=always` turns it into a crash loop — seen on 2026-10-13 (18:53–18:56, 30 restarts).
  With the flags present the log says `UpstreamDNS is statically configured ([192.168.0.30:53
  1.1.1.1:53])` and forwarding no longer follows the system resolver.
- **On the home LAN the client must not be allowed to route `192.168.0.30` through the tunnel.**
  Because that IP is pinned as `--upstream-dns`, the client installs
  `192.168.0.30/32 dev pangolin` (its own log: `Added route for remote subnet: 192.168.0.30/32`).
  That `/32` outranks the LAN's `/24`, so it hijacks **every** packet to the homelab VM into the
  tunnel — including the client's *own* WireGuard peer endpoint, which is `192.168.0.30:21820`
  (`write udp [::]:49601->192.168.0.30:21820: sendto: network is unreachable`) and Pi-hole itself.
  The tunnel can then never reach its own peer, so it never comes up, and DNS deadlocks:
  `Failed to get DNS response for topology.manu343726.com` → public IP → `tls: handshake failure`,
  forever (seen 2026-10-14, from 04:10 onwards). The proxy then falls back to its second upstream
  (`1.1.1.1`), so every estate name answers the *public* IP: the split-brain needs a query that
  arrives **from a LAN address**, and one that arrives through the tunnel is not one.
  Fixed by making the LAN win whenever some interface is on the home LAN: NetworkManager dispatcher
  `/etc/NetworkManager/dispatcher.d/50-homelab-lan-route` (recreate with `exec_run(sudo=true)`):
  ```sh
  #!/bin/sh
  # (the installed file carries a longer explanatory header here)
  set -u

  # The one LAN to care about. Anchored, and the dots are classes, for grep -E.
  LAN_RE='inet 192[.]168[.]0[.]'

  # The interface carrying the lowest-metric default route: the path the rest of the system is
  # already using, so the one to prefer when more than one interface is on the LAN.
  route_iface() {
      ip -4 route show default 2>/dev/null | awk '
          { m = 0; dev = ""
            for (i = 1; i <= NF; i++) { if ($i == "dev") dev = $(i + 1); if ($i == "metric") m = $(i + 1) }
            if (dev != "") print m, dev }' | sort -n | awk 'NR == 1 { print $2 }'
  }

  on_lan() {  # on_lan <iface>
      [ -n "$1" ] || return 1
      ip -4 addr show dev "$1" 2>/dev/null | grep -qE "$LAN_RE"
  }

  current_iface() {  # the device our metric-50 /32 currently points at, if any
      ip -4 route show 192.168.0.30/32 metric 50 2>/dev/null |
          awk '{ for (i = 1; i <= NF; i++) if ($i == "dev") { print $(i + 1); exit } }'
  }

  iface=""
  # Prefer a *wired* interface on the LAN when there is one. Ethernet and WiFi can sit on
  # 192.168.0.0/24 at the same time, and the dispatcher only fires on link events: when the
  # cable comes up while WiFi already owns the default route, route_iface() still answers
  # wlp6s0 and the /32 stays pinned to WiFi (seen 2026-10-17 23:13, reconnecting the dock's
  # Ethernet: the /32 kept pointing at wlp6s0 with the cable idle at 1 Gb/s).
  for d in /sys/class/net/*; do
      dev="$(basename "$d")"
      case "$dev" in
          en*|eth*) if on_lan "$dev"; then iface="$dev"; break; fi ;;
      esac
  done

  if [ -z "$iface" ]; then
      if on_lan "$(route_iface)"; then
          iface="$(route_iface)"
      else
          # The default route is not on the LAN (the tunnel or a VPN may own it). Any interface
          # that is on the LAN will do.
          for d in /sys/class/net/*; do
              dev="$(basename "$d")"
              if on_lan "$dev"; then iface="$dev"; break; fi
          done
      fi
  fi

  have="$(current_iface)"
  if [ -n "$iface" ]; then
      ip route replace 192.168.0.30/32 dev "$iface" metric 50
      [ "$have" = "$iface" ] || logger -t homelab-lan-route -- \
          "192.168.0.30/32 -> $iface (metric 50, LAN ahead of the tunnel)"
  else
      if [ -n "$have" ]; then
          ip route del 192.168.0.30/32 metric 50 2>/dev/null || true
          logger -t homelab-lan-route -- \
              "off the home LAN: dropped the metric-50 route to 192.168.0.30 (the tunnel's own /32 takes over)"
      fi
  fi
  exit 0
  ```
  The tunnel's own `metric 9999` route stays in the table; the LAN's `metric 50` just wins. This is
  also what restores the split-brain (`getent hosts test.manu343726.com` → `192.168.0.30`) and
  direct LAN access to the VM's services (`192.168.0.30:3004` Forgejo, `:4000` LiteLLM).
- **The script must recompute on *every* dispatcher event, not just on `up` of one interface.**
  A route is bound to a device, and the kernel drops it when that device goes down. On 2026-10-15
  the home WiFi came up *after* the dock's Ethernet, took the `/32` over (same prefix and metric, so
  `ip route replace` simply moved it), and then went down — taking the route with it. The Ethernet,
  up since boot, never fired another `up`, so nothing put it back: Pi-hole became unreachable, the
  tunnel deadlocked on `tls: handshake failure`, and the laptop resolved `homelab.manu343726.com` to
  the public address while sitting on the LAN (the user's "split brain DNS does not work when I go by
  Ethernet instead of WiFi"). It picks the lowest-metric default route's interface when that one is
  on the LAN, else any interface on the LAN, and deletes its route when none is — so a laptop away
  from home still reaches Pi-hole and the private resources through the tunnel. The unit's
  `ExecStartPre` calls it too (no arguments), so the route exists even when the client starts before
  any network event. Watch it with `journalctl -t homelab-lan-route`; it logs transitions only.
- **The system resolver *is* the client's DNS proxy, so a dead client means a dead laptop.** The
  client writes `/etc/NetworkManager/conf.d/olm-dns.conf` (`[global-dns-domain-*] servers=100.96.128.1`)
  and `/etc/resolv.conf` points only at its proxy. Stop, crash or restart the client and nothing
  resolves — and the client itself cannot start, because it resolves `pangolin.manu343726.com` before
  its proxy exists (`Error: the server appears to be down: server unreachable: Get
  "https://pangolin.manu343726.com/api/v1": dial tcp: lookup pangolin.manu343726.com on [::1]:53:
  connection refused`, seen when restarting it on 2026-10-15). Fixed by leaving NetworkManager a real
  resolver to fall back on: `nmcli con mod 'Wired connection 1' ipv4.ignore-auto-dns no` and the same
  on `NGC1701B`. The home DHCP *is* Pi-hole, so option 6 is `192.168.0.30` (`nmcli -f DHCP4.OPTION dev
  show <iface> | grep domain_name_servers`) and the client logs it as
  `Original DNS servers backed up: [192.168.0.30]`. The proxy still comes first while it runs
  (set by the olm config), which is what keeps private names resolving. Do **not** re-add a fixed
  `ipv4.dns` (a hard-coded Pi-hole would be stale on any other network; the DHCP already hands it
  out at home).

- **The server-side counterpart was FIXED on 2026-10-14 — do not re-diagnose it from the laptop.**
  Until then the site connected as `Relay`/`Disconnected` and private resources (Vaultwarden) were
  unreachable, because the Pangolin server recorded the site's endpoint as **`172.24.0.1:<port>`** —
  the Docker bridge gateway — instead of `192.168.0.30` (the site's `localEndpoints` were already
  correct, but clients were handed the bogus `endpoint`, so hole punching always failed). The cause
  was the **rootless** Docker on the homelab VM: `rootlesskit --port-driver=builtin` proxies
  published ports in user space and rewrites the source address of everything that arrives, so
  Gerbil saw every peer as one address (`No proxy mapping found for 172.24.0.1:*`), and `builtin`
  never propagates UDP source addresses at all — which is what WireGuard needs. Fixed with a drop-in
  on the VM, `~/.config/systemd/user/docker.service.d/20-source-ip-transparent.conf`:
  `DOCKERD_ROOTLESS_ROOTLESSKIT_PORT_DRIVER=slirp4netns` (that driver propagates the source IP for
  TCP *and* UDP), plus a restart of the daemon and of `pangolin-site-selfhosted.service` (the
  connector had come up with `WireGuard device is not initialized` and nothing listening on its UDP
  port). Result: `pangolin status` now reports `homelab 192.168.0.30:<port> Connected Local` and
  `https://passwords.manu343726.com/alive` answers 200. Full write-up in Homelable, document
  *Pangolin* (the repository keeps configuration, not documentation).
- **A drop-in that sets `ExecStart=` silently drops these flags.** `10-prefer-local-routes.conf`
  existed for a while and, because a drop-in `ExecStart=` *replaces* the unit's line, the
  effective command was `pangolin up --attach --prefer-local-routes` — no upstreams. It was
  removed so the unit file is the single source of truth; if you ever need a drop-in, repeat the
  **full** `ExecStart` (including `--upstream-dns`) in it.
- **`--attach` and `--silent` conflict** — the CLI refuses both (`Error: --silent and --attached
  options conflict`). systemd needs `--attach` so the process stays in the foreground for
  `Type=simple`; detached mode daemonises and systemd loses track of it.
- **`HOME`/`XDG_CONFIG_HOME` must point at the user's.** Under systemd there is no `SUDO_USER`,
  so a bare root run would look in `/root` and find no account. (The CLI already resolves the
  invoking user's home when it is invoked with `sudo`, but systemd starts it directly.)
- **It depends on the saved login.** `pangolin logout` (or an invalidated session) breaks it:
  run `pangolin login`, then `systemctl restart pangolin-client`.
- **Don't run `pangolin up` by hand while the service is up** — two clients fight over
  `/var/run/olm.sock` and the system DNS override. To use the interactive TUI:
  `sudo systemctl stop pangolin-client`.

## Verify

```sh
systemctl is-enabled --quiet pangolin-client && systemctl is-active pangolin-client
journalctl -u pangolin-client -b | grep 'UpstreamDNS is statically configured'   # 192.168.0.30:53 1.1.1.1:53
systemctl show -p ExecStart --value pangolin-client          # … --upstream-dns 192.168.0.30 --upstream-dns 1.1.1.1
pangolin status                                              # Connected, site homelab — CONNECTION: Local
getent hosts test.manu343726.com                             # 192.168.0.30 (Pi-hole split-brain)
getent hosts passwords.manu343726.com                        # 100.96.128.9 (private, via tunnel)
ip route get 192.168.0.30                                    # …dev enp9s0f4u1u4 (LAN), NOT dev pangolin
ip route show 192.168.0.30                                   # metric-50 device route + the tunnel's 9999
journalctl -t homelab-lan-route -n 3                         # last transition of that route
nmcli -f ipv4.ignore-auto-dns con show 'Wired connection 1'  # no — the client may die without killing DNS
curl -sk -o /dev/null -w '%{http_code}\n' https://passwords.manu343726.com/alive   # 200
```

If `ip route get 192.168.0.30` says `dev pangolin` instead, no interface is on `192.168.0.0/24` or the
dispatcher script is missing; re-check `/etc/NetworkManager/dispatcher.d/50-homelab-lan-route` and run
it by hand with **no arguments** (it recomputes for whatever interface is on the LAN). To prove the
dispatcher really fires, take the link down and up at the device level — `ip link set <iface> down`
makes the kernel drop the route, and bringing it back up must log a transition and reinstall it:
`journalctl -u NetworkManager-dispatcher.service -n 5` + `journalctl -t homelab-lan-route -n 3`.
The `CONNECTION` column should read `Local` while the laptop is on the home LAN (`Relay` means the
homelab VM's rootless Docker is mangling source addresses again — see the Gotchas).

Logs: `journalctl -u pangolin-client -f` (or `~/.config/pangolin/logs/client.log`).
