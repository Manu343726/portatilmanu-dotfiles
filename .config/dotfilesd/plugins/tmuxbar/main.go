package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
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
)

const barFilled = "◼"
const barEmpty = "◻"
const barSegments = 10

type CPUTempState struct {
	mu  sync.Mutex
	min float64
	max float64
}

func (s *CPUTempState) update(temp float64) (min, max float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.min == 0 && s.max == 0 {
		s.min = temp
		s.max = temp
	}
	if temp < s.min {
		s.min = temp
	}
	if temp > s.max {
		s.max = temp
	}
	return s.min, s.max
}

type tmuxBarServer struct {
	resourcesClient resourcesconnect.ResourcesServiceClient
	cpuTempState    CPUTempState
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

func topProcess(sortFlag string) string {
	out, err := exec.Command("ps", "-eo", "comm", "--sort="+sortFlag).Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) < 2 {
		return ""
	}
	name := strings.TrimSpace(lines[1])
	if name == "ps" || name == "" || name == "COMMAND" {
		if len(lines) > 2 {
			name = strings.TrimSpace(lines[2])
		} else {
			return ""
		}
	}
	return name
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

	top := topProcess("-%cpu")
	pct := int(cpu.TotalPercent)
	c := pctColor(pct)
	text := fmt.Sprintf("CPU %s%d%% (%s) %s#[default]", c, pct, top, bar(pct))

	if pc != nil {
		pc.Log().Info("▶ TmuxBar.CPUWidget", "pct", pct, "top", top)
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

	top := topProcess("-%mem")
	pct := int(ram.Percent)
	usedGiB := ram.UsedMb / 1024
	c := pctColor(pct)
	text := fmt.Sprintf("RAM %s%.2fGiB %d%% (%s) %s#[default]", c, usedGiB, pct, top, bar(pct))

	if pc != nil {
		pc.Log().Info("▶ TmuxBar.RAMWidget", "pct", pct, "used_gib", usedGiB, "top", top)
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

	temp := int(r.Msg.CpuTemp.GetTempCelsius())
	var pct int
	if temp > 0 {
		min, max := s.cpuTempState.update(float64(temp))
		range_ := max - min
		if range_ <= 0 {
			pct = 50
		} else {
			pct = int((float64(temp) - min) * 100 / range_)
		}
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
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

func (s *tmuxBarServer) AsusProfileWidget(ctx context.Context, req *connect.Request[pb.AsusProfileWidgetRequest]) (*connect.Response[pb.AsusProfileWidgetResponse], error) {
	pc := plugin.ExtractContext(ctx)

	out, err := exec.CommandContext(ctx, "asusctl", "profile", "get").Output()
	p := ""
	if err == nil {
		fields := strings.Fields(string(out))
		if len(fields) >= 3 {
			p = fields[2]
		}
	}

	var text string
	switch p {
	case "Performance":
		text = "#[fg=#E8871A]PERF#[default] "
	case "Balanced":
		text = "#[fg=#A6E22E]BAL#[default] "
	case "Quiet":
		text = "#[fg=#66D9EF]QUIET#[default] "
	default:
		text = "? "
	}

	short := ""
	switch p {
	case "Performance":
		short = "PERF"
	case "Balanced":
		short = "BAL"
	case "Quiet":
		short = "QUIET"
	}

	if pc != nil {
		pc.Log().Info("▶ TmuxBar.AsusProfileWidget", "profile", p)
	}

	if pc != nil && pc.RenderOutput() {
		fmt.Fprintln(pc.Stdout(), text)
	}

	return connect.NewResponse(&pb.AsusProfileWidgetResponse{
		Text:    text,
		Profile: short,
	}), nil
}

func (s *tmuxBarServer) GPUProfileWidget(ctx context.Context, req *connect.Request[pb.GPUProfileWidgetRequest]) (*connect.Response[pb.GPUProfileWidgetResponse], error) {
	pc := plugin.ExtractContext(ctx)

	var text string
	if raw, err := os.ReadFile("/sys/devices/platform/asus-nb-wmi/egpu_connected"); err == nil && strings.TrimSpace(string(raw)) == "1" {
		text = "#[fg=#AE81FF]EGPU#[default] "
	} else if raw, err := os.ReadFile("/sys/devices/platform/asus-nb-wmi/gpu_mux_mode"); err == nil && strings.TrimSpace(string(raw)) == "1" {
		text = "#[fg=#E8871A]NVIDIA#[default] "
	} else if raw, err := os.ReadFile("/sys/devices/platform/asus-nb-wmi/dgpu_disable"); err == nil && strings.TrimSpace(string(raw)) == "1" {
		text = "#[fg=#66D9EF]IGPU#[default] "
	} else {
		text = "#[fg=#A6E22E]HYBRID#[default] "
	}

	short := ""
	switch {
	case strings.Contains(text, "EGPU"):
		short = "EGPU"
	case strings.Contains(text, "NVIDIA"):
		short = "NVIDIA"
	case strings.Contains(text, "IGPU"):
		short = "IGPU"
	case strings.Contains(text, "HYBRID"):
		short = "HYBRID"
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

	layout := ""
	out, err := exec.CommandContext(ctx, "xkb-switch").Output()
	if err == nil {
		layout = strings.TrimSpace(string(out))
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

func findWiFiInterface() string {
	ents, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return ""
	}
	for _, e := range ents {
		wirelessDir := "/sys/class/net/" + e.Name() + "/wireless"
		if info, err := os.Stat(wirelessDir); err == nil && info.IsDir() {
			return e.Name()
		}
	}
	return ""
}

func readWiFiSignal(iface string) int {
	data, err := os.ReadFile("/proc/net/wireless")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, iface+":") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		// field 2 is link quality (with trailing dot). assume max 70 (iwlwifi scale).
		q := strings.TrimRight(fields[2], ".")
		quality, _ := strconv.Atoi(q)
		pct := quality * 100 / 70
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
		return pct
	}
	return 0
}

func readWiFiSSID(iface string) string {
	out, err := exec.Command("iw", "dev", iface, "info").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ssid ") {
			return strings.TrimPrefix(line, "ssid ")
		}
	}
	return ""
}

func (s *tmuxBarServer) WiFiWidget(ctx context.Context, req *connect.Request[pb.WiFiWidgetRequest]) (*connect.Response[pb.WiFiWidgetResponse], error) {
	pc := plugin.ExtractContext(ctx)

	iface := findWiFiInterface()
	pct := 0
	ssid := ""
	if iface != "" {
		pct = readWiFiSignal(iface)
		ssid = readWiFiSSID(iface)
	}

	c := pctColor(pct)
	text := fmt.Sprintf("WIFI %s%d%% (%s) %s#[default]", c, pct, ssid, bar(pct))

	if pc != nil {
		pc.Log().Info("▶ TmuxBar.WiFiWidget", "pct", pct, "ssid", ssid)
	}

	if pc != nil && pc.RenderOutput() {
		fmt.Fprintln(pc.Stdout(), text)
	}

	return connect.NewResponse(&pb.WiFiWidgetResponse{
		Text:    text,
		Percent: float64(pct),
		Ssid:    ssid,
	}), nil
}

func (s *tmuxBarServer) StatusBar(ctx context.Context, req *connect.Request[pb.StatusBarRequest]) (*connect.Response[pb.StatusBarResponse], error) {
	pc := plugin.ExtractContext(ctx)

	now := time.Now()
	host, _ := os.Hostname()
	username := os.Getenv("USER")
	timeStr := now.Format("15:04")
	dateStr := now.Format("02 Jan")

	var root string
	if username == "root" {
		root = "!"
	}

	// Read resources data once
	r, err := s.resourcesClient.Current(ctx, connect.NewRequest(&respb.CurrentRequest{}))
	if err != nil {
		return connect.NewResponse(&pb.StatusBarResponse{Text: "resources unavailable"}), nil
	}

	var b strings.Builder

	// ASUS profile
	out, err := exec.Command("asusctl", "profile", "get").Output()
	p := ""
	if err == nil {
		fields := strings.Fields(string(out))
		if len(fields) >= 3 {
			p = fields[2]
		}
	}
	switch p {
	case "Performance":
		b.WriteString("#[fg=#E8871A]PERF#[default] ")
	case "Balanced":
		b.WriteString("#[fg=#A6E22E]BAL#[default] ")
	case "Quiet":
		b.WriteString("#[fg=#66D9EF]QUIET#[default] ")
	}

	// GPU profile
	if raw, err := os.ReadFile("/sys/devices/platform/asus-nb-wmi/egpu_connected"); err == nil && strings.TrimSpace(string(raw)) == "1" {
		b.WriteString("#[fg=#AE81FF]EGPU#[default] ")
	} else if raw, err := os.ReadFile("/sys/devices/platform/asus-nb-wmi/gpu_mux_mode"); err == nil && strings.TrimSpace(string(raw)) == "1" {
		b.WriteString("#[fg=#E8871A]NVIDIA#[default] ")
	} else if raw, err := os.ReadFile("/sys/devices/platform/asus-nb-wmi/dgpu_disable"); err == nil && strings.TrimSpace(string(raw)) == "1" {
		b.WriteString("#[fg=#66D9EF]IGPU#[default] ")
	} else {
		b.WriteString("#[fg=#A6E22E]HYBRID#[default] ")
	}
	b.WriteString(" ")

	// Battery
	if bt := r.Msg.Battery; bt != nil {
		pct := int(bt.Percent)
		status := batteryStatusString(bt.Status)
		btBar := batteryBar(pct)
		lc := batteryLabelColor(pct)

		switch status {
		case "Charging":
			if pct >= 100 {
				b.WriteString(fmt.Sprintf("#[fg=#A6E22E]PLUGGED#[default] %s %d%%", btBar, pct))
			} else if bt.PowerNow > 0 {
				m := int((bt.EnergyFull - bt.EnergyNow) * 60 / bt.PowerNow)
				b.WriteString(fmt.Sprintf("#[fg=%s]CHARGING#[default] %s %d%% %s", lc, btBar, pct, formatDuration(m)))
			} else {
				b.WriteString(fmt.Sprintf("#[fg=%s]CHARGING#[default] %s %d%%", lc, btBar, pct))
			}
		case "Discharging":
			if bt.PowerNow > 0 {
				m := int(bt.EnergyNow * 60 / bt.PowerNow)
				b.WriteString(fmt.Sprintf("#[fg=%s]BAT#[default] %s %d%% %s", lc, btBar, pct, formatDuration(m)))
			} else {
				b.WriteString(fmt.Sprintf("#[fg=%s]BAT#[default] %s %d%%", lc, btBar, pct))
			}
		case "Full", "Not charging":
			b.WriteString(fmt.Sprintf("#[fg=#A6E22E]PLUGGED#[default] %s %d%%", btBar, pct))
		default:
			b.WriteString(fmt.Sprintf("%d%%", pct))
		}
		b.WriteString("#[default] ")
	}

	// CPU
	if cpu := r.Msg.Cpu; cpu != nil {
		pct := int(cpu.TotalPercent)
		top := topProcess("-%cpu")
		b.WriteString(fmt.Sprintf("CPU %s%d%% (%s) %s#[default]", pctColor(pct), pct, top, bar(pct)))
		b.WriteString("#[default] ")
	}

	// CPU temp
	if t := r.Msg.CpuTemp; t != nil {
		temp := int(t.TempCelsius)
		var pct int
		if temp > 0 {
			min, max := s.cpuTempState.update(float64(temp))
			range_ := max - min
			if range_ <= 0 {
				pct = 50
			} else {
				pct = int((float64(temp) - min) * 100 / range_)
			}
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
		}
		b.WriteString(fmt.Sprintf("TEMP %s%3d°C %s#[default]", pctColor(pct), temp, bar(pct)))
		b.WriteString("#[default] ")
	}

	// RAM
	if ram := r.Msg.Ram; ram != nil {
		pct := int(ram.Percent)
		usedGiB := ram.UsedMb / 1024
		top := topProcess("-%mem")
		b.WriteString(fmt.Sprintf("RAM %s%.2fGiB %d%% (%s) %s#[default]", pctColor(pct), usedGiB, pct, top, bar(pct)))
		b.WriteString("#[default] ")
	}

	// WiFi
	if iface := findWiFiInterface(); iface != "" {
		pct := readWiFiSignal(iface)
		ssid := readWiFiSSID(iface)
		b.WriteString(fmt.Sprintf("WIFI %s%d%% (%s) %s#[default]", pctColor(pct), pct, ssid, bar(pct)))
		b.WriteString("#[default] ")
	}

	// Thin separator before time
	b.WriteString("#[fg=#E8E8E2,bg=#272822,none]   ")
	b.WriteString(timeStr)
	b.WriteString(" #[fg=#E8E8E2,bg=#272822,none]   ")
	b.WriteString(dateStr)

	// Powerline arrow to red section for layout
	b.WriteString(" #[fg=#E82572,bg=#272822,none]#[fg=#A6E22E,bg=#E82572,none] ")

	out2, err := exec.Command("xkb-switch").Output()
	layout := ""
	if err == nil {
		layout = strings.TrimSpace(string(out2))
	}
	b.WriteString(layout)

	// Powerline arrow to light section for username
	b.WriteString(" #[fg=#E8E8E2,bg=#E82572,none]#[fg=#272822,bg=#E8E8E2,bold] ")
	b.WriteString(username)
	b.WriteString(root)

	// Powerline arrow back to dark section for hostname
	b.WriteString(" #[fg=#272822,bg=#E8E8E2,none]#[fg=#E8E8E2,bg=#272822,none] ")
	b.WriteString(host)
	b.WriteString(" ")

	text := b.String()

	if pc != nil {
		pc.Log().Info("▶ TmuxBar.StatusBar")
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
	svc := &tmuxBarServer{resourcesClient: resClient}
	path, handler := tmuxbarconnect.NewTmuxBarServiceHandler(svc)

	plugin.Serve(plugin.Config{
		Name:        "tmuxbar",
		DisplayName: "TmuxBar",
		Version:     "1.0.0",
		Description: "Tmux status bar widgets — drop-in replacement for shell functions",
		DocsProto:   pb.PluginDocs,
		Services: []plugin.Service{
			{
				Name:             "tmuxbar.TmuxBarService",
				Description:      "Tmux status bar widget API",
				Path:             path,
				Handler:          handler,
				PluginAccessible: true,
			},
		},
	})
}
