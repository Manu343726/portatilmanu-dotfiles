# Minesweeper

Reveal all cells without hitting a bomb.

## Controls

Press a single key (no Enter needed):

| Key | Action |
|---|---|
| `w` / `s` / `a` / `d` | Move cursor up / down / left / right |
| `r` | **R**eveal cell under cursor |
| `f` | Toggle **f**lag on cell under cursor |
| `q` | Quit |

## Rules

The board hides a fixed number of bombs. Numbers show how many adjacent cells
contain bombs. Use flags to mark suspected bombs. If you reveal a bomb it's
game over. Reveal every non-bomb cell to win.

## Options

```
dotfilesctl games Minesweeper --width 16 --height 16 --bombs 40
```

Defaults: 9×9, 10 bombs.

## Running

```
dotfilesctl games Minesweeper
```
