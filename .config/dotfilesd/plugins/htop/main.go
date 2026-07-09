package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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

func (p *tviewPty) NotifyResize(cb func()) { p.cb = cb }
func (p *tviewPty) Resize(width, height int) {
	p.tty.Resize(width, height)
	if p.cb != nil {
		p.cb()
	}
}

const monokaiBg = "#272822"
const monokaiFg = "#E8E8E2"
const cpuUserColor = "#A6E22E"
const cpuSysColor = "#E8871A"
const cpuIowColor = "#E82572"

const colPid = 0
const colUser = 1
const colPri = 2
const colNi = 3
const colThr = 4
const colState = 5
const colCpu = 6
const colMem = 7
const colIoR = 8
const colIoW = 9
const colTime = 10
const colCmd = 11

var colHeaders = []string{"PID", "USER", "PRI", "NI", "THR", "S", "CPU%", "MEM%", "IO_R", "IO_W", "TIME+", "COMMAND"}
var colAlign = []int{tview.AlignRight, tview.AlignLeft, tview.AlignRight, tview.AlignRight, tview.AlignRight, tview.AlignCenter, tview.AlignRight, tview.AlignRight, tview.AlignRight, tview.AlignRight, tview.AlignRight, tview.AlignLeft}

type sortMode int

type ioSample struct {
	readBytes  int64
	writeBytes int64
}

const (
	sortCpu sortMode = iota
	sortMem
	sortPid
	sortUser
	sortTime
	sortNice
	sortCmd
	sortIoR
	sortIoW
	sortCpuAsc
	sortMemAsc
)

func (s sortMode) String() string {
	switch s {
	case sortCpu:
		return "CPU%"
	case sortMem:
		return "MEM%"
	case sortPid:
		return "PID"
	case sortUser:
		return "USER"
	case sortTime:
		return "TIME+"
	case sortNice:
		return "NI"
	case sortIoR:
		return "IO_R"
	case sortIoW:
		return "IO_W"
	case sortCpuAsc:
		return "CPU%"
	case sortMemAsc:
		return "MEM%"
	}
	return "?"
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
	disk            *respb.DiskSnapshot
	diskIO          *respb.DiskIOSnapshot
	cpuTemp         *respb.CPUTempSnapshot
	battery         *respb.BatterySnapshot
	loadAvg1        float64
	loadAvg5        float64
	loadAvg15       float64
	uptime          float64
	procCount       int32
	threadCount     int32
	runningProcCount int32
	processes       []*respb.ProcessInfo
	sortOrder       sortMode
	filterText      string
	treeMode        bool
	ioMode          bool
	taggedPids      map[int32]bool

	prevIO  map[int32]ioSample
	ioData  map[int32]ioSample
	ppidMap map[int32]int32

	pc        plugin.Context
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

func newHtopUI(pc plugin.Context, resClient resourcesconnect.ResourcesServiceClient) *htopUI {
	h := &htopUI{
		pc:         pc,
		resClient:  resClient,
		sortOrder:  sortCpu,
		stopPoll:   make(chan struct{}),
		done:       make(chan struct{}),
		taggedPids: make(map[int32]bool),
		prevIO:     make(map[int32]ioSample),
		ioData:     make(map[int32]ioSample),
		ppidMap:    make(map[int32]int32),
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
		SetTextAlign(tview.AlignLeft)

	h.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(h.headerView, 3, 0, false).
		AddItem(h.procTable, 0, 1, true).
		AddItem(h.footerView, 1, 0, false)

	h.procTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyF2:
			h.ioMode = !h.ioMode
			if h.ioMode {
				h.mu.Lock()
				h.sortOrder = sortIoW
				h.mu.Unlock()
			}
			h.refreshTable()
			h.refreshFooter()
			return nil
		case tcell.KeyF3:
			h.showSearch()
			return nil
		case tcell.KeyF4:
			h.showFilter()
			return nil
		case tcell.KeyF5:
			h.treeMode = !h.treeMode
			h.refreshTable()
			return nil
		case tcell.KeyF6:
			h.cycleSort()
			return nil
		case tcell.KeyF7:
			h.changeNice(-1)
			return nil
		case tcell.KeyF8:
			h.changeNice(1)
			return nil
		case tcell.KeyF9:
			h.showKillMenu()
			return nil
		}
		if event.Rune() == ' ' {
			h.toggleTag()
			return nil
		}
		return event
	})
}

