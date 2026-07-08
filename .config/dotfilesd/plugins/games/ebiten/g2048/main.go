// Web-based 2048 game using Ebitengine, compiled to WebAssembly.
package main

import (
	"fmt"
	"image/color"
	"log"
	"math/rand"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const (
	boardSize  = 4
	cellSize   = 100
	cellMargin = 10
	boardPx    = boardSize*cellSize + (boardSize+1)*cellMargin
	screenW    = boardPx
	screenH    = boardPx + 80
)

type game struct {
	board [boardSize][boardSize]int
	score int
	over  bool
	won   bool
	r     *rand.Rand
}

func newGame() *game {
	g := &game{r: rand.New(rand.NewSource(rand.Int63()))}
	g.spawn()
	g.spawn()
	return g
}

func (g *game) spawn() {
	var empty [][2]int
	for y := 0; y < boardSize; y++ {
		for x := 0; x < boardSize; x++ {
			if g.board[y][x] == 0 {
				empty = append(empty, [2]int{x, y})
			}
		}
	}
	if len(empty) == 0 {
		return
	}
	p := empty[g.r.Intn(len(empty))]
	v := 2
	if g.r.Intn(10) == 0 {
		v = 4
	}
	g.board[p[1]][p[0]] = v
}

func slide(row [boardSize]int) ([boardSize]int, int) {
	var v []int
	for _, x := range row {
		if x != 0 {
			v = append(v, x)
		}
	}
	sc := 0
	for i := 0; i < len(v)-1; i++ {
		if v[i] == v[i+1] {
			v[i] *= 2
			sc += v[i]
			v = append(v[:i+1], v[i+2:]...)
		}
	}
	for len(v) < boardSize {
		v = append(v, 0)
	}
	var out [boardSize]int
	copy(out[:], v)
	return out, sc
}

func (g *game) move(dx, dy int) bool {
	changed := false
	for i := 0; i < boardSize; i++ {
		var row [boardSize]int
		if dy != 0 {
			for j := 0; j < boardSize; j++ {
				row[j] = g.board[j][i]
			}
		} else {
			row = g.board[i]
		}
		if dx < 0 || dy < 0 {
			for a, b := 0, boardSize-1; a < b; a, b = a+1, b-1 {
				row[a], row[b] = row[b], row[a]
			}
		}
		s, sc := slide(row)
		if dx < 0 || dy < 0 {
			for a, b := 0, boardSize-1; a < b; a, b = a+1, b-1 {
				s[a], s[b] = s[b], s[a]
			}
		}
		if dy != 0 {
			for j := 0; j < boardSize; j++ {
				if g.board[j][i] != s[j] {
					changed = true
				}
				g.board[j][i] = s[j]
			}
		} else {
			if g.board[i] != s {
				changed = true
			}
			g.board[i] = s
		}
		g.score += sc
	}
	return changed
}

func (g *game) canMove() bool {
	for y := 0; y < boardSize; y++ {
		for x := 0; x < boardSize; x++ {
			if g.board[y][x] == 0 {
				return true
			}
			if x < boardSize-1 && g.board[y][x] == g.board[y][x+1] {
				return true
			}
			if y < boardSize-1 && g.board[y][x] == g.board[y+1][x] {
				return true
			}
		}
	}
	return false
}

func (g *game) checkWin() {
	if g.won {
		return
	}
	for y := 0; y < boardSize; y++ {
		for x := 0; x < boardSize; x++ {
			if g.board[y][x] >= 2048 {
				g.won = true
				return
			}
		}
	}
}

func (g *game) Update() error {
	if g.over || g.won {
		return nil
	}
	var dx, dy int
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowUp) || inpututil.IsKeyJustPressed(ebiten.KeyW) {
		dy = 1
	} else if inpututil.IsKeyJustPressed(ebiten.KeyArrowDown) || inpututil.IsKeyJustPressed(ebiten.KeyS) {
		dy = -1
	} else if inpututil.IsKeyJustPressed(ebiten.KeyArrowLeft) || inpututil.IsKeyJustPressed(ebiten.KeyA) {
		dx = 1
	} else if inpututil.IsKeyJustPressed(ebiten.KeyArrowRight) || inpututil.IsKeyJustPressed(ebiten.KeyD) {
		dx = -1
	}
	if dx == 0 && dy == 0 {
		return nil
	}
	if g.move(dx, dy) {
		g.spawn()
		g.checkWin()
		if !g.canMove() {
			g.over = true
		}
	}
	return nil
}

