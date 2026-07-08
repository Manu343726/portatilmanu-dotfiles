# games

## Table of Contents

- [Services](#services)
  - [games.SolitaireService](#gamessolitaireservice)
    - [Play](#play)
    - [PlayWeb](#playweb)
  - [games.MinesweeperService](#gamesminesweeperservice)
    - [Play](#play)
    - [PlayWeb](#playweb)
  - [games.Game2048Service](#gamesgame2048service)
    - [Play](#play)
    - [PlayWeb](#playweb)
  - [games.BattleshipService](#gamesbattleshipservice)
    - [Play](#play)
    - [PlayWeb](#playweb)
  - [games.ChessService](#gameschessservice)
    - [Play](#play)
    - [PlayWeb](#playweb)
- [Messages](#messages)
  - [PlayRequest](#playrequest)
  - [MinesweeperRequest](#minesweeperrequest)
  - [PlayResponse](#playresponse)
  - [PlayWebResponse](#playwebresponse)

## Services

### games.SolitaireService

SolitaireService provides a Klondike solitaire game playable in the
terminal or web browser. Uses standard Klondike rules: draw from stock,
build tableau columns in descending alternating colors, move completed
foundations.

#### Play

Play starts a new solitaire game. The game is interactive: use D to
draw from stock, 1-7 to auto-move a tableau column, "s d n" to move
n cards from source to destination column, and Q to quit.

- **Request:** `games.PlayRequest`
- **Response:** `games.PlayResponse`

#### PlayWeb

PlayWeb returns a URL to play the game in a web browser via WASM.

- **Request:** `games.PlayRequest`
- **Response:** `games.PlayWebResponse`

### games.MinesweeperService

MinesweeperService provides a classic minesweeper game playable in the
terminal or web browser. Reveal cells, flag bombs, and try to clear the
board without detonating any mines.

#### Play

Play starts a new minesweeper game with configurable dimensions.
Controls: WASD/arrows to move cursor, R to reveal, F to flag, Q to quit.
Game ends when you reveal a bomb or clear all safe cells.

- **Request:** `games.MinesweeperRequest`
- **Response:** `games.PlayResponse`

#### PlayWeb

PlayWeb returns a URL to play the game in a web browser via WASM.

- **Request:** `games.MinesweeperRequest`
- **Response:** `games.PlayWebResponse`

### games.Game2048Service

Game2048Service provides a 2048 sliding tile puzzle game. Combine tiles
by sliding them with arrow keys to reach the 2048 tile.

#### Play

Play starts a new 2048 game on a 4x4 grid. Use WASD to slide tiles
in the respective direction. Tiles with the same value merge when they
collide. Reach 2048 to win.

- **Request:** `games.PlayRequest`
- **Response:** `games.PlayResponse`

#### PlayWeb

PlayWeb returns a URL to play the game in a web browser via WASM.

- **Request:** `games.PlayRequest`
- **Response:** `games.PlayWebResponse`

### games.BattleshipService

BattleshipService provides a battleship game against an AI opponent.
Place your ships on a 10x10 grid, then take turns firing at the enemy
grid to sink all their ships before they sink yours.

#### Play

Play starts a new battleship game. First place your ships by entering
coordinates (e.g., "A1 H" for horizontal, "A1 V" for vertical, or "R"
for random placement). Then take turns entering target coordinates
(e.g., "B4") to fire at the enemy grid.

- **Request:** `games.PlayRequest`
- **Response:** `games.PlayResponse`

#### PlayWeb

PlayWeb returns a URL to play the game in a web browser via WASM.

- **Request:** `games.PlayRequest`
- **Response:** `games.PlayWebResponse`

### games.ChessService

ChessService provides a chess game against a simple AI opponent. Uses
standard chess rules with algebraic notation for moves.

#### Play

Play starts a new chess game. Enter moves in algebraic notation
(e.g., "e2 e4"). Special moves: "O-O" for kingside castle, "O-O-O"
for queenside castle. The AI plays black automatically.

- **Request:** `games.PlayRequest`
- **Response:** `games.PlayResponse`

#### PlayWeb

PlayWeb returns a URL to play the game in a web browser via WASM.

- **Request:** `games.PlayRequest`
- **Response:** `games.PlayWebResponse`


## Messages

### PlayRequest

PlayRequest is the base request for games that don't need extra setup.

| Field | Type | Description |
|-------|------|-------------|
| `options` | string | Optional: player name or game seed (currently unused, reserved). |

### MinesweeperRequest

MinesweeperRequest configures the minesweeper board dimensions.

| Field | Type | Description |
|-------|------|-------------|
| `width` | int32 | Board width in cells (default: 9). |
| `height` | int32 | Board height in cells (default: 9). |
| `bombs` | int32 | Number of mines to place (default: 10, max: width*height-1). |
| `options` | string | Optional: player name or game seed (currently unused, reserved). |

### PlayResponse

PlayResponse reports the game outcome.

| Field | Type | Description |
|-------|------|-------------|
| `won` | bool | Whether the player won the game. |
| `summary` | string | Human-readable result summary (e.g., "You won minesweeper!", "Boom!", "Checkmate!", "Game over"). |

### PlayWebResponse

PlayWebResponse returns the URL for browser-based play via WASM.

| Field | Type | Description |
|-------|------|-------------|
| `url` | string | URL to open in a browser (e.g. "http://127.0.0.1:9190/g2048/"). |