func (h *htopUI) toggleTag() {
	row, _ := h.procTable.GetSelection()
	h.mu.RLock()
	procs := h.processes
	h.mu.RUnlock()
	if row < 1 || row-1 >= len(procs) {
		return
	}
	pid := procs[row-1].Pid
	h.taggedPids[pid] = !h.taggedPids[pid]
	h.refreshTable()
}

func (h *htopUI) cycleSort() {
	h.mu.Lock()
	h.sortOrder = nextSort(h.sortOrder, h.ioMode)
	h.mu.Unlock()
	h.refreshTable()
}

func nextSort(current sortMode, ioMode bool) sortMode {
	if ioMode {
		switch current {
		case sortIoW:
			return sortIoR
		case sortIoR:
			return sortPid
		case sortPid:
			return sortUser
		case sortUser:
			return sortTime
		case sortTime:
			return sortCmd
		default:
			// skip CPU/MEM sorts in IO mode, go to first IO sort
			return sortIoW
		}
	}
	switch current {
	case sortCpu:
		return sortMem
	case sortMem:
		return sortPid
	case sortPid:
		return sortUser
	case sortUser:
		return sortTime
	case sortTime:
		return sortNice
	case sortNice:
		return sortCpuAsc
	case sortCpuAsc:
		return sortMemAsc
	case sortMemAsc:
		return sortCpu
	default:
		return sortCpu
	}
}

func (h *htopUI) start(app *tview.Application) {
	h.app = app
	go h.pollLoop()
}

