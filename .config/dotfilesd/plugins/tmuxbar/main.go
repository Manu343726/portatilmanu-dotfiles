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
	widgetCPU     widgetKey = "cpu"
	widgetRAM     widgetKey = "ram"
	widgetBattery widgetKey = "battery"
	widgetTemp    widgetKey = "temp"
	widgetWifi    widgetKey = "wifi"
	widgetPower   widgetKey = "power_profile"
	widgetGPU     widgetKey = "gpu_profile"
)

// defaultPriorities returns the default display order of the bar widgets.
// Lower values are rendered first and survive width truncation first.
func defaultPriorities() map[widgetKey]int {
	return map[widgetKey]int{
		widgetCPU:     0,
		widgetRAM:     1,
		widgetBattery: 2,
		widgetTemp:    3,
		widgetWifi:    4,
		widgetPower:   5,
		widgetGPU:     6,
	}
}

type barWidget struct {
	key           widgetKey
	renderFull    func(*respb.CurrentResponse) string
	renderCompact func(*respb.CurrentResponse) string
}

func cpuWidgetFull(r *respb.CurrentResponse) string {
	if r.Cpu == nil {
		return ""
	}
	pct := int(r.Cpu.TotalPercent)
	return fmt.Sprintf("CPU %s%d%% (%s) %s#[default] ", pctColor(pct), pct, r.TopCpuProcess, bar(pct))
}

func cpuWidgetCompact(r *respb.CurrentResponse) string {
	if r.Cpu == nil {
		return ""
	}
	pct := int(r.Cpu.TotalPercent)
	return fmt.Sprintf("CPU %s%d%%#[default] ", pctColor(pct), pct)
}

func ramWidgetFull(r *respb.CurrentResponse) string {
	if r.Ram == nil {
		return ""
	}
	pct := int(r.Ram.Percent)
	usedGiB := r.Ram.UsedMb / 1024
	return fmt.Sprintf("RAM %s%.2fGiB %d%% (%s) %s#[default] ", pctColor(pct), usedGiB, pct, r.TopMemProcess, bar(pct))
}

func ramWidgetCompact(r *respb.CurrentResponse) string {
	if r.Ram == nil {
		return ""
	}
	pct := int(r.Ram.Percent)
	return fmt.Sprintf("RAM %s%d%%#[default] ", pctColor(pct), pct)
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

func wifiWidgetFull(r *respb.CurrentResponse) string {
	if r.Wifi == nil || r.Wifi.Percent <= 0 {
		return ""
	}
	ipct := int(r.Wifi.Percent)
	return fmt.Sprintf("WIFI %s%d%% (%s) %s#[default] ", pctColor(100-ipct), ipct, r.Wifi.Ssid, bar(ipct))
}

func wifiWidgetCompact(r *respb.CurrentResponse) string {
	if r.Wifi == nil || r.Wifi.Percent <= 0 {
		return ""
	}
	ipct := int(r.Wifi.Percent)
	return fmt.Sprintf("WIFI %s%d%%#[default] ", pctColor(100-ipct), ipct)
}

var barWidgets = []barWidget{
	{key: widgetCPU, renderFull: cpuWidgetFull, renderCompact: cpuWidgetCompact},
	{key: widgetRAM, renderFull: ramWidgetFull, renderCompact: ramWidgetCompact},
	{key: widgetBattery, renderFull: batteryWidgetFull, renderCompact: batteryWidgetCompact},
	{key: widgetTemp, renderFull: tempWidgetFull, renderCompact: tempWidgetCompact},
	{key: widgetWifi, renderFull: wifiWidgetFull, renderCompact: wifiWidgetCompact},
	{key: widgetPower, renderFull: powerWidgetBoth, renderCompact: powerWidgetBoth},
	{key: widgetGPU, renderFull: gpuWidgetBoth, renderCompact: gpuWidgetBoth},
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

func renderPinned(r *respb.CurrentResponse, username, root, host, timeStr, dateStr string) string {
	var b strings.Builder
	b.WriteString("#[fg=#E8E8E2,bg=#272822,none]   ")
	b.WriteString(timeStr)
	b.WriteString(" #[fg=#E8E8E2,bg=#272822,none]   ")
	b.WriteString(dateStr)
	b.WriteString(" #[fg=#E82572,bg=#272822,none]#[fg=#A6E22E,bg=#E82572,none] ")
	b.WriteString(r.KeyboardLayout)
	b.WriteString(" #[fg=#E8E8E2,bg=#E82572,none]#[fg=#272822,bg=#E8E8E2,bold] ")
	b.WriteString(username)
	b.WriteString(root)
	b.WriteString(" #[fg=#272822,bg=#E8E8E2,none]#[fg=#E8E8E2,bg=#272822,none] ")
	b.WriteString(host)
	b.WriteString(" ")
	return b.String()
}

func (s *tmuxBarServer) renderBar(r *respb.CurrentResponse, username, root, host, timeStr, dateStr string, maxWidth int) string {
	if maxWidth <= 0 {
		maxWidth = 9999
	}

	pinned := renderPinned(r, username, root, host, timeStr, dateStr)
	pinnedW := visibleWidth(pinned)
	remaining := maxWidth - pinnedW

	var widgetsStr string
	for _, w := range s.sortedWidgets() {
		if remaining <= 0 {
			break
		}

		full := w.renderFull(r)
		if full == "" {
			continue
		}
		fw := visibleWidth(full)

		if fw <= remaining {
			widgetsStr += full
			remaining -= fw
			continue
		}

		compact := w.renderCompact(r)
		if compact == "" {
			continue
		}
		cw := visibleWidth(compact)
		if cw <= remaining {
			widgetsStr += compact
			remaining -= cw
		}
	}

	return widgetsStr + pinned
}

// sortedWidgets returns the bar widgets ordered by their current display
// priority (stable, so equal priorities keep their definition order).
func (s *tmuxBarServer) sortedWidgets() []barWidget {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]barWidget, len(barWidgets))
	copy(out, barWidgets)
	sort.SliceStable(out, func(i, j int) bool {
		return s.priorities[out[i].key] < s.priorities[out[j].key]
	})
	return out
}

