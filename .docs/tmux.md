# Tmux

Based on [Oh My Tmux!](https://github.com/gpakosz/.tmux) with a Monokai override.

## Files

| File | Purpose |
|------|---------|
| `~/.tmux.conf` | Main config (Oh My Tmux! release) |
| `~/.tmux.conf.local` | Symlink → `~/.config/tmux/tmux.conf.local` |
| `~/.config/tmux/tmux.conf.local` | Monokai overrides, custom functions |

## Key bindings

| Binding | Action |
|---------|--------|
| `C-b` / `C-a` | Prefix |
| `C-b` `r` | Reload config |
| `C-b` `e` | Edit local config |
| `C-b` `n` / `p` | Next / previous window |
| `C-b` `m` | Toggle mouse mode |
| `C-b` `c` | New window |
| `C-b` `,` | Rename window |
| `C-b` `$` | Rename session |
| `C-b` `w` | List windows |
| `C-b` `[` | Enter copy mode |
| `C-b` `]` | Paste tmux buffer |

## Status bar

The right side shows (left to right):

```
PERF BAT ██████░░ 85%  CPU ██████░░ 45%  TEMP ██████░░ 76°C  RAM ████░░░░ 30% , 23:59 , 19 Jun | us | user | hostname
```

### Indicators

| Variable | What | Example |
|----------|------|---------|
| `#{asus_profile}` | ASUS ROG power profile | `PERF` (green), `BAL` (yellow), `QUIET` (red) |
| `#{cpu_info}` | CPU usage with 10-segment bar | `CPU ██████░░ 45%` |
| `#{cpu_temp}` | CPU temperature with 10-segment bar + min/max tracking | `TEMP ██░░░░░░ 55°C` |
| `#{ram_info}` | RAM usage with 10-segment bar | `RAM ████░░░░ 30%` |
| `#{layout_info}` | Active keyboard layout | calls `xkb_group` |

### Color gradients

**CPU / RAM bars** (% usage):
| Range | Color |
|-------|-------|
| `< 25%` | green `#A6E22E` |
| `25-50%` | yellow `#E6DB74` |
| `50-75%` | orange `#E8871A` |
| `> 75%` | red `#E82572` |

**CPU temperature bar** — inverted battery gradient, each block individually colored:
| Position | Color |
|----------|-------|
| 1–3 (cool) | green `#A6E22E` |
| 4–5 | yellow `#E6DB74` |
| 6–7 | orange `#E8871A` |
| 8–10 (hot) | red `#E82572` |

The temperature bar adapts its range by tracking min/max values in `~/.cache/tmux-cpu-temp-state`, updated on every read.

### kmscon note

When using tmux inside kmscon (the KMS virtual console), the `TERM` variable is set to `kmscon` by default which can cause rendering issues. The `~/.zshrc` overrides this to `xterm-256color` when running under kmscon.

## Copy mode

| Key | Action |
|-----|--------|
| `v` | Start selection |
| `y` | Yank to clipboard (and exit) |
| `Enter` | Yank to clipboard (and exit) |
| Mouse drag | Select (sets PRIMARY selection) |

## Custom functions

Defined in `~/.config/tmux/tmux.conf.local` between `# EOF` and `# "$@"`:

| Function | Displays | Source |
|----------|----------|--------|
| `asus_profile` | Power profile (PERF/BAL/QUIET) | `asusctl profile get` |
| `gpu_profile` | GPU profile (IGPU/HYBRID/NVIDIA/EGPU) | `supergfxctl -g` |
| `cpu_info` | CPU usage with 10-segment bar | `/proc/stat` |
| `cpu_temp` | CPU temp with 10-segment bar, min/max tracking | `/sys/class/hwmon/hwmon5/temp1_input` |
| `ram_info` | RAM usage with 10-segment bar | `free` |
| `layout_info` | Active keyboard layout | `xkb_group` script |
