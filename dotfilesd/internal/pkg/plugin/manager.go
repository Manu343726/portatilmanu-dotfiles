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
	"strings"
	"sync"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"connectrpc.com/connect"
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

// Manager orchestrates plugin lifecycle.
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

// LoadPlugins discovers, builds, launches, and registers all plugins.
func (m *Manager) LoadPlugins(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pluginsDir := expandHome(m.PluginsDir)
	cacheDir := expandHome(m.CacheDir)

	slog.Info("loading plugins", "dir", pluginsDir)

	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return fmt.Errorf("read plugins dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		sourceDir := filepath.Join(pluginsDir, name)

		if !isGoPlugin(sourceDir) {
			continue
		}

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
