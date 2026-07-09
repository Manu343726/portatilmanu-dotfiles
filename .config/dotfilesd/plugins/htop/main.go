package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"dotfilesd/plugin"
	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"
	respb "plugins/resources/proto/resources"
	"plugins/resources/proto/resources/resourcesconnect"
	pb "plugins/htop/proto/htop"
	"plugins/htop/proto/htop/htopconnect"

	"github.com/gdamore/tcell/v2"
	"github.com/gdamore/tcell/v2/terminfo"
	"github.com/rivo/tview"
	"connectrpc.com/connect"
)

type tviewPty struct {
	tty plugin.TTYConn
	cb  func()
}

func newTviewPty(tty plugin.TTYConn) *tviewPty {
	return &tviewPty{tty: tty}
}

func (p *tviewPty) Read(b []byte) (int, error)  { return p.tty.Read(b) }
func (p *tviewPty) Write(b []byte) (int, error) { return p.tty.Write(b) }
func (p *tviewPty) Close() error                { return p.tty.Close() }
func (p *tviewPty) Start() error                { return nil }
func (p *tviewPty) Stop() error                 { return nil }
func (p *tviewPty) Drain() error                { return nil }

func (p *tviewPty) WindowSize() (tcell.WindowSize, error) {
	w, h, err := p.tty.Getsize()
	if err != nil {
		return tcell.WindowSize{}, err
	}
	return tcell.WindowSize{Width: w, Height: h}, nil
}

func (p *tviewPty) NotifyResize(cb func()) {
	p.cb = cb
}

func (p *tviewPty) Resize(width, height int) {
	p.tty.Resize(width, height)
	if p.cb != nil {
		p.cb()
	}
}

type htopUI struct {
	app        *tview.Application
	flex       *tview.Flex
	headerView *tview.TextView
	procTable  *tview.Table
	footerView *tview.TextView

	mu              sync.RWMutex
	cpu             *respb.CPUSnapshot
	ram             *respb.RAMSnapshot
	loadAvg1        float64
	loadAvg5        float64
	loadAvg15       float64
	uptime          float64
	procCount       int32
	threadCount     int32
	runningProcCount int32
	processes       []*respb.ProcessInfo
	sortBy          string

	resClient resourcesconnect.ResourcesServiceClient
	stopPoll  chan struct{}
	done      chan struct{}
}

func initResourcesClient() resourcesconnect.ResourcesServiceClient {
	daemonURL := "http://127.0.0.1:9105"
	httpClient := &http.Client{}
	regClient := dotfilesdv1connect.NewPluginRegistryServiceClient(httpClient, daemonURL)
	regResp, err := regClient.GetPlugin(context.Background(), connect.NewRequest(&dotfilesdv1.RegistryGetPluginRequest{
		PluginName: "resources",
	}))
	if err != nil {
		return nil
	}
	return resourcesconnect.NewResourcesServiceClient(httpClient, regResp.Msg.Url)
}

func newHtopUI(resClient resourcesconnect.ResourcesServiceClient) *htopUI {
	h := &htopUI{
		resClient: resClient,
		sortBy:    "cpu",
		stopPoll:  make(chan struct{}),
		done:      make(chan struct{}),
	}
	h.buildUI()
	return h
}

func (h *htopUI) buildUI() {
	h.headerView = tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(false).
		SetWrap(true).
		SetWordWrap(true)

	h.procTable = tview.NewTable().
		SetSelectable(true, false).
		SetFixed(1, 0)

	h.footerView = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft).
		SetText("[::b]F6[::-]:Sort  [::b]q[::-]:Quit")

	h.procTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyF6:
			h.cycleSort()
		}
		return event
	})

	h.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(h.headerView, 5, 0, false).
		AddItem(h.procTable, 0, 1, true).
		AddItem(h.footerView, 1, 0, false)
}

func (h *htopUI) cycleSort() {
	h.mu.Lock()
	if h.sortBy == "cpu" {
		h.sortBy = "mem"
	} else {
		h.sortBy = "cpu"
	}
	h.mu.Unlock()
	h.refreshTable()
}

func (h *htopUI) start(app *tview.Application) {
	h.app = app
	go h.pollLoop()
}

func (h *htopUI) pollLoop() {
	ctx := context.Background()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Initial fetch
	h.fetchData(ctx)

	for {
		select {
		case <-h.stopPoll:
			return
		case <-ticker.C:
			h.fetchData(ctx)
		}
	}
}

