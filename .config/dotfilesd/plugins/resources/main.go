// Resources plugin — monitors system resources (RAM, CPU, disk, disk I/O).
//
// This plugin demonstrates the background worker pattern: a goroutine
// periodically collects system stats via ctx.Exec() and stores them in
// shared state. Tools then read that state to provide instant responses.
package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"dotfilesd/plugin"
	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

// Data types — snapshot values produced by the background collector
// =========================================================================

// RAMSnapshot is a point-in-time measurement of system memory.
type RAMSnapshot struct {
	TotalMB     float64
	UsedMB      float64
	AvailableMB float64
	Percent     float64
}

// CPUSnapshot is a point-in-time measurement of aggregate CPU usage.
type CPUSnapshot struct {
	UserPercent   float64
	SystemPercent float64
	IdlePercent   float64
	IowaitPercent float64
	TotalPercent  float64
	NumCores      int
}

// DiskSnapshot is a point-in-time measurement of a mount point's usage.
type DiskSnapshot struct {
	MountPoint string
	TotalGB    float64
	UsedGB     float64
	AvailGB    float64
	Percent    float64
}

// DiskIOSnapshot is a point-in-time measurement of I/O rates for a device.
type DiskIOSnapshot struct {
	Device           string
	ReadsPerSec      float64
	WritesPerSec     float64
	ReadBytesPerSec  float64
	WriteBytesPerSec float64
}

// ProcessInfo describes a single running process.
type ProcessInfo struct {
	PID     int
	PPID    int
	User    string
	CPU     float64 // percent of one core
	RAM     float64 // percent of total RAM
	RSS_MB  float64 // resident set size
	VSZ_MB  float64 // virtual memory size
	State   string
	Command string
}

// SystemSnapshot is a complete point-in-time view of system resources.
type SystemSnapshot struct {
	Timestamp time.Time
	RAM       RAMSnapshot
	CPU       CPUSnapshot
	Disks     []DiskSnapshot
	DiskIO    []DiskIOSnapshot
}

// =========================================================================
// Raw delta state — previous values for rate/percent calculations
// =========================================================================

type cpuRaw struct {
	user, nice, system, idle, iowait, irq, softirq, steal uint64
}

type diskIORaw struct {
	readOps, writeOps         uint64
	readSectors, writeSectors uint64
}

// =========================================================================
// Monitor — shared state with mutex, background collection
// =========================================================================

type monitor struct {
	mu sync.RWMutex

	current SystemSnapshot
	history []SystemSnapshot // ring buffer, maxHistory entries

	// Previous raw values for delta computation.
	prevCPU    cpuRaw
	prevDiskIO map[string]diskIORaw
	firstCPU   bool // false until we have two samples for a delta
	numCores   int
}

const (
	collectInterval = 3 * time.Second
	maxHistory      = 100 // ~5 minutes at 3s intervals
)