func tileColor(v int) color.RGBA {
	switch v {
	case 2:
		return color.RGBA{0xEE, 0xE4, 0xDA, 0xFF}
	case 4:
		return color.RGBA{0xED, 0xE0, 0xC8, 0xFF}
	case 8:
		return color.RGBA{0xF2, 0xB1, 0x79, 0xFF}
	case 16:
		return color.RGBA{0xF5, 0x95, 0x63, 0xFF}
	case 32:
		return color.RGBA{0xF6, 0x7C, 0x5F, 0xFF}
	case 64:
		return color.RGBA{0xF6, 0x5E, 0x3B, 0xFF}
	case 128:
		return color.RGBA{0xED, 0xCF, 0x72, 0xFF}
	case 256:
		return color.RGBA{0xED, 0xCC, 0x61, 0xFF}
	case 512:
		return color.RGBA{0xED, 0xC8, 0x50, 0xFF}
	case 1024:
		return color.RGBA{0xED, 0xC5, 0x3F, 0xFF}
	case 2048:
		return color.RGBA{0xED, 0xC2, 0x2E, 0xFF}
	default:
		return color.RGBA{0x3C, 0x3A, 0x32, 0xFF}
	}
}

func textColor(v int) color.RGBA {
	if v <= 4 {
		return color.RGBA{0x77, 0x6E, 0x65, 0xFF}
	}
	return color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
}

func (g *game) Draw(screen *ebiten.Image) {
	screen.Fill(color.RGBA{0xFA, 0xF8, 0xEF, 0xFF})

	for y := 0; y < boardSize; y++ {
		for x := 0; x < boardSize; x++ {
			px := float32(cellMargin + x*(cellSize+cellMargin))
			py := float32(cellMargin + y*(cellSize+cellMargin))
			v := g.board[y][x]
			bg := color.RGBA{0xCD, 0xC1, 0xB4, 0xFF}
			if v > 0 {
				bg = tileColor(v)
			}
			vector.DrawFilledRect(screen, px, py, float32(cellSize), float32(cellSize), bg, true)
		}
	}

	for y := 0; y < boardSize; y++ {
		for x := 0; x < boardSize; x++ {
			v := g.board[y][x]
			if v == 0 {
				continue
			}
			px := cellMargin + x*(cellSize+cellMargin)
			py := cellMargin + y*(cellSize+cellMargin)
			s := fmt.Sprintf("%d", v)
			tw := len(s) * 7
			th := 13
			tx := px + (cellSize-tw)/2
			ty := py + (cellSize+th)/2
			ebitenutil.DebugPrintAt(screen, s, tx, ty)
		}
	}

	ebitenutil.DebugPrintAt(screen, fmt.Sprintf("Score: %d", g.score), 10, boardPx+10)

	if g.over {
		vector.DrawFilledRect(screen, 0, 0, float32(screenW), float32(screenH), color.RGBA{0x00, 0x00, 0x00, 0xB0}, true)
		ebitenutil.DebugPrintAt(screen, "GAME OVER", screenW/2-7*9/2, screenH/2)
		ebitenutil.DebugPrintAt(screen, "Refresh to restart", screenW/2-7*18/2, screenH/2+20)
	}
	if g.won {
		vector.DrawFilledRect(screen, 0, 0, float32(screenW), float32(screenH), color.RGBA{0x00, 0x00, 0x00, 0xB0}, true)
		ebitenutil.DebugPrintAt(screen, "YOU WIN!", screenW/2-7*9/2, screenH/2)
		ebitenutil.DebugPrintAt(screen, "Refresh to restart", screenW/2-7*18/2, screenH/2+20)
	}
}

func (g *game) Layout(outsideW, outsideH int) (int, int) {
	return screenW, screenH
}

func main() {
	g := newGame()
	ebiten.SetWindowSize(screenW, screenH)
	ebiten.SetWindowTitle("2048")
	if err := ebiten.RunGame(g); err != nil {
		log.Fatal(err)
	}
}
