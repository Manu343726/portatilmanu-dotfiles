package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"

	"dotfilesd/plugin"
	pb "plugins/resources/proto/resources"
	"plugins/resources/proto/resources/resourcesconnect"

	"connectrpc.com/connect"
)

type RAMSnapshot struct {
	TotalMB     float64
	UsedMB      float64
	AvailableMB float64
	Percent     float64
}

type CPUSnapshot struct {
	TotalPercent  float64
	UserPercent   float64
	SystemPercent float64
	IOwaitPercent float64
	NumCores      int
}

type DiskSnapshot struct {
	MountPoint string
	TotalGB    float64
	UsedGB     float64
	AvailGB    float64
	Percent    float64
}

type DiskIOSnapshot struct {
	Device           string
	ReadsPerSec      float64
	WritesPerSec     float64
	ReadBytesPerSec  float64
	WriteBytesPerSec float64
}

type CPUTempSnapshot struct {
	TempCelsius float64
}

type BatterySnapshot struct {
	Percent    float64
	Charging   bool
	Plugged    bool
	Status     string
	EnergyNow  int64
	EnergyFull int64
	PowerNow   int64
}

type WiFiSnapshot struct {
	Interface string
	Percent   float64
	SSID      string
}

type ProcessInfo struct {
	PID        int
	Name       string
	CPUPercent float64
	MemPercent float64
	MemMB      float64
	State      string
}

type SharedState struct {
	mu     sync.RWMutex
	ram    RAMSnapshot
	cpu    CPUSnapshot
	disk   DiskSnapshot
	diskIO DiskIOSnapshot
	cpuTemp CPUTempSnapshot
	battery BatterySnapshot
	wifi    WiFiSnapshot

	asusProfile     pb.ASUSProfile
	gpuProfile      pb.GPUProfile
	keyboardLayout  string
	topCPUProcess   string
	topMemProcess   string

	pollCount int64

	// History ring buffers (100 entries each)
	ramHistory      []float64
	cpuHistory      []float64
	diskHistory     []float64
	cpuTempHistory  []float64
	batteryHistory  []float64
	maxHistory      int
}

func newSharedState() *SharedState {
	return &SharedState{
		maxHistory:     100,
		ramHistory:     make([]float64, 0, 100),
		cpuHistory:     make([]float64, 0, 100),
		diskHistory:    make([]float64, 0, 100),
		cpuTempHistory: make([]float64, 0, 100),
		batteryHistory: make([]float64, 0, 100),
	}
}

type systemSnapshot struct {
	ram    RAMSnapshot
	cpu    CPUSnapshot
	disk   DiskSnapshot
	diskIO DiskIOSnapshot
	cpuTemp CPUTempSnapshot
	battery BatterySnapshot
	wifi    WiFiSnapshot
}

func (s *SharedState) update(snap systemSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ram = snap.ram
	s.cpu = snap.cpu
	s.disk = snap.disk
	s.diskIO = snap.diskIO
	s.cpuTemp = snap.cpuTemp
	s.battery = snap.battery
	s.wifi = snap.wifi
	s.asusProfile = collectASUSProfile()
	s.gpuProfile = collectGPUProfile()
	s.keyboardLayout = collectKeyboardLayout()
	s.topCPUProcess = collectTopCPUProcess()
	s.topMemProcess = collectTopMemProcess()

	s.ramHistory = appendRing(s.ramHistory, snap.ram.Percent, s.maxHistory)
	s.cpuHistory = appendRing(s.cpuHistory, snap.cpu.TotalPercent, s.maxHistory)
	s.diskHistory = appendRing(s.diskHistory, snap.disk.Percent, s.maxHistory)
	s.cpuTempHistory = appendRing(s.cpuTempHistory, snap.cpuTemp.TempCelsius, s.maxHistory)
	s.batteryHistory = appendRing(s.batteryHistory, snap.battery.Percent, s.maxHistory)
}

func appendRing(buf []float64, val float64, max int) []float64 {
	if len(buf) < max {
		return append(buf, val)
	}
	buf = append(buf[1:], val)
	return buf
}

func (s *SharedState) get() (RAMSnapshot, CPUSnapshot, DiskSnapshot, DiskIOSnapshot, CPUTempSnapshot, BatterySnapshot, WiFiSnapshot, pb.ASUSProfile, pb.GPUProfile, string, string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ram, s.cpu, s.disk, s.diskIO, s.cpuTemp, s.battery, s.wifi,
		s.asusProfile, s.gpuProfile, s.keyboardLayout, s.topCPUProcess, s.topMemProcess
}