// collect runs in a background goroutine, sampling system stats.
func (m *monitor) collect(ctx plugin.Context, stop <-chan struct{}) {
	ctx.Log().Info("starting resource collection background loop", "interval", collectInterval.String())

	// Prime the CPU with an initial sample so we can compute deltas.
	for retries := 0; retries < 3; retries++ {
		if raw, err := readCPUStat(ctx); err == nil {
			m.prevCPU = raw
			m.firstCPU = true
			ctx.Log().Trace("initial CPU sample collected")
			break
		} else if retries < 2 {
			ctx.Log().Debug("retrying initial CPU sample", "retry", retries+1, "error", err)
			time.Sleep(500 * time.Millisecond)
		} else {
			ctx.Log().Warn("failed to collect initial CPU sample after retries", "error", err)
		}
	}

	// Get core count once. Retry because the daemon's context server
	// may not be ready when the background goroutine starts (race).
	for retries := 0; retries < 5; retries++ {
		if rawList, err := readCPUPerCore(ctx); err == nil {
			m.numCores = len(rawList)
			ctx.Log().Debug("detected CPU cores", "count", m.numCores)
			break
		} else if retries < 4 {
			ctx.Log().Debug("retrying CPU core detection", "retry", retries+1, "error", err)
			time.Sleep(500 * time.Millisecond)
		} else {
			ctx.Log().Warn("failed to detect CPU cores, defaulting to 1", "error", err)
		}
	}
	if m.numCores == 0 {
		m.numCores = 1
	}

	for {
		select {
		case <-stop:
			ctx.Log().Info("resource collection stopped")
			return
		case <-time.After(collectInterval):
		}

		snap := SystemSnapshot{Timestamp: time.Now()}

		// RAM.
		if mem, err := collectRAM(ctx); err == nil {
			snap.RAM = mem
		} else {
			ctx.Log().Debug("failed to collect RAM", "error", err)
		}

		// CPU (delta-based).
		if raw, err := readCPUStat(ctx); err == nil && !m.firstCPU {
			snap.CPU = computeCPU(m.prevCPU, raw, m.numCores)
		}
		if raw, err := readCPUStat(ctx); err == nil {
			m.prevCPU = raw
			m.firstCPU = false
		} else {
			ctx.Log().Debug("failed to read CPU stats", "error", err)
		}

		// Disk usage.
		if disks, err := collectDisks(ctx); err == nil {
			snap.Disks = disks
		} else {
			ctx.Log().Debug("failed to collect disk usage", "error", err)
		}

		// Disk I/O rates (delta-based).
		if ioList, err := collectDiskIO(ctx, m.prevDiskIO); err == nil {
			snap.DiskIO = ioList
		}
		if rawMap, err := readDiskIORaw(ctx); err == nil {
			m.prevDiskIO = rawMap
		} else {
			ctx.Log().Debug("failed to read disk I/O raw", "error", err)
		}

		m.mu.Lock()
		m.current = snap
		m.history = append(m.history, snap)
		if len(m.history) > maxHistory {
			m.history = m.history[len(m.history)-maxHistory:]
		}
		m.mu.Unlock()

		ctx.Log().Trace("resource snapshot collected",
			"ram_pct", snap.RAM.Percent,
			"cpu_pct", snap.CPU.TotalPercent,
			"disks", len(snap.Disks),
			"disk_io", len(snap.DiskIO),
		)
	}
}

// =========================================================================
// Collector helpers — each reads and parses a single /proc file
// =========================================================================

func collectRAM(ctx plugin.Context) (RAMSnapshot, error) {
	out, err := ctx.Exec("cat /proc/meminfo")
	if err != nil {
		return RAMSnapshot{}, fmt.Errorf("read meminfo: %w", err)
	}
	return parseMeminfo(out.Stdout)
}

func parseMeminfo(data string) (RAMSnapshot, error) {
	var totalKB, freeKB, availKB, bufKB, cacheKB float64
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		val, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			continue
		}
		// Values in kB.
		switch {
		case strings.HasPrefix(parts[0], "MemTotal"):
			totalKB = val
		case strings.HasPrefix(parts[0], "MemFree"):
			freeKB = val
		case strings.HasPrefix(parts[0], "MemAvailable"):
			availKB = val
		case strings.HasPrefix(parts[0], "Buffers"):
			bufKB = val
		case strings.HasPrefix(parts[0], "Cached"):
			cacheKB = val
		}
	}
	if totalKB == 0 {
		return RAMSnapshot{}, fmt.Errorf("could not parse MemTotal")
	}
	// Used = total - available (best approximation of actively used).
	usedKB := totalKB - availKB
	if usedKB < 0 {
		usedKB = totalKB - freeKB - bufKB - cacheKB
	}
	if usedKB < 0 {
		usedKB = 0
	}
	return RAMSnapshot{
		TotalMB:     totalKB / 1024,
		UsedMB:      usedKB / 1024,
		AvailableMB: availKB / 1024,
		Percent:     usedKB / totalKB * 100,
	}, nil
}

func readCPUStat(ctx plugin.Context) (cpuRaw, error) {
	out, err := ctx.Exec("head -1 /proc/stat")
	if err != nil {
		return cpuRaw{}, fmt.Errorf("read /proc/stat: %w", err)
	}
	return parseCPUStatLine(out.Stdout)
}

func readCPUPerCore(ctx plugin.Context) ([]cpuRaw, error) {
	out, err := ctx.Exec("cat /proc/stat")
	if err != nil {
		return nil, fmt.Errorf("read /proc/stat: %w", err)
	}
	var cores []cpuRaw
	for _, line := range strings.Split(out.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "cpu") {
			continue
		}
		// "cpu" (aggregate) has no digit after — skip it.
		if line == "cpu" || len(line) < 4 || line[3] < '0' || line[3] > '9' {
			continue
		}
		raw, err := parseCPUStatLine(line)
		if err != nil {
			continue
		}
		cores = append(cores, raw)
	}
	return cores, nil
}

