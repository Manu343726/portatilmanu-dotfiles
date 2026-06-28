// Resources plugin — monitors system resources (RAM, CPU, disk, disk I/O).
//
// This plugin demonstrates the background worker pattern: a goroutine
// periodically collects system stats via ctx.Exec() and stores them in
// shared state. RPC handlers read that state to provide instant responses.
package main

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"dotfilesd/plugin"
	pb "plugins/resources/proto/resources"
	"plugins/resources/proto/resources/resourcesconnect"

	"connectrpc.com/connect"
)

// Data types — snapshot values produced by the background collector.

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

type ProcessInfo struct {
	PID        int
	Name       string
	CPUPercent float64
	MemPercent float64
	MemMB      float64
	State      string
}

// SharedState holds the latest snapshot, updated by the background goroutine.
type SharedState struct {
	mu     sync.RWMutex
	ram    RAMSnapshot
	cpu    CPUSnapshot
	disk   DiskSnapshot
	diskIO DiskIOSnapshot

	// History ring buffers (100 entries each)
	ramHistory  []float64
	cpuHistory  []float64
	diskHistory []float64
	maxHistory  int
}

func newSharedState() *SharedState {
	return &SharedState{
		maxHistory:  100,
		ramHistory:  make([]float64, 0, 100),
		cpuHistory:  make([]float64, 0, 100),
		diskHistory: make([]float64, 0, 100),
	}
}

func (s *SharedState) update(ram RAMSnapshot, cpu CPUSnapshot, disk DiskSnapshot, diskIO DiskIOSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ram = ram
	s.cpu = cpu
	s.disk = disk
	s.diskIO = diskIO

	// Append to history ring buffers.
	s.ramHistory = appendRing(s.ramHistory, ram.Percent, s.maxHistory)
	s.cpuHistory = appendRing(s.cpuHistory, cpu.TotalPercent, s.maxHistory)
	s.diskHistory = appendRing(s.diskHistory, disk.Percent, s.maxHistory)
}

func appendRing(buf []float64, val float64, max int) []float64 {
	if len(buf) < max {
		return append(buf, val)
	}
	buf = append(buf[1:], val)
	return buf
}

func (s *SharedState) get() (RAMSnapshot, CPUSnapshot, DiskSnapshot, DiskIOSnapshot) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ram, s.cpu, s.disk, s.diskIO
}

func (s *SharedState) getHistory(resource string, count int) []float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch resource {
	case "cpu":
		return lastN(s.cpuHistory, count)
	case "disk":
		return lastN(s.diskHistory, count)
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

// parseMem parses a line like "MemTotal:       16283988 kB" and returns the value in MB.
func parseMemLine(line, prefix string) float64 {
	for _, f := range strings.Fields(line) {
		if v, err := strconv.ParseFloat(f, 64); err == nil {
			return v / 1024 // kB to MB
		}
	}
	return 0
}

// resourcesServer implements the type-safe ResourcesService.
type resourcesServer struct {
	state *SharedState
}

func (s *resourcesServer) Current(ctx context.Context, req *connect.Request[pb.CurrentRequest]) (*connect.Response[pb.CurrentResponse], error) {
	ram, cpu, disk, diskIO := s.state.get()

	pc := plugin.ExtractContext(ctx)
	if pc != nil && pc.RenderOutput() {
		fmt.Fprintf(pc.Stdout(), "📊 Resources — RAM: %.0f/%.0f MB (%.0f%%) | CPU: %.0f%% (%.0f%% user, %.0f%% sys, %.0f%% iowait) | Disk: %.1f/%.1f GB (%.0f%%) on %s | Disk I/O: %.0f r/s %.0f w/s on %s\n",
			ram.UsedMB, ram.TotalMB, ram.Percent,
			cpu.TotalPercent, cpu.UserPercent, cpu.SystemPercent, cpu.IOwaitPercent,
			disk.UsedGB, disk.TotalGB, disk.Percent, disk.MountPoint,
			diskIO.ReadsPerSec, diskIO.WritesPerSec, diskIO.Device,
		)
	}

	return connect.NewResponse(&pb.CurrentResponse{
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
	}), nil
}

func (s *resourcesServer) Top(ctx context.Context, req *connect.Request[pb.TopRequest]) (*connect.Response[pb.TopResponse], error) {
	// For now return the current snapshot as the main data.
	// Detailed 'top' implementation would parse `ps aux` output, but
	// the minimal version just returns what we have.
	ram, cpu, disk, _ := s.state.get()
	_ = disk // not needed for top

	processes := []*pb.ProcessInfo{
		{
			Pid:        1,
			Name:       "system",
			MemPercent: ram.Percent,
			MemMb:      ram.UsedMB,
		},
	}

	// If CPU data available, add a synthetic process entry.
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
	// Simplified: return just the collector's aggregate data.
	ram, cpu, _, _ := s.state.get()
	_ = cpu

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
	resource := req.Msg.Resource
	if resource == "" {
		resource = "ram"
	}
	count := int(req.Msg.Count)
	if count <= 0 {
		count = 20
	}

	values := s.state.getHistory(resource, count)
	unit := "%"
	if resource == "disk" {
		unit = "%"
	}

	return connect.NewResponse(&pb.HistoryResponse{
		Values:   values,
		Resource: resource,
		Unit:     unit,
	}), nil
}

func backgroundCollector(ctx plugin.Context, state *SharedState, stop <-chan struct{}) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			ram := collectRAM(ctx)
			cpu := collectCPU(ctx)
			disk := collectDisk(ctx)
			diskIO := collectDiskIO(ctx)
			state.update(ram, cpu, disk, diskIO)
		}
	}
}