func (s *SharedState) getHistory(resource pb.ResourceType, count int) []float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch resource {
	case pb.ResourceType_RESOURCE_TYPE_CPU:
		return lastN(s.cpuHistory, count)
	case pb.ResourceType_RESOURCE_TYPE_DISK:
		return lastN(s.diskHistory, count)
	case pb.ResourceType_RESOURCE_TYPE_CPU_TEMP:
		return lastN(s.cpuTempHistory, count)
	case pb.ResourceType_RESOURCE_TYPE_BATTERY:
		return lastN(s.batteryHistory, count)
	default:
		return lastN(s.ramHistory, count)
	}
}

func lastN(buf []float64, n int) []float64 {
	if n > len(buf) {
		n = len(buf)
	}
	result := make([]float64, n)
	copy(result, buf[len(buf)-n:])
	return result
}

func parseMemValue(line string) float64 {
	for _, f := range strings.Fields(line) {
		if v, err := strconv.ParseFloat(f, 64); err == nil {
			return v / 1024
		}
	}
	return 0
}

type subscriber struct {
	ch chan *pb.CurrentResponse
}

type resourcesServer struct {
	state   *SharedState
	poller  *SmartPoller
	subs    map[string]*subscriber
	subMu   sync.Mutex
	subID   int64
}

func (s *resourcesServer) buildResponse() *pb.CurrentResponse {
	ram, cpu, disk, diskIO, cpuTemp, battery, wifi, asusProfile, gpuProfile, keyboardLayout, topCPUProc, topMemProc := s.state.get()

	return &pb.CurrentResponse{
		Ram: &pb.RAMSnapshot{
			TotalMb:     ram.TotalMB,
			UsedMb:      ram.UsedMB,
			AvailableMb: ram.AvailableMB,
			Percent:     ram.Percent,
		},
		Cpu: &pb.CPUSnapshot{
			TotalPercent:  cpu.TotalPercent,
			UserPercent:   cpu.UserPercent,
			SystemPercent: cpu.SystemPercent,
			IowaitPercent: cpu.IOwaitPercent,
			NumCores:      int32(cpu.NumCores),
		},
		Disk: &pb.DiskSnapshot{
			MountPoint: disk.MountPoint,
			TotalGb:    disk.TotalGB,
			UsedGb:     disk.UsedGB,
			AvailGb:    disk.AvailGB,
			Percent:    disk.Percent,
		},
		DiskIo: &pb.DiskIOSnapshot{
			Device:           diskIO.Device,
			ReadsPerSec:      diskIO.ReadsPerSec,
			WritesPerSec:     diskIO.WritesPerSec,
			ReadBytesPerSec:  diskIO.ReadBytesPerSec,
			WriteBytesPerSec: diskIO.WriteBytesPerSec,
		},
		CpuTemp: &pb.CPUTempSnapshot{
			TempCelsius: cpuTemp.TempCelsius,
		},
		Battery: &pb.BatterySnapshot{
			Percent:    battery.Percent,
			Charging:   battery.Charging,
			Plugged:    battery.Plugged,
			Status:     batteryStatusToProto(battery.Status),
			EnergyNow:  battery.EnergyNow,
			EnergyFull: battery.EnergyFull,
			PowerNow:   battery.PowerNow,
		},
		Wifi: &pb.WiFiSnapshot{
			Interface: wifi.Interface,
			Percent:   wifi.Percent,
			Ssid:      wifi.SSID,
		},
		AsusProfile:    asusProfile,
		GpuProfile:     gpuProfile,
		KeyboardLayout: keyboardLayout,
		TopCpuProcess:  topCPUProc,
		TopMemProcess:  topMemProc,
	}
}

func (s *resourcesServer) broadcast() {
	resp := s.buildResponse()
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for _, sub := range s.subs {
		select {
		case sub.ch <- resp:
		default:
		}
	}
}

func (s *resourcesServer) Watch(ctx context.Context, req *connect.Request[pb.WatchRequest], stream *connect.ServerStream[pb.CurrentResponse]) error {
	sub := &subscriber{ch: make(chan *pb.CurrentResponse, 4)}
	s.subMu.Lock()
	s.subID++
	id := fmt.Sprintf("sub_%d", s.subID)
	if s.subs == nil {
		s.subs = make(map[string]*subscriber)
	}
	s.subs[id] = sub
	s.subMu.Unlock()

	defer func() {
		s.subMu.Lock()
		delete(s.subs, id)
		s.subMu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case resp := <-sub.ch:
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	}
}