func parseCPUStatLine(line string) (cpuRaw, error) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return cpuRaw{}, fmt.Errorf("short cpu line: %q", line)
	}
	var vals [8]uint64
	for i := 1; i < len(fields) && i-1 < len(vals); i++ {
		v, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return cpuRaw{}, fmt.Errorf("bad cpu field %d: %w", i, err)
		}
		vals[i-1] = v
	}
	return cpuRaw{
		user:    vals[0],
		nice:    vals[1],
		system:  vals[2],
		idle:    vals[3],
		iowait:  vals[4],
		irq:     vals[5],
		softirq: vals[6],
		steal:   vals[7],
	}, nil
}

func computeCPU(prev, curr cpuRaw, numCores int) CPUSnapshot {
	prevIdle := prev.idle + prev.iowait
	idle := curr.idle + curr.iowait

	prevTotal := prev.user + prev.nice + prev.system + prev.idle + prev.iowait + prev.irq + prev.softirq + prev.steal
	total := curr.user + curr.nice + curr.system + curr.idle + curr.iowait + curr.irq + curr.softirq + curr.steal

	totalDelta := total - prevTotal
	if totalDelta == 0 {
		return CPUSnapshot{NumCores: numCores}
	}

	idleDelta := idle - prevIdle

	userDelta := curr.user - prev.user
	sysDelta := curr.system - prev.system
	iowaitDelta := curr.iowait - prev.iowait

	return CPUSnapshot{
		UserPercent:   float64(userDelta) / float64(totalDelta) * 100,
		SystemPercent: float64(sysDelta) / float64(totalDelta) * 100,
		IowaitPercent: float64(iowaitDelta) / float64(totalDelta) * 100,
		IdlePercent:   float64(idleDelta) / float64(totalDelta) * 100,
		TotalPercent:  float64(totalDelta-idleDelta) / float64(totalDelta) * 100,
		NumCores:      numCores,
	}
}

func collectDisks(ctx plugin.Context) ([]DiskSnapshot, error) {
	out, err := ctx.Exec("df -B1 --type=ext4 --type=ext3 --type=ext2 --type=btrfs --type=xfs --type=zfs 2>/dev/null | tail -n+2")
	if err != nil {
		// Fallback without type filter.
		out, err = ctx.Exec("df -B1 2>/dev/null | tail -n+2")
		if err != nil {
			return nil, fmt.Errorf("df: %w", err)
		}
	}
	return parseDF(out.Stdout)
}

func parseDF(data string) ([]DiskSnapshot, error) {
	var disks []DiskSnapshot
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		// Fields: filesystem, 1B-blocks, used, avail, use%, mountpoint.
		total, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			continue
		}
		used, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			continue
		}
		avail, err := strconv.ParseFloat(fields[3], 64)
		if err != nil {
			continue
		}
		pctStr := strings.TrimSuffix(fields[4], "%")
		pct, err := strconv.ParseFloat(pctStr, 64)
		if err != nil {
			continue
		}
		// Skip pseudo filesystems and non-root paths.
		mnt := fields[5]
		if strings.HasPrefix(mnt, "/dev") || strings.HasPrefix(mnt, "/sys") ||
			strings.HasPrefix(mnt, "/proc") || strings.HasPrefix(mnt, "/run") {
			continue
		}
		disks = append(disks, DiskSnapshot{
			MountPoint: mnt,
			TotalGB:    total / (1024 * 1024 * 1024),
			UsedGB:     used / (1024 * 1024 * 1024),
			AvailGB:    avail / (1024 * 1024 * 1024),
			Percent:    pct,
		})
	}
	if len(disks) == 0 {
		// No real filesystems found; that's fine.
		return disks, nil
	}
	return disks, nil
}

func readDiskIORaw(ctx plugin.Context) (map[string]diskIORaw, error) {
	out, err := ctx.Exec("cat /proc/diskstats")
	if err != nil {
		return nil, fmt.Errorf("read diskstats: %w", err)
	}
	return parseDiskStats(out.Stdout)
}

func parseDiskStats(data string) (map[string]diskIORaw, error) {
	result := make(map[string]diskIORaw)
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 14 {
			continue
		}
		name := fields[2]
		// Skip partitions, loop, zram, dm-, ram.
		if isVirtualDisk(name) {
			continue
		}
		ro, _ := strconv.ParseUint(fields[3], 10, 64)
		rs, _ := strconv.ParseUint(fields[5], 10, 64)
		wo, _ := strconv.ParseUint(fields[7], 10, 64)
		ws, _ := strconv.ParseUint(fields[9], 10, 64)
		result[name] = diskIORaw{
			readOps:      ro,
			readSectors:  rs,
			writeOps:     wo,
			writeSectors: ws,
		}
	}
	return result, nil
}

