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

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"connectrpc.com/connect"

	"dotfilesd/internal/pkg/rpcreflection"
)

// PluginInfo holds metadata for a loaded plugin.
type PluginInfo struct {
	Name        string
	DisplayName string
	Version     string
	Description string
	URL         string
	Services    []string // service names from grpcreflect discovery
	Process     *os.Process
	SourceDir   string
	CacheDir    string

	// DocsCache holds documentation fetched from the plugin's
	// DocumentationService.
	DocsCache map[string]string

	// Schemas holds full introspection data (methods, fields, types, enums).
	Schemas []*dotfilesdv1.ServiceSchema
}

// Manager orchestrates plugin lifecycle with dependency-aware builds
// and crash supervision (exponential backoff restart).
type Manager struct {
	PluginsDir string
	CacheDir   string
	CtxURL     string
	CtxToken   string

	mu          sync.RWMutex
	plugins     map[string]*PluginInfo
	supervisors map[string]*supervisor
}

// NewManager creates a new plugin manager.
func NewManager(pluginsDir, cacheDir, ctxURL, ctxToken string) *Manager {
	return &Manager{
		PluginsDir:  pluginsDir,
		CacheDir:    cacheDir,
		CtxURL:      ctxURL,
		CtxToken:    ctxToken,
		plugins:     make(map[string]*PluginInfo),
		supervisors: make(map[string]*supervisor),
	}
}

// handshake is the JSON structure a plugin writes to stdout on startup.
type handshake struct {
	Protocol    string `json:"protocol"`
	URL         string `json:"url"`
	SessionID   string `json:"session_id"`
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
}

// stepProto compiles proto files for a plugin if it has a proto/<name>/ directory.
func stepProto(sourceDir, name string) error {
	protoDir := filepath.Join(sourceDir, "proto", name)
	matches, err := filepath.Glob(filepath.Join(protoDir, "*.proto"))
	if err != nil || len(matches) == 0 {
		return nil // no proto files to compile
	}
	cmd := exec.Command("protoc",
		"--proto_path="+sourceDir,
		"--go_out="+sourceDir, "--go_opt=paths=source_relative",
		"--connect-go_out="+sourceDir, "--connect-go_opt=paths=source_relative",
	)
	cmd.Args = append(cmd.Args, matches...)
	cmd.Dir = sourceDir
	// Ensure protoc can find protoc-gen-go and protoc-gen-connect-go in PATH.
	home, _ := os.UserHomeDir()
	cmd.Env = append(os.Environ(), "PATH="+filepath.Join(home, "go", "bin")+":"+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("proto compile: %w\n%s", err, string(out))
	}
	return nil
}

