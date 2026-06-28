package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"sync"
	"time"

	"dotfilesd/internal/pkg/rpcreflection"
)

// supervisor manages a single plugin process with crash detection and
// exponential backoff restart.
type supervisor struct {
	name       string
	binaryPath string
	sourceDir  string
	cacheDir   string
	ctxURL     string
	ctxToken   string
	sessionID  string
	envExtra   []string // additional environment variables

	mu       sync.RWMutex
	process  *os.Process
	url      string // current handshake URL
	services []string

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// newSupervisor creates a supervisor for a plugin process.
func newSupervisor(name, binaryPath, sourceDir, cacheDir, ctxURL, ctxToken, sessionID string) *supervisor {
	return &supervisor{
		name:       name,
		binaryPath: binaryPath,
		sourceDir:  sourceDir,
		cacheDir:   cacheDir,
		ctxURL:     ctxURL,
		ctxToken:   ctxToken,
		sessionID:  sessionID,
		stopCh:     make(chan struct{}),
	}
}

// startAdopt takes an already-launched plugin (from LoadPlugins), stores
// its state, and starts a background monitor that restarts the plugin
// with exponential backoff if it crashes.
func (s *supervisor) startAdopt(ctx context.Context, info *PluginInfo) error {
	s.mu.Lock()
	s.process = info.Process
	s.url = info.URL
	s.services = info.Services
	s.mu.Unlock()

	// Start crash monitor in background.
	s.wg.Add(1)
	go s.monitor(ctx)

	return nil
}

// launch spawns the plugin process, reads the handshake, and discovers services.
func (s *supervisor) launch(ctx context.Context) (*PluginInfo, error) {
	slog.Debug("launching plugin", "name", s.name)

	procCmd := exec.CommandContext(ctx, s.binaryPath)
	procCmd.Env = append(os.Environ(),
		"EXECUTION_CONTEXT_URL="+s.ctxURL,
		"EXECUTION_CONTEXT_TOKEN="+s.ctxToken,
		"SESSION_ID="+s.sessionID,
	)
	procCmd.Env = append(procCmd.Env, s.envExtra...)

	stdout, err := procCmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	procCmd.Stderr = os.Stderr

	if err := procCmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	slog.Info("plugin process started", "name", s.name, "pid", procCmd.Process.Pid)

	// Step d: Read handshake JSON from stdout.
	var hs handshake
	if err := readJSONLine(stdout, &hs); err != nil {
		procCmd.Process.Kill()
		return nil, fmt.Errorf("handshake read: %w", err)
	}
	if hs.Protocol != "dotfilesd-plugin-v1" {
		procCmd.Process.Kill()
		return nil, fmt.Errorf("unknown handshake protocol: %s", hs.Protocol)
	}
	slog.Info("plugin handshake complete", "name", s.name, "url", hs.URL)

	// Step e: grpcreflect discovery via rpcreflection.
	refClient := rpcreflection.NewClient(hs.URL)
	svcInfos, discErr := refClient.DiscoverServices(ctx)
	if discErr != nil {
		procCmd.Process.Kill()
		return nil, fmt.Errorf("grpcreflect discovery: %w", discErr)
	}

	var services []string
	var nonSystemInfos []rpcreflection.ServiceInfo
	for _, si := range svcInfos {
		if rpcreflection.IsSystemService(si.FullName) {
			continue
		}
		services = append(services, si.FullName)
		nonSystemInfos = append(nonSystemInfos, si)
	}

	schemas := rpcreflection.BuildServiceSchemas(nonSystemInfos)

	// Build PluginInfo without Process field (set in start()).
	displayName := hs.DisplayName
	if displayName == "" {
		displayName = hs.Name
	}
	if displayName == "" {
		displayName = s.name
	}
	info := &PluginInfo{
		Name:        s.name,
		DisplayName: displayName,
		Version:     hs.Version,
		Description: hs.Description,
		URL:         hs.URL,
		Services:    services,
		Schemas:     schemas,
		Process:     procCmd.Process,
		SourceDir:   s.sourceDir,
		CacheDir:    s.cacheDir,
	}
	return info, nil
}

// monitor watches the plugin process and restarts it on crash with
// exponential backoff (1s, 2s, 4s, 8s, …, max 30s).
func (s *supervisor) monitor(ctx context.Context) {
	defer s.wg.Done()

	backoff := 1 * time.Second
	const maxBackoff = 30 * time.Second
	const maxAttempts = 0 // 0 = unlimited

	attempt := 0
	for {
		// Wait for process to exit. The process could exit because:
		//   a) It crashed (exit code != 0 or nil error).
		//   b) It was killed via s.stop().
		//   c) The context was cancelled.
		ps, err := s.process.Wait()

		// Check for stop signal or context cancellation first.
		select {
		case <-s.stopCh:
			slog.Info("plugin supervisor stopped", "name", s.name)
			return
		case <-ctx.Done():
			slog.Info("plugin supervisor context done", "name", s.name, "err", ctx.Err())
			return
		default:
		}

		// If we killed it ourselves during stop (ps.Exited() with our signal),
		// the stopCh would have been closed above. If context is cancelled,
		// we also exit above. Otherwise, treat exit != 0 as a crash.
		if ps == nil || ps.Success() {
			// Clean exit (exit code 0). Don't restart.
			slog.Info("plugin exited cleanly, not restarting", "name", s.name, "pid", s.process.Pid)
			return
		}

		// Crash detected.
		attempt++
		slog.Warn("plugin crashed, restarting", "name", s.name,
			"attempt", attempt, "backoff", backoff,
			"pid", s.process.Pid, "exit_code", ps.ExitCode())

		if maxAttempts > 0 && attempt >= maxAttempts {
			slog.Error("plugin max restart attempts reached, giving up", "name", s.name)
			return
		}

		// Wait with backoff (interruptible by stopCh).
		select {
		case <-s.stopCh:
			slog.Info("plugin supervisor stopped during backoff", "name", s.name)
			return
		case <-time.After(backoff):
		}

		// Rebuild the plugin binary (source may have changed).
		if err := s.rebuild(); err != nil {
			slog.Error("plugin rebuild failed, will retry", "name", s.name, "error", err)
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
			continue
		}

		// Re-launch.
		info, err := s.launch(ctx)
		if err != nil {
			slog.Error("plugin re-launch failed, will retry", "name", s.name, "error", err)
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
			continue
		}

		// Success — update state and reset backoff.
		s.mu.Lock()
		s.process = info.Process
		s.url = info.URL
		s.services = info.Services
		s.mu.Unlock()

		backoff = 1 * time.Second
		slog.Info("plugin restarted successfully", "name", s.name,
			"url", info.URL, "pid", info.Process.Pid, "services", info.Services)
	}
}

// rebuild runs proto compilation (if needed) then 'go build' on the plugin's
// source directory. This ensures plugin proto changes are picked up before
// the build step, matching the same two-phase build used in LoadPlugins.
func (s *supervisor) rebuild() error {
	// Step 1: Recompile protos (best-effort — skip if no proto dir).
	if err := stepProto(s.sourceDir, s.name); err != nil {
		slog.Debug("proto recompilation failed during rebuild", "plugin", s.name, "error", err)
		// Non-fatal: the plugin may not have protos.
	}

	// Step 2: Build the plugin binary.
	cmd := exec.Command("go", "build", "-o", s.binaryPath, ".")
	cmd.Dir = s.sourceDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build: %w\n%s", err, string(out))
	}
	return nil
}

// stop kills the plugin process and waits for the monitor goroutine to exit.
func (s *supervisor) stop() {
	close(s.stopCh)

	s.mu.RLock()
	proc := s.process
	s.mu.RUnlock()

	if proc != nil {
		slog.Debug("killing plugin process", "name", s.name, "pid", proc.Pid)
		proc.Kill()
	}

	s.wg.Wait()
}

// snapshot returns a copy of the current plugin state.
func (s *supervisor) snapshot() PluginInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return PluginInfo{
		Name:     s.name,
		URL:      s.url,
		Services: s.services,
		Process:  s.process,
	}
}
