# Display / Multi-monitor

ASUS ROG Flow X13 with AMD iGPU + NVIDIA dGPU (supergfxctl `AsusMuxDgpu` mode).

Internal display: `eDP` (1920x1200 @ 120Hz)
External display: `DisplayPort-1-1` (2560x1440 @ 144Hz)

## Scripts

| File | Purpose |
|------|---------|
| `~/.local/bin/auto-external` | Called from i3 config (`exec --no-startup-id auto-external`). On boot, detects if an external monitor is connected and switches to external-only. |
| `~/.local/bin/screen-layout` | Called from i3 binding (`$mod+Ctrl+s`). Rofi menu: Laptop only / External only / Both extended / Both mirrored. |
| `~/.local/bin/autorandr-trigger` | Called by udev on DRM change events. Sleeps 1s, detects DisplayPort-1-1 state, runs xrandr. |

## Hotplug detection

A udev rule triggers `autorandr-trigger` when a monitor is plugged/unplugged.

**File:** `/etc/udev/rules.d/99-monitor-hotplug.rules`
```
ACTION=="change", SUBSYSTEM=="drm", RUN+="/home/manu343726/.local/bin/autorandr-trigger"
```

To install after a fresh clone:
```bash
echo 'ACTION=="change", SUBSYSTEM=="drm", RUN+="/home/manu343726/.local/bin/autorandr-trigger"' | sudo tee /etc/udev/rules.d/99-monitor-hotplug.rules
sudo udevadm control --reload-rules
```

## autorandr profiles

`autorandr` saves display configurations as profiles:

```
~/.config/autorandr/
├── external-docked/
│   ├── config     # DisplayPort-1-1 primary, DP-0 off
│   └── setup      # EDID fingerprints
└── laptop-only/
    ├── config     # DP-0 primary, everything off
    └── setup      # EDID fingerprints
```

Re-save after fresh clone:
```bash
# When docked with external only:
autorandr --save external-docked --force
# When undocked (laptop only):
autorandr --save laptop-only --force
```
