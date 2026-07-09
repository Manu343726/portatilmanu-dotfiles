# Display / Multi-monitor

ASUS ROG Flow X13 with AMD iGPU + NVIDIA dGPU (supergfxctl `AsusMuxDgpu` mode).

Internal display: `eDP` (Integrated mode) or `DP-0` (Mux/DGPU mode) — 1920x1200 @ 120Hz
External display: `DisplayPort-1-1` — 2560x1440 @ 144Hz

## Display plugin (dotfilesd)

All display management is handled by the **display plugin** (`~/.config/dotfilesd/plugins/display/`),
a Connect RPC service auto-exposed via `dotfilesctl`:

```sh
dotfilesctl display get-outputs              # list displays
dotfilesctl display set-layout --layout=EXTERNAL_ONLY
dotfilesctl display auto-external            # boot-time auto-detect
dotfilesctl display autorandr-trigger        # udev hotplug handler
```

The plugin dynamically detects which display is internal (eDP vs DP-0 depending on
GPU mode), so it works correctly in both Integrated and AsusMuxDgpu modes.

## Scripts (thin wrappers)

These scripts now delegate to `dotfilesctl display`:

| File | Purpose |
|------|---------|
| `~/.local/bin/auto-external` | Called from i3 config (`exec --no-startup-id auto-external`). Waits for daemon, then calls `dotfilesctl display auto-external`. |
| `~/.local/bin/screen-layout` | Called from i3 binding (`$mod+Ctrl+s`). Rofi menu: Laptop only / External only / Both extended / Both mirrored. Calls `dotfilesctl display set-layout`. |
| `~/.local/bin/autorandr-trigger` | Called by udev on DRM change events. Probes display state, calls `dotfilesctl display autorandr-trigger`. |

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

## DisplayLayout enum values

| CLI value | Description |
|-----------|-------------|
| `LAPTOP_ONLY` | Internal laptop panel only, external disabled |
| `EXTERNAL_ONLY` | External display only, internal panel disabled |
| `EXTENDED` | Both displays, external to the right of internal |
| `MIRROR` | Both displays, mirrored (same-as clone) |
