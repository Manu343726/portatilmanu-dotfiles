# Chess

Play chess against a simple AI opponent (random legal moves).

## Controls

Type standard algebraic notation and press Enter:

| Command | Action |
|---|---|
| `<from> <to>` | Move piece (e.g. `e2 e4`, `g1 f3`) |
| `d7 d8q` | Pawn promotion to queen |
| `O-O` | Kingside castle |
| `O-O-O` | Queenside castle |
| `q` | Quit |

### Examples

- `e2 e4` — move pawn from e2 to e4
- `g1 f3` — develop knight to f3
- `d7 d8q` — promote pawn to queen
- `O-O` — castle kingside

### Pieces

Red = black pieces, green = white pieces:

| Symbol | Piece |
|---|---|
| `P` | Pawn |
| `N` | Knight |
| `B` | Bishop |
| `R` | Rook |
| `Q` | Queen |
| `K` | King |

## Rules

Standard chess rules. The AI picks a random legal move each turn. Promotion
auto-promotes to queen. Check, checkmate, and stalemate are detected. The
game ends on checkmate — no resign or draw offers.

## Running

```
dotfilesctl games Chess
```