func (s *resourcesServer) ensureFreshData() {
	s.poller.NoteCall()
	if s.poller.IsStale() {
		s.poller.PollNow()
	}
}

func (s *resourcesServer) Current(ctx context.Context, req *connect.Request[pb.CurrentRequest]) (*connect.Response[pb.CurrentResponse], error) {
	s.ensureFreshData()
	ram, cpu, disk, diskIO, cpuTemp, battery, _, _, _, _, _, _ := s.state.get()

	pc := plugin.ExtractContext(ctx)
	if pc != nil {
		peer := req.Peer()
		pc.Log().Info("▶ Resources.Current",
			"peer", peer.Addr,
			"protocol", peer.Protocol,
			"render_output", pc.RenderOutput(),
			"ram_pct", ram.Percent,
			"cpu_pct", cpu.TotalPercent,
			"cpu_temp", cpuTemp.TempCelsius,
			"battery_pct", battery.Percent,
			"battery_charging", battery.Charging,
			"ac_plugged", battery.Plugged,
		)

		if result, err := pc.Exec("uname -a"); err == nil {
			pc.Log().Info("Current exec check", "stdout", strings.TrimSpace(result.Stdout))
		}
	}
	if pc != nil && pc.RenderOutput() {
		batteryStr := ""
		if cpuTemp.TempCelsius > 0 {
			batteryStr = fmt.Sprintf(" | CPU temp: %.0f°C", cpuTemp.TempCelsius)
		}
		if battery.Percent > 0 {
			batteryStr += fmt.Sprintf(" | Battery: %.0f%%", battery.Percent)
			switch {
			case battery.Charging:
				batteryStr += " (charging)"
			case battery.Plugged:
				batteryStr += " (plugged)"
			default:
				batteryStr += " (discharging)"
			}
		}
		fmt.Fprintf(pc.Stdout(), " RAM: %.0f/%.0f MB (%.0f%%) | CPU: %.0f%% (%.0f%% user, %.0f%% sys, %.0f%% iowait) | Disk: %.1f/%.1f GB (%.0f%%) on %s | Disk I/O: %.0f r/s %.0f w/s on %s%s\n",
			ram.UsedMB, ram.TotalMB, ram.Percent,
			cpu.TotalPercent, cpu.UserPercent, cpu.SystemPercent, cpu.IOwaitPercent,
			disk.UsedGB, disk.TotalGB, disk.Percent, disk.MountPoint,
			diskIO.ReadsPerSec, diskIO.WritesPerSec, diskIO.Device,
			batteryStr,
		)
	}

	return connect.NewResponse(s.buildResponse()), nil
}

func (s *resourcesServer) Top(ctx context.Context, req *connect.Request[pb.TopRequest]) (*connect.Response[pb.TopResponse], error) {
	s.ensureFreshData()
	ram, cpu, disk, _, _, _, _, _, _, _, _, _ := s.state.get()
	_ = disk

	pc := plugin.ExtractContext(ctx)
	if pc != nil {
		peer := req.Peer()
		pc.Log().Info("▶ Resources.Top",
			"peer", peer.Addr,
			"protocol", peer.Protocol,
			"render_output", pc.RenderOutput(),
			"count", req.Msg.Count,
			"sort", req.Msg.Sort,
		)
	}

	processes := []*pb.ProcessInfo{
		{
			Pid:        1,
			Name:       "system",
			MemPercent: ram.Percent,
			MemMb:      ram.UsedMB,
		},
	}

	if cpu.TotalPercent > 0 {
		processes = append(processes, &pb.ProcessInfo{
			Pid:        0,
			Name:       "cpu",
			CpuPercent: cpu.TotalPercent,
		})
	}

	return connect.NewResponse(&pb.TopResponse{
		Processes: processes,
	}), nil
}