func collectRAM(ctx plugin.Context) RAMSnapshot {
	result, err := ctx.Exec("cat /proc/meminfo")
	if err != nil {
		return RAMSnapshot{}
	}

	var total, free, buffers, cached, available float64
	for _, line := range strings.Split(result.Stdout, "\n") {
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			total = parseMemLine(line, "MemTotal:")
		case strings.HasPrefix(line, "MemFree:"):
			free = parseMemLine(line, "MemFree:")
		case strings.HasPrefix(line, "Buffers:"):
			buffers = parseMemLine(line, "Buffers:")
		case strings.HasPrefix(line, "Cached:"):
			cached = parseMemLine(line, "Cached:")
		case strings.HasPrefix(line, "MemAvailable:"):
			available = parseMemLine(line, "MemAvailable:")
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

func collectCPU(ctx plugin.Context) CPUSnapshot {
	result, err := ctx.Exec("cat /proc/stat | grep '^cpu '")
	if err != nil || result.Stdout == "" {
		return CPUSnapshot{NumCores: 1}
	}

	fields := strings.Fields(result.Stdout)
	if len(fields) < 5 {
		return CPUSnapshot{NumCores: 1}
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
		NumCores:      runtimeCPUCount(ctx),
	}
}

func runtimeCPUCount(ctx plugin.Context) int {
	result, err := ctx.Exec("nproc")
	if err != nil || result.Stdout == "" {
		return 1
	}
	n, _ := strconv.Atoi(strings.TrimSpace(result.Stdout))
	if n < 1 {
		return 1
	}
	return n
}

func collectDisk(ctx plugin.Context) DiskSnapshot {
	result, err := ctx.Exec("df -h / | tail -1")
	if err != nil || result.Stdout == "" {
		return DiskSnapshot{}
	}

	fields := strings.Fields(result.Stdout)
	if len(fields) < 6 {
		return DiskSnapshot{}
	}

	total := parseGigabytes(fields[1])
	used := parseGigabytes(fields[2])
	avail := parseGigabytes(fields[3])
	percentStr := strings.TrimSuffix(fields[4], "%")
	percent, _ := strconv.ParseFloat(percentStr, 64)

	return DiskSnapshot{
		MountPoint: fields[5],
		TotalGB:    total,
		UsedGB:     used,
		AvailGB:    avail,
		Percent:    percent,
	}
}

func parseGigabytes(s string) float64 {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasSuffix(s, "G"):
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "G"), 64)
		return v
	case strings.HasSuffix(s, "M"):
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "M"), 64)
		return v / 1024
	case strings.HasSuffix(s, "T"):
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "T"), 64)
		return v * 1024
	default:
		v, _ := strconv.ParseFloat(s, 64)
		return v
	}
}

func collectDiskIO(ctx plugin.Context) DiskIOSnapshot {
	result, err := ctx.Exec("cat /proc/diskstats | grep -E 'sd[a-z]|nvme[0-9]' | head -1")
	if err != nil || result.Stdout == "" {
		return DiskIOSnapshot{}
	}

	fields := strings.Fields(result.Stdout)
	if len(fields) < 14 {
		return DiskIOSnapshot{}
	}

	device := fields[2]
	reads, _ := strconv.ParseFloat(fields[3], 64)
	writes, _ := strconv.ParseFloat(fields[7], 64)
	readSectors, _ := strconv.ParseFloat(fields[5], 64)
	writeSectors, _ := strconv.ParseFloat(fields[9], 64)

	// Sectors are 512 bytes each. Convert to bytes.
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

func main() {
	state := newSharedState()

	svc := &resourcesServer{state: state}
	path, handler := resourcesconnect.NewResourcesServiceHandler(svc)

	plugin.Serve(plugin.Config{
		Name:        "resources",
		DisplayName: "Resources",
		Version:     "1.0.0",
		Description: "System resource monitoring (RAM, CPU, disk, I/O)",
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
			backgroundCollector(ctx, state, stop)
		},
	})
}
