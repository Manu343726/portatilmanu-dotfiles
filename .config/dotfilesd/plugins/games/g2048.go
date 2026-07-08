package main

import (
	"strconv"

	"dotfilesd/plugin"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var tileColors = map[int]tcell.Color{
	2:    tcell.NewRGBColor(0xA6, 0xE2, 0x2E),
	4:    tcell.NewRGBColor(0x66, 0xD9, 0xEF),
	8:    tcell.NewRGBColor(0xFD, 0x97, 0x1F),
	16:   tcell.NewRGBColor(0xF9, 0x26, 0x72),
	32:   tcell.NewRGBColor(0xAE, 0x81, 0xFF),
	64:   tcell.NewRGBColor(0xE6, 0xDB, 0x74),
	128:  tcell.NewRGBColor(0xFD, 0x97, 0x1F),
	256:  tcell.NewRGBColor(0xF9, 0x26, 0x72),
	512:  tcell.NewRGBColor(0xAE, 0x81, 0xFF),
	1024: tcell.NewRGBColor(0xE6, 0xDB, 0x74),
	2048: tcell.NewRGBColor(0xA6, 0xE2, 0x2E),
}

var tileBGs = map[int]tcell.Color{
	2:    tcell.NewRGBColor(0x2E, 0x3E, 0x20),
	4:    tcell.NewRGBColor(0x20, 0x2E, 0x3E),
	8:    tcell.NewRGBColor(0x3E, 0x2E, 0x20),
	16:   tcell.NewRGBColor(0x3E, 0x20, 0x28),
	32:   tcell.NewRGBColor(0x2E, 0x20, 0x3E),
	64:   tcell.NewRGBColor(0x3E, 0x3E, 0x20),
	128:  tcell.NewRGBColor(0x3E, 0x2E, 0x20),
	256:  tcell.NewRGBColor(0x3E, 0x20, 0x28),
	512:  tcell.NewRGBColor(0x2E, 0x20, 0x3E),
	1024: tcell.NewRGBColor(0x3E, 0x3E, 0x20),
	2048: tcell.NewRGBColor(0x2E, 0x3E, 0x20),
}

func tileColor(v int) tcell.Color {
	if c, ok := tileColors[v]; ok {
		return c
	}
	return MonoGray
}

func tileBG(v int) tcell.Color {
	if c, ok := tileBGs[v]; ok {
		return c
	}
	return tcell.NewRGBColor(0x2E, 0x2F, 0x2A)
}

func run2048(ctx plugin.Context) bool {
	ga, err := newGameApp(ctx)
	if err != nil {
		return false
	}
	defer ga.close()

	s := new2048()

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

	table := tview.NewTable()
	table.SetFixed(0, 0)
	table.SetSelectable(false, false)
	table.SetBorder(true)
	table.SetBorderColor(MonoOrange)
	table.SetTitle(" [::b]2048[::-] ")
	table.SetTitleColor(MonoFg)
	table.SetBackgroundColor(tcell.NewRGBColor(0x27, 0x28, 0x22))

	rebuild := func() {
		table.Clear()
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				v := s.g[y][x]
				text := "    "
				color := tcell.NewRGBColor(0x40, 0x40, 0x35)
				bg := tcell.NewRGBColor(0x2E, 0x2F, 0x2A)
				if v > 0 {
					text = " " + strconv.Itoa(v)
					for len(text) < 4 {
						text += " "
					}
					color = tileColor(v)
					bg = tileBG(v)
				}
				table.SetCell(y, x, tview.NewTableCell(text).
					SetTextColor(color).SetBackgroundColor(bg).SetAlign(tview.AlignCenter))
			}
		}

		t := "[::d]Score: " + strconv.Itoa(s.sc)
		if s.ov {
			t += "  [red::b]  GAME OVER  [::-]"
		} else if s.wn {
			t += "  [green::b]  YOU WIN!  [::-]"
		}
		status.SetText(t)

		maxTile := 0
		empty := 0
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				v := s.g[y][x]
				if v > maxTile {
					maxTile = v
				}
				if v == 0 {
					empty++
				}
			}
		}
		si := "[::b]Stats[::-][::d]\n"
		si += "Score: " + strconv.Itoa(s.sc) + "\n"
		si += "Max tile: " + strconv.Itoa(maxTile) + "\n"
		si += "Empty cells: " + strconv.Itoa(empty) + "/16\n"
		si += "Target: 2048\n"
		si += "\n[::b]Controls[::-][::d]\n"
		si += "arrows/WASD=slide\nR(over)=restart\nQ=quit[::-]"
		info.SetText(si)
	}

	handleMove := func(dx, dy int) {
		if s.ov || s.wn {
			return
		}
		if s.mv(dx, dy) {
			s.sp()
			if !s.can() {
				s.ov = true
			}
			check2048Win(s)
			rebuild()
		}
	}

	ga.app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyRune {
			switch ev.Rune() {
			case 'q', 'Q':
				select { case ga.stopCh <- struct{}{}: default: }
				return nil
			case 'r', 'R':
				if s.ov || s.wn {
					s = new2048()
					rebuild()
				}
				return nil
			case 'w', 'W':
				handleMove(0, 1)
				return nil
			case 's', 'S':
				handleMove(0, -1)
				return nil
			case 'a', 'A':
				handleMove(1, 0)
				return nil
			case 'd', 'D':
				handleMove(-1, 0)
				return nil
			}
		} else {
			switch ev.Key() {
			case tcell.KeyUp:
				handleMove(0, 1)
				return nil
			case tcell.KeyDown:
				handleMove(0, -1)
				return nil
			case tcell.KeyLeft:
				handleMove(1, 0)
				return nil
			case tcell.KeyRight:
				handleMove(-1, 0)
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
	return s.wn
}

func check2048Win(s *g2048State) {
	if s.wn {
		return
	}
	for y := 0; y < 4 && !s.wn; y++ {
		for x := 0; x < 4 && !s.wn; x++ {
			if s.g[y][x] >= 2048 {
				s.wn = true
			}
		}
	}
}
