package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
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

	r, err := s.resourcesClient.Current(ctx, connect.NewRequest(&respb.CurrentRequest{}))
	p := respb.ASUSProfile_ASUS_PROFILE_UNSPECIFIED
	if err == nil {
		p = r.Msg.AsusProfile
	}

	var text, short string
	switch p {
	case respb.ASUSProfile_ASUS_PROFILE_PERF:
		text = "#[fg=#E8871A]PERF#[default] "
		short = "PERF"
	case respb.ASUSProfile_ASUS_PROFILE_BAL:
		text = "#[fg=#A6E22E]BAL#[default] "
		short = "BAL"
	case respb.ASUSProfile_ASUS_PROFILE_QUIET:
		text = "#[fg=#66D9EF]QUIET#[default] "
		short = "QUIET"
	default:
		text = "? "
	}

	if pc != nil {
		pc.Log().Info("▶ TmuxBar.AsusProfileWidget", "profile", short)
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

	r, err := s.resourcesClient.Current(ctx, connect.NewRequest(&respb.CurrentRequest{}))
	p := respb.GPUProfile_GPU_PROFILE_UNSPECIFIED
	if err == nil {
		p = r.Msg.GpuProfile
	}

	var text, short string
	switch p {
	case respb.GPUProfile_GPU_PROFILE_EGPU:
		text = "#[fg=#AE81FF]EGPU#[default] "
		short = "EGPU"
	case respb.GPUProfile_GPU_PROFILE_NVIDIA:
		text = "#[fg=#E8871A]NVIDIA#[default] "
		short = "NVIDIA"
	case respb.GPUProfile_GPU_PROFILE_IGPU:
		text = "#[fg=#66D9EF]IGPU#[default] "
		short = "IGPU"
	default:
		text = "#[fg=#A6E22E]HYBRID#[default] "
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
	switch r.Msg.AsusProfile {
	case respb.ASUSProfile_ASUS_PROFILE_PERF:
		b.WriteString("#[fg=#E8871A]PERF#[default] ")
	case respb.ASUSProfile_ASUS_PROFILE_BAL:
		b.WriteString("#[fg=#A6E22E]BAL#[default] ")
	case respb.ASUSProfile_ASUS_PROFILE_QUIET:
		b.WriteString("#[fg=#66D9EF]QUIET#[default] ")
	}

	// GPU profile
	switch r.Msg.GpuProfile {
	case respb.GPUProfile_GPU_PROFILE_EGPU:
		b.WriteString("#[fg=#AE81FF]EGPU#[default] ")
	case respb.GPUProfile_GPU_PROFILE_NVIDIA:
		b.WriteString("#[fg=#E8871A]NVIDIA#[default] ")
	case respb.GPUProfile_GPU_PROFILE_IGPU:
		b.WriteString("#[fg=#66D9EF]IGPU#[default] ")
	default:
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
		b.WriteString(fmt.Sprintf("CPU %s%d%% (%s) %s#[default]", pctColor(pct), pct, r.Msg.TopCpuProcess, bar(pct)))
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
		b.WriteString(fmt.Sprintf("RAM %s%.2fGiB %d%% (%s) %s#[default]", pctColor(pct), usedGiB, pct, r.Msg.TopMemProcess, bar(pct)))
		b.WriteString("#[default] ")
	}

	// WiFi
	if w := r.Msg.Wifi; w != nil && w.Percent > 0 {
		ipct := int(w.Percent)
		b.WriteString(fmt.Sprintf("WIFI %s%d%% (%s) %s#[default]", pctColor(100-ipct), ipct, w.Ssid, bar(ipct)))
		b.WriteString("#[default] ")
	}

	// Thin separator before time
	b.WriteString("#[fg=#E8E8E2,bg=#272822,none]   ")
	b.WriteString(timeStr)
	b.WriteString(" #[fg=#E8E8E2,bg=#272822,none]   ")
	b.WriteString(dateStr)

	// Powerline arrow to red section for layout
	b.WriteString(" #[fg=#E82572,bg=#272822,none]#[fg=#A6E22E,bg=#E82572,none] ")
	b.WriteString(r.Msg.KeyboardLayout)

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
		Background: func(ctx plugin.Context, stop <-chan struct{}) {
			go func() {
				for {
					stream, err := resClient.Watch(context.Background(), connect.NewRequest(&respb.WatchRequest{}))
					if err != nil {
						select {
						case <-stop:
							return
						case <-time.After(5 * time.Second):
						}
						continue
					}
					for stream.Receive() {
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
