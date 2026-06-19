# Keyboard layout

## Layouts

| Layout | Description |
|--------|-------------|
| `us` | US QWERTY (default) |
| `es` | Spanish QWERTY |

Toggle between layouts with **Alt+Shift**.

## System config

Set via `localectl` and persisted in `/etc/X11/xorg.conf.d/00-keyboard.conf`:

```
XkbLayout:  us,es
XkbOptions: grp:alt_shift_toggle
```

## Indicators

- **sbxkb** — tray icon showing current layout (`us` / `es`)
- **Tmux layout_info** — shows current layout in status bar via `xkb-switch`

## i3 integration

The i3 config runs `exec_always setxkbmap -layout us,es -option grp:alt_shift_toggle` so the layout survives i3 reloads.
