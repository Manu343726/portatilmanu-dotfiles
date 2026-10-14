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
  forever (seen 2026-10-14, from 04:10 onwards). Fixed by making the LAN win whenever Ethernet is
  actually on the home LAN — NetworkManager dispatcher
  `/etc/NetworkManager/dispatcher.d/50-homelab-lan-route` (recreate with `exec_run(sudo=true)`):
  ```sh
  ip -4 addr show dev "$IFACE" | grep -q 'inet 192[.]168[.]0[.]' || exit 0
  ip route replace 192.168.0.30/32 dev "$IFACE" metric 50
  ```
  The tunnel's own `metric 9999` route stays in the table; the LAN's `metric 50` just wins. This is
  also what restores the split-brain (`getent hosts test.manu343726.com` → `192.168.0.30`) and
  direct LAN access to the VM's services (`192.168.0.30:3004` Forgejo, `:4000` LiteLLM).
- **KNOWN OPEN ISSUE (server side, not this laptop):** even with the route above, the `homelab`
  site still connects as `Relay`/`Disconnected` and private resources (Vaultwarden) stay
  unreachable, because the Pangolin server records the site's endpoint as **`172.24.0.1:<port>`** —
  the Docker bridge gateway — instead of `192.168.0.30`. The site's `localEndpoints` are correct
  (`["192.168.0.30:56926","172.25.73.178:56926"]`) but the server hands clients the bogus
  `endpoint`, so hole punching always fails (`Rapid test: site 1 holepunch FAILED after 5 attempts,
  will relay`). Diagnosed and handed to the homelab VM's agent on 2026-10-14; the write-up lives in
  the `docker-compose-services` repo. Don't chase this from the laptop.
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
pangolin status                                              # Connected, site homelab
getent hosts test.manu343726.com                             # 192.168.0.30 (Pi-hole split-brain)
getent hosts passwords.manu343726.com                        # 100.96.128.9 (private, via tunnel)
ip route get 192.168.0.30                                    # …dev enp9s0f4u1u4 (LAN), NOT dev pangolin
ip route show 192.168.0.30                                   # metric-50 device route + the tunnel's 9999
curl -sk -o /dev/null -w '%{http_code}\n' https://passwords.manu343726.com/alive   # 200
```

If `ip route get 192.168.0.30` says `dev pangolin` instead, the dispatcher script is missing or the
interface is not on `192.168.0.0/24`; re-check
`/etc/NetworkManager/dispatcher.d/50-homelab-lan-route` and run it by hand with the interface name.
The Vaultwarden check above also depends on the server-side issue noted in the Gotchas, so it can
still time out even when the client itself is healthy.

Logs: `journalctl -u pangolin-client -f` (or `~/.config/pangolin/logs/client.log`).
