package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"
)

// PluginInfo holds metadata for a loaded plugin.
type PluginInfo struct {
	Name      string
	URL       string
	Info      *dotfilesdv1.GetInfoResponse
	Services  []*dotfilesdv1.ServiceDescriptor
	Process   *os.Process
	SourceDir string
	CacheDir  string
}

// Manager orchestrates plugin lifecycle with dependency-aware builds.
type Manager struct {
	PluginsDir string
	CacheDir   string
	CtxURL     string
	CtxToken   string

	mu      sync.RWMutex
	plugins map[string]*PluginInfo
}

// NewManager creates a new plugin manager.
func NewManager(pluginsDir, cacheDir, ctxURL, ctxToken string) *Manager {
	return &Manager{
		PluginsDir: pluginsDir,
		CacheDir:   cacheDir,
		CtxURL:     ctxURL,
		CtxToken:   ctxToken,
		plugins:    make(map[string]*PluginInfo),
	}
}

// LoadPlugins discovers, builds (in dependency order), launches, and registers all plugins.
func (m *Manager) LoadPlugins(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pluginsDir := expandHome(m.PluginsDir)
	cacheDir := expandHome(m.CacheDir)

	slog.Info("loading plugins", "dir", pluginsDir)

	// Discover plugin directories and build dependency graph.
	pluginDeps, err := m.discoverPlugins(pluginsDir)
	if err != nil {
		return fmt.Errorf("discover plugins: %w", err)
	}

	// Topological sort for build order.
	buildOrder := topologicalSort(pluginDeps)
	slog.Info("plugin build order", "order", buildOrder)

	for _, name := range buildOrder {
		sourceDir := filepath.Join(pluginsDir, name)

		slog.Info("building plugin", "name", name)
		binaryPath := filepath.Join(cacheDir, name, name)
		if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
			slog.Error("mkdir failed", "name", name, "error", err)
			continue
		}

		cmd := exec.Command("go", "build", "-o", binaryPath, ".")
		cmd.Dir = sourceDir
		if out, err := cmd.CombinedOutput(); err != nil {
			slog.Error("build failed", "name", name, "error", err, "output", string(out))
			continue
		}

		sessionID := fmt.Sprintf("plugin-%s", name)
		procCmd := exec.Command(binaryPath)
		procCmd.Env = append(os.Environ(),
			"EXECUTION_CONTEXT_URL="+m.CtxURL,
			"EXECUTION_CONTEXT_TOKEN="+m.CtxToken,
			"SESSION_ID="+sessionID,
		)

		stdout, err := procCmd.StdoutPipe()
		if err != nil {
			slog.Error("stdout pipe failed", "name", name, "error", err)
			continue
		}
		procCmd.Stderr = os.Stderr

		if err := procCmd.Start(); err != nil {
			slog.Error("start failed", "name", name, "error", err)
			continue
		}

		var hs struct {
			Protocol string `json:"protocol"`
			URL      string `json:"url"`
			Session  string `json:"session_id"`
		}
		if err := readJSONLine(stdout, &hs); err != nil {
			procCmd.Process.Kill()
			slog.Error("handshake failed", "name", name, "error", err)
			continue
		}

		if hs.Protocol != "dotfilesd-plugin-v1" {
			procCmd.Process.Kill()
			slog.Error("unknown protocol", "name", name, "protocol", hs.Protocol)
			continue
		}

		slog.Info("plugin launched", "name", name, "url", hs.URL, "pid", procCmd.Process.Pid)

		// Call PluginBaseService.GetInfo and ListServices.
		httpClient := &http.Client{}
		baseClient := dotfilesdv1connect.NewPluginBaseServiceClient(httpClient, hs.URL)

		infoResp, err := baseClient.GetInfo(ctx, connect.NewRequest(&dotfilesdv1.GetInfoRequest{}))
		if err != nil {
			procCmd.Process.Kill()
			slog.Error("GetInfo failed", "name", name, "error", err)
			continue
		}

		svcResp, err := baseClient.ListServices(ctx, connect.NewRequest(&dotfilesdv1.ListServicesRequest{}))
		if err != nil {
			procCmd.Process.Kill()
			slog.Error("ListServices failed", "name", name, "error", err)
			continue
		}

		info := &PluginInfo{
			Name:      name,
			URL:       hs.URL,
			Info:      infoResp.Msg,
			Services:  svcResp.Msg.Services,
			Process:   procCmd.Process,
			SourceDir: sourceDir,
			CacheDir:  cacheDir,
		}

		m.plugins[name] = info
		slog.Info("plugin registered", "name", name, "services", len(info.Services))
	}

	slog.Info("plugins loaded", "count", len(m.plugins))
	return nil
}

