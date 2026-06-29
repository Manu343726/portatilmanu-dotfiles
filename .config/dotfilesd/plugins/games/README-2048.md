# 2048

Merge tiles to reach **2048**.

## Controls

Press a single key (no Enter needed):

| Key | Action |
|---|---|
| `w` | Slide tiles **up** |
| `s` | Slide tiles **down** |
| `a` | Slide tiles **left** |
| `d` | Slide tiles **right** |
| `q` | Quit |

## Rules

Tiles slide as far as possible in the chosen direction. Identical adjacent
tiles merge into one with double the value. After each move a new `2` (or
rarely `4`) appears in an empty cell.

The game ends when the board fills up with no possible merges. You win when
a tile reaches 2048 (you can keep playing past 2048 for a higher score).

## Running

```
dotfilesctl games Game2048
```
