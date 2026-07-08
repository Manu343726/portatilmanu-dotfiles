package main

import (
	"os"

	"dotfilesd/plugin"
	"github.com/gdamore/tcell/v2"
	"github.com/gdamore/tcell/v2/terminfo"
	"github.com/rivo/tview"
)

type tviewPty struct {
	conn     plugin.TTYConn
	width    int
	height   int
	resizeCb func()
}

func newTviewPty(ctx plugin.Context, termWidth, termHeight int) (*tviewPty, error) {
	conn, err := ctx.PtyTtyConn()
	if err != nil {
		return nil, err
	}
	if termWidth <= 0 {
		termWidth = 132
	}
	if termHeight <= 0 {
		termHeight = 43
	}
	_ = conn.Resize(termWidth, termHeight)
	return &tviewPty{conn: conn, width: termWidth, height: termHeight}, nil
}

func (t *tviewPty) Start() error          { return nil }
func (t *tviewPty) Stop() error           { return nil }
func (t *tviewPty) Drain() error          { return nil }
func (t *tviewPty) Close() error          { return t.conn.Close() }
func (t *tviewPty) Read(b []byte) (int, error)  { return t.conn.Read(b) }
func (t *tviewPty) Write(b []byte) (int, error) { return t.conn.Write(b) }

func (t *tviewPty) NotifyResize(cb func()) { t.resizeCb = cb }

func (t *tviewPty) Resize(width, height int) {
	t.width = width
	t.height = height
	_ = t.conn.Resize(width, height)
	if t.resizeCb != nil {
		t.resizeCb()
	}
}

func (t *tviewPty) WindowSize() (tcell.WindowSize, error) {
	if w, h, err := t.conn.Getsize(); err == nil && w > 0 && h > 0 {
		t.width, t.height = w, h
		return tcell.WindowSize{Height: h, Width: w}, nil
	}
	return tcell.WindowSize{Height: t.height, Width: t.width}, nil
}

func getTerminfo() *terminfo.Terminfo {
	ti, err := terminfo.LookupTerminfo(os.Getenv("TERM"))
	if err != nil {
		ti, _ = terminfo.LookupTerminfo("xterm-256color")
	}
	return ti
}

type gameApp struct {
	app    *tview.Application
	stopCh chan struct{}
	pty    *tviewPty
}

func newGameApp(ctx plugin.Context) (*gameApp, error) {
	pty, err := newTviewPty(ctx, 0, 0)
	if err != nil {
		return nil, err
	}
	ti := getTerminfo()
	screen, err := tcell.NewTerminfoScreenFromTtyTerminfo(pty, ti)
	if err != nil {
		pty.Close()
		return nil, err
	}
	app := tview.NewApplication().SetScreen(screen)
	stopCh := make(chan struct{}, 1)
	go func() {
		<-stopCh
		app.Stop()
	}()
	return &gameApp{app: app, stopCh: stopCh, pty: pty}, nil
}

func (ga *gameApp) close() {
	ga.pty.Close()
}