func (h *htopUI) fetchData(ctx context.Context) {
	if h.resClient == nil {
		return
	}

	currentResp, err := h.resClient.Current(ctx, connect.NewRequest(&respb.CurrentRequest{}))
	if err != nil {
		return
	}

	psCount := 200
	psResp, err := h.resClient.PS(ctx, connect.NewRequest(&respb.PSRequest{
		Count: int32(psCount),
	}))
	if err != nil {
		psResp = nil
	}

	h.mu.Lock()
	if currentResp.Msg.Cpu != nil {
		h.cpu = currentResp.Msg.Cpu
	}
	if currentResp.Msg.Ram != nil {
		h.ram = currentResp.Msg.Ram
	}
	h.loadAvg1 = currentResp.Msg.LoadAverage_1
	h.loadAvg5 = currentResp.Msg.LoadAverage_5
	h.loadAvg15 = currentResp.Msg.LoadAverage_15
	h.uptime = currentResp.Msg.UptimeSeconds
	h.procCount = currentResp.Msg.ProcessCount
	h.threadCount = currentResp.Msg.ThreadCount
	h.runningProcCount = currentResp.Msg.RunningProcessCount
	if psResp != nil {
		h.processes = psResp.Msg.Processes
	}
	h.mu.Unlock()

	if h.app != nil {
		h.app.QueueUpdateDraw(func() {
			h.refreshHeader()
			h.refreshTable()
		})
	}
}

func formatUptime(seconds float64) string {
	s := int(seconds)
	d := s / 86400
	s %= 86400
	h := s / 3600
	s %= 3600
	m := s / 60
	if d > 0 {
		return fmt.Sprintf("%dd %dh%02dm", d, h, m)
	}
	return fmt.Sprintf("%dh%02dm", h, m)
}

