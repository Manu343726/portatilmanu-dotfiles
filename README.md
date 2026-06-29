# portatilmanu dotfiles

Monokai-themed dotfiles for Manjaro i3 on an ASUS ROG Flow X13.

## Quick start

```sh
git clone https://github.com/manu343726/portatilmanu-dotfiles.git ~
tmux source-file ~/.tmux.conf
i3-msg reload
```

### Post-clone setup

```sh
# kmscon — symlink config into /etc (needed because kmscon runs as root)
sudo rm -f /etc/kmscon/kmscon.conf
sudo ln -s ~/.config/kmscon/kmscon.conf /etc/kmscon/kmscon.conf
sudo systemctl enable --now kmsconvt@tty2.service
```

See `.docs/` for per-component setup guides.

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
| **ASUS ROG** | `~/.local/bin/asus-profile` | Power profile switcher via `asusctl` |
| **ASUS GPU** | `~/.local/bin/gpu-profile` | GPU profile switcher via `supergfxctl` |
| kmscon      | `~/.config/kmscon/kmscon.conf` → `/etc/kmscon/` | `.docs/kmscon.md` |

### ASUS ROG Flow X13 extras

| Feature | Binding / Trigger | Description |
|---------|-------------------|-------------|
| Profile switcher | `$mod+Ctrl+p` | rofi menu for Quiet / Balanced / Performance |
| GPU profile switcher | `$mod+Ctrl+g` | rofi menu for Integrated / Hybrid / AsusMuxDgpu / AsusEgpu |
| CPU temperature | tmux status bar | Adaptive bar with min/max tracking via `k10temp` |
| ASUS profile indicator | tmux status bar | `PERF`/`BAL`/`QUIET` with color |
| GPU profile indicator | tmux status bar | `IGPU`/`HYBRID`/`NVIDIA`/`EGPU` with color |

