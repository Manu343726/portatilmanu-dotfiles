// Package plugin provides the daemon-side plugin management: building,
// launching, and communicating with extension plugins.
package plugin

import (
	"context"
	"fmt"
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
		return nil
	}

	pluginsDir := expandHome(m.PluginsDir)
	cacheDir := expandHome(m.CacheDir)

	// Ensure directories exist.
	os.MkdirAll(cacheDir, 0o755)

	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			m.loaded = true
			return nil // no plugins directory yet, that's OK
		}
		return fmt.Errorf("read plugins dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		sourceDir := filepath.Join(pluginsDir, name)

		// Skip if not a Go plugin (must have go.mod or main.go).
		if !isGoPlugin(sourceDir) {
			continue
		}

		if err := m.loadPlugin(ctx, name, sourceDir, cacheDir); err != nil {
			// Log and continue; a single bad plugin shouldn't block others.
			fmt.Fprintf(os.Stderr, "plugin %q: load failed: %v\n", name, err)
		}
	}

	m.loaded = true
	return nil
}

// loadPlugin builds, launches, and registers a single plugin.
func (m *Manager) loadPlugin(ctx context.Context, name, sourceDir, cacheDir string) error {
	// 1. Build (or load from cache).
	result, err := m.builder.Build(name, sourceDir)
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}

	// 2. Create a session ID for this plugin.
	sessionID := fmt.Sprintf("plugin-%s", name)

	// 3. Launch the plugin subprocess.
	proc, err := Launch(result.BinaryPath, m.CtxURL, m.CtxToken, sessionID, nil)
	if err != nil {
		return fmt.Errorf("launch: %w", err)
	}

	// 4. Connect to its Extension API.
	client := NewClient(proc.URL)

	// 5. Fetch descriptor.
	desc, err := client.GetDescriptor(ctx)
	if err != nil {
		proc.Kill()
		return fmt.Errorf("get descriptor: %w", err)
	}

	// 6. Register.
	return m.registry.Register(name, &PluginInfo{
		Descriptor: desc,
		Client:     client,
		Process:    proc,
	})
}

// CallTool invokes a tool on a loaded plugin.
func (m *Manager) CallTool(ctx context.Context, pluginName, toolName string, args map[string]string) (string, bool, string, error) {
	info, ok := m.registry.Get(pluginName)
	if !ok {
		return "", false, "", fmt.Errorf("plugin %q not loaded", pluginName)
	}

	return info.Client.CallTool(ctx, toolName, args)
}

// GetDescriptor returns the cached descriptor for a plugin.
func (m *Manager) GetDescriptor(pluginName string) (*ExtensionDescriptor, bool) {
	info, ok := m.registry.Get(pluginName)
	if !ok {
		return nil, false
	}
	return info.Descriptor, true
}

// ListPlugins returns all loaded plugin descriptors.
func (m *Manager) ListPlugins() []ExtensionDescriptor {
	infos := m.registry.List()
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
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registry.Clear()
	m.loaded = false
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