func formatTime(jiffies int64) string {
	cs := jiffies * 10 / 100
	s := cs / 100
	cs %= 100
	m := s / 60
	s %= 60
	if m >= 60 {
		h := m / 60
		m %= 60
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d.%02d", m, s, cs)
}

func bar(pct float64, fillColor string, width int) string {
	filled := int(pct * float64(width) / 100)
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	s := "[" + fillColor + "]"
	for i := 0; i < filled; i++ {
		s += "\u2588"
	}
	s += "[#555555]"
	for i := filled; i < width; i++ {
		s += "\u2581"
	}
	s += "[default]"
	return s
}

func colorForPct(pct float64) string {
	switch {
	case pct >= 90:
		return "#E82572"
	case pct >= 70:
		return "#E8871A"
	case pct >= 50:
		return "#E6DB74"
	default:
		return "#A6E22E"
	}
}

func (h *htopUI) refreshHeader() {
	h.mu.RLock()
	cpu := h.cpu
	ram := h.ram
	la1 := h.loadAvg1
	la5 := h.loadAvg5
	la15 := h.loadAvg15
	uptime := h.uptime
	pc := h.procCount
	tc := h.threadCount
	rpc := h.runningProcCount
	h.mu.RUnlock()

	text := ""

	if cpu != nil {
		coresPerRow := 8
		ncpu := len(cpu.PerCorePercent)
		if ncpu <= coresPerRow {
			coresPerRow = ncpu
		}
		for i := 0; i < ncpu; i += coresPerRow {
			for j := 0; j < coresPerRow && i+j < ncpu; j++ {
				p := cpu.PerCorePercent[i+j]
				c := colorForPct(p)
				text += fmt.Sprintf(" %2d [%s]%s[default] [%s]%5.1f%%[default]",
					i+j, c, bar(p, c, 8), c, p)
			}
			text += "\n"
		}
	}

	if ram != nil {
		c := colorForPct(ram.Percent)
		text += fmt.Sprintf(" Mem [%s]%s[default] [%s]%5.1f%%[default]  %.1f/%.0f MB\n",
			c, bar(ram.Percent, c, 20), c, ram.Percent, ram.UsedMb, ram.TotalMb)
	}

	text += fmt.Sprintf(" Tasks: %d (%d running)  Threads: %d  Load: %.2f %.2f %.2f  Uptime: %s\n",
		pc, rpc, tc, la1, la5, la15, formatUptime(uptime))

	h.headerView.SetText(text)
}

func (h *htopUI) refreshTable() {
	h.mu.RLock()
	procs := h.processes
	sortBy := h.sortBy
	h.mu.RUnlock()

	table := h.procTable
	table.Clear()

	headers := []string{"PID", "USER", "PRI", "NI", "CPU%", "MEM%", "TIME+", "COMMAND"}
	for i, hdr := range headers {
		table.SetCell(0, i, tview.NewTableCell(hdr).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold).
			SetTextColor(tcell.ColorYellow))
	}

	sorted := make([]*respb.ProcessInfo, len(procs))
	copy(sorted, procs)

	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			var less bool
			if sortBy == "cpu" {
				less = sorted[i].CpuPercent < sorted[j].CpuPercent
			} else {
				less = sorted[i].MemPercent < sorted[j].MemPercent
			}
			if less {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	for row, p := range sorted {
		r := row + 1
		pidColor := tcell.ColorWhite
		switch p.State {
		case respb.ProcessState_PROCESS_STATE_RUNNING:
			pidColor = tcell.ColorGreen
		case respb.ProcessState_PROCESS_STATE_DISK_SLEEP:
			pidColor = tcell.ColorRed
		case respb.ProcessState_PROCESS_STATE_ZOMBIE:
			pidColor = tcell.ColorMaroon
		}

		table.SetCell(r, 0, tview.NewTableCell(fmt.Sprintf("%d", p.Pid)).
			SetAlign(tview.AlignRight).
			SetTextColor(pidColor))
		table.SetCell(r, 1, tview.NewTableCell(p.User).
			SetTextColor(tcell.ColorWhite))
		table.SetCell(r, 2, tview.NewTableCell(fmt.Sprintf("%d", p.Priority)).
			SetAlign(tview.AlignRight).
			SetTextColor(tcell.ColorWhite))
		table.SetCell(r, 3, tview.NewTableCell(fmt.Sprintf("%d", p.Nice)).
			SetAlign(tview.AlignRight).
			SetTextColor(tcell.ColorWhite))
		table.SetCell(r, 4, tview.NewTableCell(fmt.Sprintf("%.1f", p.CpuPercent)).
			SetAlign(tview.AlignRight).
			SetTextColor(tcell.ColorWhite))
		table.SetCell(r, 5, tview.NewTableCell(fmt.Sprintf("%.1f", p.MemPercent)).
			SetAlign(tview.AlignRight).
			SetTextColor(tcell.ColorWhite))
		table.SetCell(r, 6, tview.NewTableCell(formatTime(p.Time)).
			SetAlign(tview.AlignRight).
			SetTextColor(tcell.ColorWhite))
		table.SetCell(r, 7, tview.NewTableCell(p.Command).
			SetTextColor(tcell.ColorWhite))
	}

	table.ScrollToBeginning()
}

type htopServer struct {
	resClient resourcesconnect.ResourcesServiceClient
}

func (s *htopServer) Open(ctx context.Context, req *connect.Request[pb.OpenRequest]) (*connect.Response[pb.OpenResponse], error) {
	pc := plugin.ExtractContext(ctx)
	if pc == nil {
		return nil, fmt.Errorf("no plugin context")
	}

	width := int(req.Msg.TerminalWidth)
	height := int(req.Msg.TerminalHeight)
	if width <= 0 {
		width = 132
	}
	if height <= 0 {
		height = 43
	}

	tty, err := pc.PtyTtyConn()
	if err != nil {
		return nil, fmt.Errorf("PtyTtyConn: %w", err)
	}

	tty.Resize(width, height)

	term := os.Getenv("TERM")
	if term == "" {
		term = "xterm-256color"
	}
	ti, err := terminfo.LookupTerminfo(term)
	if err != nil {
		ti, err = terminfo.LookupTerminfo("xterm-256color")
		if err != nil {
			return nil, fmt.Errorf("LookupTerminfo(%q): %w", term, err)
		}
	}

	ptyWrap := newTviewPty(tty)
	screen, err := tcell.NewTerminfoScreenFromTtyTerminfo(ptyWrap, ti)
	if err != nil {
		return nil, fmt.Errorf("NewTerminfoScreen: %w", err)
	}

	ui := newHtopUI(s.resClient)
	ui.flex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 'q' {
			ui.stopPoll <- struct{}{}
			go func() {
				time.Sleep(100 * time.Millisecond)
				ui.app.Stop()
			}()
			return nil
		}
		return event
	})

	app := tview.NewApplication().SetScreen(screen).SetRoot(ui.flex, true)
	ui.start(app)

	if err := app.Run(); err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.OpenResponse{}), nil
}

func main() {
	resClient := initResourcesClient()
	if resClient == nil {
		fmt.Fprintf(os.Stderr, "htop: failed to connect to resources plugin\n")
		os.Exit(1)
	}

	svc := &htopServer{resClient: resClient}
	path, handler := htopconnect.NewHtopServiceHandler(svc)

	plugin.Serve(plugin.Config{
		Name:        "htop",
		DisplayName: "Htop",
		Version:     "1.0.0",
		Description: "Interactive process viewer using system resources",
		Services: []plugin.Service{
			{
				Name:               "htop.HtopService",
				Description:        "Full-screen process viewer TUI",
				Path:               path,
				Handler:            handler,
				PluginAccessible:   false,
				InteractiveMethods: []string{"Open"},
			},
		},
	})
}
