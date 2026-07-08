package main

import (
	"math/rand"
	"strconv"

	"dotfilesd/plugin"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func runBattleship(ctx plugin.Context) bool {
	ga, err := newGameApp(ctx)
	if err != nil {
		return false
	}
	defer ga.close()

	game := newBS()
	sz := []int{5, 4, 3, 3, 2}
	shipNames := []string{"Carrier(5)", "Battleship(4)", "Cruiser(3)", "Submarine(3)", "Destroyer(2)"}

	cursorStyle := tcell.StyleDefault.Background(tcell.NewRGBColor(0x50, 0x50, 0x40))

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

	playerBoard := tview.NewTable()
	playerBoard.SetFixed(1, 1)
	playerBoard.SetSelectable(true, true)
	playerBoard.SetSelectedStyle(cursorStyle)
	playerBoard.SetBorder(true)
	playerBoard.SetBorderColor(MonoBlue)
	playerBoard.SetTitle(" [::b]Your Fleet[::-] ")
	playerBoard.SetTitleColor(MonoBlue)
	playerBoard.SetBackgroundColor(tcell.NewRGBColor(0x27, 0x28, 0x22))

	enemyBoard := tview.NewTable()
	enemyBoard.SetFixed(1, 1)
	enemyBoard.SetSelectable(true, true)
	enemyBoard.SetSelectedStyle(cursorStyle)
	enemyBoard.SetBorder(true)
	enemyBoard.SetBorderColor(MonoRed)
	enemyBoard.SetTitle(" [::b]Enemy Waters[::-] ")
	enemyBoard.SetTitleColor(MonoRed)
	enemyBoard.SetBackgroundColor(tcell.NewRGBColor(0x27, 0x28, 0x22))

	horizontal := true

	setCell := func(t *tview.Table, row, col int, text string, color tcell.Color) {
		t.SetCell(row, col, tview.NewTableCell(text).
			SetTextColor(color).SetAlign(tview.AlignCenter))
	}

	drawBoard := func(g *[10][10]int8, t *tview.Table, hide bool) {
		t.Clear()
		for x := 0; x < 10; x++ {
			setCell(t, 0, x+1, " "+string(rune('A'+x))+" ", MonoGray)
		}
		for y := 0; y < 10; y++ {
			setCell(t, y+1, 0, " "+string(rune('1'+y))+" ", MonoGray)
			for x := 0; x < 10; x++ {
				text := " . "
				c := MonoComment
				bg := tcell.NewRGBColor(0x2E, 0x2F, 0x2A)
				switch g[y][x] {
				case 1:
					if !hide {
						text = " \u2588 "
						c = MonoBlue
						bg = tcell.NewRGBColor(0x20, 0x30, 0x28)
					}
				case 2:
					text = " \u2717 "
					c = MonoRed
					bg = tcell.NewRGBColor(0x30, 0x20, 0x20)
				case 3:
					text = " \u25CB "
					c = MonoComment
					bg = tcell.NewRGBColor(0x20, 0x20, 0x20)
				}
				t.SetCell(y+1, x+1, tview.NewTableCell(text).
					SetTextColor(c).SetBackgroundColor(bg).SetAlign(tview.AlignCenter))
			}
		}
	}

	playerFlex := tview.NewFlex()
	playerFlex.SetDirection(tview.FlexRow)
	playerFlex.AddItem(playerBoard, 0, 1, true)

	enemyFlex := tview.NewFlex()
	enemyFlex.SetDirection(tview.FlexRow)
	enemyFlex.AddItem(enemyBoard, 0, 1, true)

	spacer := tview.NewTextView()
	spacer.SetText(" ")
	spacer.SetBackgroundColor(tcell.NewRGBColor(0x1E, 0x1F, 0x1C))

	allBoards := tview.NewFlex()
	allBoards.SetDirection(tview.FlexColumn)
	allBoards.AddItem(playerFlex, 0, 1, false)
	allBoards.AddItem(spacer, 0, 0, false)
	allBoards.AddItem(enemyFlex, 0, 1, false)
	allBoards.SetBackgroundColor(tcell.NewRGBColor(0x1E, 0x1F, 0x1C))

	boardWrap := tview.NewFlex()
	boardWrap.SetDirection(tview.FlexRow)
	boardWrap.AddItem(allBoards, 0, 0, true)

	lp := tview.NewBox()
	lp.SetBackgroundColor(tcell.NewRGBColor(0x27, 0x28, 0x22))
	rp := tview.NewBox()
	rp.SetBackgroundColor(tcell.NewRGBColor(0x27, 0x28, 0x22))

	center := tview.NewFlex()
	center.SetDirection(tview.FlexColumn)
	center.AddItem(lp, 0, 1, false)
	center.AddItem(boardWrap, 0, 3, true)
	center.AddItem(rp, 0, 1, false)

	pages := tview.NewPages()

	previewShip := func() {
		drawBoard(&game.pg, playerBoard, false)
		r, c := playerBoard.GetSelection()
		px, py := c-1, r-1
		for i := 0; i < sz[game.ph]; i++ {
			xx, yy := px, py
			if horizontal {
				xx += i
			} else {
				yy += i
			}
			if xx >= 0 && xx < 10 && yy >= 0 && yy < 10 {
				playerBoard.SetCell(yy+1, xx+1, tview.NewTableCell(" \u2588 ").
					SetTextColor(MonoGreen).SetBackgroundColor(tcell.NewRGBColor(0x28, 0x38, 0x20)).
					SetAlign(tview.AlignCenter))
			}
		}
		status.SetText("[::b]Place your " + shipNames[game.ph] + "[::-]")
		dir := "H"
		if !horizontal {
			dir = "V"
		}
		si := "[::b]Placement Phase[::-][::d]\n"
		si += "Ship: " + shipNames[game.ph] + "\n"
		si += "Direction: " + dir + "\n"
		si += "Ships placed: " + strconv.Itoa(game.ph) + "/5\n"
		si += "\n[::b]Fleet[::-][::d]\n"
		pl := []string{"Carrier", "Battleship", "Cruiser", "Submarine", "Destroyer"}
		for i := 0; i < 5; i++ {
			mark := " "
			if i < game.ph {
				mark = "[green::b]\u2713[::-][::d]"
			}
			si += mark + " " + pl[i] + "\n"
		}
		si += "\n[::b]Controls[::-][::d]\n"
		si += "H/V=dir  Enter=place\nR=random  Q=quit[::-]"
		info.SetText(si)
	}

	updateBattleInfo := func() {
		phits, pmisses := 0, 0
		ehits, emisses := 0, 0
		for y := 0; y < 10; y++ {
			for x := 0; x < 10; x++ {
				if game.pg[y][x] == 2 {
					phits++
				} else if game.pg[y][x] == 3 {
					pmisses++
				}
				if game.ag[y][x] == 2 {
					ehits++
				} else if game.ag[y][x] == 3 {
					emisses++
				}
			}
		}
		si := "[::b]Battle Stats[::-][::d]\n"
		si += "Your hits: " + strconv.Itoa(phits) + "\n"
		si += "Your misses: " + strconv.Itoa(pmisses) + "\n"
		si += "Enemy hits: " + strconv.Itoa(ehits) + "\n"
		si += "Enemy misses: " + strconv.Itoa(emisses) + "\n"
		si += "\n[::b]Controls[::-][::d]\n"
		si += "arrows=aim  Enter=fire\nQ=quit[::-]"
		info.SetText(si)
	}

	beginBattle := func() {
		playerBoard.SetSelectable(false, false)
		drawBoard(&game.pg, playerBoard, false)
		drawBoard(&game.ag, enemyBoard, true)
		status.SetText("[::b]Fire at enemy![::-]")
		pages.SwitchToPage("battle")
		ga.app.SetFocus(enemyBoard)
		updateBattleInfo()
	}

	endGame := func(won bool) {
		if won {
			status.SetText("[green::b]  All ships sunk! You win!  [::-]")
		} else {
			status.SetText("[red::b]  Fleet destroyed! You lose!  [::-]")
		}
		drawBoard(&game.ag, enemyBoard, false)
		drawBoard(&game.pg, playerBoard, false)
		pages.SwitchToPage("battle")
		updateBattleInfo()
	}

	placementMain := tview.NewFlex()
	placementMain.SetDirection(tview.FlexColumn)
	placementMain.AddItem(center, 0, 1, true)
	placementMain.AddItem(info, 22, 0, false)

	placementFlex := tview.NewFlex()
	placementFlex.SetDirection(tview.FlexRow)
	placementFlex.AddItem(status, 1, 0, false)
	placementFlex.AddItem(placementMain, 0, 1, true)

	battleCenter := tview.NewFlex()
	battleCenter.SetDirection(tview.FlexColumn)
	battleCenter.AddItem(lp, 0, 1, false)
	battleCenter.AddItem(boardWrap, 0, 3, true)
	battleCenter.AddItem(rp, 0, 1, false)

	battleMain := tview.NewFlex()
	battleMain.SetDirection(tview.FlexColumn)
	battleMain.AddItem(battleCenter, 0, 1, true)
	battleMain.AddItem(info, 22, 0, false)

	battleFlex := tview.NewFlex()
	battleFlex.SetDirection(tview.FlexRow)
	battleFlex.AddItem(status, 1, 0, false)
	battleFlex.AddItem(battleMain, 0, 1, true)

	pages.AddPage("placement", placementFlex, true, true)
	pages.AddPage("battle", battleFlex, true, false)

	getSel := func() (x, y int) {
		r, c := enemyBoard.GetSelection()
		return c - 1, r - 1
	}

	fire := func() {
		if game.ov || game.wn {
			return
		}
		x, y := getSel()
		if x < 0 || x >= 10 || y < 0 || y >= 10 || game.ag[y][x] >= 2 {
			return
		}
		if game.ag[y][x] == 1 {
			game.ag[y][x] = 2
		} else {
			game.ag[y][x] = 3
		}
		if game.sunk(&game.ag) {
			game.wn = true
			endGame(true)
			return
		}
		for {
			ax, ay := rand.Intn(10), rand.Intn(10)
			if game.pg[ay][ax] < 2 {
				if game.pg[ay][ax] == 1 {
					game.pg[ay][ax] = 2
				} else {
					game.pg[ay][ax] = 3
				}
				if game.sunk(&game.pg) {
					game.ov = true
				}
				break
			}
		}
		drawBoard(&game.pg, playerBoard, false)
		drawBoard(&game.ag, enemyBoard, true)
		updateBattleInfo()
		if game.ov {
			endGame(false)
		}
	}

	ga.app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if game.ph < 5 {
			switch ev.Key() {
			case tcell.KeyEnter:
				r, c := playerBoard.GetSelection()
				px, py := c-1, r-1
				if px >= 0 && py >= 0 && game.can(&game.pg, px, py, sz[game.ph], horizontal) {
					game.p(&game.pg, px, py, sz[game.ph], horizontal)
					game.ph++
					if game.ph >= 5 {
						beginBattle()
					} else {
						previewShip()
					}
				}
				return nil
			case tcell.KeyRune:
				switch ev.Rune() {
				case 'q', 'Q':
					select { case ga.stopCh <- struct{}{}: default: }
					return nil
				case 'h', 'H':
					_, c := playerBoard.GetSelection()
					px := c - 1
					if px+sz[game.ph] <= 10 {
						horizontal = true
						previewShip()
					}
					return nil
				case 'v', 'V':
					r, _ := playerBoard.GetSelection()
					py := r - 1
					if py+sz[game.ph] <= 10 {
						horizontal = false
						previewShip()
					}
					return nil
				case 'r', 'R':
					for i := game.ph; i < 5; i++ {
						for {
							rx, ry := rand.Intn(10), rand.Intn(10)
							rh := rand.Intn(2) == 0
							if game.can(&game.pg, rx, ry, sz[i], rh) {
								game.p(&game.pg, rx, ry, sz[i], rh)
								break
							}
						}
					}
					game.ph = 5
					beginBattle()
					return nil
				}
			}
		} else {
			switch ev.Key() {
			case tcell.KeyEnter:
				fire()
				return nil
			case tcell.KeyRune:
				if ev.Rune() == 'q' || ev.Rune() == 'Q' {
					select { case ga.stopCh <- struct{}{}: default: }
					return nil
				}
			}
		}
		return ev
	})

	previewShip()
	ga.app.SetRoot(pages, true)

	if err := ga.app.Run(); err != nil {
		return false
	}
	return game.wn
}