func isVirtualDisk(name string) bool {
	if len(name) > 0 && name[len(name)-1] >= '0' && name[len(name)-1] <= '9' &&
		!strings.HasPrefix(name, "nvme") {
		// Partition: ends in a digit but isn't nvme (nvme0n1p1 is ok, nvme0n1 is not).
		return true
	}
	// nvme partitions look like nvme0n1p1 — keep the base device (nvme0n1)
	// but skip partitions (nvme0n1p1, etc).
	if strings.HasPrefix(name, "nvme") && strings.Contains(name, "p") {
		// Check if there's a digit after 'p'.
		parts := strings.SplitN(name, "p", 2)
		if len(parts) == 2 && len(parts[1]) > 0 && parts[1][0] >= '0' && parts[1][0] <= '9' {
			return true
		}
	}
	return strings.HasPrefix(name, "loop") ||
		strings.HasPrefix(name, "zram") ||
		strings.HasPrefix(name, "dm-") ||
		strings.HasPrefix(name, "ram")
}

func collectDiskIO(ctx plugin.Context, prev map[string]diskIORaw) ([]DiskIOSnapshot, error) {
	curr, err := readDiskIORaw(ctx)
	if err != nil || prev == nil {
		return nil, err
	}
	interval := collectInterval.Seconds()

	var list []DiskIOSnapshot
	for name, cur := range curr {
		p, ok := prev[name]
		if !ok {
			continue
		}
		readDelta := cur.readOps - p.readOps
		writeDelta := cur.writeOps - p.writeOps
		sectorReadDelta := cur.readSectors - p.readSectors
		sectorWriteDelta := cur.writeSectors - p.writeSectors

		// A sector is 512 bytes.
		bytesRead := float64(sectorReadDelta) * 512
		bytesWrite := float64(sectorWriteDelta) * 512

		list = append(list, DiskIOSnapshot{
			Device:           name,
			ReadsPerSec:      float64(readDelta) / interval,
			WritesPerSec:     float64(writeDelta) / interval,
			ReadBytesPerSec:  bytesRead / interval,
			WriteBytesPerSec: bytesWrite / interval,
		})
	}
	return list, nil
}

// =========================================================================
// Process listing (used by top and ps tools)
// =========================================================================

func collectProcesses(ctx plugin.Context, sortBy string, count int) ([]ProcessInfo, error) {
	// Sort flag: -%cpu or -%mem (descending).
	sortFlag := "-%cpu"
	if sortBy == "mem" {
		sortFlag = "-%mem"
	}

	cmd := fmt.Sprintf(
		"ps -eo pid,ppid,user,%%cpu,%%mem,rss,vsz,stat,comm --no-headers --sort=%s 2>/dev/null | head -%d",
		sortFlag, count,
	)
	out, err := ctx.Exec(cmd)
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	return parsePS(out.Stdout)
}

func collectProcessByPID(ctx plugin.Context, pid int) (ProcessInfo, error) {
	cmd := fmt.Sprintf(
		"ps -p %d -eo pid,ppid,user,%%cpu,%%mem,rss,vsz,stat,comm --no-headers 2>/dev/null",
		pid,
	)
	out, err := ctx.Exec(cmd)
	if err != nil {
		return ProcessInfo{}, fmt.Errorf("ps for pid %d: %w", pid, err)
	}
	list, err := parsePS(out.Stdout)
	if err != nil {
		return ProcessInfo{}, err
	}
	if len(list) == 0 {
		return ProcessInfo{}, fmt.Errorf("no process with PID %d", pid)
	}
	return list[0], nil
}

func parsePS(data string) ([]ProcessInfo, error) {
	var procs []ProcessInfo
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// ps output is space-separated. We parse from the right because
		// the command may contain spaces.
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}
		// Parse from left: pid, ppid, user, %cpu, %mem, rss, vsz, stat.
		// Everything after is the command.
		pid, _ := strconv.Atoi(fields[0])
		ppid, _ := strconv.Atoi(fields[1])
		user := fields[2]
		cpu, _ := strconv.ParseFloat(fields[3], 64)
		mem, _ := strconv.ParseFloat(fields[4], 64)
		rssKB, _ := strconv.ParseFloat(fields[5], 64)
		vszKB, _ := strconv.ParseFloat(fields[6], 64)
		state := fields[7]
		cmd := strings.Join(fields[8:], " ")

		procs = append(procs, ProcessInfo{
			PID:     pid,
			PPID:    ppid,
			User:    user,
			CPU:     cpu,
			RAM:     mem,
			RSS_MB:  rssKB / 1024,
			VSZ_MB:  vszKB / 1024,
			State:   state,
			Command: cmd,
		})
	}
	return procs, nil
}

