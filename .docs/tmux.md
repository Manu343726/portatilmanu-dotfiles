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

The right side is rendered by the dotfilesd `tmuxbar` plugin
(`dotfilesctl tmuxbar status-bar --max-width #{client_width}`, wired in
`~/.config/tmux/tmux.conf.local`):

```
CPU ██████░░ 45% (proc)  RAM ████░░░░ 30% (proc)  PLUGGED  TEMP ██░░ 55°C  WIFI 67% (ssid)  P  H   23:59  19 Jun | es | user | hostname
```

### Indicators

| Widget | What | Values |
|--------|------|--------|
| `CPU` | CPU usage with 10-segment bar + top process | percent |
| `RAM` | RAM usage with 10-segment bar + top process | GiB / percent |
| `PLUGGED`/`BAT` | Battery status + 10-segment bar | percent |
| `TEMP` | CPU temperature with 10-segment bar | °C |
| `WIFI` | WiFi signal + 10-segment bar | percent / SSID |
| Power profile | TLP profile, single letter | `P` (orange), `B` (green), `S` (blue) |
| GPU profile | GPU mode, single letter | `N` (orange), `H` (green), `I` (blue), `E` (purple) |

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
| `status-bar` | Combined bar: CPU, RAM, battery, temp, WiFi, power + GPU profiles | `dotfilesctl tmuxbar status-bar` |
| `power-profile-widget` | TLP power profile (`P`/`B`/`S`) | `dotfilesctl tmuxbar power-profile-widget` (reads `tlpctl get`) |
| `gpu-profile-widget` | GPU mode (`N`/`H`/`I`/`E`) | `dotfilesctl tmuxbar gpu-profile-widget` (reads `supergfxctl -g`) |