func (s *resourcesServer) PS(ctx context.Context, req *connect.Request[pb.PSRequest]) (*connect.Response[pb.PSResponse], error) {
	s.ensureFreshData()
	ram, cpu, _, _, _, _, _, _, _, _, _, _ := s.state.get()
	_ = cpu

	pc := plugin.ExtractContext(ctx)
	if pc != nil {
		peer := req.Peer()
		pc.Log().Info("▶ Resources.PS",
			"peer", peer.Addr,
			"protocol", peer.Protocol,
			"render_output", pc.RenderOutput(),
			"pid", req.Msg.Pid,
			"count", req.Msg.Count,
		)
	}

	return connect.NewResponse(&pb.PSResponse{
		Processes: []*pb.ProcessInfo{
			{
				Pid:        1,
				Name:       "system",
				MemPercent: ram.Percent,
				MemMb:      ram.UsedMB,
			},
		},
	}), nil
}

func (s *resourcesServer) History(ctx context.Context, req *connect.Request[pb.HistoryRequest]) (*connect.Response[pb.HistoryResponse], error) {
	s.ensureFreshData()
	resource := req.Msg.Resource
	if resource == pb.ResourceType_RESOURCE_TYPE_UNSPECIFIED {
		resource = pb.ResourceType_RESOURCE_TYPE_RAM
	}
	count := int(req.Msg.Count)
	if count <= 0 {
		count = 20
	}

	pc := plugin.ExtractContext(ctx)
	if pc != nil {
		peer := req.Peer()
		pc.Log().Info("▶ Resources.History",
			"peer", peer.Addr,
			"protocol", peer.Protocol,
			"render_output", pc.RenderOutput(),
			"resource", resource,
			"count", count,
		)
	}

	values := s.state.getHistory(resource, count)
	unit := pb.Unit_UNIT_PERCENT
	if resource == pb.ResourceType_RESOURCE_TYPE_CPU_TEMP {
		unit = pb.Unit_UNIT_CELSIUS
	}

	return connect.NewResponse(&pb.HistoryResponse{
		Values:   values,
		Resource: resource,
		Unit:     unit,
	}), nil
}

func collectAll(state *SharedState) {
	snap := systemSnapshot{
		ram:     collectRAM(),
		cpu:     collectCPU(),
		disk:    collectDisk(),
		diskIO:  collectDiskIO(),
		cpuTemp: collectCPUTemp(),
		battery: collectBattery(),
		wifi:    collectWiFi(),
	}
	state.update(snap)
}

func collectRAM() RAMSnapshot {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return RAMSnapshot{}
	}

	var total, free, buffers, cached, available float64
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			total = parseMemValue(line)
		case strings.HasPrefix(line, "MemFree:"):
			free = parseMemValue(line)
		case strings.HasPrefix(line, "Buffers:"):
			buffers = parseMemValue(line)
		case strings.HasPrefix(line, "Cached:"):
			cached = parseMemValue(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			available = parseMemValue(line)
		}
	}

	if available == 0 {
		available = free + buffers + cached
	}
	used := total - available
	percent := math.Round(used/total*100*10) / 10

	return RAMSnapshot{
		TotalMB:     math.Round(total*10) / 10,
		UsedMB:      math.Round(used*10) / 10,
		AvailableMB: math.Round(available*10) / 10,
		Percent:     percent,
	}
}

func collectCPU() CPUSnapshot {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return CPUSnapshot{NumCores: runtime.NumCPU()}
	}

	var cpuLine string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cpu ") {
			cpuLine = line
			break
		}
	}

	if cpuLine == "" {
		return CPUSnapshot{NumCores: runtime.NumCPU()}
	}

	fields := strings.Fields(cpuLine)
	if len(fields) < 5 {
		return CPUSnapshot{NumCores: runtime.NumCPU()}
	}

	user, _ := strconv.ParseFloat(fields[1], 64)
	nice, _ := strconv.ParseFloat(fields[2], 64)
	system, _ := strconv.ParseFloat(fields[3], 64)
	idle, _ := strconv.ParseFloat(fields[4], 64)
	iowait, _ := strconv.ParseFloat(fields[5], 64)

	total := user + nice + system + idle + iowait
	active := total - idle

	percent := math.Round(active/total*100*10) / 10
	userPercent := math.Round((user+nice)/total*100*10) / 10
	sysPercent := math.Round(system/total*100*10) / 10
	ioPercent := math.Round(iowait/total*100*10) / 10

	return CPUSnapshot{
		TotalPercent:  percent,
		UserPercent:   userPercent,
		SystemPercent: sysPercent,
		IOwaitPercent: ioPercent,
		NumCores:      runtime.NumCPU(),
	}
}