// =========================================================================
// Formatting helpers
// =========================================================================

func fmtBytesPerSec(bps float64) string {
	switch {
	case bps >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GB/s", bps/(1024*1024*1024))
	case bps >= 1024*1024:
		return fmt.Sprintf("%.1f MB/s", bps/(1024*1024))
	case bps >= 1024:
		return fmt.Sprintf("%.1f KB/s", bps/1024)
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}

func fmtBytes(mb float64) string {
	switch {
	case mb >= 1024:
		return fmt.Sprintf("%.1f GB", mb/1024)
	default:
		return fmt.Sprintf("%.0f MB", mb)
	}
}

func sparkline(pct float64, width int) string {
	n := int(math.Round(pct / 100 * float64(width)))
	if n < 0 {
		n = 0
	}
	if n > width {
		n = width
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	bar := make([]rune, width)
	for i := 0; i < width; i++ {
		idx := int(float64(i) / float64(width) * float64(len(blocks)-1))
		if i < n {
			bar[i] = blocks[idx]
		} else {
			bar[i] = ' '
		}
	}
	return "│" + string(bar) + "│"
}

// =========================================================================
// Tool handlers
// =========================================================================

// =========================================================================
// Tool handlers
// =========================================================================

func (m *monitor) currentTool(ctx plugin.Context, _ map[string]string) error {
	ctx.Log().Debug("current resource snapshot requested")

	m.mu.RLock()
	snap := m.current
	m.mu.RUnlock()

	// Use the latest timestamp from the snapshot, or now if zero.
	ts := snap.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	fmt.Fprintf(ctx.Stdout(), "System Resources — %s\n", ts.Format("2006-01-02 15:04:05"))
	fmt.Fprintln(ctx.Stdout(), strings.Repeat("─", 50))

	// RAM.
	r := snap.RAM
	if r.TotalMB > 0 {
		fmt.Fprintf(ctx.Stdout(), "RAM:   %s / %s  (%.1f%%)\n",
			fmtBytes(r.UsedMB), fmtBytes(r.TotalMB), r.Percent)
	} else {
		fmt.Fprintln(ctx.Stdout(), "RAM:   (no data yet)")
	}

	// CPU.
	c := snap.CPU
	if c.TotalPercent > 0 || c.NumCores > 0 {
		fmt.Fprintf(ctx.Stdout(), "CPU:   %.1f%%  (user: %.1f%%, sys: %.1f%%, iowait: %.1f%%)  [%d cores]\n",
			c.TotalPercent, c.UserPercent, c.SystemPercent, c.IowaitPercent, c.NumCores)
	} else {
		fmt.Fprintln(ctx.Stdout(), "CPU:   (no data yet)")
	}

	// Disks.
	if len(snap.Disks) > 0 {
		for _, d := range snap.Disks {
			fmt.Fprintf(ctx.Stdout(), "Disk %-12s %s / %s  (%.1f%%)\n",
				d.MountPoint, fmtBytes(d.UsedGB*1024), fmtBytes(d.TotalGB*1024), d.Percent)
		}
	} else {
		fmt.Fprintln(ctx.Stdout(), "Disk:  (no data yet)")
	}

	// Disk I/O.
	if len(snap.DiskIO) > 0 {
		for _, io := range snap.DiskIO {
			fmt.Fprintf(ctx.Stdout(), "I/O %-10s  read: %s  write: %s\n",
				io.Device, fmtBytesPerSec(io.ReadBytesPerSec), fmtBytesPerSec(io.WriteBytesPerSec))
		}
	}

	ctx.Log().Trace("current snapshot served",
		"ram_pct", snap.RAM.Percent,
		"cpu_pct", snap.CPU.TotalPercent,
		"disks", len(snap.Disks),
		"disk_io", len(snap.DiskIO),
	)
	return nil
}

func (m *monitor) topTool(ctx plugin.Context, args map[string]string) error {
	ctx.Log().Debug("top processes requested", "count", args["count"], "sort", args["sort"])
	count := 10
	sortBy := "cpu"
	if v := args["count"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			count = n
		}
	}
	if v := args["sort"]; v == "mem" {
		sortBy = "mem"
	}

	procs, err := collectProcesses(ctx, sortBy, count)
	if err != nil {
		return fmt.Errorf("failed to list processes: %w", err)
	}

	// Show total system RAM for context.
	m.mu.RLock()
	totalRAM := m.current.RAM.TotalMB
	numCores := m.current.CPU.NumCores
	m.mu.RUnlock()

	sortLabel := "CPU"
	if sortBy == "mem" {
		sortLabel = "MEM"
	}
	fmt.Fprintf(ctx.Stdout(), "Top %d processes by %s usage  (RAM: %s, cores: %d):\n",
		len(procs), sortLabel, fmtBytes(totalRAM), numCores)
	fmt.Fprintln(ctx.Stdout(), strings.Repeat("─", 80))
	fmt.Fprintf(ctx.Stdout(), "%-6s %-8s %6s %6s %7s %7s %s\n",
		"PID", "USER", "%CPU", "%MEM", "RSS", "VSZ", "COMMAND")
	fmt.Fprintln(ctx.Stdout(), strings.Repeat("─", 80))

	for _, p := range procs {
		cpuDisplay := p.CPU
		if numCores > 1 {
			cpuDisplay = p.CPU / float64(numCores)
		}
		_ = cpuDisplay // keep raw %cpu for display, it's what ps shows
		fmt.Fprintf(ctx.Stdout(), "%-6d %-8s %5.1f%% %5.1f%% %7s %7s %s\n",
			p.PID, truncate(p.User, 8), p.CPU, p.RAM,
			fmtBytes(p.RSS_MB), fmtBytes(p.VSZ_MB), truncate(p.Command, 40))
	}

	if len(procs) == 0 {
		fmt.Fprintln(ctx.Stdout(), "(no processes found)")
	}

	return nil
}

func (m *monitor) psTool(ctx plugin.Context, args map[string]string) error {
	ctx.Log().Debug("process list requested", "pid", args["pid"], "count", args["count"], "sort", args["sort"])

	// Optional specific PID.
	if pidStr := args["pid"]; pidStr != "" {
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			return fmt.Errorf("invalid pid: %q", pidStr)
		}
		p, err := collectProcessByPID(ctx, pid)
		if err != nil {
			return err
		}
		m.printProcessDetail(ctx, p)
		return nil
	}

	count := 20
	sortBy := "cpu"
	if v := args["count"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			count = n
		}
	}
	if v := args["sort"]; v == "mem" {
		sortBy = "mem"
	}

	procs, err := collectProcesses(ctx, sortBy, count)
	if err != nil {
		return fmt.Errorf("failed to list processes: %w", err)
	}

	m.mu.RLock()
	totalRAM := m.current.RAM.TotalMB
	numCores := m.current.CPU.NumCores
	m.mu.RUnlock()

	fmt.Fprintf(ctx.Stdout(), "%-6s %-8s %5s %5s %8s %8s %4s %s\n",
		"PID", "USER", "%CPU", "%MEM", "RSS", "VSZ", "PCPU", "COMMAND")
	fmt.Fprintln(ctx.Stdout(), strings.Repeat("─", 80))

	for _, p := range procs {
		// Percent of total system CPU.
		pctOfTotal := p.CPU
		if numCores > 0 {
			pctOfTotal = p.CPU / float64(numCores)
		}
		fmt.Fprintf(ctx.Stdout(), "%-6d %-8s %5.1f %5.1f %8s %8s %4.0f%% %s\n",
			p.PID, truncate(p.User, 8), p.CPU, p.RAM,
			fmtBytes(p.RSS_MB), fmtBytes(p.VSZ_MB), pctOfTotal, truncate(p.Command, 36))
	}

	if len(procs) == 0 {
		fmt.Fprintln(ctx.Stdout(), "(no processes found)")
	}

	// Show per-process bar chart for top 5.
	if len(procs) > 0 {
		fmt.Fprintln(ctx.Stdout())
		fmt.Fprintln(ctx.Stdout(), "Per-process resource usage:")
		limit := 5
		if len(procs) < limit {
			limit = len(procs)
		}
		for _, p := range procs[:limit] {
			bar := sparkline(p.CPU, 20)
			fmt.Fprintf(ctx.Stdout(), "  %5.1f%% CPU %s  %s\n", p.CPU, bar, truncate(p.Command, 30))
		}
		if totalRAM > 0 {
			for _, p := range procs[:limit] {
				bar := sparkline(p.RAM, 20)
				fmt.Fprintf(ctx.Stdout(), "  %5.1f%% MEM %s  %s\n", p.RAM, bar, truncate(p.Command, 30))
			}
		}
	}

	return nil
}

