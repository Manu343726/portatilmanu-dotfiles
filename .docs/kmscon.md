# kmscon

KMS virtual console with Monokai palette and TrueType font rendering, replacing the
Linux framebuffer TTY (agetty). Runs on a separate VT alongside the display manager.

## Files

| File | Purpose |
|------|---------|
| `~/.config/kmscon/kmscon.conf` | Config (tracked in dotfiles repo) |
| `/etc/kmscon/kmscon.conf` | Symlink → `~/.config/kmscon/kmscon.conf` |
| `/usr/lib/systemd/system/kmsconvt@.service` | Systemd unit template |

## Setup

```sh
# Install
sudo pacman -S kmscon fontconfig libx11

# Symlink config (kmscon runs as root, reads from /etc)
sudo rm -f /etc/kmscon/kmscon.conf
sudo ln -s ~/.config/kmscon/kmscon.conf /etc/kmscon/kmscon.conf

# Enable on tty2 (side-by-side with display manager on tty1)
sudo systemctl enable --now kmsconvt@tty2.service
```

Switch with `Ctrl+Alt+F2` (or whichever VT you enabled). `Ctrl+Alt+F1` returns to the display manager.

## Configuration

| Setting | Value |
|---------|-------|
| Font | DejaVu Sans Mono, 14px (freetype engine) |
| Background | `#272822` (Monokai bg) |
| Foreground | `#F8F8F2` (Monokai fg) |
| Palette | Monokai — green `#A6E22E`, red `#F92672`, yellow `#E6DB74`, orange `#FD971F`, cyan `#66D9EF`, purple `#AE81FF`, comment `#75715E` |
| Keyboard | US layout (`xkb-layout=us`) |

## tmux compatibility

kmscon sets `TERM=kmscon` by default. The `.zshrc` overrides this to
`xterm-256color` when running under kmscon so tmux renders correctly.

## Notes

- Configured on tty2 only — tty1 stays with the DM for safety
- Falls back to `getty@.service` if kmscon fails to start
- `Alt+Fn` does not switch VTs inside kmscon; use `Ctrl+Alt+Fn`
- Multi-monitor: `largest` mode — TTY shows only on the largest connected display
  (external when docked, laptop when undocked). Disabled outputs in the TTY are
  automatically re-enabled by the display manager when switching back to X11.
