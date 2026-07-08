package main

import (
	"strconv"
	"strings"

	"dotfilesd/plugin"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func runSolitaire(ctx plugin.Context) bool {
	ga, err := newGameApp(ctx)
	if err != nil {
		return false
	}
	defer ga.close()

	game := newSol()

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

	board := tview.NewTable()
	board.SetFixed(0, 0)
	board.SetSelectable(false, false)
	board.SetBorder(true)
	board.SetBorderColor(MonoOrange)
	board.SetTitle(" [::b]SOLITAIRE[::-] ")
	board.SetTitleColor(MonoFg)
	board.SetBackgroundColor(tcell.NewRGBColor(0x27, 0x28, 0x22))

	input := tview.NewInputField()
	input.SetLabel("[::b]> [::-]")
	input.SetFieldWidth(0)
	input.SetFieldTextColor(MonoGreen)
	input.SetBackgroundColor(tcell.NewRGBColor(0x27, 0x28, 0x22))
	input.SetTitle(" Command ")
	input.SetBorder(true)
	input.SetBorderColor(MonoGray)

	cardCol := func(c card) (string, tcell.Color) {
		ss := []string{"\u2660", "\u2665", "\u2666", "\u2663"}
		rs := []string{"", "A", "2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K"}
		text := rs[c.r] + ss[c.s]
		color := tcell.NewRGBColor(0xF8, 0xF8, 0xF2)
		if c.s == 1 || c.s == 2 {
			color = MonoRed
		}
		return text, color
	}

	rebuild := func() {
		board.Clear()

		stockText := "[ ]"
		stockColor := MonoComment
		stockBg := tcell.NewRGBColor(0x3E, 0x3D, 0x32)
		if len(game.st) > 0 {
			stockText = "\u2592\u2592"
			stockColor = MonoGray
		}
		board.SetCell(0, 0, tview.NewTableCell(" "+stockText+" ").
			SetTextColor(stockColor).SetBackgroundColor(stockBg).SetAlign(tview.AlignCenter))

		wasteText := " \u2591 "
		wasteColor := MonoComment
		if len(game.wst) > 0 {
			c := game.wst[len(game.wst)-1]
			text, color := cardCol(c)
			wasteText = " " + text + " "
			wasteColor = color
		}
		board.SetCell(0, 1, tview.NewTableCell(wasteText).
			SetTextColor(wasteColor).
			SetBackgroundColor(tcell.NewRGBColor(0x3E, 0x3D, 0x32)).
			SetAlign(tview.AlignCenter))

		board.SetCell(0, 2, tview.NewTableCell(" ").
			SetTextColor(MonoComment).
			SetBackgroundColor(tcell.NewRGBColor(0x27, 0x28, 0x22)))

		for i := 0; i < 4; i++ {
			text := " \u2591 "
			color := MonoComment
			if len(game.fd[i]) > 0 {
				c := game.fd[i][len(game.fd[i])-1]
				txt, col := cardCol(c)
				text = " " + txt + " "
				color = col
			}
			board.SetCell(0, i+3, tview.NewTableCell(text).
				SetTextColor(color).
				SetBackgroundColor(tcell.NewRGBColor(0x3E, 0x3D, 0x32)).
				SetAlign(tview.AlignCenter))
		}

		maxRow := 0
		for c := 0; c < 7; c++ {
			if len(game.tb[c]) > maxRow {
				maxRow = len(game.tb[c])
			}
		}

		for c := 0; c < 7; c++ {
			for r := 0; r < len(game.tb[c]); r++ {
				card := game.tb[c][r]
				text := " \u2591 "
				color := MonoComment
				bg := tcell.NewRGBColor(0x3E, 0x3D, 0x32)
				if card.f {
					txt, col := cardCol(card)
					text = " " + txt + " "
					color = col
					bg = tcell.NewRGBColor(0x2E, 0x2F, 0x2A)
				}
				board.SetCell(r+1, c, tview.NewTableCell(text).
					SetTextColor(color).SetBackgroundColor(bg).SetAlign(tview.AlignCenter))
			}
		}

		if game.ov {
			status.SetText("[red::b]  GAME OVER  [::-]")
		} else if game.wn {
			status.SetText("[green::b]  YOU WIN!  [::-]")
		} else {
			status.SetText("[::d]Solitaire[::-]")
		}

		fd := 0
		for i := 0; i < 4; i++ {
			fd += len(game.fd[i])
		}
		tb := 0
		for i := 0; i < 7; i++ {
			tb += len(game.tb[i])
		}
		faceUp := 0
		for i := 0; i < 7; i++ {
			for _, c := range game.tb[i] {
				if c.f {
					faceUp++
				}
			}
		}

		si := "[::b]Stats[::-][::d]\n"
		si += "Stock: " + strconv.Itoa(len(game.st)) + "\n"
		si += "Waste: " + strconv.Itoa(len(game.wst)) + "\n"
		si += "Foundation: " + strconv.Itoa(fd) + "/52\n"
		si += "Tableau: " + strconv.Itoa(tb) + "\n"
		si += "Face up: " + strconv.Itoa(faceUp) + "\n"
		si += "Face down: " + strconv.Itoa(tb-faceUp) + "\n"
		si += "\n[::b]Controls[::-][::d]\n"
		si += "D=draw  Tab=focus input\n"
		si += "1-7=to foundation\n"
		si += "ex: '3 5' move 3->5\n"
		si += "ex: 'w 2' waste->col 2\n"
		si += "Q=quit[::-]"
		info.SetText(si)
	}

	lp := tview.NewBox()
	lp.SetBackgroundColor(tcell.NewRGBColor(0x27, 0x28, 0x22))
	rp := tview.NewBox()
	rp.SetBackgroundColor(tcell.NewRGBColor(0x27, 0x28, 0x22))

	center := tview.NewFlex()
	center.SetDirection(tview.FlexColumn)
	center.AddItem(lp, 0, 1, false)
	center.AddItem(board, 0, 3, true)
	center.AddItem(rp, 0, 1, false)

	main := tview.NewFlex()
	main.SetDirection(tview.FlexColumn)
	main.AddItem(center, 0, 1, true)
	main.AddItem(info, 22, 0, false)

	flex := tview.NewFlex()
	flex.SetDirection(tview.FlexRow)
	flex.AddItem(status, 1, 0, false)
	flex.AddItem(main, 0, 1, true)
	flex.AddItem(input, 3, 0, true)

	ga.app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyRune && (ev.Rune() == 'q' || ev.Rune() == 'Q') {
			select { case ga.stopCh <- struct{}{}: default: }
			return nil
		}
		if ev.Key() == tcell.KeyTab {
			ga.app.SetFocus(input)
			return nil
		}
		return ev
	})

	input.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter {
			return
		}
		cmd := strings.TrimSpace(strings.ToLower(input.GetText()))
		input.SetText("")

		if game.ov || game.wn {
			return
		}

		switch {
		case cmd == "q":
			select { case ga.stopCh <- struct{}{}: default: }
			return
		case cmd == "d":
			game.draw()
		case cmd >= "1" && cmd <= "7":
			n, _ := strconv.Atoi(cmd)
			if c := game.tp(n - 1); c != nil && c.f && game.fdx(*c) >= 0 {
				game.auto()
			}
		default:
			ps := strings.Fields(cmd)
			if len(ps) == 2 && ps[0] == "w" {
				n, _ := strconv.Atoi(ps[1])
				if len(game.wst) > 0 && game.can(game.wst[len(game.wst)-1], n-1) {
					game.tb[n-1] = append(game.tb[n-1], game.wst[len(game.wst)-1])
					game.wst = game.wst[:len(game.wst)-1]
				}
			} else if len(ps) >= 2 {
				s, _ := strconv.Atoi(ps[0])
				d, _ := strconv.Atoi(ps[1])
				n := 1
				if len(ps) >= 3 {
					n, _ = strconv.Atoi(ps[2])
				}
				game.move(s-1, d-1, n)
			}
		}
		game.auto()
		game.wn = len(game.fd[0]) == 13 && len(game.fd[1]) == 13 && len(game.fd[2]) == 13 && len(game.fd[3]) == 13
		rebuild()
	})

	ga.app.SetRoot(flex, true).SetFocus(input)
	rebuild()

	if err := ga.app.Run(); err != nil {
		return false
	}
	return game.wn
}
