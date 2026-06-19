# i3status

## Config

`~/.config/i3status/config` (overrides `/etc/i3status.conf`)

## Order

1. IPv6
2. Wireless (SSID, quality, IP)
3. Ethernet (IP, speed)
4. Battery (status, percentage, remaining)
5. Disk `/` (available)
6. Load (1 min)
7. Memory (used / available)
8. Clock (YYYY-MM-DD HH:MM:SS)

## Monokai status colors

| State | Color | Meaning |
|-------|-------|---------|
| Good | `#A6E22E` | Battery discharging normally, disk OK |
| Degraded | `#FD971F` | Battery low, memory warning |
| Bad | `#F92672` | Battery critical |
| Separator | `#75715E` | Between status blocks |

Interval: 5 seconds.
