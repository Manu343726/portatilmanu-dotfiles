# Games Plugin

Five terminal-based games you can play directly from your shell via `dotfilesctl`:

```
dotfilesctl games <game>
```

| Command | Game |
|---|---|
| `dotfilesctl games Game2048` | 2048 |
| `dotfilesctl games Minesweeper` | Minesweeper |
| `dotfilesctl games Solitaire` | Klondike Solitaire |
| `dotfilesctl games Battleship` | Battleship |
| `dotfilesctl games Chess` | Chess (vs AI) |

All games support `--json` for programmatic output: `dotfilesctl games Game2048 --json`.

---

## 2048

Merge tiles to reach **2048**.

**Controls** — press a single key (no Enter needed):

| Key | Action |
|---|---|
| `w` | Slide tiles **up** |
| `s` | Slide tiles **down** |
| `a` | Slide tiles **left** |
| `d` | Slide tiles **right** |
| `q` | Quit |

**Rules**: Tiles slide as far as possible in the chosen direction. Identical
adjacent tiles merge into one with double the value. After each move a new `2`
(or rarely `4`) appears in an empty cell. The game ends when the board fills up
with no possible merges. You win when a tile reaches 2048 (you can keep playing
past 2048 for a higher score).

---

## Minesweeper

Reveal all cells without hitting a bomb.

**Controls** — press a single key (no Enter needed):

| Key | Action |
|---|---|
| `w` / `s` / `a` / `d` | Move cursor up / down / left / right |
| `r` | **R**eveal cell under cursor |
| `f` | Toggle **f**lag on cell under cursor |
| `q` | Quit |

**Rules**: The board hides a fixed number of bombs. Numbers show how many
adjacent cells contain bombs. Use flags to mark suspected bombs. If you reveal
a bomb it's game over. Reveal every non-bomb cell to win.

**Options**: You can configure the board size and bomb count:

```
dotfilesctl games Minesweeper --width 16 --height 16 --bombs 40
```

Defaults: 9×9, 10 bombs.

---

## Solitaire (Klondike)

Classic Klondike solitaire — move all cards to the four foundation piles in
ascending order by suit.

**Controls** — type a command and press Enter:

| Command | Action |
|---|---|
| `d` | **D**raw a card from the stock |
| `1`–`7` | Auto-play the top card of a tableau column to its foundation (if eligible) |
| `w <n>` | Move the drawn waste-card to tableau column `<n>` (1–7) |
| `<s> <d> [n]` | Move **n** cards (default 1) from column `<s>` to column `<d>` (1–7) |
| `q` | Quit |

**Examples**:

- `w 3` — place the drawn card from the waste onto column 3
- `5 2` — move the top face-up card from column 5 to column 2
- `5 2 4` — move 4 face-up cards from column 5 to column 2

**Rules**: Build tableau columns in descending order (King to Ace), alternating
colors. Build foundation piles in ascending order (Ace to King) by suit. Only
Kings (or sequences starting with a King) can fill empty tableau columns. The
game auto-plays eligible cards to foundations after every move.

---

## Battleship

Place your fleet then sink the enemy's ships before they sink yours.

**Placement phase** — place 5 ships by typing coordinates and orientation:

| Command | Action |
|---|---|
| `<col><row> h` | Place current ship **h**orizontally (e.g. `a1 h`) |
| `<col><row> v` | Place current ship **v**ertically (e.g. `b5 v`) |
| `r` | **R**andomly place all remaining ships |
| `q` | Quit |

Ships to place (in order): Carrier (5), Battleship (4), Cruiser (3),
Submarine (3), Destroyer (2). Your board shows `#` for placed ship cells.

**Battle phase** — guess enemy positions:

| Command | Action |
|---|---|
| `<col><row>` | Fire at a coordinate (e.g. `a1`, `j10`) |
| `q` | Quit |

**Legend**:

| Symbol | Meaning |
|---|---|
| `.` | Unknown / empty |
| `#` | Your ship (placement phase only) |
| `X` | Hit |
| `o` | Miss |

**Rules**: Take turns firing at coordinates. `X` means hit, `o` means miss.
Sink all five enemy ships before your fleet is destroyed.

---

## Chess

Play chess against a simple AI opponent (random legal moves).

**Controls** — type standard algebraic notation and press Enter:

| Command | Action |
|---|---|
| `<from> <to>` | Move piece (e.g. `e2 e4`, `g1 f3`, `d7 d8q` for promotion) |
| `O-O` | Kingside castle |
| `O-O-O` | Queenside castle |
| `q` | Quit |

**Examples**:

- `e2 e4` — move pawn from e2 to e4
- `g1 f3` — develop knight to f3
- `d7 d8q` — promote pawn to queen
- `O-O` — castle kingside

**Pieces** (red = black, green = white):

| Symbol | Piece |
|---|---|
| `P` | Pawn |
| `N` | Knight |
| `B` | Bishop |
| `R` | Rook |
| `Q` | Queen |
| `K` | King |

**Rules**: Standard chess rules. The AI picks a random legal move each turn.
Promotion auto-promotes to queen. Check, checkmate, and stalemate are detected.
The game ends on checkmate — no resign or draw offers.

---

## General tips

- **Ctrl+C** exits any game cleanly (terminal is restored automatically).
- Games where you press single keys (2048, minesweeper) process the key
  immediately — no Enter needed.
- Games with multi-key commands (solitaire, battleship, chess) require Enter
  so you can type longer input.
- The terminal is put in raw mode during play so keystrokes aren't echoed.
