# portatilmanu dotfiles

Monokai-themed dotfiles for a Manjaro i3 setup on a ThinkPad.

## Palette

| Token       | Hex       | Usage                |
|-------------|-----------|----------------------|
| Background  | `#272822` | i3bar, kitty bg      |
| Foreground  | `#F8F8F2` | i3bar text, kitty fg |
| Green       | `#A6E22E` | focused workspace    |
| Orange      | `#FD971F` | binding mode         |
| Pink        | `#F92672` | urgent / error       |
| Purple      | `#AE81FF` | accent               |
| Blue        | `#66D9EF` | accent               |
| Yellow      | `#E6DB74` | accent               |
| Grey        | `#75715E` | separators, inactive |

## Components

### tmux (`~/.tmux.conf` + `~/.config/tmux/tmux.conf.local`)

- Oh My Tmux! with powerline chevrons
- Monokai status bar: CPU, RAM, battery, date, keyboard layout
- Custom shell functions with color-coded bars
- `Ctrl+b` `r` to reload

### kitty (`~/.config/kitty/kitty.conf`)

- Hack Nerd Font Mono 11pt
- Monokai 16-color ANSI palette
- Full color table mapped to Monokai values

### Zsh (`~/.zshrc`)

- Oh My Zsh, agnoster theme
- Plugins: `zsh-autosuggestions`, `zsh-syntax-highlighting`
- Autostarts tmux
- Aliases: `ls` → `eza`, `cat` → `bat`, `grep` → `rg`

### i3 (`~/.i3/config`)

- Monokai bar colors (green focused badge, grey inactive)
- `$mod+Return` → kitty
- Gaps: inner 14, outer -2
- Smart gaps / borders on

### i3status (`~/.config/i3status/config`)

- Monokai status colors: green/orange/pink for good/degraded/bad
- Ethernet, WiFi, battery, disk, load, memory, clock

### Keyboard layout

- US + Spanish, toggle with **Alt+Shift**
- sbxkb tray icon + tmux `layout_info` indicator

### Modern CLI

- `eza`, `bat`, `ripgrep`, `fd`, `fzf`, `zoxide`

## Files

```
~/.i3/config                    i3 window manager
~/.config/i3status/config       i3status bar
~/.tmux.conf                    Oh My Tmux! base
~/.config/tmux/tmux.conf.local  Monokai tmux theme
~/.config/kitty/kitty.conf      Kitty terminal
~/.zshrc                        Zsh + Oh My Zsh
~/.profile                      Login shell config
~/.xprofile                     X session config
~/.local/bin/xkb_group          Keyboard layout helper
.gitignore                      Ignore everything but dotfiles
```

## Quick reference

| Action                    | Binding                 |
|---------------------------|-------------------------|
| Open terminal             | `$mod+Return`           |
| Reload i3                 | `$mod+Shift+c`          |
| Reload tmux               | `Ctrl+b` `r`            |
| Toggle keyboard layout    | `Alt+Shift`             |
| Application launcher      | `$mod+d`                |
| Lock screen               | `$mod+9`                |
| Resize mode               | `$mod+r`                |
| Gap mode                  | `$mod+Shift+g`          |

## First-time setup

1. Install dependencies: `tmux`, `kitty`, `ttf-hack-nerd`, `zsh`, `eza`, `bat`, `ripgrep`, `fd`, `fzf`, `zoxide`, `oh-my-zsh`, `xkb-switch`
2. Clone this repo to `~`
3. Run `tmux source-file ~/.tmux.conf`
4. Reload i3: `$mod+Shift+c`

## Repo

Mirrored at `ssh://git@172.25.10.159:2222/manu343726/portatilmanu-dotfiles`
