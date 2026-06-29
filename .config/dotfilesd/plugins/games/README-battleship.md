# Battleship

Place your fleet then sink the enemy's ships before they sink yours.

## Placement phase

Place 5 ships by typing coordinates and orientation, then press Enter:

| Command | Action |
|---|---|
| `<col><row> h` | Place current ship **h**orizontally (e.g. `a1 h`) |
| `<col><row> v` | Place current ship **v**ertically (e.g. `b5 v`) |
| `r` | **R**andomly place all remaining ships |
| `q` | Quit |

Ships to place (in order): Carrier (5), Battleship (4), Cruiser (3),
Submarine (3), Destroyer (2). Your board shows `#` for placed ship cells.

## Battle phase

Take turns guessing enemy positions:

| Command | Action |
|---|---|
| `<col><row>` | Fire at a coordinate (e.g. `a1`, `j10`) |
| `q` | Quit |

### Legend

| Symbol | Meaning |
|---|---|
| `.` | Unknown / empty |
| `#` | Your ship (placement phase only) |
| `X` | Hit |
| `o` | Miss |

## Rules

Take turns firing at coordinates. `X` means hit, `o` means miss. Sink all
five enemy ships before your fleet is destroyed.

## Running

```
dotfilesctl games Battleship
```