// LoadPlugins discovers, builds (in dependency order), launches, and registers all plugins.
func (m *Manager) LoadPlugins(ctx context.Context) error {
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

		// Step a: Proto compilation.
		if err := stepProto(sourceDir, name); err != nil {
			slog.Error("proto compile failed", "name", name, "error", err)
			continue
		}

		// Step b: Go build.
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

		// Step c: Launch subprocess.
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

		// Step d: Read handshake JSON from stdout.
		var hs handshake
		if err := readJSONLine(stdout, &hs); err != nil {
			procCmd.Process.Kill()
			slog.Error("handshake read failed", "name", name, "error", err)
			continue
		}
		if hs.Protocol != "dotfilesd-plugin-v1" {
			procCmd.Process.Kill()
			slog.Error("unknown handshake protocol", "name", name, "protocol", hs.Protocol)
			continue
		}
		slog.Info("plugin launched", "name", name, "url", hs.URL, "pid", procCmd.Process.Pid)

		// Step e: grpcreflect discovery via rpcreflection — get full method/type metadata.
		refClient := rpcreflection.NewClient(hs.URL)
		svcInfos, discErr := refClient.DiscoverServices(ctx)
		if discErr != nil {
			procCmd.Process.Kill()
			slog.Error("grpcreflect discovery failed", "name", name, "error", discErr)
			continue
		}

		// Extract service names (filtering out reflection services).
		var services []string
		var nonSystemInfos []rpcreflection.ServiceInfo
		for _, si := range svcInfos {
			if rpcreflection.IsSystemService(si.FullName) {
				continue
			}
			services = append(services, si.FullName)
			nonSystemInfos = append(nonSystemInfos, si)
		}

		// Build full type introspection schemas for CLI/MCP clients.
		schemas := rpcreflection.BuildServiceSchemas(nonSystemInfos)

		// Step f: If DocumentationService is exposed, call it (best-effort).
		httpClient := &http.Client{}
		docsCache := fetchDocumentation(ctx, httpClient, hs.URL, services)

		// Step g: Store PluginInfo + start supervisor.
		displayName := hs.DisplayName
		if displayName == "" {
			displayName = hs.Name
		}
		if displayName == "" {
			displayName = name
		}
		info := &PluginInfo{
			Name:        name,
			DisplayName: displayName,
			Version:     hs.Version,
			Description: hs.Description,
			URL:         hs.URL,
			Services:    services,
			DocsCache:   docsCache,
			Schemas:     schemas,
			Process:     procCmd.Process,
			SourceDir:   sourceDir,
			CacheDir:    cacheDir,
		}
		// Register plugin BEFORE starting supervisor so that cross-plugin
		// discovery (e.g. tmuxbar calling GetPlugin for resources) works
		// without deadlocking. Lock is only held during map writes.
		m.mu.Lock()
		m.plugins[name] = info
		m.mu.Unlock()

		// Start crash supervisor with exponential backoff (adopts existing process).
		sup := newSupervisor(name, binaryPath, sourceDir, cacheDir, m.CtxURL, m.CtxToken, sessionID)
		if err := sup.startAdopt(ctx, info); err != nil {
			procCmd.Process.Kill()
			slog.Error("supervisor start failed", "name", name, "error", err)
			m.mu.Lock()
			delete(m.plugins, name)
			m.mu.Unlock()
			continue
		}
		m.mu.Lock()
		m.supervisors[name] = sup
		m.mu.Unlock()
		slog.Info("plugin registered with supervisor", "name", name, "services", services)
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

// Shutdown stops all plugin supervisors and kills all plugin processes.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, sup := range m.supervisors {
		slog.Debug("shutting down plugin supervisor", "name", name)
		sup.stop()
	}
	m.supervisors = make(map[string]*supervisor)
	m.plugins = make(map[string]*PluginInfo)
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

// fetchDocumentation calls the plugin's DocumentationService (if exposed)
// and returns a map of service name → markdown content. The empty-string key
// holds plugin-level docs. Returns nil if the plugin doesn't expose the
// DocumentationService or if all calls fail.
func fetchDocumentation(ctx context.Context, httpClient *http.Client, pluginURL string, services []string) map[string]string {
	hasDocs := false
	for _, s := range services {
		if s == "dotfilesd.v1.DocumentationService" {
			hasDocs = true
			break
		}
	}
	if !hasDocs {
		return nil
	}

	docsClient := dotfilesdv1connect.NewDocumentationServiceClient(httpClient, pluginURL)
	cache := make(map[string]string)

	// Fetch plugin-level docs (empty service_name).
	if resp, err := docsClient.GetDocumentation(ctx, connect.NewRequest(&dotfilesdv1.DocumentationRequest{})); err == nil {
		if resp.Msg.Content != "" {
			cache[""] = resp.Msg.Content
		}
	} else {
		slog.Debug("GetDocumentation(plugin-level) failed", "url", pluginURL, "error", err)
	}

	// Fetch per-service docs.
	for _, svc := range services {
		if svc == "grpc.reflection.v1.ServerReflection" || svc == "grpc.reflection.v1alpha.ServerReflection" || svc == "dotfilesd.v1.DocumentationService" {
			continue
		}
		if resp, err := docsClient.GetDocumentation(ctx, connect.NewRequest(&dotfilesdv1.DocumentationRequest{ServiceName: svc})); err == nil {
			if resp.Msg.Content != "" {
				cache[svc] = resp.Msg.Content
			}
		} else {
			slog.Debug("GetDocumentation failed", "service", svc, "error", err)
		}
	}

	if len(cache) == 0 {
		return nil
	}
	return cache
}
