package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"dotfilesd/plugin"
	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"
	respb "plugins/resources/proto/resources"
	"plugins/resources/proto/resources/resourcesconnect"
	pb "plugins/tmuxbar/proto/tmuxbar"
	"plugins/tmuxbar/proto/tmuxbar/tmuxbarconnect"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
)

const barFilled = "◼"
const barEmpty = "◻"
const barSegments = 10

type tmuxBarServer struct {
	resourcesClient resourcesconnect.ResourcesServiceClient
	cache           widgetCache
	hostname        string
	username        string
	root            string

	mu         sync.RWMutex
	priorities map[widgetKey]int
}

type widgetCache struct {
	mu   sync.RWMutex
	data *respb.CurrentResponse
}

func (c *widgetCache) get() *respb.CurrentResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data
}

func (c *widgetCache) set(data *respb.CurrentResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = data
}

// stripTmuxStyles removes tmux #[...] color/style sequences.
func stripTmuxStyles(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '#' && i+1 < len(s) && s[i+1] == '[' {
			for j := i + 2; j < len(s); j++ {
				if s[j] == ']' {
					i = j
					break
				}
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// visibleWidth returns the number of terminal columns s occupies.
// Counts runes (all our glyphs are single-column in modern terminals).
func visibleWidth(s string) int {
	return len([]rune(stripTmuxStyles(s)))
}

func batteryStatusString(s respb.BatteryStatus) string {
	switch s {
	case respb.BatteryStatus_BATTERY_STATUS_CHARGING:
		return "Charging"
	case respb.BatteryStatus_BATTERY_STATUS_DISCHARGING:
		return "Discharging"
	case respb.BatteryStatus_BATTERY_STATUS_FULL:
		return "Full"
	case respb.BatteryStatus_BATTERY_STATUS_NOT_CHARGING:
		return "Not charging"
	default:
		return "Unknown"
	}
}

func bar(pct int) string {
	n := barSegments
	filled := (pct*n + 99) / 100
	if filled > n {
		filled = n
	}
	return strings.Repeat(barFilled, filled) + strings.Repeat(barEmpty, n-filled)
}

func pctColor(pct int) string {
	switch {
	case pct < 25:
		return "#[fg=#A6E22E]"
	case pct < 50:
		return "#[fg=#E6DB74]"
	case pct < 75:
		return "#[fg=#E8871A]"
	default:
		return "#[fg=#E82572]"
	}
}

func batteryBar(pct int) string {
	var b strings.Builder
	n := barSegments
	for i := 0; i < n; i++ {
		if i*100 < pct*n {
			switch {
			case i < 2:
				b.WriteString("#[fg=#E82572]")
			case i < 4:
				b.WriteString("#[fg=#E8871A]")
			case i < 6:
				b.WriteString("#[fg=#E6DB74]")
			default:
				b.WriteString("#[fg=#A6E22E]")
			}
			b.WriteString(barFilled)
		} else {
			b.WriteString("#[default]" + barEmpty)
		}
	}
	b.WriteString("#[default]")
	return b.String()
}

func batteryLabelColor(pct int) string {
	switch {
	case pct < 25:
		return "#E82572"
	case pct < 50:
		return "#E8871A"
	case pct < 75:
		return "#E6DB74"
	default:
		return "#A6E22E"
	}
}

func formatDuration(m int) string {
	if m <= 0 {
		return "<1m"
	}
	if m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	h := m / 60
	m = m % 60
	return fmt.Sprintf("%dh%dm", h, m)
}

type widgetKey string

const (
	widgetCPUPercent widgetKey = "cpu_percent"
	widgetCPUProc    widgetKey = "cpu_process"
	widgetRAMPercent widgetKey = "ram_percent"
	widgetRAMProc    widgetKey = "ram_process"
	widgetBattery    widgetKey = "battery"
	widgetTemp       widgetKey = "temp"
	widgetLayout     widgetKey = "layout"
	widgetPower      widgetKey = "power_profile"
	widgetGPU        widgetKey = "gpu_profile"
	widgetWiFiPercent widgetKey = "wifi_percent"
	widgetWiFiSSID   widgetKey = "wifi_ssid"
	widgetTime       widgetKey = "time"
	widgetDate       widgetKey = "date"
	widgetUser       widgetKey = "user"
	widgetHost       widgetKey = "hostname"
)

// defaultPriorities returns the default display order of the bar segments.
// Lower values are rendered first (to the left) and survive width truncation
// first; higher values are dropped first when the bar runs out of space.
func defaultPriorities() map[widgetKey]int {
	return map[widgetKey]int{
		widgetCPUPercent:  0,
		widgetRAMPercent:  1,
		widgetBattery:     2,
		widgetTemp:        3,
		widgetLayout:      4,
		widgetPower:       14,
		widgetGPU:         14,
		widgetWiFiPercent: 15,
		widgetCPUProc:     16,
		widgetRAMProc:     16,
		widgetWiFiSSID:    17,
		widgetTime:        18,
		widgetDate:        19,
		widgetUser:        20,
		widgetHost:        20,
	}
}

// renderCtx carries the data every bar segment needs to render.
type renderCtx struct {
	r        *respb.CurrentResponse
	timeStr  string
	dateStr  string
	username string
	root     string
	hostname string
}

type barSegment struct {
	key           widgetKey
	renderFull    func(*renderCtx) string
	renderCompact func(*renderCtx) string
}

func segCPUPercent(c *renderCtx) string {
	r := c.r
	if r.Cpu == nil {
		return ""
	}
	pct := int(r.Cpu.TotalPercent)
	return fmt.Sprintf("CPU %s%d%% %s#[default] ", pctColor(pct), pct, bar(pct))
}

func segCPUCompact(c *renderCtx) string {
	if c.r.Cpu == nil {
		return ""
	}
	pct := int(c.r.Cpu.TotalPercent)
	return fmt.Sprintf("CPU %s%d%%#[default] ", pctColor(pct), pct)
}

func segCPUProc(c *renderCtx) string {
	if c.r.Cpu == nil || c.r.TopCpuProcess == "" {
		return ""
	}
	return fmt.Sprintf("(%s) ", c.r.TopCpuProcess)
}

func segRAMPercent(c *renderCtx) string {
	r := c.r
	if r.Ram == nil {
		return ""
	}
	pct := int(r.Ram.Percent)
	usedGiB := r.Ram.UsedMb / 1024
	return fmt.Sprintf("RAM %s%.2fGiB %d%% %s#[default] ", pctColor(pct), usedGiB, pct, bar(pct))
}

func segRAMCompact(c *renderCtx) string {
	if c.r.Ram == nil {
		return ""
	}
	pct := int(c.r.Ram.Percent)
	usedGiB := c.r.Ram.UsedMb / 1024
	return fmt.Sprintf("RAM %s%.2fGiB %d%%#[default] ", pctColor(pct), usedGiB, pct)
}

func segRAMProc(c *renderCtx) string {
	if c.r.Ram == nil || c.r.TopMemProcess == "" {
		return ""
	}
	return fmt.Sprintf("(%s) ", c.r.TopMemProcess)
}

func segBattery(c *renderCtx) string {
	return batteryWidgetFull(c.r)
}

func segBatteryCompact(c *renderCtx) string {
	return batteryWidgetCompact(c.r)
}

func segTemp(c *renderCtx) string {
	return tempWidgetFull(c.r)
}

func segTempCompact(c *renderCtx) string {
	return tempWidgetCompact(c.r)
}

func segLayout(c *renderCtx) string {
	if c.r.KeyboardLayout == "" {
		return ""
	}
	return fmt.Sprintf("#[fg=#E82572,bg=#272822,none]#[fg=#A6E22E,bg=#E82572,none] %s ", c.r.KeyboardLayout)
}

func segPower(c *renderCtx) string {
	return powerWidgetBoth(c.r)
}

func segGPU(c *renderCtx) string {
	return gpuWidgetBoth(c.r)
}

func segWiFiPercent(c *renderCtx) string {
	if c.r.Wifi == nil || c.r.Wifi.Percent <= 0 {
		return ""
	}
	ipct := int(c.r.Wifi.Percent)
	return fmt.Sprintf("WIFI %s%d%% %s#[default] ", pctColor(100-ipct), ipct, bar(ipct))
}

func segWiFiCompact(c *renderCtx) string {
	if c.r.Wifi == nil || c.r.Wifi.Percent <= 0 {
		return ""
	}
	ipct := int(c.r.Wifi.Percent)
	return fmt.Sprintf("WIFI %s%d%%#[default] ", pctColor(100-ipct), ipct)
}

func segWiFiSSID(c *renderCtx) string {
	if c.r.Wifi == nil || c.r.Wifi.Percent <= 0 || c.r.Wifi.Ssid == "" {
		return ""
	}
	return fmt.Sprintf("(%s) ", c.r.Wifi.Ssid)
}

func segTime(c *renderCtx) string {
	return fmt.Sprintf("#[fg=#E8E8E2,bg=#272822,none]   %s ", c.timeStr)
}

func segDate(c *renderCtx) string {
	return fmt.Sprintf("#[fg=#E8E8E2,bg=#272822,none]   %s ", c.dateStr)
}

func segUser(c *renderCtx) string {
	return fmt.Sprintf("#[fg=#E8E8E2,bg=#E82572,none]#[fg=#272822,bg=#E8E8E2,bold] %s%s ", c.username, c.root)
}

func segHost(c *renderCtx) string {
	return fmt.Sprintf("#[fg=#272822,bg=#E8E8E2,none]#[fg=#E8E8E2,bg=#272822,none] %s ", c.hostname)
}

// allSegments returns every bar segment in definition order (priorities are
// applied at render time via sortedSegments).
func (s *tmuxBarServer) allSegments() []barSegment {
	return []barSegment{
		{key: widgetCPUPercent, renderFull: segCPUPercent, renderCompact: segCPUCompact},
		{key: widgetCPUProc, renderFull: segCPUProc},
		{key: widgetRAMPercent, renderFull: segRAMPercent, renderCompact: segRAMCompact},
		{key: widgetRAMProc, renderFull: segRAMProc},
		{key: widgetBattery, renderFull: segBattery, renderCompact: segBatteryCompact},
		{key: widgetTemp, renderFull: segTemp, renderCompact: segTempCompact},
		{key: widgetWiFiPercent, renderFull: segWiFiPercent, renderCompact: segWiFiCompact},
		{key: widgetWiFiSSID, renderFull: segWiFiSSID},
		{key: widgetPower, renderFull: segPower},
		{key: widgetGPU, renderFull: segGPU},
		{key: widgetTime, renderFull: segTime},
		{key: widgetDate, renderFull: segDate},
		{key: widgetLayout, renderFull: segLayout},
		{key: widgetUser, renderFull: segUser},
		{key: widgetHost, renderFull: segHost},
	}
}

func batteryWidgetFull(r *respb.CurrentResponse) string {
	if r.Battery == nil {
		return ""
	}
	bt := r.Battery
	pct := int(bt.Percent)
	status := batteryStatusString(bt.Status)
	btBar := batteryBar(pct)
	lc := batteryLabelColor(pct)
	switch status {
	case "Charging":
		if pct >= 100 {
			return fmt.Sprintf("#[fg=#A6E22E]PLUGGED#[default] %s %d%% ", btBar, pct)
		} else if bt.PowerNow > 0 {
			m := int((bt.EnergyFull - bt.EnergyNow) * 60 / bt.PowerNow)
			return fmt.Sprintf("#[fg=%s]CHARGING#[default] %s %d%% %s ", lc, btBar, pct, formatDuration(m))
		}
		return fmt.Sprintf("#[fg=%s]CHARGING#[default] %s %d%% ", lc, btBar, pct)
	case "Discharging":
		if bt.PowerNow > 0 {
			m := int(bt.EnergyNow * 60 / bt.PowerNow)
			return fmt.Sprintf("#[fg=%s]BAT#[default] %s %d%% %s ", lc, btBar, pct, formatDuration(m))
		}
		return fmt.Sprintf("#[fg=%s]BAT#[default] %s %d%% ", lc, btBar, pct)
	case "Full", "Not charging":
		return fmt.Sprintf("#[fg=#A6E22E]PLUGGED#[default] %s %d%% ", btBar, pct)
	default:
		return fmt.Sprintf("%d%% ", pct)
	}
}

func batteryWidgetCompact(r *respb.CurrentResponse) string {
	if r.Battery == nil {
		return ""
	}
	bt := r.Battery
	pct := int(bt.Percent)
	status := batteryStatusString(bt.Status)
	lc := batteryLabelColor(pct)
	switch status {
	case "Charging":
		if pct >= 100 {
			return fmt.Sprintf("#[fg=#A6E22E]PLUGGED#[default] ")
		}
		return fmt.Sprintf("#[fg=%s]CHARGING#[default] %d%% ", lc, pct)
	case "Discharging":
		return fmt.Sprintf("#[fg=%s]BAT#[default] %d%% ", lc, pct)
	case "Full", "Not charging":
		return fmt.Sprintf("#[fg=#A6E22E]PLUGGED#[default] ")
	default:
		return fmt.Sprintf("%d%% ", pct)
	}
}

func tempWidgetFull(r *respb.CurrentResponse) string {
	if r.CpuTemp == nil || r.CpuTemp.TempCelsius <= 0 {
		return ""
	}
	pct := int(r.CpuTemp.BarPct)
	temp := int(r.CpuTemp.TempCelsius)
	return fmt.Sprintf("TEMP %s%3d°C %s#[default] ", pctColor(pct), temp, bar(pct))
}

func tempWidgetCompact(r *respb.CurrentResponse) string {
	if r.CpuTemp == nil || r.CpuTemp.TempCelsius <= 0 {
		return ""
	}
	temp := int(r.CpuTemp.TempCelsius)
	return fmt.Sprintf("TEMP %3d°C ", temp)
}

func powerWidgetBoth(r *respb.CurrentResponse) string {
	// Power profile — single letter for compactness.
	switch r.PowerProfile {
	case respb.PowerProfile_POWER_PROFILE_PERF:
		return "#[fg=#E8871A]P#[default] "
	case respb.PowerProfile_POWER_PROFILE_BAL:
		return "#[fg=#A6E22E]B#[default] "
	case respb.PowerProfile_POWER_PROFILE_SAV:
		return "#[fg=#66D9EF]S#[default] "
	default:
		return ""
	}
}

func gpuWidgetBoth(r *respb.CurrentResponse) string {
	// GPU mode — single letter for compactness.
	switch r.GpuProfile {
	case respb.GPUProfile_GPU_PROFILE_EGPU:
		return "#[fg=#AE81FF]E#[default] "
	case respb.GPUProfile_GPU_PROFILE_NVIDIA:
		return "#[fg=#E8871A]N#[default] "
	case respb.GPUProfile_GPU_PROFILE_IGPU:
		return "#[fg=#66D9EF]I#[default] "
	case respb.GPUProfile_GPU_PROFILE_HYBRID:
		return "#[fg=#A6E22E]H#[default] "
	default:
		return ""
	}
}

func (s *tmuxBarServer) CPUWidget(ctx context.Context, req *connect.Request[pb.CPUWidgetRequest]) (*connect.Response[pb.CPUWidgetResponse], error) {
	pc := plugin.ExtractContext(ctx)

	r, err := s.resourcesClient.Current(ctx, connect.NewRequest(&respb.CurrentRequest{}))
	if err != nil {
		return connect.NewResponse(&pb.CPUWidgetResponse{Text: "CPU N/A"}), nil
	}
	cpu := r.Msg.Cpu
	if cpu == nil {
		return connect.NewResponse(&pb.CPUWidgetResponse{Text: "CPU N/A"}), nil
	}

	pct := int(cpu.TotalPercent)
	c := pctColor(pct)
	text := fmt.Sprintf("CPU %s%d%% (%s) %s#[default]", c, pct, r.Msg.TopCpuProcess, bar(pct))

	if pc != nil {
		pc.Log().Info("▶ TmuxBar.CPUWidget", "pct", pct, "top", r.Msg.TopCpuProcess)
	}

	if pc != nil && pc.RenderOutput() {
		fmt.Fprintln(pc.Stdout(), text)
	}

	return connect.NewResponse(&pb.CPUWidgetResponse{
		Text:    text,
		Percent: cpu.TotalPercent,
	}), nil
}

func (s *tmuxBarServer) RAMWidget(ctx context.Context, req *connect.Request[pb.RAMWidgetRequest]) (*connect.Response[pb.RAMWidgetResponse], error) {
	pc := plugin.ExtractContext(ctx)

	r, err := s.resourcesClient.Current(ctx, connect.NewRequest(&respb.CurrentRequest{}))
	if err != nil {
		return connect.NewResponse(&pb.RAMWidgetResponse{Text: "RAM N/A"}), nil
	}
	ram := r.Msg.Ram
	if ram == nil {
		return connect.NewResponse(&pb.RAMWidgetResponse{Text: "RAM N/A"}), nil
	}

	pct := int(ram.Percent)
	usedGiB := ram.UsedMb / 1024
	c := pctColor(pct)
	text := fmt.Sprintf("RAM %s%.2fGiB %d%% (%s) %s#[default]", c, usedGiB, pct, r.Msg.TopMemProcess, bar(pct))

	if pc != nil {
		pc.Log().Info("▶ TmuxBar.RAMWidget", "pct", pct, "used_gib", usedGiB, "top", r.Msg.TopMemProcess)
	}

	if pc != nil && pc.RenderOutput() {
		fmt.Fprintln(pc.Stdout(), text)
	}

	return connect.NewResponse(&pb.RAMWidgetResponse{
		Text:    text,
		Percent: ram.Percent,
	}), nil
}

func (s *tmuxBarServer) CPUTempWidget(ctx context.Context, req *connect.Request[pb.CPUTempWidgetRequest]) (*connect.Response[pb.CPUTempWidgetResponse], error) {
	pc := plugin.ExtractContext(ctx)

	r, err := s.resourcesClient.Current(ctx, connect.NewRequest(&respb.CurrentRequest{}))
	if err != nil {
		return connect.NewResponse(&pb.CPUTempWidgetResponse{Text: "TEMP N/A"}), nil
	}

	t := r.Msg.CpuTemp
	pct := 0
	temp := 0
	if t != nil {
		temp = int(t.TempCelsius)
		pct = int(t.BarPct)
	}

	c := pctColor(pct)
	text := fmt.Sprintf("TEMP %s%3d°C %s#[default]", c, temp, bar(pct))

	if pc != nil {
		pc.Log().Info("▶ TmuxBar.CPUTempWidget", "temp", temp, "pct", pct)
	}

	if pc != nil && pc.RenderOutput() {
		fmt.Fprintln(pc.Stdout(), text)
	}

	return connect.NewResponse(&pb.CPUTempWidgetResponse{
		Text:        text,
		Temperature: float64(temp),
	}), nil
}

func (s *tmuxBarServer) BatteryWidget(ctx context.Context, req *connect.Request[pb.BatteryWidgetRequest]) (*connect.Response[pb.BatteryWidgetResponse], error) {
	pc := plugin.ExtractContext(ctx)

	r, err := s.resourcesClient.Current(ctx, connect.NewRequest(&respb.CurrentRequest{}))
	if err != nil {
		return connect.NewResponse(&pb.BatteryWidgetResponse{Text: "BAT N/A"}), nil
	}

	bt := r.Msg.Battery
	if bt == nil {
		return connect.NewResponse(&pb.BatteryWidgetResponse{Text: "BAT N/A"}), nil
	}

	pct := int(bt.Percent)
	status := batteryStatusString(bt.Status)
	powerNow := bt.PowerNow
	energyNow := bt.EnergyNow
	energyFull := bt.EnergyFull

	btBar := batteryBar(pct)
	lc := batteryLabelColor(pct)

	var text string
	switch status {
	case "Charging":
		if pct >= 100 {
			text = fmt.Sprintf("#[fg=#A6E22E]PLUGGED#[default] %s %d%%", btBar, pct)
		} else if powerNow > 0 {
			m := int((energyFull - energyNow) * 60 / powerNow)
			text = fmt.Sprintf("#[fg=%s]CHARGING#[default] %s %d%% %s", lc, btBar, pct, formatDuration(m))
		} else {
			text = fmt.Sprintf("#[fg=%s]CHARGING#[default] %s %d%%", lc, btBar, pct)
		}
	case "Discharging":
		if powerNow > 0 {
			m := int(energyNow * 60 / powerNow)
			text = fmt.Sprintf("#[fg=%s]BAT#[default] %s %d%% %s", lc, btBar, pct, formatDuration(m))
		} else {
			text = fmt.Sprintf("#[fg=%s]BAT#[default] %s %d%%", lc, btBar, pct)
		}
	case "Full", "Not charging":
		text = fmt.Sprintf("#[fg=#A6E22E]PLUGGED#[default] %s %d%%", btBar, pct)
	default:
		text = fmt.Sprintf("%d%%", pct)
	}

	if pc != nil {
		pc.Log().Info("▶ TmuxBar.BatteryWidget", "pct", pct, "status", status, "plugged", bt.Plugged)
	}

	if pc != nil && pc.RenderOutput() {
		fmt.Fprintln(pc.Stdout(), text)
	}

	return connect.NewResponse(&pb.BatteryWidgetResponse{
		Text:     text,
		Percent:  float64(pct),
		Charging: status == "Charging",
	}), nil
}

func (s *tmuxBarServer) PowerProfileWidget(ctx context.Context, req *connect.Request[pb.PowerProfileWidgetRequest]) (*connect.Response[pb.PowerProfileWidgetResponse], error) {
	pc := plugin.ExtractContext(ctx)

	r, err := s.resourcesClient.Current(ctx, connect.NewRequest(&respb.CurrentRequest{}))
	p := respb.PowerProfile_POWER_PROFILE_UNSPECIFIED
	if err == nil {
		p = r.Msg.PowerProfile
	}

	var text, short string
	switch p {
	case respb.PowerProfile_POWER_PROFILE_PERF:
		text = "#[fg=#E8871A]P#[default] "
		short = "P"
	case respb.PowerProfile_POWER_PROFILE_BAL:
		text = "#[fg=#A6E22E]B#[default] "
		short = "B"
	case respb.PowerProfile_POWER_PROFILE_SAV:
		text = "#[fg=#66D9EF]S#[default] "
		short = "S"
	default:
		text = "? "
		short = "?"
	}

	if pc != nil {
		pc.Log().Info("▶ TmuxBar.PowerProfileWidget", "profile", short)
	}

	if pc != nil && pc.RenderOutput() {
		fmt.Fprintln(pc.Stdout(), text)
	}

	return connect.NewResponse(&pb.PowerProfileWidgetResponse{
		Text:    text,
		Profile: short,
	}), nil
}

func (s *tmuxBarServer) GPUProfileWidget(ctx context.Context, req *connect.Request[pb.GPUProfileWidgetRequest]) (*connect.Response[pb.GPUProfileWidgetResponse], error) {
	pc := plugin.ExtractContext(ctx)

	r, err := s.resourcesClient.Current(ctx, connect.NewRequest(&respb.CurrentRequest{}))
	p := respb.GPUProfile_GPU_PROFILE_UNSPECIFIED
	if err == nil {
		p = r.Msg.GpuProfile
	}

	var text, short string
	switch p {
	case respb.GPUProfile_GPU_PROFILE_EGPU:
		text = "#[fg=#AE81FF]E#[default] "
		short = "E"
	case respb.GPUProfile_GPU_PROFILE_NVIDIA:
		text = "#[fg=#E8871A]N#[default] "
		short = "N"
	case respb.GPUProfile_GPU_PROFILE_IGPU:
		text = "#[fg=#66D9EF]I#[default] "
		short = "I"
	case respb.GPUProfile_GPU_PROFILE_HYBRID:
		text = "#[fg=#A6E22E]H#[default] "
		short = "H"
	default:
		text = "? "
		short = "?"
	}

	if pc != nil {
		pc.Log().Info("▶ TmuxBar.GPUProfileWidget", "profile", short)
	}

	if pc != nil && pc.RenderOutput() {
		fmt.Fprintln(pc.Stdout(), text)
	}

	return connect.NewResponse(&pb.GPUProfileWidgetResponse{
		Text:    text,
		Profile: short,
	}), nil
}

func (s *tmuxBarServer) LayoutWidget(ctx context.Context, req *connect.Request[pb.LayoutWidgetRequest]) (*connect.Response[pb.LayoutWidgetResponse], error) {
	pc := plugin.ExtractContext(ctx)

	r, err := s.resourcesClient.Current(ctx, connect.NewRequest(&respb.CurrentRequest{}))
	layout := ""
	if err == nil {
		layout = r.Msg.KeyboardLayout
	}

	if pc != nil {
		pc.Log().Info("▶ TmuxBar.LayoutWidget", "layout", layout)
	}

	if pc != nil && pc.RenderOutput() {
		fmt.Fprintln(pc.Stdout(), layout)
	}

	return connect.NewResponse(&pb.LayoutWidgetResponse{
		Text:   layout,
		Layout: layout,
	}), nil
}

func (s *tmuxBarServer) WiFiWidget(ctx context.Context, req *connect.Request[pb.WiFiWidgetRequest]) (*connect.Response[pb.WiFiWidgetResponse], error) {
	pc := plugin.ExtractContext(ctx)

	r, err := s.resourcesClient.Current(ctx, connect.NewRequest(&respb.CurrentRequest{}))
	pct := 0.0
	ssid := ""
	if err == nil && r.Msg.Wifi != nil {
		pct = r.Msg.Wifi.Percent
		ssid = r.Msg.Wifi.Ssid
	}

	ipct := int(pct)
	c := pctColor(100 - ipct)
	text := fmt.Sprintf("WIFI %s%d%% (%s) %s#[default]", c, ipct, ssid, bar(ipct))

	if pc != nil {
		pc.Log().Info("▶ TmuxBar.WiFiWidget", "pct", ipct, "ssid", ssid)
	}

	if pc != nil && pc.RenderOutput() {
		fmt.Fprintln(pc.Stdout(), text)
	}

	return connect.NewResponse(&pb.WiFiWidgetResponse{
		Text:    text,
		Percent: pct,
		Ssid:    ssid,
	}), nil
}

func (s *tmuxBarServer) renderBar(r *respb.CurrentResponse, maxWidth int) string {
	if maxWidth <= 0 {
		maxWidth = 9999
	}

	now := time.Now()
	ctx := &renderCtx{
		r:        r,
		timeStr:  now.Format("15:04"),
		dateStr:  now.Format("02 Jan"),
		username: s.username,
		root:     s.root,
		hostname: s.hostname,
	}

	// Segments always render in their fixed visual order; priority only
	// decides which segments are hidden when the bar is truncated.
	type item struct {
		full    string
		compact string
		fw      int
		cw      int
		prio    int
	}
	var items []item
	total := 0
	for _, seg := range s.allSegments() {
		full := seg.renderFull(ctx)
		if full == "" {
			continue
		}
		fw := visibleWidth(full)
		var compact string
		var cw int
		if seg.renderCompact != nil {
			compact = seg.renderCompact(ctx)
			cw = visibleWidth(compact)
		}
		items = append(items, item{full: full, compact: compact, fw: fw, cw: cw, prio: s.priority(seg.key)})
		total += fw
	}

	// Hide the lowest-priority segments (highest priority value) until the
	// bar fits. Ties are broken rightmost-first so the chevron chain at the
	// right of the bar collapses cleanly.
	hidden := make([]bool, len(items))
	if total > maxWidth {
		idx := make([]int, len(items))
		for i := range items {
			idx[i] = i
		}
		sort.SliceStable(idx, func(a, b int) bool {
			pa, pb := items[idx[a]].prio, items[idx[b]].prio
			if pa != pb {
				return pa > pb
			}
			return idx[a] > idx[b]
		})
		for _, i := range idx {
			if total <= maxWidth {
				break
			}
			hidden[i] = true
			total -= items[i].fw
		}
	}

	// Render the survivors in fixed order, compacting any that still don't
	// fit (only relevant when even the core segments are too wide).
	remaining := maxWidth
	var b strings.Builder
	for i, it := range items {
		if hidden[i] {
			continue
		}
		if it.fw <= remaining {
			b.WriteString(it.full)
			remaining -= it.fw
		} else if it.cw > 0 && it.cw <= remaining {
			b.WriteString(it.compact)
			remaining -= it.cw
		}
	}
	return b.String()
}

// priority returns the current display priority for a segment.
func (s *tmuxBarServer) priority(key widgetKey) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.priorities[key]
}

func toProtoPriorities(m map[widgetKey]int) *pb.WidgetPriorities {
	cpuPercent := int32(m[widgetCPUPercent])
	cpuProc := int32(m[widgetCPUProc])
	ramPercent := int32(m[widgetRAMPercent])
	ramProc := int32(m[widgetRAMProc])
	battery := int32(m[widgetBattery])
	temp := int32(m[widgetTemp])
	layout := int32(m[widgetLayout])
	power := int32(m[widgetPower])
	gpu := int32(m[widgetGPU])
	wifiPercent := int32(m[widgetWiFiPercent])
	wifiSSID := int32(m[widgetWiFiSSID])
	tm := int32(m[widgetTime])
	date := int32(m[widgetDate])
	user := int32(m[widgetUser])
	host := int32(m[widgetHost])
	return &pb.WidgetPriorities{
		CpuPercent:   &cpuPercent,
		CpuProcess:   &cpuProc,
		RamPercent:   &ramPercent,
		RamProcess:   &ramProc,
		Battery:      &battery,
		Temp:         &temp,
		Layout:       &layout,
		PowerProfile: &power,
		GpuProfile:   &gpu,
		WifiPercent:  &wifiPercent,
		WifiSsid:     &wifiSSID,
		Time:         &tm,
		Date:         &date,
		User:         &user,
		Hostname:     &host,
	}
}

func (s *tmuxBarServer) SetWidgetPriorities(ctx context.Context, req *connect.Request[pb.SetWidgetPrioritiesRequest]) (*connect.Response[pb.SetWidgetPrioritiesResponse], error) {
	pc := plugin.ExtractContext(ctx)
	p := req.Msg.GetPriorities()

	s.mu.Lock()
	if p != nil {
		if p.CpuPercent != nil {
			s.priorities[widgetCPUPercent] = int(*p.CpuPercent)
		}
		if p.CpuProcess != nil {
			s.priorities[widgetCPUProc] = int(*p.CpuProcess)
		}
		if p.RamPercent != nil {
			s.priorities[widgetRAMPercent] = int(*p.RamPercent)
		}
		if p.RamProcess != nil {
			s.priorities[widgetRAMProc] = int(*p.RamProcess)
		}
		if p.Battery != nil {
			s.priorities[widgetBattery] = int(*p.Battery)
		}
		if p.Temp != nil {
			s.priorities[widgetTemp] = int(*p.Temp)
		}
		if p.Layout != nil {
			s.priorities[widgetLayout] = int(*p.Layout)
		}
		if p.PowerProfile != nil {
			s.priorities[widgetPower] = int(*p.PowerProfile)
		}
		if p.GpuProfile != nil {
			s.priorities[widgetGPU] = int(*p.GpuProfile)
		}
		if p.WifiPercent != nil {
			s.priorities[widgetWiFiPercent] = int(*p.WifiPercent)
		}
		if p.WifiSsid != nil {
			s.priorities[widgetWiFiSSID] = int(*p.WifiSsid)
		}
		if p.Time != nil {
			s.priorities[widgetTime] = int(*p.Time)
		}
		if p.Date != nil {
			s.priorities[widgetDate] = int(*p.Date)
		}
		if p.User != nil {
			s.priorities[widgetUser] = int(*p.User)
		}
		if p.Hostname != nil {
			s.priorities[widgetHost] = int(*p.Hostname)
		}
	}
	applied := make(map[widgetKey]int, len(s.priorities))
	for k, v := range s.priorities {
		applied[k] = v
	}
	s.mu.Unlock()

	msg := "widget priorities updated"
	if p == nil {
		msg = "no priorities in request"
	}
	if pc != nil {
		pc.Log().Info("▶ TmuxBar.SetWidgetPriorities", "priorities", applied)
	}
	if pc != nil && pc.RenderOutput() {
		fmt.Fprintln(pc.Stdout(), msg)
	}

	return connect.NewResponse(&pb.SetWidgetPrioritiesResponse{
		Priorities: toProtoPriorities(applied),
		Message:    msg,
	}), nil
}

func (s *tmuxBarServer) StatusBar(ctx context.Context, req *connect.Request[pb.StatusBarRequest]) (*connect.Response[pb.StatusBarResponse], error) {
	pc := plugin.ExtractContext(ctx)
	data := s.cache.get()

	r := &respb.CurrentResponse{}
	if data != nil {
		r = data
	}
	text := s.renderBar(r, int(req.Msg.MaxWidth))

	if pc != nil {
		pc.Log().Info("▶ TmuxBar.StatusBar", "max_width", req.Msg.MaxWidth, "len", len(text))
	}

	if pc != nil && pc.RenderOutput() {
		fmt.Fprintln(pc.Stdout(), text)
	}

	return connect.NewResponse(&pb.StatusBarResponse{
		Text: text,
	}), nil
}

func initResourcesClient() resourcesconnect.ResourcesServiceClient {
	daemonURL := "http://127.0.0.1:9105"
	httpClient := &http.Client{}
	regClient := dotfilesdv1connect.NewPluginRegistryServiceClient(httpClient, daemonURL)
	regResp, err := regClient.GetPlugin(context.Background(), connect.NewRequest(&dotfilesdv1.RegistryGetPluginRequest{
		PluginName: "resources",
	}))
	if err != nil {
		fmt.Fprintf(os.Stderr, "tmuxbar: initResourcesClient: GetPlugin failed: %v\n", err)
		return nil
	}
	return resourcesconnect.NewResourcesServiceClient(httpClient, regResp.Msg.Url)
}

func main() {
	resClient := initResourcesClient()
	host, _ := os.Hostname()
	username := os.Getenv("USER")
	root := ""
	if username == "root" {
		root = "!"
	}
	svc := &tmuxBarServer{
		resourcesClient: resClient,
		hostname:        host,
		username:        username,
		root:            root,
		priorities:      defaultPriorities(),
	}
	path, handler := tmuxbarconnect.NewTmuxBarServiceHandler(svc)

	plugin.Serve(plugin.Config{
		Name:        "tmuxbar",
		DisplayName: "TmuxBar",
		Version:     "1.0.0",
		Description: "Tmux status bar widgets — drop-in replacement for shell functions",
		Services: []plugin.Service{
			{
				Name:             "tmuxbar.TmuxBarService",
				Description:      "Tmux status bar widget API",
				Path:             path,
				Handler:          handler,
				PluginAccessible: true,
			},
		},
		Background: func(ctx plugin.Context, stop <-chan struct{}) {
			go func() {
				filter := &respb.WatchFilter{
					Ram:             true,
					Cpu:             true,
					Disk:            true,
					DiskIo:          true,
					CpuTemp:         true,
					Battery:         true,
					Wifi:            true,
					PowerProfile:    true,
					GpuProfile:      true,
					KeyboardLayout:  true,
					TopProcesses:    true,
				}
				for {
					stream, err := resClient.Watch(context.Background(), connect.NewRequest(&respb.WatchRequest{Filter: filter}))
					if err != nil {
						select {
						case <-stop:
							return
						case <-time.After(5 * time.Second):
						}
						continue
					}
					for stream.Receive() {
						msg := stream.Msg()
						svc.cache.set(proto.Clone(msg).(*respb.CurrentResponse))
						exec.Command("tmux", "refresh-client", "-S").Run()
					}
					select {
					case <-stop:
						return
					default:
					}
				}
			}()
		},
	})
}
