# portatilmanu dotfiles

Monokai-themed dotfiles for Manjaro i3 on an ASUS ROG Flow X13.

## Quick start

```sh
git clone ssh://git@172.25.10.159:2222/manu343726/portatilmanu-dotfiles.git ~
tmux source-file ~/.tmux.conf
i3-msg reload
```

## What's here

| Component | Config | Docs |
|-----------|--------|------|
| i3        | `~/.i3/config` | `.docs/i3.md` |
| i3status  | `~/.config/i3status/config` | `.docs/i3status.md` |
| tmux      | `~/.tmux.conf` + `~/.tmux.conf.local` | `.docs/tmux.md` |
| kitty     | `~/.config/kitty/kitty.conf` | `.docs/kitty.md` |
| zsh       | `~/.zshrc` | `.docs/zsh.md` |
| keyboard  | — | `.docs/keyboard.md` |
| clipboard | — | `.docs/clipboard.md` |
| palette   | — | `.docs/index.md` |
| dotfilesd | `~/dotfilesd/` | `dotfilesd/README.md` + `dotfilesd/docs/` |
| **ASUS ROG** | `~/.local/bin/asus-profile` | Profile switcher via `asusctl` |

### ASUS ROG Flow X13 extras

| Feature | Binding / Trigger | Description |
|---------|-------------------|-------------|
| Profile switcher | `$mod+Ctrl+p` | rofi menu for Quiet / Balanced / Performance |
| CPU temperature | tmux status bar | Adaptive bar with min/max tracking via `k10temp` |
| ASUS profile indicator | tmux status bar | `PERF`/`BAL`/`QUIET` with color |

