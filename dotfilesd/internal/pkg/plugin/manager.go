// Package plugin provides the daemon-side plugin management: building,
// launching, and communicating with extension plugins.
package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// DefaultPluginsDir is the default directory where user-installed plugin
// sources are stored. Controlled via config.
const DefaultPluginsDir = "~/.config/dotfilesd/plugins"

// DefaultCacheDir is the default directory for compiled plugin binaries.
const DefaultCacheDir = "~/.cache/dotfilesd/plugins"

// Manager orchestrates plugin lifecycle: discovery, build, launch, and
// communication. One Manager per daemon instance.
type Manager struct {
	PluginsDir string
	CacheDir   string
	CtxURL     string // Execution Context URL the daemon exposes
	CtxToken   string // shared secret for Execution Context auth

	registry *Registry
	builder  *Builder
	mu       sync.Mutex
	loaded   bool
}

// NewManager creates a new plugin manager.
func NewManager(pluginsDir, cacheDir, ctxURL, ctxToken string) *Manager {
	return &Manager{
		PluginsDir: pluginsDir,
		CacheDir:   cacheDir,
		CtxURL:     ctxURL,
		CtxToken:   ctxToken,
		registry:   NewRegistry(),
		builder:    &Builder{CacheDir: cacheDir},
	}
}

// LoadPlugins discovers plugin source directories, builds them, launches
// the resulting binaries, and registers their capabilities.
//
// Only called once at daemon startup. Idempotent on consecutive calls.
func (m *Manager) LoadPlugins(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.loaded {
		slog.Debug("plugins already loaded, skipping")
		return nil
	}

	pluginsDir := expandHome(m.PluginsDir)
	cacheDir := expandHome(m.CacheDir)

	slog.Debug("loading plugins", "plugins_dir", pluginsDir, "cache_dir", cacheDir)

	// Ensure directories exist.
	os.MkdirAll(cacheDir, 0o755)

	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("plugins directory does not exist, skipping", "dir", pluginsDir)
			m.loaded = true
			return nil // no plugins directory yet, that's OK
		}
		return fmt.Errorf("read plugins dir: %w", err)
	}

	slog.Debug("scanning plugin entries", "count", len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			slog.Debug("skipping non-directory entry", "name", entry.Name())
			continue
		}
		name := entry.Name()
		sourceDir := filepath.Join(pluginsDir, name)

		// Skip if not a Go plugin (must have go.mod or main.go).
		if !isGoPlugin(sourceDir) {
			slog.Debug("skipping non-Go plugin directory", "name", name)
			continue
		}

		slog.Debug("loading plugin", "name", name, "source_dir", sourceDir)
		if err := m.loadPlugin(ctx, name, sourceDir, cacheDir); err != nil {
			// Log and continue; a single bad plugin shouldn't block others.
			slog.Error("plugin load failed", "name", name, "error", err)
		}
	}

	slog.Debug("plugin loading complete", "loaded", m.loaded, "count", m.registry.Len())
	m.loaded = true
	return nil
}

// loadPlugin builds, launches, and registers a single plugin.
func (m *Manager) loadPlugin(ctx context.Context, name, sourceDir, cacheDir string) error {
	slog.Debug("loading plugin step: build", "name", name)
	// 1. Build (or load from cache).
	result, err := m.builder.Build(name, sourceDir)
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}
	slog.Debug("plugin build result", "name", name, "binary", result.BinaryPath, "from_cache", result.FromCache)

	// 2. Create a session ID for this plugin.
	sessionID := fmt.Sprintf("plugin-%s", name)

	// 3. Launch the plugin subprocess.
	slog.Debug("launching plugin process", "name", name, "binary", result.BinaryPath, "ctx_url", m.CtxURL, "session_id", sessionID)
	proc, err := Launch(result.BinaryPath, m.CtxURL, m.CtxToken, sessionID, nil)
	if err != nil {
		return fmt.Errorf("launch: %w", err)
	}
	slog.Debug("plugin process launched", "name", name, "url", proc.URL, "pid", proc.Cmd.Process.Pid)

	// 4. Connect to its Extension API.
	client := NewClient(proc.URL)
	slog.Debug("connected to plugin extension API", "name", name, "url", proc.URL)

	// 5. Fetch descriptor.
	slog.Debug("fetching plugin descriptor", "name", name)
	desc, err := client.GetDescriptor(ctx)
	if err != nil {
		proc.Kill()
		return fmt.Errorf("get descriptor: %w", err)
	}
	slog.Debug("plugin descriptor received", "name", name, "display_name", desc.DisplayName, "version", desc.Version, "tools", len(desc.Tools))
	for _, t := range desc.Tools {
		slog.Debug("  plugin tool", "name", name, "tool", t.Name, "description", t.Description)
	}

	// 6. Register.
	slog.Debug("registering plugin", "name", name)
	if err := m.registry.Register(name, &PluginInfo{
		Descriptor: desc,
		Client:     client,
		Process:    proc,
	}); err != nil {
		proc.Kill()
		return fmt.Errorf("register: %w", err)
	}
	slog.Debug("plugin registered successfully", "name", name)
	return nil
}

// CallTool invokes a tool on a loaded plugin.
func (m *Manager) CallTool(ctx context.Context, pluginName, toolName string, args map[string]string) (string, bool, string, error) {
	slog.Debug("manager CallTool", "plugin", pluginName, "tool", toolName, "args", args)
	info, ok := m.registry.Get(pluginName)
	if !ok {
		return "", false, "", fmt.Errorf("plugin %q not loaded", pluginName)
	}
	slog.Debug("found plugin in registry, forwarding call", "plugin", pluginName, "url", info.Process.URL)
	return info.Client.CallTool(ctx, toolName, args)
}

// GetDescriptor returns the cached descriptor for a plugin.
func (m *Manager) GetDescriptor(pluginName string) (*ExtensionDescriptor, bool) {
	slog.Debug("manager GetDescriptor", "plugin", pluginName)
	info, ok := m.registry.Get(pluginName)
	if !ok {
		slog.Debug("plugin not found in registry", "plugin", pluginName)
		return nil, false
	}
	return info.Descriptor, true
}

// ListPlugins returns all loaded plugin descriptors.
func (m *Manager) ListPlugins() []ExtensionDescriptor {
	slog.Debug("manager ListPlugins")
	infos := m.registry.List()
	slog.Debug("registry returned plugins", "count", len(infos))
	result := make([]ExtensionDescriptor, len(infos))
	for i, info := range infos {
		if info.Descriptor != nil {
			result[i] = *info.Descriptor
		}
	}
	return result
}

// Shutdown kills all running plugins and clears the registry.
func (m *Manager) Shutdown() {
	slog.Debug("manager Shutdown: killing all plugins and clearing registry")
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registry.Clear()
	m.loaded = false
	slog.Debug("manager shutdown complete")
}

// expandHome replaces "~" with the user's home directory.
func expandHome(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

// isGoPlugin checks if a directory contains a Go plugin (go.mod or main.go).
func isGoPlugin(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "main.go")); err == nil {
		return true
	}
	return false
}
