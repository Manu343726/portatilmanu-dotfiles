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

Force ZeroTier interfaces down to `1500` whenever a `zt*` interface is added, so
it survives reboots and interface re-creation.

**Why the naive rule is not enough:** a bare `ATTR{mtu}="1500"` udev rule *does*
run on the `add` event, but `zerotier-one` re-sets the MTU to `2800` right after
creating the tap device, racing past udev and winning. Result: the interface
comes back at `2800` after reboot even with the rule installed. The fix retries
for ~10s from the `add` event, so it catches and re-caps the MTU after ZeroTier
finishes its own bring-up.

**File:** `/etc/udev/rules.d/90-zerotier-mtu.rules`
```
SUBSYSTEM=="net", ACTION=="add", KERNEL=="zt*", ATTR{mtu}="1500", RUN+="/usr/local/bin/zt-mtu-fix %k"
```

**Script:** `/usr/local/bin/zt-mtu-fix`
```sh
#!/usr/bin/env bash
set -u
iface="$1"
[ -e "/sys/class/net/$iface" ] || exit 0
for _ in $(seq 1 40); do
  ip link set dev "$iface" mtu 1500 2>/dev/null || true
  if [ "$(cat "/sys/class/net/$iface/mtu" 2>/dev/null)" = "1500" ]; then
    echo "$(date '+%F %T') $iface: mtu capped to 1500" >> /var/log/zt-mtu-fix.log
    exit 0
  fi
  sleep 0.25
done
echo "$(date '+%F %T') $iface: FAILED to cap mtu" >> /var/log/zt-mtu-fix.log
exit 1
```

Apply to the running interface and install:

```sh
sudo ip link set ztyxa6ggpt mtu 1500     # fix the live interface now
sudo tee /etc/udev/rules.d/90-zerotier-mtu.rules >/dev/null <<'EOF'
SUBSYSTEM=="net", ACTION=="add", KERNEL=="zt*", ATTR{mtu}="1500", RUN+="/usr/local/bin/zt-mtu-fix %k"
EOF
sudo tee /usr/local/bin/zt-mtu-fix >/dev/null <<'EOF'
#!/usr/bin/env bash
set -u
iface="$1"
[ -e "/sys/class/net/$iface" ] || exit 0
for _ in $(seq 1 40); do
  ip link set dev "$iface" mtu 1500 2>/dev/null || true
  [ "$(cat "/sys/class/net/$iface/mtu" 2>/dev/null)" = "1500" ] && exit 0
  sleep 0.25
done
exit 1
EOF
sudo chmod +x /usr/local/bin/zt-mtu-fix
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
| `/etc/udev/rules.d/90-zerotier-mtu.rules` | **Permanent fix** — caps `zt*` MTU at 1500 via `ATTR{mtu}` + `RUN+=` |
| `/usr/local/bin/zt-mtu-fix` | retry-loop script run by udev; beats the ZeroTier MTU race |
| `/var/log/zt-mtu-fix.log` | execution log for the fix script |
| `~/.ssh/config` | `Host pcgordo-wsl` → `172.25.209.143` port `2222`, user `manu343726` |