func collectDisk() DiskSnapshot {
	var st unix.Statfs_t
	if err := unix.Statfs("/", &st); err != nil {
		return DiskSnapshot{MountPoint: "/"}
	}

	totalBytes := float64(st.Blocks) * float64(st.Bsize)
	freeBytes := float64(st.Bfree) * float64(st.Bsize)
	availBytes := float64(st.Bavail) * float64(st.Bsize)
	usedBytes := totalBytes - freeBytes

	const gb = 1024 * 1024 * 1024
	totalGB := totalBytes / gb
	usedGB := usedBytes / gb
	availGB := availBytes / gb
	percent := math.Round(usedBytes/totalBytes*100*10) / 10

	return DiskSnapshot{
		MountPoint: "/",
		TotalGB:    math.Round(totalGB*10) / 10,
		UsedGB:     math.Round(usedGB*10) / 10,
		AvailGB:    math.Round(availGB*10) / 10,
		Percent:    percent,
	}
}

func collectDiskIO() DiskIOSnapshot {
	data, err := os.ReadFile("/proc/diskstats")
	if err != nil {
		return DiskIOSnapshot{}
	}

	var device string
	var reads, writes, readSectors, writeSectors float64

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 14 {
			continue
		}
		name := fields[2]
		if !isPhysicalDisk(name) {
			continue
		}
		device = name
		reads, _ = strconv.ParseFloat(fields[3], 64)
		writes, _ = strconv.ParseFloat(fields[7], 64)
		readSectors, _ = strconv.ParseFloat(fields[5], 64)
		writeSectors, _ = strconv.ParseFloat(fields[9], 64)
		break
	}

	readBytes := readSectors * 512
	writeBytes := writeSectors * 512

	return DiskIOSnapshot{
		Device:           device,
		ReadsPerSec:      reads,
		WritesPerSec:     writes,
		ReadBytesPerSec:  readBytes,
		WriteBytesPerSec: writeBytes,
	}
}

func isPhysicalDisk(name string) bool {
	if len(name) >= 2 && name[0] == 's' && name[1] == 'd' {
		return len(name) == 2 || (name[2] >= 'a' && name[2] <= 'z')
	}
	if len(name) >= 4 && name[:4] == "nvme" {
		return len(name) > 4 && name[4] >= '0' && name[4] <= '9'
	}
	return false
}

func collectCPUTemp() CPUTempSnapshot {
	zones, err := os.ReadDir("/sys/class/thermal")
	if err != nil {
		return CPUTempSnapshot{}
	}

	for _, z := range zones {
		name := z.Name()
		if !strings.HasPrefix(name, "thermal_zone") {
			continue
		}

		typePath := "/sys/class/thermal/" + name + "/type"
		typeData, err := os.ReadFile(typePath)
		if err != nil {
			continue
		}

		// Prefer the acpitz zone (closest to CPU socket).
		if strings.TrimSpace(string(typeData)) != "acpitz" {
			continue
		}

		tempPath := "/sys/class/thermal/" + name + "/temp"
		tempData, err := os.ReadFile(tempPath)
		if err != nil {
			continue
		}

		millideg, _ := strconv.ParseFloat(strings.TrimSpace(string(tempData)), 64)
		if millideg > 0 {
			return CPUTempSnapshot{TempCelsius: millideg / 1000}
		}
	}

	return CPUTempSnapshot{}
}

func collectBattery() BatterySnapshot {
	supplies, err := os.ReadDir("/sys/class/power_supply")
	if err != nil {
		return BatterySnapshot{}
	}

	var bat BatterySnapshot

	for _, s := range supplies {
		name := s.Name()
		typePath := "/sys/class/power_supply/" + name + "/type"

		typeData, err := os.ReadFile(typePath)
		if err != nil {
			continue
		}
		typ := strings.TrimSpace(string(typeData))

		switch typ {
		case "Mains":
			onlinePath := "/sys/class/power_supply/" + name + "/online"
			if data, err := os.ReadFile(onlinePath); err == nil {
				bat.Plugged = strings.TrimSpace(string(data)) == "1"
			}

		case "Battery":
			capPath := "/sys/class/power_supply/" + name + "/capacity"
			if data, err := os.ReadFile(capPath); err == nil {
				bat.Percent, _ = strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
			}

			statusPath := "/sys/class/power_supply/" + name + "/status"
			if data, err := os.ReadFile(statusPath); err == nil {
				bat.Status = strings.TrimSpace(string(data))
				bat.Charging = bat.Status == "Charging"
			}

			for _, f := range []struct {
				path string
				dst  *int64
			}{
				{"/sys/class/power_supply/" + name + "/energy_now", &bat.EnergyNow},
				{"/sys/class/power_supply/" + name + "/energy_full", &bat.EnergyFull},
				{"/sys/class/power_supply/" + name + "/power_now", &bat.PowerNow},
			} {
				if data, err := os.ReadFile(f.path); err == nil {
					*f.dst, _ = strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
				}
			}
		}
	}

	return bat
}

