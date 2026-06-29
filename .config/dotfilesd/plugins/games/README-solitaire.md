# Solitaire (Klondike)

Classic Klondike solitaire — move all cards to the four foundation piles in
ascending order by suit.

## Controls

Type a command and press Enter:

| Command | Action |
|---|---|
| `d` | **D**raw a card from the stock |
| `w <n>` | Move the drawn waste-card to tableau column `<n>` (1–7) |
| `1`–`7` | Auto-play top card of a column to its foundation (if eligible) |
| `<s> <d> [n]` | Move **n** cards (default 1) from column `<s>` to column `<d>` (1–7) |
| `q` | Quit |

### Examples

- `w 3` — place the drawn card from the waste onto column 3
- `5 2` — move the top face-up card from column 5 to column 2
- `5 2 4` — move 4 face-up cards from column 5 to column 2

## Rules

- Build tableau columns in descending order (King to Ace), alternating colors.
- Build foundation piles in ascending order (Ace to King) by suit.
- Only Kings (or sequences starting with a King) can fill empty tableau columns.
- The game auto-plays eligible cards to foundations after every move.

## Running

```
dotfilesctl games Solitaire
```
