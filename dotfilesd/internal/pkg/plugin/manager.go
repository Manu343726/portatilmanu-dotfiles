// Package plugin provides the daemon-side plugin management: building,
// launching, and communicating with extension plugins.
package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"gopkg.in/yaml.v3"
)

// DefaultPluginsDir is the default directory where user-installed plugin
// sources are stored. Controlled via config.
const DefaultPluginsDir = "~/.config/dotfilesd/plugins"

// DefaultCacheDir is the default directory for compiled plugin binaries.
const DefaultCacheDir = "~/.cache/dotfilesd/plugins"

// ---------------------------------------------------------------------------
// Front matter types (same structure as scripts — README.md YAML front matter)
// ---------------------------------------------------------------------------

// DirFrontMatter is YAML front matter from a README.md in a plugin directory.
type DirFrontMatter struct {
	Description string   `yaml:"description"`
	Enabled     bool     `yaml:"enabled"`
	Exclude     []string `yaml:"exclude"`
}

// PluginTreeEntry represents a node in the plugin directory hierarchy.
// Directories are category groups; leaf entries are loaded Go plugins.
type PluginTreeEntry struct {
	Name        string
	Path        string
	IsDirectory bool
	Description string
	Enabled     bool
	Children    []PluginTreeEntry
	Plugin      *ExtensionDescriptor // set only for loaded leaf plugins
}

// ---------------------------------------------------------------------------
// Manager
// ---------------------------------------------------------------------------

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
	tree     []PluginTreeEntry // cached tree from last LoadPlugins
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

// LoadPlugins discovers plugin source directories recursively, builds them,
// launches the resulting binaries, and registers their capabilities.
//
// Directories that are themselves Go plugin sources (contain go.mod or main.go)
// are treated as leaf plugins. Subdirectories that are not Go plugins are
// treated as category groups. Each directory may contain a README.md with
// YAML front matter for descriptions, enable/disable, and exclude lists.
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
			return nil
		}
		return fmt.Errorf("read plugins dir: %w", err)
	}

	// Read root README.md first.
	var rootFM *DirFrontMatter
	if rm := findReadme(entries); rm != nil {
		dfm, err := parseDirFrontMatter(filepath.Join(pluginsDir, rm.Name()))
		if err != nil {
			slog.Warn("parse root README front matter", "error", err)
		} else {
			rootFM = dfm
		}
	}

	// Recursive scan.
	tree, err := m.scanPluginDir(ctx, pluginsDir, "", rootFM, cacheDir)
	if err != nil {
		slog.Error("plugin directory scan failed", "error", err)
		// Partial results are still usable.
	}
	m.tree = tree

	slog.Debug("plugin loading complete", "loaded", m.loaded, "count", m.registry.Len())
	m.loaded = true
	return nil
}

// scanPluginDir recursively scans a plugin directory, building and loading
// any Go plugin leaf directories it finds. Returns the directory's tree
// entries. readmeConfig carries the parent README's exclude/enabled settings.
func (m *Manager) scanPluginDir(ctx context.Context, dir, prefix string, readmeConfig *DirFrontMatter, cacheDir string) ([]PluginTreeEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	// Check for README.md in this directory.
	dirFM := readmeConfig
	if rm := findReadme(entries); rm != nil {
		dfm, err := parseDirFrontMatter(filepath.Join(dir, rm.Name()))
		if err != nil {
			slog.Warn("parse README front matter", "path", rm.Name(), "error", err)
		} else {
			dirFM = dfm
		}
	}

	var result []PluginTreeEntry

	// Subdirectories (sorted).
	var subDirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			subDirs = append(subDirs, e.Name())
		}
	}
	sort.Strings(subDirs)

	for _, d := range subDirs {
		childPath := d
		if prefix != "" {
			childPath = prefix + "/" + d
		}
		fullDir := filepath.Join(dir, d)

		// Check exclusion list from README front matter.
		if dirFM != nil && stringInSlice(d, dirFM.Exclude) {
			slog.Debug("plugin excluded by README front matter", "name", d)
			continue
		}

		// Determine enabled status.
		enabled := true
		if dirFM != nil {
			enabled = dirFM.Enabled
		}

		if isGoPlugin(fullDir) {
			// Leaf Go plugin directory.
			desc := ""
			if dirFM != nil {
				desc = dirFM.Description
			}

			if enabled {
				slog.Debug("loading plugin", "name", d, "source_dir", fullDir, "path", childPath)
				if err := m.loadPlugin(ctx, d, fullDir, cacheDir); err != nil {
					slog.Error("plugin load failed", "name", d, "path", childPath, "error", err)
					// Still include a disabled entry in the tree for visibility.
					result = append(result, PluginTreeEntry{
						Name:        d,
						Path:        childPath,
						IsDirectory: false,
						Description: fmt.Sprintf("%s (load failed: %v)", d, err),
						Enabled:     false,
					})
					continue
				}

				// Get descriptor from registry to include in tree entry.
				info, ok := m.registry.Get(d)
				if ok && info.Descriptor != nil {
					desc = info.Descriptor.Description
					if desc == "" {
						desc = info.Descriptor.DisplayName
					}
					result = append(result, PluginTreeEntry{
						Name:        d,
						Path:        childPath,
						IsDirectory: false,
						Description: desc,
						Enabled:     true,
						Plugin:      info.Descriptor,
					})
				} else {
					result = append(result, PluginTreeEntry{
						Name:        d,
						Path:        childPath,
						IsDirectory: false,
						Description: d,
						Enabled:     true,
					})
				}
			} else {
				// Disabled — don't load but include in tree.
				result = append(result, PluginTreeEntry{
					Name:        d,
					Path:        childPath,
					IsDirectory: false,
					Description: desc,
					Enabled:     false,
				})
			}
		} else {
			// Category directory — recurse.
			children, err := m.scanPluginDir(ctx, fullDir, childPath, dirFM, cacheDir)
			if err != nil {
				slog.Warn("scan plugin subdir", "dir", d, "error", err)
				continue
			}
			if children == nil {
				children = []PluginTreeEntry{}
			}

			desc := ""
			if dirFM != nil {
				desc = dirFM.Description
			}

			result = append(result, PluginTreeEntry{
				Name:        d,
				Path:        childPath,
				IsDirectory: true,
				Description: desc,
				Enabled:     enabled,
				Children:    children,
			})
		}
	}

	return result, nil
}

