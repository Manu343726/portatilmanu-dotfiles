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

Force ZeroTier interfaces down to `1500` via a udev rule that fires whenever a
`zt*` interface is added, so it survives reboots and interface re-creation.

**File:** `/etc/udev/rules.d/90-zerotier-mtu.rules`
```
SUBSYSTEM=="net", ACTION=="add", KERNEL=="zt*", ATTR{mtu}="1500"
```

Apply to the running interface and install the rule:

```sh
sudo ip link set ztyxa6ggpt mtu 1500     # fix the live interface now
sudo tee /etc/udev/rules.d/90-zerotier-mtu.rules >/dev/null <<'EOF'
SUBSYSTEM=="net", ACTION=="add", KERNEL=="zt*", ATTR{mtu}="1500"
EOF
sudo udevadm control --reload
sudo udevadm test --action=add /sys/class/net/ztyxa6ggpt   # verify mtu 1500
```

## Verify

```sh
ssh pcgordo-wsl hostname        # should connect and run the command
ip link show ztyxa6ggpt         # should report "mtu 1500"
```

## Files involved

| File | Purpose |
|------|---------|
| `/etc/udev/rules.d/90-zerotier-mtu.rules` | **Permanent fix** — caps `zt*` MTU at 1500 |
| `~/.ssh/config` | `Host pcgordo-wsl` → `172.25.209.143` port `2222`, user `manu343726` |