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
BAT ██████░░ 85%  CPU ██████░░ 45%  RAM ████░░░░ 30% , 23:59 , 19 Jun | us | user | hostname
```

Custom shell functions (`cpu_info`, `ram_info`) use Monokai color-coded bar segments:
- `< 25%`: green `#A6E22E`
- `25-50%`: yellow `#E6DB74`
- `50-75%`: orange `#E8871A`
- `> 75%`: pink `#E82572`

Keyboard layout (`layout_info`) calls `~/.local/bin/xkg_group` which runs `xkb-switch`.

## Copy mode

| Key | Action |
|-----|--------|
| `v` | Start selection |
| `y` | Yank to clipboard (and exit) |
| `Enter` | Yank to clipboard (and exit) |
| Mouse drag | Select (sets PRIMARY selection) |

## Custom functions

Defined in `~/.config/tmux/tmux.conf.local` between `# EOF` and `# "$@"`:

- `cpu_info` — CPU usage with 10-segment bar
- `ram_info` — RAM usage with 10-segment bar
- `layout_info` — Active keyboard layout