func (m *monitor) printProcessDetail(ctx plugin.Context, p ProcessInfo) {
	m.mu.RLock()
	totalRAM := m.current.RAM.TotalMB
	numCores := m.current.CPU.NumCores
	m.mu.RUnlock()

	pctOfTotalCPU := p.CPU
	if numCores > 0 {
		pctOfTotalCPU = p.CPU / float64(numCores)
	}

	fmt.Fprintf(ctx.Stdout(), "Process %d (%s)\n", p.PID, p.Command)
	fmt.Fprintln(ctx.Stdout(), strings.Repeat("─", 50))
	fmt.Fprintf(ctx.Stdout(), "  PID:     %d\n", p.PID)
	fmt.Fprintf(ctx.Stdout(), "  PPID:    %d\n", p.PPID)
	fmt.Fprintf(ctx.Stdout(), "  User:    %s\n", p.User)
	fmt.Fprintf(ctx.Stdout(), "  State:   %s\n", stateDesc(p.State))
	fmt.Fprintf(ctx.Stdout(), "  CPU:     %.1f%%  (%.1f%% of total system CPU)\n", p.CPU, pctOfTotalCPU)
	fmt.Fprintf(ctx.Stdout(), "  Memory:  %.1f%% of RAM  (%s RSS / %s VSZ)\n",
		p.RAM, fmtBytes(p.RSS_MB), fmtBytes(p.VSZ_MB))
	if totalRAM > 0 {
		fmt.Fprintf(ctx.Stdout(), "  RAM:     %s / %s total\n", fmtBytes(p.RSS_MB), fmtBytes(totalRAM))
	}

	// Sparkline bar.
	barCPU := sparkline(p.CPU, 30)
	barMEM := sparkline(p.RAM, 30)
	fmt.Fprintf(ctx.Stdout(), "\n  CPU: %s  %.1f%%\n", barCPU, p.CPU)
	fmt.Fprintf(ctx.Stdout(), "  MEM: %s  %.1f%%\n", barMEM, p.RAM)
}