// discoverPlugins scans the plugins directory and builds a dependency graph.
// Returns a map of plugin name -> list of plugin names it depends on.
func (m *Manager) discoverPlugins(pluginsDir string) (map[string][]string, error) {
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return nil, err
	}

	deps := make(map[string][]string)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		sourceDir := filepath.Join(pluginsDir, name)

		if !isGoPlugin(sourceDir) {
			continue
		}

		// Parse go.mod for plugin dependencies.
		pluginDeps, err := parsePluginDeps(sourceDir, pluginsDir)
		if err != nil {
			slog.Warn("parse deps failed", "name", name, "error", err)
			pluginDeps = []string{}
		}
		deps[name] = pluginDeps
		slog.Debug("plugin dependencies", "name", name, "deps", pluginDeps)
	}
	return deps, nil
}

// parsePluginDeps reads go.mod and extracts dependencies on other plugins.
func parsePluginDeps(sourceDir, pluginsDir string) ([]string, error) {
	goModPath := filepath.Join(sourceDir, "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return nil, err
	}

	var deps []string
	lines := strings.Split(string(data), "\n")
	inRequire := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "require (" {
			inRequire = true
			continue
		}
		if line == ")" {
			inRequire = false
			continue
		}
		if inRequire || strings.HasPrefix(line, "require ") {
			// Format: "module/path v1.2.3" or "module/path v1.2.3 // indirect"
			parts := strings.Fields(strings.TrimPrefix(line, "require "))
			if len(parts) < 2 {
				continue
			}
			modulePath := parts[0]
			// Check if this module path matches a plugin directory.
			pluginName := filepath.Base(modulePath)
			pluginDir := filepath.Join(pluginsDir, pluginName)
			if _, err := os.Stat(pluginDir); err == nil {
				// This is a local plugin dependency.
				deps = append(deps, pluginName)
			}
		}
	}
	return deps, nil
}

// topologicalSort returns plugins in dependency order (dependencies first).
func topologicalSort(deps map[string][]string) []string {
	var result []string
	visited := make(map[string]bool)
	tempMark := make(map[string]bool)

	var visit func(string)
	visit = func(n string) {
		if tempMark[n] {
			return // cycle detected, skip
		}
		if visited[n] {
			return
		}
		tempMark[n] = true
		for _, dep := range deps[n] {
			visit(dep)
		}
		tempMark[n] = false
		visited[n] = true
		result = append(result, n)
	}

	// Sort keys for deterministic order.
	keys := make([]string, 0, len(deps))
	for k := range deps {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		visit(k)
	}
	return result
}

// GetPlugin returns info for a named plugin.
func (m *Manager) GetPlugin(name string) (*PluginInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	info, ok := m.plugins[name]
	return info, ok
}

// ListPlugins returns all loaded plugins.
func (m *Manager) ListPlugins() []*PluginInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*PluginInfo, 0, len(m.plugins))
	for _, info := range m.plugins {
		result = append(result, info)
	}
	return result
}

func isGoPlugin(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "main.go")); err == nil {
		return true
	}
	return false
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func readJSONLine(r io.Reader, v interface{}) error {
	buf := make([]byte, 4096)
	n, err := r.Read(buf)
	if err != nil {
		return err
	}
	return json.Unmarshal(buf[:n], v)
}