func batteryStatusToProto(s string) pb.BatteryStatus {
	switch s {
	case "Charging":
		return pb.BatteryStatus_BATTERY_STATUS_CHARGING
	case "Discharging":
		return pb.BatteryStatus_BATTERY_STATUS_DISCHARGING
	case "Full":
		return pb.BatteryStatus_BATTERY_STATUS_FULL
	case "Not charging":
		return pb.BatteryStatus_BATTERY_STATUS_NOT_CHARGING
	default:
		return pb.BatteryStatus_BATTERY_STATUS_UNSPECIFIED
	}
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

func collectWiFi() WiFiSnapshot {
	iface := findWiFiInterface()
	if iface == "" {
		return WiFiSnapshot{}
	}
	return WiFiSnapshot{
		Interface: iface,
		Percent:   float64(readWiFiSignal(iface)),
		SSID:      readWiFiSSID(iface),
	}
}

func collectASUSProfile() pb.ASUSProfile {
	out, err := exec.Command("asusctl", "profile", "get").Output()
	if err != nil {
		return pb.ASUSProfile_ASUS_PROFILE_UNSPECIFIED
	}
	fields := strings.Fields(string(out))
	if len(fields) < 3 {
		return pb.ASUSProfile_ASUS_PROFILE_UNSPECIFIED
	}
	switch fields[2] {
	case "Performance":
		return pb.ASUSProfile_ASUS_PROFILE_PERF
	case "Balanced":
		return pb.ASUSProfile_ASUS_PROFILE_BAL
	case "Quiet":
		return pb.ASUSProfile_ASUS_PROFILE_QUIET
	}
	return pb.ASUSProfile_ASUS_PROFILE_UNSPECIFIED
}

func collectGPUProfile() pb.GPUProfile {
	if raw, err := os.ReadFile("/sys/devices/platform/asus-nb-wmi/egpu_connected"); err == nil && strings.TrimSpace(string(raw)) == "1" {
		return pb.GPUProfile_GPU_PROFILE_EGPU
	}
	if raw, err := os.ReadFile("/sys/devices/platform/asus-nb-wmi/gpu_mux_mode"); err == nil && strings.TrimSpace(string(raw)) == "1" {
		return pb.GPUProfile_GPU_PROFILE_NVIDIA
	}
	if raw, err := os.ReadFile("/sys/devices/platform/asus-nb-wmi/dgpu_disable"); err == nil && strings.TrimSpace(string(raw)) == "1" {
		return pb.GPUProfile_GPU_PROFILE_IGPU
	}
	return pb.GPUProfile_GPU_PROFILE_HYBRID
}

func collectKeyboardLayout() string {
	out, err := exec.Command("xkb-switch").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func collectTopCPUProcess() string {
	out, err := exec.Command("ps", "-eo", "comm", "--sort=-%cpu").Output()
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
		}
	}
	return name
}

func collectTopMemProcess() string {
	out, err := exec.Command("ps", "-eo", "comm", "--sort=-%mem").Output()
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
		}
	}
	return name
}

func main() {
	state := newSharedState()
	poller := NewSmartPoller()

	svc := &resourcesServer{state: state, poller: poller}
	path, handler := resourcesconnect.NewResourcesServiceHandler(svc)

	plugin.Serve(plugin.Config{
		Name:        "resources",
		DisplayName: "Resources",
		Version:     "1.0.0",
		Description: "System resource monitoring (RAM, CPU, disk, I/O)",
		DocsProto:   pb.PluginDocs,
		Services: []plugin.Service{
			{
				Name:             "resources.ResourcesService",
				Description:      "Type-safe resource monitoring API for plugin-to-plugin calls",
				Path:             path,
				Handler:          handler,
				PluginAccessible: true,
			},
		},
		Background: func(ctx plugin.Context, stop <-chan struct{}) {
			poller.Run(stop, func() {
				collectAll(state)
				svc.broadcast()
			})
		},
	})
}