func (m *monitor) historyTool(ctx plugin.Context, args map[string]string) error {
	ctx.Log().Debug("resource history requested", "resource", args["resource"], "count", args["count"])

	resource := "ram"
	if v := args["resource"]; v != "" {
		switch v {
		case "ram", "cpu", "disk":
			resource = v
		default:
			return fmt.Errorf("unknown resource %q (use ram, cpu, or disk)", v)
		}
	}
	count := 20
	if v := args["count"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			count = n
		}
	}

	m.mu.RLock()
	hist := make([]SystemSnapshot, len(m.history))
	copy(hist, m.history)
	m.mu.RUnlock()

	if len(hist) == 0 {
		fmt.Fprintln(ctx.Stdout(), "(no data collected yet — wait a few seconds)")
		return nil
	}

	if count > len(hist) {
		count = len(hist)
	}
	hist = hist[len(hist)-count:]

	switch resource {
	case "ram":
		fmt.Fprintln(ctx.Stdout(), "RAM usage history:")
		fmt.Fprintln(ctx.Stdout(), strings.Repeat("─", 60))
		for _, s := range hist {
			bar := sparkline(s.RAM.Percent, 20)
			fmt.Fprintf(ctx.Stdout(), "%s  %5.1f%%  %s\n",
				s.Timestamp.Format("15:04:05"), s.RAM.Percent, bar)
		}
	case "cpu":
		fmt.Fprintln(ctx.Stdout(), "CPU usage history:")
		fmt.Fprintln(ctx.Stdout(), strings.Repeat("─", 60))
		for _, s := range hist {
			bar := sparkline(s.CPU.TotalPercent, 20)
			fmt.Fprintf(ctx.Stdout(), "%s  %5.1f%%  %s\n",
				s.Timestamp.Format("15:04:05"), s.CPU.TotalPercent, bar)
		}
	case "disk":
		if len(hist) == 0 || len(hist[0].Disks) == 0 {
			fmt.Fprintln(ctx.Stdout(), "(no disk data)")
			return nil
		}
		// Show disk usage for each mount point.
		for i := range hist[0].Disks {
			mnt := hist[0].Disks[i].MountPoint
			fmt.Fprintf(ctx.Stdout(), "Disk usage (%s):\n", mnt)
			fmt.Fprintln(ctx.Stdout(), strings.Repeat("─", 60))
			for _, s := range hist {
				if i < len(s.Disks) {
					bar := sparkline(s.Disks[i].Percent, 20)
					fmt.Fprintf(ctx.Stdout(), "%s  %5.1f%%  %s\n",
						s.Timestamp.Format("15:04:05"), s.Disks[i].Percent, bar)
				}
			}
		}
	}

	return nil
}

