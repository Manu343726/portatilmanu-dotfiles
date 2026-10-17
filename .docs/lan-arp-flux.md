# Ethernet sin IPv4 — ARP flux + ACD self-conflict

## Symptoms

With the USB Ethernet adapter (`enp9s0f4u1u4`) plugged in, the link comes up
(`nmcli` says `connected`) but has **no IPv4 address at all**:

```
enp9s0f4u1u4     UP              fe80::c158:b7de:4ed7:47d2/64     ← only IPv6 link-local
```

Intermittent: sometimes it gets `192.168.0.100`, sometimes nothing, and there is
no error anywhere — activation "succeeds" because `ipv4.may-fail=yes`.

## Root cause

Not the cable, the adapter or the DHCP server: the router **does** deliver the
lease. NetworkManager then **withdraws** it because its IPv4 **ACD** (Address
Conflict Detection, `ipv4.dad-timeout`, enabled by default) thinks the address
is already in use:

```
dhcp4 (enp9s0f4u1u4): state changed new lease, address=192.168.0.100, acd pending
device (enp9s0f4u1u4): conflict detected for IP address 192.168.0.100 with host E8:FB:1C:14:E4:21
```

That MAC is **the laptop's own WiFi** (`wlp6s0`) — there is no rogue device.

Both NICs sit on the same L2 segment (`192.168.0.0/24`) and Linux runs with the
default *weak host* ARP model, `arp_ignore=0` / `arp_announce=0`:

- `arp_ignore=0` → the WiFi **answers ARP for any local address**, including the
  wired one.
- `arp_announce=0` → a NIC can emit ARP whose sender IP is **an address of the
  other interface**.

Captured live during an activation (this is the actual failure mechanism):

```
WIFI  e8:fb:1c:14:e4:21 > 8c:c1:21:af:ff:f0  Reply 192.168.0.100 is-at e8:fb:1c:14:e4:21   # WiFi answering for the wired IP
WIRED 5c:85:7e:3a:f3:4d > ff:ff:ff:ff:ff:ff  Request who-has 116.203.91.91 tell 192.168.0.87 # wired announcing with the WiFi IP
```

Sequence:

1. DHCP hands `.100` to the wired NIC; NM enters the `acd pending` phase and
   probes the LAN ("who-has 192.168.0.100 tell 0.0.0.0").
2. The address is already in the kernel, so the **WiFi answers the probe with
   its own MAC** (same broadcast domain → the reply reaches the wired NIC).
3. NM sees "another host owns `.100`" → conflict → removes the address.
4. The connection stays up with IPv6 link-local only. No user-visible error.

It's a **race**: if the probe window closes before the cross-reply lands, the
address is kept. Hence "sometimes it works". Lease renewals trigger the same
thing, which is why the conflicts appear at random times (16 Oct 17:28, 17 Oct
05:29, 16:43, 21:07).

## Fix

Disable the weak-host ARP model in `/etc/sysctl.d/99-arp-flux.conf`:

```ini
net.ipv4.conf.all.arp_ignore = 1
net.ipv4.conf.all.arp_announce = 2
net.ipv4.conf.default.arp_ignore = 1
net.ipv4.conf.default.arp_announce = 2
```

Meaning:

- `arp_ignore=1` — reply to ARP only when the target IP is configured on the
  **interface the request arrived on**. The WiFi stops answering for `.100`.
- `arp_announce=2` — always pick the source IP from the **outgoing** interface,
  never another NIC's address. Kills the `tell 192.168.0.87` frames on the wire.

Apply and verify:

```sh
sudo sysctl --system
sysctl net.ipv4.conf.all.arp_ignore net.ipv4.conf.all.arp_announce   # 1 / 2
nmcli connection up "Wired connection 1"
nmcli -f IP4.ADDRESS,IP4.GATEWAY dev show enp9s0f4u1u4               # 192.168.0.100/24 + gw
journalctl -b -u NetworkManager | grep "conflict detected"           # must be silent
```

`all` is enough for interfaces already present (the kernel takes
`max(all, iface)`) and `default` covers interfaces created later — important
here because `enp9s0f4u1u4` is a **USB** adapter that may not exist at boot, so
per-interface lines in the sysctl file would just error out.

### Recreate after reinstall

```sh
sudo tee /etc/sysctl.d/99-arp-flux.conf >/dev/null <<'EOF'
# Two NICs of the same host on the same L2 segment: avoid ARP flux, which makes
# NetworkManager's IPv4 ACD drop the wired DHCP address. See ~/.docs/lan-arp-flux.md
net.ipv4.conf.all.arp_ignore = 1
net.ipv4.conf.all.arp_announce = 2
net.ipv4.conf.default.arp_ignore = 1
net.ipv4.conf.default.arp_announce = 2
EOF
sudo sysctl --system
```

## Why not just disable ACD?

`nmcli con modify "Wired connection 1" ipv4.dad-timeout 0` also stops the
conflict, and it is a valid fallback. It was not chosen because it leaves the
ARP flux in place: the WiFi keeps answering ARP for the wired address, so LAN
peers send that traffic to the wrong MAC (works only thanks to the kernel's
loose host model), and the same class of confusion can reach other multi-homed
setups on this machine.

## Notes

- `192.168.0.30/32 dev <iface> metric 50` (the Pangolin/homelab route from
  `.docs/pangolin-client.md`) coexists fine with this; it is a /32 device route,
  so ARP for Pi-hole is resolved on whichever interface carries it.
- With both NICs up there are two default routes (Ethernet metric 100, WiFi
  600) — Ethernet wins, WiFi stays as fallback. Expected.
- `rp_filter` is `2` (loose) on both NICs, set elsewhere; unrelated to this bug
  but it's why flux still "worked" partially instead of dropping traffic.