func (h *htopUI) pollLoop() {
	ctx := context.Background()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
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

func (h *htopUI) fetchData(_ context.Context) {
	if h.resClient == nil {
		return
	}
	currentResp, err := h.resClient.Current(context.Background(), connect.NewRequest(&respb.CurrentRequest{}))
	if err != nil {
		return
	}
	psResp, err := h.resClient.PS(context.Background(), connect.NewRequest(&respb.PSRequest{Count: 500}))
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
	if currentResp.Msg.Disk != nil {
		h.disk = currentResp.Msg.Disk
	}
	if currentResp.Msg.DiskIo != nil {
		h.diskIO = currentResp.Msg.DiskIo
	}
	if currentResp.Msg.CpuTemp != nil && currentResp.Msg.CpuTemp.TempCelsius > 0 {
		h.cpuTemp = currentResp.Msg.CpuTemp
	}
	if currentResp.Msg.Battery != nil && currentResp.Msg.Battery.Percent > 0 {
		h.battery = currentResp.Msg.Battery
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
		h.collectIO(psResp.Msg.Processes)
		h.collectPPID(psResp.Msg.Processes)
	}
	h.mu.Unlock()
	if h.app != nil {
		h.app.QueueUpdateDraw(func() {
			h.refreshHeader()
			h.refreshTable()
			h.refreshFooter()
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

func formatBytesMB(mb float64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.1fG", mb/1024)
	}
	return fmt.Sprintf("%.0fM", mb)
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
	s += "[#555555]"
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

func stateChar(s respb.ProcessState) string {
	switch s {
	case respb.ProcessState_PROCESS_STATE_RUNNING:
		return "R"
	case respb.ProcessState_PROCESS_STATE_SLEEPING:
		return "S"
	case respb.ProcessState_PROCESS_STATE_DISK_SLEEP:
		return "D"
	case respb.ProcessState_PROCESS_STATE_ZOMBIE:
		return "Z"
	case respb.ProcessState_PROCESS_STATE_STOPPED:
		return "T"
	case respb.ProcessState_PROCESS_STATE_TRACE_STOP:
		return "t"
	case respb.ProcessState_PROCESS_STATE_DEAD:
		return "X"
	}
	return "?"
}

func stateColor(s respb.ProcessState) string {
	switch s {
	case respb.ProcessState_PROCESS_STATE_RUNNING:
		return "green"
	case respb.ProcessState_PROCESS_STATE_DISK_SLEEP:
		return "red"
	case respb.ProcessState_PROCESS_STATE_ZOMBIE:
		return "maroon"
	case respb.ProcessState_PROCESS_STATE_STOPPED:
		return "yellow"
	default:
		return "white"
	}
}

func (h *htopUI) refreshHeader() {
	h.mu.RLock()
	cpu := h.cpu
	ram := h.ram
	disk := h.disk
	diskIO := h.diskIO
	cpuTemp := h.cpuTemp
	battery := h.battery
	la1 := h.loadAvg1
	la5 := h.loadAvg5
	la15 := h.loadAvg15
	uptime := h.uptime
	pc := h.procCount
	tc := h.threadCount
	rpc := h.runningProcCount
	h.mu.RUnlock()

	var text string

	if cpu != nil {
		ncpu := len(cpu.PerCorePercent)
		perRow := 8
		for i := 0; i < ncpu; i += perRow {
			for j := 0; j < perRow && i+j < ncpu; j++ {
				p := cpu.PerCorePercent[i+j]
				c := colorForPct(p)
				text += fmt.Sprintf(" %2d [%s]%s[#555555] [%s]%5.1f%%[#555555]",
					i+j, c, bar(p, c, 8), c, p)
			}
			text += "\n"
		}
		userP := cpu.UserPercent
		sysP := cpu.SystemPercent
		iowP := cpu.IowaitPercent
		totalP := cpu.TotalPercent
		text += fmt.Sprintf(" CPU[%s]\u2588[#555555][%s]%5.1f%%[#555555] ", cpuUserColor, cpuUserColor, userP)
		text += fmt.Sprintf("sys[%s]%4.1f%%[#555555] ", cpuSysColor, sysP)
		text += fmt.Sprintf("io[%s]%4.1f%%[#555555] ", cpuIowColor, iowP)
		text += fmt.Sprintf("total[%s]%4.1f%%[#555555]\n", colorForPct(totalP), totalP)
	}

	if ram != nil {
		c := colorForPct(ram.Percent)
		avail := ram.TotalMb - ram.UsedMb
		text += fmt.Sprintf(" Mem [%s]%s[#555555] [%s]%5.1f%%[#555555]  used:%s avail:%s total:%s\n",
			c, bar(ram.Percent, c, 20), c, ram.Percent,
			formatBytesMB(ram.UsedMb), formatBytesMB(avail), formatBytesMB(ram.TotalMb))
	}

	if disk != nil {
		c := colorForPct(disk.Percent)
		text += fmt.Sprintf(" Disk[%s]%s[#555555] [%s]%5.1f%%[#555555]  used:%s avail:%s total:%s\n",
			c, bar(disk.Percent, c, 20), c, disk.Percent,
			formatBytesMB(disk.UsedGb*1024), formatBytesMB(disk.AvailGb*1024), formatBytesMB(disk.TotalGb*1024))
	}

	if cpuTemp != nil {
		tc := colorForPct(cpuTemp.BarPct)
		text += fmt.Sprintf(" Temp[%s]\u2588[#555555] %3.0f\u00b0C [%s]%4.0f%%[#555555]",
			tc, cpuTemp.TempCelsius, tc, cpuTemp.BarPct)
	}
	if battery != nil {
		btPct := battery.Percent
		btColor := colorForPct(100 - btPct)
		status := "BAT"
		if battery.Charging || battery.Plugged {
			status = "CHR"
		}
		if btPct >= 100 {
			status = "FUL"
		}
		text += fmt.Sprintf("  %s[%s]\u2588[#555555] %3.0f%%", status, btColor, btPct)
	}
	if cpuTemp != nil || battery != nil {
		text += "\n"
	}

	if diskIO != nil {
		var rRate, wRate string
		if diskIO.ReadBytesPerSec >= 1<<30 {
			rRate = fmt.Sprintf("%.1fGB/s", diskIO.ReadBytesPerSec/float64(1<<30))
		} else if diskIO.ReadBytesPerSec >= 1<<20 {
			rRate = fmt.Sprintf("%.1fMB/s", diskIO.ReadBytesPerSec/float64(1<<20))
		} else if diskIO.ReadBytesPerSec >= 1024 {
			rRate = fmt.Sprintf("%.0fKB/s", diskIO.ReadBytesPerSec/1024)
		} else {
			rRate = fmt.Sprintf("%.0fB/s", diskIO.ReadBytesPerSec)
		}
		if diskIO.WriteBytesPerSec >= 1<<30 {
			wRate = fmt.Sprintf("%.1fGB/s", diskIO.WriteBytesPerSec/float64(1<<30))
		} else if diskIO.WriteBytesPerSec >= 1<<20 {
			wRate = fmt.Sprintf("%.1fMB/s", diskIO.WriteBytesPerSec/float64(1<<20))
		} else if diskIO.WriteBytesPerSec >= 1024 {
			wRate = fmt.Sprintf("%.0fKB/s", diskIO.WriteBytesPerSec/1024)
		} else {
			wRate = fmt.Sprintf("%.0fB/s", diskIO.WriteBytesPerSec)
		}
		text += fmt.Sprintf("  IO R:%s W:%s\n", rRate, wRate)
	}

	totalProcs := pc
	sleeping := totalProcs - rpc
	if sleeping < 0 {
		sleeping = 0
	}
	text += fmt.Sprintf(" Tasks: %d total (%d running, %d sleeping)  Threads: %d  Load: %.2f %.2f %.2f  Uptime: %s\n",
		totalProcs, rpc, sleeping, tc, la1, la5, la15, formatUptime(uptime))

	h.headerView.SetText(text)
}

func (h *htopUI) refreshFooter() {
	treeIndicator := ""
	if h.treeMode {
		treeIndicator = " [TREE]"
	}
	ioIndicator := ""
	if h.ioMode {
		ioIndicator = " [IO]"
	}
	h.mu.RLock()
	filter := h.filterText
	h.mu.RUnlock()
	filterIndicator := ""
	if filter != "" {
		filterIndicator = fmt.Sprintf(" [FILTER: %s]", filter)
	}
	sortName := h.sortOrder.String()
	text := fmt.Sprintf("[::b]F2[::-]:IO  [::b]F3[::-]:Search  [::b]F4[::-]:Filter  [::b]F5[::-]:Tree%s  [::b]F6[::-]:Sort(%s)%s%s  [::b]F7[::-]:Nice-  [::b]F8[::-]:Nice+  [::b]F9[::-]:Kill  [::b]q[::-]:Quit",
		ioIndicator, sortName, treeIndicator, filterIndicator)
	h.footerView.SetText(text)
}

func (h *htopUI) refreshTable() {
	h.mu.RLock()
	procs := h.processes
	sortBy := h.sortOrder
	filter := h.filterText
	h.mu.RUnlock()

	table := h.procTable
	table.Clear()

	for i, hdr := range colHeaders {
		disp := hdr
		if i == colCpu && (sortBy == sortCpu || sortBy == sortCpuAsc) {
			disp += " \u25bc"
		} else if i == colMem && (sortBy == sortMem || sortBy == sortMemAsc) {
			disp += " \u25bc"
		} else if i == colPid && sortBy == sortPid {
			disp += " \u25bc"
		} else if i == colUser && sortBy == sortUser {
			disp += " \u25bc"
		} else if i == colTime && sortBy == sortTime {
			disp += " \u25bc"
		} else if i == colNi && sortBy == sortNice {
			disp += " \u25bc"
		} else if i == colIoR && sortBy == sortIoR {
			disp += " \u25bc"
		} else if i == colIoW && sortBy == sortIoW {
			disp += " \u25bc"
		} else if i == colCmd && sortBy == sortCmd {
			disp += " \u25bc"
		}
		table.SetCell(0, i, tview.NewTableCell(disp).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold).
			SetTextColor(tcell.ColorYellow).
			SetAlign(colAlign[i]))
	}

	var filtered []*respb.ProcessInfo
	for _, p := range procs {
		if filter != "" && !strings.Contains(strings.ToLower(p.Command), strings.ToLower(filter)) &&
			!strings.Contains(strings.ToLower(p.Name), strings.ToLower(filter)) {
			continue
		}
		filtered = append(filtered, p)
	}

	var sorted []*respb.ProcessInfo
	if h.treeMode {
		sorted = h.flattenTree(filtered)
	} else {
		sorted = make([]*respb.ProcessInfo, len(filtered))
		copy(sorted, filtered)
		h.sortProcesses(sorted, sortBy)
	}

	// Precompute tree indent levels
	indent := make(map[int32]int)
	if h.treeMode {
		for _, p := range sorted {
			indent[p.Pid] = getIndentLevel(p.Pid, h.ppidMap)
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
		case respb.ProcessState_PROCESS_STATE_STOPPED:
			pidColor = tcell.ColorYellow
		}

		isTagged := h.taggedPids[p.Pid]

		table.SetCell(r, colPid, tview.NewTableCell(fmt.Sprintf("%d", p.Pid)).
			SetAlign(tview.AlignRight).SetTextColor(pidColor))

		table.SetCell(r, colUser, tview.NewTableCell(truncateStr(p.User, 8)).
			SetTextColor(tcell.ColorWhite))

		table.SetCell(r, colPri, tview.NewTableCell(fmt.Sprintf("%d", p.Priority)).
			SetAlign(tview.AlignRight).SetTextColor(tcell.ColorWhite))

		niColor := tcell.ColorWhite
		if p.Nice < 0 {
			niColor = tcell.ColorRed
		} else if p.Nice > 0 {
			niColor = tcell.ColorGreen
		}
		table.SetCell(r, colNi, tview.NewTableCell(fmt.Sprintf("%d", p.Nice)).
			SetAlign(tview.AlignRight).SetTextColor(niColor))

		table.SetCell(r, colThr, tview.NewTableCell(fmt.Sprintf("%d", p.ThreadCount)).
			SetAlign(tview.AlignRight).SetTextColor(tcell.ColorWhite))

		table.SetCell(r, colState, tview.NewTableCell(stateChar(p.State)).
			SetAlign(tview.AlignCenter).SetTextColor(tcell.GetColor(stateColor(p.State))))

		cpuC := colorForPct(p.CpuPercent)
		table.SetCell(r, colCpu, tview.NewTableCell(fmt.Sprintf("%.1f", p.CpuPercent)).
			SetAlign(tview.AlignRight).SetTextColor(tcell.GetColor(cpuC)))

		table.SetCell(r, colMem, tview.NewTableCell(fmt.Sprintf("%.1f", p.MemPercent)).
			SetAlign(tview.AlignRight).SetTextColor(tcell.ColorWhite))

		ioR := h.ioData[p.Pid].readBytes
		ioW := h.ioData[p.Pid].writeBytes
		table.SetCell(r, colIoR, tview.NewTableCell(formatIORate(ioR)).
			SetAlign(tview.AlignRight).SetTextColor(tcell.ColorWhite))
		table.SetCell(r, colIoW, tview.NewTableCell(formatIORate(ioW)).
			SetAlign(tview.AlignRight).SetTextColor(tcell.ColorWhite))

		table.SetCell(r, colTime, tview.NewTableCell(formatTime(p.Time)).
			SetAlign(tview.AlignRight).SetTextColor(tcell.ColorWhite))

		cmdText := p.Command
		if cmdText == "" {
			cmdText = p.Name
		}
		if isTagged {
			cmdText = "\u2713 " + cmdText
		}
		if h.treeMode {
			lvl := indent[p.Pid]
			if lvl > 0 {
				prefix := strings.Repeat("  ", lvl)
				cmdText = prefix + "\u2514 " + cmdText
			}
		}
		table.SetCell(r, colCmd, tview.NewTableCell(cmdText).
			SetTextColor(tcell.ColorWhite))
	}
	table.ScrollToBeginning()
}

type treeNode struct {
	proc     *respb.ProcessInfo
	children []*treeNode
	level    int
}

func (h *htopUI) flattenTree(sorted []*respb.ProcessInfo) []*respb.ProcessInfo {
	// Build node map
	nodes := make(map[int32]*treeNode, len(sorted))
	for _, p := range sorted {
		nodes[p.Pid] = &treeNode{proc: p}
	}

	// Build parent-child links (iterate original list to preserve order)
	for _, p := range sorted {
		n := nodes[p.Pid]
		ppid, ok := h.ppidMap[p.Pid]
		if !ok {
			continue
		}
		parent, ok := nodes[ppid]
		if !ok || parent == n {
			continue
		}
		parent.children = append(parent.children, n)
	}

	// Roots = processes whose parent isn't in the list (preserve order)
	var roots []*treeNode
	for _, p := range sorted {
		n := nodes[p.Pid]
		ppid, ok := h.ppidMap[p.Pid]
		if !ok {
			roots = append(roots, n)
			continue
		}
		if _, found := nodes[ppid]; !found {
			roots = append(roots, n)
		}
	}

	var result []*respb.ProcessInfo
	var walk func(n *treeNode, level int)
	walk = func(n *treeNode, level int) {
		n.level = level
		result = append(result, n.proc)
		for _, c := range n.children {
			walk(c, level+1)
		}
	}
	for _, n := range roots {
		walk(n, 0)
	}

	return result
}

func (h *htopUI) sortProcesses(sorted []*respb.ProcessInfo, mode sortMode) {
	sort.SliceStable(sorted, func(i, j int) bool {
		var less bool
		switch mode {
		case sortCpu:
			less = sorted[i].CpuPercent > sorted[j].CpuPercent
		case sortMem:
			less = sorted[i].MemPercent > sorted[j].MemPercent
		case sortPid:
			less = sorted[i].Pid < sorted[j].Pid
		case sortUser:
			less = sorted[i].User < sorted[j].User
		case sortTime:
			less = sorted[i].Time > sorted[j].Time
		case sortNice:
			less = sorted[i].Nice > sorted[j].Nice
		case sortCmd:
			less = sorted[i].Command < sorted[j].Command
		case sortIoR:
			less = h.ioData[sorted[i].Pid].readBytes > h.ioData[sorted[j].Pid].readBytes
		case sortIoW:
			less = h.ioData[sorted[i].Pid].writeBytes > h.ioData[sorted[j].Pid].writeBytes
		case sortCpuAsc:
			less = sorted[i].CpuPercent < sorted[j].CpuPercent
		case sortMemAsc:
			less = sorted[i].MemPercent < sorted[j].MemPercent
		}
		return less
	})
}

func (h *htopUI) showSearch() {
	h.showInputDialog("Search")
}

func (h *htopUI) showFilter() {
	h.showInputDialog("Filter")
}

func (h *htopUI) showInputDialog(title string) {
	var inputField *tview.InputField
	inputField = tview.NewInputField().
		SetLabel(title + ": ").
		SetFieldWidth(30).
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEnter {
				h.mu.Lock()
				h.filterText = inputField.GetText()
				h.mu.Unlock()
				h.refreshTable()
				h.refreshFooter()
			}
			h.app.SetRoot(h.flex, true)
		})
	box := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(inputField, 1, 0, true).
			AddItem(nil, 0, 1, false), 40, 0, true).
		AddItem(nil, 0, 1, false)
	h.app.SetRoot(box, true)
}

func (h *htopUI) changeNice(delta int) {
	h.mu.RLock()
	procs := h.processes
	h.mu.RUnlock()

	row, _ := h.procTable.GetSelection()
	if row < 1 || row-1 >= len(procs) {
		return
	}
	p := procs[row-1]
	newNice := int(p.Nice) + delta
	if newNice < -20 {
		newNice = -20
	}
	if newNice > 19 {
		newNice = 19
	}
	if h.pc != nil {
		cmd := fmt.Sprintf("renice %d %d", newNice, p.Pid)
		h.pc.Exec(cmd)
	}
}

func (h *htopUI) showKillMenu() {
	h.mu.RLock()
	procs := h.processes
	h.mu.RUnlock()

	row, _ := h.procTable.GetSelection()
	if row < 1 || row-1 >= len(procs) {
		return
	}
	p := procs[row-1]

	signals := []struct {
		label string
		sig   int
	}{
		{"SIGTERM (15)", 15},
		{"SIGKILL (9)", 9},
		{"SIGINT (2)", 2},
		{"SIGHUP (1)", 1},
		{"SIGQUIT (3)", 3},
		{"SIGSTOP (19)", 19},
		{"SIGCONT (18)", 18},
	}

	list := tview.NewList()
	list.SetTitle(fmt.Sprintf(" Kill PID %d (%s) ", p.Pid, truncateStr(p.Command, 30))).
		SetTitleAlign(tview.AlignLeft).
		SetBorder(true)

	for _, s := range signals {
		s := s
		list.AddItem(s.label, "", 0, func() {
			if h.pc != nil {
				cmd := fmt.Sprintf("kill -%d %d", s.sig, p.Pid)
				h.pc.Exec(cmd)
			}
			h.app.SetRoot(h.flex, true)
		})
	}
	list.AddItem("Cancel", "", 0, func() {
		h.app.SetRoot(h.flex, true)
	})

	h.app.SetRoot(list, true)
}

func (h *htopUI) collectIO(procs []*respb.ProcessInfo) {
	now := make(map[int32]ioSample, len(procs))
	for _, p := range procs {
		sample, err := readProcIO(int(p.Pid))
		if err != nil {
			continue
		}
		now[p.Pid] = sample
	}
	// Calculate rate (bytes/sec) from delta since last poll
	h.ioData = make(map[int32]ioSample, len(now))
	for pid, cur := range now {
		prev, ok := h.prevIO[pid]
		if !ok {
			h.ioData[pid] = cur
		} else {
			rd := cur.readBytes - prev.readBytes
			wd := cur.writeBytes - prev.writeBytes
			if rd < 0 {
				rd = 0
			}
			if wd < 0 {
				wd = 0
			}
			h.ioData[pid] = ioSample{readBytes: rd / 2, writeBytes: wd / 2}
		}
	}
	h.prevIO = now
}

func (h *htopUI) collectPPID(procs []*respb.ProcessInfo) {
	m := make(map[int32]int32, len(procs))
	for _, p := range procs {
		ppid, err := readProcPPID(int(p.Pid))
		if err == nil {
			m[p.Pid] = ppid
		}
	}
	h.ppidMap = m
}

func readProcPPID(pid int) (int32, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, err
	}
	s := string(data)
	// Find the closing paren of comm field, then skip the space + state char
	i := strings.LastIndex(s, ")")
	if i < 0 {
		return 0, fmt.Errorf("bad stat format")
	}
	rest := strings.Fields(s[i+2:]) // skip ") "
	if len(rest) < 2 {
		return 0, fmt.Errorf("bad stat format")
	}
	ppid, err := strconv.ParseInt(rest[1], 10, 32)
	if err != nil {
		return 0, err
	}
	return int32(ppid), nil
}