// =========================================================================
// Utilities
// =========================================================================

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

func stateDesc(state string) string {
	switch {
	case strings.HasPrefix(state, "R"):
		return "R (running)"
	case strings.HasPrefix(state, "S"):
		return "S (sleeping)"
	case strings.HasPrefix(state, "D"):
		return "D (uninterruptible sleep)"
	case strings.HasPrefix(state, "Z"):
		return "Z (zombie)"
	case strings.HasPrefix(state, "T"):
		return "T (stopped)"
	case strings.HasPrefix(state, "t"):
		return "t (tracing stop)"
	case strings.HasPrefix(state, "X"):
		return "X (dead)"
	case strings.HasPrefix(state, "I"):
		return "I (idle)"
	default:
		return state
	}
}

// =========================================================================
// Main
// =========================================================================

func main() {
	m := &monitor{
		prevDiskIO: make(map[string]diskIORaw),
	}

	plugin.Serve(plugin.Config{
		Name:        "resources",
		DisplayName: "System Resources",
		Version:     "1.0.0",
		Description: "Monitor system resources: RAM, CPU, disk usage, disk I/O, and processes",
		Background:  m.collect,
		Tools: []plugin.Tool{
			plugin.NewTool("current", "Show current system resource usage snapshot",
				&dotfilesdv1.ToolInputSchema{
					Properties: map[string]*dotfilesdv1.PropertySchema{},
				},
				&dotfilesdv1.CLIHints{
					CommandPath: "resources current",
					Category:    "system",
				},
				m.currentTool,
			),
			plugin.NewTool("top", "Show top N processes by CPU or memory usage",
				&dotfilesdv1.ToolInputSchema{
					Properties: map[string]*dotfilesdv1.PropertySchema{
						"count": {
							Type:        dotfilesdv1.PropertyType_PROPERTY_TYPE_STRING,
							Description: "Number of processes to show (default: 10)",
							Default:     "10",
						},
						"sort": {
							Type:        dotfilesdv1.PropertyType_PROPERTY_TYPE_STRING,
							Description: "Sort by 'cpu' or 'mem' (default: cpu)",
							Default:     "cpu",
						},
					},
				},
				&dotfilesdv1.CLIHints{
					CommandPath: "resources top",
					Category:    "system",
				},
				m.topTool,
			),
			plugin.NewTool("ps", "List processes with detailed per-process metrics and percent of total",
				&dotfilesdv1.ToolInputSchema{
					Properties: map[string]*dotfilesdv1.PropertySchema{
						"pid": {
							Type:        dotfilesdv1.PropertyType_PROPERTY_TYPE_STRING,
							Description: "Show details for a specific PID (optional)",
						},
						"count": {
							Type:        dotfilesdv1.PropertyType_PROPERTY_TYPE_STRING,
							Description: "Number of processes to show (default: 20)",
							Default:     "20",
						},
						"sort": {
							Type:        dotfilesdv1.PropertyType_PROPERTY_TYPE_STRING,
							Description: "Sort by 'cpu' or 'mem' (default: cpu)",
							Default:     "cpu",
						},
					},
				},
				&dotfilesdv1.CLIHints{
					CommandPath: "resources ps",
					Category:    "system",
				},
				m.psTool,
			),
			plugin.NewTool("history", "Show historical resource usage as sparkline graphs",
				&dotfilesdv1.ToolInputSchema{
					Properties: map[string]*dotfilesdv1.PropertySchema{
						"resource": {
							Type:        dotfilesdv1.PropertyType_PROPERTY_TYPE_STRING,
							Description: "Resource to graph: ram, cpu, or disk (default: ram)",
							Default:     "ram",
						},
						"count": {
							Type:        dotfilesdv1.PropertyType_PROPERTY_TYPE_STRING,
							Description: "Number of data points to show (default: 20)",
							Default:     "20",
						},
					},
				},
				&dotfilesdv1.CLIHints{
					CommandPath: "resources history",
					Category:    "system",
				},
				m.historyTool,
			),
		},
	})
}