// loadPlugin builds, launches, and registers a single plugin.
func (m *Manager) loadPlugin(ctx context.Context, name, sourceDir, cacheDir string) error {
	slog.Debug("loading plugin step: build", "name", name)
	result, err := m.builder.Build(name, sourceDir)
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}
	slog.Debug("plugin build result", "name", name, "binary", result.BinaryPath, "from_cache", result.FromCache)

	sessionID := fmt.Sprintf("plugin-%s", name)

	slog.Debug("launching plugin process", "name", name, "binary", result.BinaryPath, "ctx_url", m.CtxURL, "session_id", sessionID)
	proc, err := Launch(result.BinaryPath, m.CtxURL, m.CtxToken, sessionID, nil)
	if err != nil {
		return fmt.Errorf("launch: %w", err)
	}
	slog.Debug("plugin process launched", "name", name, "url", proc.URL, "pid", proc.Cmd.Process.Pid)

	client := NewClient(proc.URL)
	slog.Debug("connected to plugin extension API", "name", name, "url", proc.URL)

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

// ListPlugins returns all loaded plugin descriptors (flat list).
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

// ListPluginTree returns the tree of plugin directory entries from the
// last LoadPlugins call.
func (m *Manager) ListPluginTree() []PluginTreeEntry {
	slog.Debug("manager ListPluginTree", "entries", len(m.tree))
	return m.tree
}

// ToProtoPluginTree converts a PluginTreeEntry (internal) to a protobuf
// PluginTreeEntry.
func ToProtoPluginTree(entry *PluginTreeEntry) *dotfilesdv1.PluginTreeEntry {
	pe := &dotfilesdv1.PluginTreeEntry{
		Name:        entry.Name,
		Path:        entry.Path,
		IsDirectory: entry.IsDirectory,
		Description: entry.Description,
		Enabled:     entry.Enabled,
	}
	if entry.Plugin != nil {
		pe.Plugin = ToProtoDescriptor(entry.Plugin)
	}
	for i := range entry.Children {
		pe.Children = append(pe.Children, ToProtoPluginTree(&entry.Children[i]))
	}
	return pe
}

// Shutdown kills all running plugins and clears the registry.
func (m *Manager) Shutdown() {
	slog.Debug("manager Shutdown: killing all plugins and clearing registry")
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registry.Clear()
	m.loaded = false
	m.tree = nil
	slog.Debug("manager shutdown complete")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

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

// splitFrontMatter splits raw text into (bodyWithoutFrontMatter, yamlString).
func splitFrontMatter(text string) (body string, frontMatter string) {
	text = strings.TrimLeft(text, "\n\r\t ")
	if !strings.HasPrefix(text, "---") {
		return text, ""
	}
	rest := text[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return text, ""
	}
	frontMatter = strings.TrimSpace(rest[:idx])
	body = strings.TrimLeft(rest[idx+4:], "\n\r")
	return body, frontMatter
}

// parseDirFrontMatter reads a README.md and extracts its YAML front matter.
func parseDirFrontMatter(path string) (*DirFrontMatter, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	_, yamlStr := splitFrontMatter(string(data))
	if yamlStr == "" {
		return &DirFrontMatter{Enabled: true, Exclude: nil}, nil
	}
	var fm DirFrontMatter
	if err := yaml.Unmarshal([]byte(yamlStr), &fm); err != nil {
		return nil, err
	}
	return &fm, nil
}

// findReadme looks for a README.md entry in a directory listing.
func findReadme(entries []os.DirEntry) os.DirEntry {
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(e.Name(), "README.md") {
			return e
		}
	}
	return nil
}

// stringInSlice checks if a string is in a slice.
func stringInSlice(s string, slice []string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