func readProcIO(pid int) (ioSample, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "io"))
	if err != nil {
		return ioSample{}, err
	}
	var s ioSample
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "read_bytes:") {
			s.readBytes, _ = strconv.ParseInt(strings.TrimSpace(line[11:]), 10, 64)
		} else if strings.HasPrefix(line, "write_bytes:") {
			s.writeBytes, _ = strconv.ParseInt(strings.TrimSpace(line[12:]), 10, 64)
		}
	}
	return s, nil
}

func formatIORate(bytes int64) string {
	if bytes <= 0 {
		return "0"
	}
	b := float64(bytes)
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fG", b/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fM", b/float64(1<<20))
	case b >= 1024:
		return fmt.Sprintf("%.0fK", b/1024)
	default:
		return fmt.Sprintf("%.0f", b)
	}
}

func getIndentLevel(pid int32, ppidMap map[int32]int32) int {
	level := 0
	seen := make(map[int32]bool)
	current := pid
	for {
		if seen[current] {
			return level
		}
		seen[current] = true
		ppid, ok := ppidMap[current]
		if !ok || ppid == 0 || ppid == current {
			return level
		}
		if _, ok := ppidMap[ppid]; !ok {
			return level
		}
		current = ppid
		level++
		if level > 100 {
			return level
		}
	}
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\u2026"
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
			return nil, fmt.Errorf("LookupTerminfo: %w", err)
		}
	}

	ptyWrap := newTviewPty(tty)
	screen, err := tcell.NewTerminfoScreenFromTtyTerminfo(ptyWrap, ti)
	if err != nil {
		return nil, fmt.Errorf("NewTerminfoScreen: %w", err)
	}

	ui := newHtopUI(pc, s.resClient)

	ui.flex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 'q' || event.Rune() == 'Q' {
			ui.stopPoll <- struct{}{}
			go func() {
				time.Sleep(100 * time.Millisecond)
				ui.app.Stop()
			}()
			return nil
		}
		if event.Rune() == 'h' || event.Rune() == 'H' || event.Key() == tcell.KeyF1 {
			ui.showHelp()
			return nil
		}
		if event.Key() == tcell.KeyUp {
			r, _ := ui.procTable.GetSelection()
			if r > 1 {
				ui.procTable.Select(r-1, 0)
			}
			return nil
		}
		if event.Key() == tcell.KeyDown {
			r, _ := ui.procTable.GetSelection()
			ui.procTable.Select(r+1, 0)
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

func (h *htopUI) showHelp() {
	helpText := `[yellow]htop[/] — interactive process viewer

[yellow]Keys:[/]
  [green]F1 / h[/]      Help
  [green]F2[/]          Toggle I/O mode (per-process read/write rates)
  [green]F3[/]          Search by name
  [green]F4[/]          Filter by name
  [green]F5[/]          Toggle tree view
  [green]F6[/]          Cycle sort order
  [green]F7[/]          Decrease nice value (+1)
  [green]F8[/]          Increase nice value (-1)
  [green]F9[/]          Kill process menu
  [green]Space[/]        Tag/un-tag process
  [green]q[/]            Quit

[yellow]Columns:[/]
  PID    Process ID
  USER   Owner
  PRI    Kernel priority
  NI     Nice value
  THR    Thread count
  S      State (R=run, S=sleep, D=disk, Z=zombie, T=stop)
  CPU%   CPU usage
  MEM%   Memory usage
  IO_R   Disk read rate (B/s)
  IO_W   Disk write rate (B/s)
  TIME+  Cumulative CPU time
  COMMAND Command line

[green]Press any key to close[/]`

	textView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(helpText)

	textView.SetBorder(true).SetTitle(" Help ").SetTitleAlign(tview.AlignLeft)
	textView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		h.app.SetRoot(h.flex, true)
		return nil
	})
	textView.SetDoneFunc(func(key tcell.Key) {
		h.app.SetRoot(h.flex, true)
	})

	h.app.SetRoot(textView, true)
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
		Version:     "2.0.0",
		Description: "Interactive process viewer — htop clone with CPU, Mem, Disk, Temp, Battery meters",
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
