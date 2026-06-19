# Clipboard

Synchronizes three clipboard layers: tmux buffer, X11 PRIMARY selection, and X11 CLIPBOARD selection.

## Tmux config

Set in `~/.config/tmux/tmux.conf.local`:

- `set -g set-clipboard external` — enables OSC 52 (terminal-level clipboard)
- `xclip` bridges tmux selections to X11 selections

## Copy

| Action | Goes to | Paste with |
|--------|---------|------------|
| `y` / `Enter` in copy mode | CLIPBOARD | `Ctrl+V` |
| Mouse drag in tmux | PRIMARY | Middle-click / `Shift+Insert` |
| Terminal native selection | PRIMARY | Middle-click / `Shift+Insert` |

## Paste

| Binding | Pastes from |
|---------|-------------|
| Middle-click | PRIMARY |
| `Insert` | PRIMARY |
| `prefix` + `Ctrl+P` | CLIPBOARD |

## How it works

When you yank in tmux copy mode (`y` or `Enter`), tmux pipes the selection to `xclip -in -selection clipboard`, which makes it available for `Ctrl+V` everywhere.

When you mouse-drag text in tmux (with mouse mode on), tmux pipes the selection to `xclip -in -selection primary`, which makes it available for middle-click / `Shift+Insert`.

`xclip -o -selection primary | tmux load-buffer - && tmux paste-buffer` reads the PRIMARY selection and pastes it into tmux.