func toProtoPriorities(m map[widgetKey]int) *pb.WidgetPriorities {
	cpu := int32(m[widgetCPU])
	ram := int32(m[widgetRAM])
	battery := int32(m[widgetBattery])
	temp := int32(m[widgetTemp])
	wifi := int32(m[widgetWifi])
	power := int32(m[widgetPower])
	gpu := int32(m[widgetGPU])
	return &pb.WidgetPriorities{
		Cpu:          &cpu,
		Ram:          &ram,
		Battery:      &battery,
		Temp:         &temp,
		Wifi:         &wifi,
		PowerProfile: &power,
		GpuProfile:   &gpu,
	}
}

func (s *tmuxBarServer) SetWidgetPriorities(ctx context.Context, req *connect.Request[pb.SetWidgetPrioritiesRequest]) (*connect.Response[pb.SetWidgetPrioritiesResponse], error) {
	pc := plugin.ExtractContext(ctx)
	p := req.Msg.GetPriorities()

	s.mu.Lock()
	if p != nil {
		if p.Cpu != nil {
			s.priorities[widgetCPU] = int(*p.Cpu)
		}
		if p.Ram != nil {
			s.priorities[widgetRAM] = int(*p.Ram)
		}
		if p.Battery != nil {
			s.priorities[widgetBattery] = int(*p.Battery)
		}
		if p.Temp != nil {
			s.priorities[widgetTemp] = int(*p.Temp)
		}
		if p.Wifi != nil {
			s.priorities[widgetWifi] = int(*p.Wifi)
		}
		if p.PowerProfile != nil {
			s.priorities[widgetPower] = int(*p.PowerProfile)
		}
		if p.GpuProfile != nil {
			s.priorities[widgetGPU] = int(*p.GpuProfile)
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

	now := time.Now()
	timeStr := now.Format("15:04")
	dateStr := now.Format("02 Jan")

	var text string
	if data != nil {
		text = s.renderBar(data, s.username, s.root, s.hostname, timeStr, dateStr, int(req.Msg.MaxWidth))
	} else {
		r := &respb.CurrentResponse{}
		text = renderPinned(r, s.username, s.root, s.hostname, timeStr, dateStr)
	}

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
