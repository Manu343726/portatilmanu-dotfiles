package main

import (
	"strconv"
	"time"

	"dotfilesd/plugin"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	whitePieceChars = []string{"", "♙", "♘", "♗", "♖", "♕", "♔"}
	blackPieceChars = []string{"", "♟", "♞", "♝", "♜", "♛", "♚"}
	pieceNames      = map[int8]string{cP: "Pawn", cN: "Knight", cB: "Bishop", cR: "Rook", cQ: "Queen", cK: "King"}
)

func runChess(ctx plugin.Context) bool {
	ga, err := newGameApp(ctx)
	if err != nil {
		return false
	}
	defer ga.close()

	game := newCh()
	selX, selY := -1, -1

	cursorStyle := tcell.StyleDefault.Background(tcell.NewRGBColor(0x50, 0x50, 0x40))

	table := tview.NewTable()
	table.SetFixed(1, 1)
	table.SetSelectable(true, true)
	table.SetSelectedStyle(cursorStyle)
	table.SetBorder(true)
	table.SetBorderColor(MonoOrange)
	table.SetTitle(" [::b]CHESS[::-] ")
	table.SetTitleColor(MonoFg)
	table.SetBackgroundColor(tcell.NewRGBColor(0x27, 0x28, 0x22))

	status := tview.NewTextView()
	status.SetDynamicColors(true)
	status.SetTextAlign(tview.AlignCenter)
	status.SetBackgroundColor(tcell.NewRGBColor(0x1E, 0x1F, 0x1C))

	info := tview.NewTextView()
	info.SetDynamicColors(true)
	info.SetBorder(true)
	info.SetBorderColor(MonoGreen)
	info.SetTitle(" [::b]Info[::-] ")
	info.SetTitleColor(MonoGreen)
	info.SetBackgroundColor(tcell.NewRGBColor(0x1E, 0x1F, 0x1C))

	lightSq := tcell.NewRGBColor(0x3E, 0x3D, 0x32)
	darkSq := tcell.NewRGBColor(0x1E, 0x1F, 0x1C)
	selSq := tcell.NewRGBColor(0x6A, 0x8E, 0x1E)
	whitePiece := tcell.NewRGBColor(0xF8, 0xF8, 0xF2)
	blackPiece := tcell.NewRGBColor(0x80, 0x80, 0x80)

	padCols := 4
	padCell := tview.NewTableCell("   ").
		SetBackgroundColor(tcell.NewRGBColor(0x27, 0x28, 0x22))

	rebuild := func() {
		table.Clear()
		for y := 0; y < 9; y++ {
			for p := 0; p < padCols; p++ {
				table.SetCell(y, p, padCell)
				table.SetCell(y, 8+1+padCols+p, padCell)
			}
		}

		for x := 0; x < 8; x++ {
			table.SetCell(0, padCols+1+x, tview.NewTableCell(" "+string(rune('a'+x))+" ").
				SetAlign(tview.AlignCenter).SetTextColor(MonoGray).
				SetBackgroundColor(tcell.NewRGBColor(0x27, 0x28, 0x22)))
		}
		for y := 0; y < 8; y++ {
			table.SetCell(y+1, padCols, tview.NewTableCell(" "+string(rune('8'-y))+" ").
				SetAlign(tview.AlignCenter).SetTextColor(MonoGray).
				SetBackgroundColor(tcell.NewRGBColor(0x27, 0x28, 0x22)))
			for x := 0; x < 8; x++ {
				v := game.b[y][x]
				text := "   "
				color := whitePiece
				bg := lightSq
				if (x+y)%2 == 1 {
					bg = darkSq
				}

				if v > 0 {
					text = " " + whitePieceChars[v] + " "
				} else if v < 0 {
					text = " " + blackPieceChars[-v] + " "
					color = blackPiece
				}

				if selX == x && selY == y {
					bg = selSq
				}

				table.SetCell(y+1, padCols+1+x, tview.NewTableCell(text).
					SetTextColor(color).SetBackgroundColor(bg).SetAlign(tview.AlignCenter))
			}
		}

		t := "[::d]"
		if game.t == 1 {
			t += "White"
		} else {
			t += "Black"
		}
		t += " to move"
		if game.ov {
			if game.wi > 0 {
				t += "  [green::b]  White wins!  [::-]"
			} else {
				t += "  [green::b]  Black wins!  [::-]"
			}
		} else if game.ic(game.t) {
			t += "  [red::b]  CHECK!  [::-]"
		}
		status.SetText(t)

		si := "[::d]"
		if game.t == 1 {
			si += "Turn: White\n"
		} else {
			si += "Turn: Black\n"
		}
		if selX >= 0 {
			v := game.b[selY][selX]
			pc := "?"
			if v != 0 {
				pc = pieceNames[game.ab(v)]
			}
			si += "Selected: " + string(rune('a'+selX)) + string(rune('8'-selY))
			si += " (" + pc + ")\n"
		} else {
			si += "Selected: -\n"
		}
		if game.ic(game.t) {
			si += "Check: [red::b]YES[::-][::d]\n"
		} else {
			si += "Check: No\n"
		}

		wc := 0
		bc := 0
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				v := game.b[y][x]
				if v > 0 {
					wc++
				} else if v < 0 {
					bc++
				}
			}
		}
		si += "White pieces: " + strconv.Itoa(wc) + "\n"
		si += "Black pieces: " + strconv.Itoa(bc) + "\n"
		si += "\n[::b]Controls[::-][::d]\n"
		si += "arrows=move\nEnter=select/move\nEsc=desel\nQ=quit[::-]"
		info.SetText(si)
	}

	getCell := func() (x, y int) {
		r, c := table.GetSelection()
		return c - 1, r - 1
	}

	restart := func() {
		game = newCh()
		selX, selY = -1, -1
		rebuild()
	}

	handleEnter := func() {
		if game.ov {
			return
		}
		cx, cy := getCell()
		if selX < 0 {
			v := game.b[cy][cx]
			if v != 0 && ((v > 0 && game.t == 1) || (v < 0 && game.t == -1)) {
				selX, selY = cx, cy
				rebuild()
			}
		} else {
			if cy == selY && cx == selX {
				selX, selY = -1, -1
				rebuild()
				return
			}
			if game.lg(int8(selX), int8(selY), int8(cx), int8(cy)) {
				game.ap(int8(selX), int8(selY), int8(cx), int8(cy))
				selX, selY = -1, -1
				rebuild()
				if !game.ov {
					time.AfterFunc(50*time.Millisecond, func() {
						ga.app.QueueUpdateDraw(func() {
							game.ai()
							rebuild()
						})
					})
				}
			} else {
				selX, selY = -1, -1
				rebuild()
			}
		}
	}

	ga.app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyRune {
			switch ev.Rune() {
			case 'q', 'Q':
				select { case ga.stopCh <- struct{}{}: default: }
				return nil
			case 'r', 'R':
				if game.ov {
					restart()
				}
				return nil
			}
		}
		switch ev.Key() {
		case tcell.KeyEnter:
			handleEnter()
			return nil
		case tcell.KeyEscape:
			if selX >= 0 {
				selX, selY = -1, -1
				rebuild()
				return nil
			}
		}
		return ev
	})

	lp := tview.NewBox()
	lp.SetBackgroundColor(tcell.NewRGBColor(0x27, 0x28, 0x22))
	rp := tview.NewBox()
	rp.SetBackgroundColor(tcell.NewRGBColor(0x27, 0x28, 0x22))

	center := tview.NewFlex()
	center.SetDirection(tview.FlexColumn)
	center.AddItem(lp, 0, 1, false)
	center.AddItem(table, 0, 3, true)
	center.AddItem(rp, 0, 1, false)

	main := tview.NewFlex()
	main.SetDirection(tview.FlexColumn)
	main.AddItem(center, 0, 1, true)
	main.AddItem(info, 22, 0, false)

	flex := tview.NewFlex()
	flex.SetDirection(tview.FlexRow)
	flex.AddItem(status, 1, 0, false)
	flex.AddItem(main, 0, 1, true)

	ga.app.SetRoot(flex, true)
	rebuild()

	if err := ga.app.Run(); err != nil {
		return false
	}
	return game.wi > 0
}
