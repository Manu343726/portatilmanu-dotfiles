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
ExecStart=/home/manu343726/.local/bin/pangolin up --attach
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
ExecStart=/home/manu343726/.local/bin/pangolin up --attach
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
pangolin status                                              # Connected, site homelab
getent hosts passwords.manu343726.com                        # 100.96.128.8
curl -sk -o /dev/null -w '%{http_code}\n' https://passwords.manu343726.com/alive   # 200
```

Logs: `journalctl -u pangolin-client -f` (or `~/.config/pangolin/logs/client.log`).
