# SSH over ZeroTier — MTU fix

## Symptoms

`ssh` to a ZeroTier host (e.g. `pcgordo-wsl`) hangs during the key exchange:

```
debug1: expecting SSH2_MSG_KEX_ECDH_REPLY   ← stalls here, never returns
```

`ping` to the host shows wildly varying latency (e.g. `0.4ms → 154ms`), which is
the classic signature of **IP fragmentation / packet loss** on large packets.

## Root cause

ZeroTier creates its virtual interface (`zt*`, here `ztyxa6ggpt`) with an MTU of
**2800** by default. The physical links underneath (Ethernet/Wi-Fi, MTU 1500) and
WSL's PMTU cannot carry that. The SSH key-exchange (KEX) messages are large; when
sent over an MTU bigger than what the path can fragment correctly, those packets
get dropped and the handshake never completes.

This only affects big packets — small ICMP and short payloads still pass, which
is why the host pings fine but SSH stalls.

## Diagnosis

`ip route get <host>` shows the ZeroTier link is the path. Check the MTU mismatch:

```sh
ip link show ztyxa6ggpt            # mtu 2800  ← too big
ip link show enp9s0f3u1u4          # mtu 1500  ← physical link
```

## Permanent fix

Force ZeroTier interfaces down to `1500`, both when a `zt*` interface is added
**and** periodically afterward, so it survives reboots and the late MTU re-zero.

**Why the udev-only rule is not enough:** the naive `ATTR{mtu}="1500"` udev rule
runs on the `add` event, but `zerotier-one` sets the MTU back to `2800` *after* the
tap device is created. The one-shot retry loop catches ZeroTier's initial bring-up,
but `zerotier-one` re-applies `2800` once its network comes up **after** the ~10s
udev window closes — so on the next reboot SSH stalls again. The lasting fix adds a
**systemd timer** (`zt-mtu-fix.timer`) that re-caps every 60s, self-healing the
interface whenever ZeroTier drifts it back to `2800`.

**File:** `/etc/udev/rules.d/90-zerotier-mtu.rules`
```
SUBSYSTEM=="net", ACTION=="add", KERNEL=="zt*", ATTR{mtu}="1500", RUN+="/usr/local/bin/zt-mtu-fix %k"
```

**Script:** `/usr/local/bin/zt-mtu-fix`
- With an interface argument (udev `RUN+=`), caps that one interface with a short retry loop.
- With no arguments (called by the timer), caps **all** `zt*` interfaces.

```sh
#!/usr/bin/env bash
set -u
fix_iface() {
  local iface="$1"
  [ -e "/sys/class/net/$iface" ] || return 0
  for _ in $(seq 1 8); do
    ip link set dev "$iface" mtu 1500 2>/dev/null || true
    if [ "$(cat "/sys/class/net/$iface/mtu" 2>/dev/null)" = "1500" ]; then
      echo "$(date '+%F %T') $iface: mtu capped to 1500" >> /var/log/zt-mtu-fix.log
      return 0
    fi
    sleep 0.25
  done
  return 0
}
if [ $# -gt 0 ]; then
  fix_iface "$1"
else
  for d in /sys/class/net/zt*; do
    [ -e "$d/mtu" ] && fix_iface "${d##*/}"
  done
fi
```

**Systemd units:** `/etc/systemd/system/zt-mtu-fix.timer` + `zt-mtu-fix.service`
- `.service`: `Type=oneshot`, `ExecStart=/usr/local/bin/zt-mtu-fix`, run after `network.target` and `zerotier-one.service`.
- `.timer`: `OnBootSec=10s`, `OnUnitActiveSec=60s`, `Persistent=true`, `WantedBy=timers.target`. This re-caps every 60s so any late `2800` re-zero self-heals.

Apply to the running interface and install:

```sh
sudo ip link set ztyxa6ggpt mtu 1500     # fix the live interface now
sudo tee /etc/udev/rules.d/90-zerotier-mtu.rules >/dev/null <<'EOF'
SUBSYSTEM=="net", ACTION=="add", KERNEL=="zt*", ATTR{mtu}="1500", RUN+="/usr/local/bin/zt-mtu-fix %k"
EOF
sudo tee /usr/local/bin/zt-mtu-fix >/dev/null <<'EOF'
#!/usr/bin/env bash
set -u
fix_iface() {
  local iface="$1"
  [ -e "/sys/class/net/$iface" ] || return 0
  for _ in $(seq 1 8); do
    ip link set dev "$iface" mtu 1500 2>/dev/null || true
    if [ "$(cat "/sys/class/net/$iface/mtu" 2>/dev/null)" = "1500" ]; then
      echo "$(date '+%F %T') $iface: mtu capped to 1500" >> /var/log/zt-mtu-fix.log
      return 0
    fi
    sleep 0.25
  done
  return 0
}
if [ $# -gt 0 ]; then
  fix_iface "$1"
else
  for d in /sys/class/net/zt*; do
    [ -e "$d/mtu" ] && fix_iface "${d##*/}"
  done
fi
EOF
sudo chmod +x /usr/local/bin/zt-mtu-fix
sudo tee /etc/systemd/system/zt-mtu-fix.service >/dev/null <<'EOF'
[Unit]
Description=Cap ZeroTier zt* MTU to 1500
After=network.target zerotier-one.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/zt-mtu-fix
EOF
sudo tee /etc/systemd/system/zt-mtu-fix.timer >/dev/null <<'EOF'
[Unit]
Description=Periodically re-cap ZeroTier zt* MTU to 1500

[Timer]
OnBootSec=10s
OnUnitActiveSec=60s
Persistent=true

[Install]
WantedBy=timers.target
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now zt-mtu-fix.timer
sudo udevadm control --reload
# verify live (bump MTU back up, re-fire the add event, expect 1500):
sudo ip link set ztyxa6ggpt mtu 2800
sudo udevadm trigger --action=add /sys/class/net/ztyxa6ggpt
sleep 3
ip link show ztyxa6ggpt        # should report "mtu 1500"
tail -1 /var/log/zt-mtu-fix.log
```

## Verify

```sh
ssh pcgordo-wsl hostname        # should connect and run the command
ip link show ztyxa6ggpt         # should report "mtu 1500"
```

## Files involved

| File | Purpose |
|------|---------|
| `/etc/udev/rules.d/90-zerotier-mtu.rules` | **Fast boot fix** — caps `zt*` MTU at 1500 on the `add` event |
| `/usr/local/bin/zt-mtu-fix` | retry-loop script run by udev (single iface) or the timer (all `zt*`) |
| `/etc/systemd/system/zt-mtu-fix.service` | oneshot service wrapping `zt-mtu-fix` |
| `/etc/systemd/system/zt-mtu-fix.timer` | **Self-heal** — re-caps MTU to 1500 every 60s, defeating ZeroTier's late re-zero |
| `/var/log/zt-mtu-fix.log` | execution log for the fix script |
| `~/.ssh/config` | `Host pcgordo-wsl` → `172.25.209.143` port `2222`, user `manu343726` |