package main

import (
	"strconv"

	"dotfilesd/plugin"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func runMinesweeper(ctx plugin.Context, w, h, bombs int) bool {
	ga, err := newGameApp(ctx)
	if err != nil {
		return false
	}
	defer ga.close()

	if w <= 0 {
		w = 9
	}
	if h <= 0 {
		h = 9
	}
	if bombs <= 0 {
		bombs = 10
	}
	if bombs >= w*h {
		bombs = w*h - 1
	}

	game := newMS(w, h, bombs)

	cursorStyle := tcell.StyleDefault.Background(tcell.NewRGBColor(0x60, 0x60, 0x50))

	table := tview.NewTable()
	table.SetFixed(1, 1)
	table.SetSelectable(true, true)
	table.SetSelectedStyle(cursorStyle)
	table.SetBorder(true)
	table.SetBorderColor(MonoOrange)
	table.SetTitle(" [::b]MINESWEEPER[::-] ")
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

	numColors := []tcell.Color{MonoGray, MonoBlue, MonoGreen, MonoRed, MonoOrange, MonoRed, MonoBlue, MonoGray, MonoGray}

	rebuild := func() {
		table.Clear()
		for x := 0; x < w; x++ {
			table.SetCell(0, x+1, tview.NewTableCell(strconv.Itoa(x%10)).
				SetAlign(tview.AlignCenter).SetTextColor(MonoGray).
				SetBackgroundColor(tcell.NewRGBColor(0x27, 0x28, 0x22)))
		}
		for y := 0; y < h; y++ {
			table.SetCell(y+1, 0, tview.NewTableCell(strconv.Itoa(y%10)).
				SetAlign(tview.AlignCenter).SetTextColor(MonoGray).
				SetBackgroundColor(tcell.NewRGBColor(0x27, 0x28, 0x22)))
			for x := 0; x < w; x++ {
				b := game.g[y][x]
				text := " "
				color := MonoGray
				bg := tcell.NewRGBColor(0x3E, 0x3D, 0x32)

				switch b.st {
				case 0:
					text = "\u2592"
					bg = tcell.NewRGBColor(0x4E, 0x4D, 0x42)
				case 2:
					text = "\u2691"
					color = MonoRed
					bg = tcell.NewRGBColor(0x3E, 0x3D, 0x32)
				case 1:
					if b.bomb {
						text = "\u2668"
						color = MonoRed
						bg = tcell.NewRGBColor(0x30, 0x20, 0x20)
					} else if b.n == 0 {
						text = " "
						bg = tcell.NewRGBColor(0x2E, 0x2F, 0x2A)
					} else {
						text = strconv.Itoa(int(b.n))
						if int(b.n) < len(numColors) {
							color = numColors[b.n]
						}
						bg = tcell.NewRGBColor(0x2E, 0x2F, 0x2A)
					}
				}

				table.SetCell(y+1, x+1, tview.NewTableCell(" "+text+" ").
					SetTextColor(color).SetBackgroundColor(bg).SetAlign(tview.AlignCenter))
			}
		}

		t := "[::d]Bombs: " + strconv.Itoa(game.bombs) + "  [::d]Flags: " + strconv.Itoa(game.flags)
		if game.over {
			t += "  [red::b]  GAME OVER  [::-]"
		} else if game.won {
			t += "  [green::b]  YOU WIN!  [::-]"
		}
		status.SetText(t)

		cx, cy := 0, 0
		r, c := table.GetSelection()
		cx, cy = c-1, r-1
		cellState := "hidden"
		if cx >= 0 && cx < w && cy >= 0 && cy < h {
			b := game.g[cy][cx]
			switch b.st {
			case 1:
				if b.bomb {
					cellState = "mine"
				} else {
					cellState = "revealed"
				}
			case 2:
				cellState = "flagged"
			}
		}
		adj := "R=reveal  F=flag  arrows=move"
		si := "[::d]Cell: [" + strconv.Itoa(cx) + "," + strconv.Itoa(cy) + "]\n"
		si += "State: " + cellState + "\n"
		si += "Bombs: " + strconv.Itoa(game.bombs) + "\n"
		si += "Flags: " + strconv.Itoa(game.flags) + "\n"
		si += "Revealed: " + strconv.Itoa(game.w*h - game.flags) + "\n"
		si += "\n[::b]Controls[::-]\n"
		si += adj + "[::-]"
		info.SetText(si)
	}

	getCell := func() (x, y int) {
		r, c := table.GetSelection()
		return c - 1, r - 1
	}

	reveal := func() {
		x, y := getCell()
		if x >= 0 && x < w && y >= 0 && y < h && game.g[y][x].st == 0 {
			game.reveal(x, y)
			game.won = game.win()
			rebuild()
		}
	}

	toggleFlag := func() {
		x, y := getCell()
		if x >= 0 && x < w && y >= 0 && y < h {
			st := &game.g[y][x].st
			if *st == 0 {
				*st = 2
				game.flags--
				rebuild()
			} else if *st == 2 {
				*st = 0
				game.flags++
				rebuild()
			}
		}
	}

	restart := func() {
		game = newMS(w, h, bombs)
		rebuild()
	}

	ga.app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyRune {
			switch ev.Rune() {
			case 'q', 'Q':
				select { case ga.stopCh <- struct{}{}: default: }
				return nil
			case 'r', 'R':
				if game.over || game.won {
					restart()
				} else {
					reveal()
				}
				return nil
			case 'f', 'F':
				if !game.over && !game.won {
					toggleFlag()
				}
				return nil
			case ' ':
				if !game.over && !game.won {
					reveal()
				}
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
	return game.won
}
