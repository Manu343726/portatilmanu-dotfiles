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

	// PluginDocs holds the structured Documentation proto when the plugin
	// serves embedded docs via protoc-gen-docs.
	PluginDocs *dotfilesdv1.Documentation

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
	depGraph    map[string][]string // plugin → its dependencies (cached)
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
		depGraph:    make(map[string][]string),
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

// stepAPIClient generates OpenAPI clients from api/*.yaml specs.
// Each spec generates code into api/gen/client.gen.go (package gen).
func stepAPIClient(sourceDir string) error {
	specMatches, err := filepath.Glob(filepath.Join(sourceDir, "api", "*.yaml"))
	if err != nil || len(specMatches) == 0 {
		return nil // no spec to generate from
	}
	home, _ := os.UserHomeDir()
	goBin := filepath.Join(home, "go", "bin")

	for _, specFile := range specMatches {
		outDir := filepath.Join(sourceDir, "api", "gen")
		outFile := filepath.Join(outDir, "client.gen.go")
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return fmt.Errorf("mkdir api/gen: %w", err)
		}

		cmd := exec.Command(filepath.Join(goBin, "oapi-codegen"),
			"--package=gen",
			"--generate=types,client,spec",
			"-o", outFile,
			specFile,
		)
		cmd.Env = os.Environ()
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("oapi-codegen: %w\n%s", err, string(out))
		}
	}
	return nil
}

// stepProto compiles proto files for a plugin if it has a proto/<name>/ directory.
func stepProto(sourceDir, name string) error {
	protoDir := filepath.Join(sourceDir, "proto", name)
	matches, err := filepath.Glob(filepath.Join(protoDir, "*.proto"))
	if err != nil || len(matches) == 0 {
		return nil // no proto files to compile
	}
	home, _ := os.UserHomeDir()
	pathEnv := "PATH=" + filepath.Join(home, "go", "bin") + ":" + os.Getenv("PATH")

	// Generate Go code and markdown docs.
	cmd := exec.Command("protoc",
		"--proto_path="+sourceDir,
		"--go_out="+sourceDir, "--go_opt=paths=source_relative",
		"--connect-go_out="+sourceDir, "--connect-go_opt=paths=source_relative",
		"--docs_out="+sourceDir,
	)
	cmd.Args = append(cmd.Args, matches...)
	cmd.Dir = sourceDir
	cmd.Env = append(os.Environ(), pathEnv)
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

	// Cache the dependency graph for dependents-check at unload time.
	m.mu.Lock()
	m.depGraph = make(map[string][]string, len(pluginDeps))
	for k, v := range pluginDeps {
		m.depGraph[k] = v
	}
	m.mu.Unlock()

	// Topological sort for build order.
	buildOrder := topologicalSort(pluginDeps)
	slog.Info("plugin build order", "order", buildOrder)

	for _, name := range buildOrder {
		sourceDir := filepath.Join(pluginsDir, name)
		if err := m.loadPlugin(ctx, name, sourceDir, cacheDir); err != nil {
			slog.Error("load plugin failed", "name", name, "error", err)
			continue
		}
	}

	slog.Info("plugins loaded", "count", len(m.plugins))
	return nil
}

// LoadPluginByName loads a single plugin and its dependencies by name.
// Returns the loaded PluginInfo and a list of dependency names that were
// loaded as a side effect. Returns error if the plugin or a dependency
// cannot be found or fails to load.
func (m *Manager) LoadPluginByName(ctx context.Context, name string) (*PluginInfo, []string, error) {
	pluginsDir := expandHome(m.PluginsDir)
	cacheDir := expandHome(m.CacheDir)
	sourceDir := filepath.Join(pluginsDir, name)

	if !isGoPlugin(sourceDir) {
		return nil, nil, fmt.Errorf("plugin %q not found in %s", name, pluginsDir)
	}

	// Check if already loaded.
	m.mu.RLock()
	_, exists := m.plugins[name]
	m.mu.RUnlock()
	if exists {
		info, _ := m.GetPlugin(name)
		return info, nil, nil
	}

	// Parse deps and load dependencies first.
	deps, err := parsePluginDeps(sourceDir, pluginsDir)
	if err != nil {
		slog.Warn("parse deps failed", "name", name, "error", err)
		deps = []string{}
	}

	var loadedDeps []string
	for _, dep := range deps {
		depDir := filepath.Join(pluginsDir, dep)
		if !isGoPlugin(depDir) {
			continue
		}
		m.mu.RLock()
		_, depExists := m.plugins[dep]
		m.mu.RUnlock()
		if depExists {
			continue
		}
		if err := m.loadPlugin(ctx, dep, depDir, cacheDir); err != nil {
			return nil, loadedDeps, fmt.Errorf("load dependency %q: %w", dep, err)
		}
		loadedDeps = append(loadedDeps, dep)
	}

	if err := m.loadPlugin(ctx, name, sourceDir, cacheDir); err != nil {
		return nil, loadedDeps, err
	}

	info, _ := m.GetPlugin(name)
	return info, loadedDeps, nil
}

// UnloadPluginByName stops a plugin supervisor and removes it from the registry.
// Returns an error if any other loaded plugin depends on this one.
func (m *Manager) UnloadPluginByName(name string) error {
	m.mu.Lock()

	// Check for dependents before unloading.
	var dependents []string
	for pluginName, deps := range m.depGraph {
		if pluginName == name {
			continue
		}
		// Only consider loaded plugins.
		if _, loaded := m.plugins[pluginName]; !loaded {
			continue
		}
		for _, dep := range deps {
			if dep == name {
				dependents = append(dependents, pluginName)
				break
			}
		}
	}
	if len(dependents) > 0 {
		m.mu.Unlock()
		return fmt.Errorf("cannot unload %q: %d loaded plugin(s) depend on it: %v",
			name, len(dependents), dependents)
	}

	sup, hasSup := m.supervisors[name]
	info, hasInfo := m.plugins[name]
	if hasSup {
		delete(m.supervisors, name)
	}
	if hasInfo {
		delete(m.plugins, name)
	}
	delete(m.depGraph, name)
	m.mu.Unlock()

	if !hasInfo {
		return fmt.Errorf("plugin %q not found", name)
	}

	if hasSup {
		sup.stop()
	} else if info.Process != nil {
		info.Process.Kill()
	}

	return nil
}

// loadPlugin performs the full build-launch-discover-register cycle for one plugin.
// This is the shared implementation used by both LoadPlugins (startup) and
// LoadPluginByName (runtime dynamic loading).
func (m *Manager) loadPlugin(ctx context.Context, name, sourceDir, cacheDir string) error {
	// Cache dependency info for this plugin.
	pluginsDir := expandHome(m.PluginsDir)
	deps, _ := parsePluginDeps(sourceDir, pluginsDir)
	m.mu.Lock()
	m.depGraph[name] = deps
	m.mu.Unlock()

	// Step a: Proto compilation.
	if err := stepProto(sourceDir, name); err != nil {
		return fmt.Errorf("proto compile: %w", err)
	}

	// Step a': OpenAPI client generation (if plugin has api/zerotier-central.yaml).
	if err := stepAPIClient(sourceDir); err != nil {
		return fmt.Errorf("api client: %w", err)
	}

	// Step b: Go build.
	binaryPath := filepath.Join(cacheDir, name, name)
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	// Resolve transitive dependencies so plugins don't need manual go.sum entries.
	slog.Info("resolving plugin dependencies", "plugin", name)
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = sourceDir
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		slog.Error("plugin dependency resolution failed",
			"plugin", name, "error", err, "output", string(out))
	}

	slog.Info("building plugin", "plugin", name, "binary", binaryPath)
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = sourceDir
	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Error("plugin build failed", "plugin", name, "error", err, "output", string(out))
		return fmt.Errorf("go build: %w\n%s", err, string(out))
	}
	slog.Info("plugin built successfully", "plugin", name)

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
		return fmt.Errorf("stdout pipe: %w", err)
	}
	procCmd.Stderr = os.Stderr
	if err := procCmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	// Step d: Read handshake JSON from stdout.
	var hs handshake
	if err := readJSONLine(stdout, &hs); err != nil {
		procCmd.Process.Kill()
		return fmt.Errorf("handshake read: %w", err)
	}
	if hs.Protocol != "dotfilesd-plugin-v1" {
		procCmd.Process.Kill()
		return fmt.Errorf("unknown handshake protocol: %s", hs.Protocol)
	}
	slog.Info("plugin launched", "name", name, "url", hs.URL, "pid", procCmd.Process.Pid)

	// Step e: grpcreflect discovery via rpcreflection.
	refClient := rpcreflection.NewClient(hs.URL)
	svcInfos, discErr := refClient.DiscoverServices(ctx)
	if discErr != nil {
		procCmd.Process.Kill()
		return fmt.Errorf("grpcreflect discovery: %w", discErr)
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

	// Build schemas for CLI/MCP clients.
	schemas := rpcreflection.BuildServiceSchemas(nonSystemInfos)

	// Step f1: Fetch method hints (interactive stdin requirements) from the
	// plugin's /__dotfiles/method_hints endpoint and merge into schemas.
	httpClient := &http.Client{}
	if hints := fetchMethodHints(ctx, httpClient, hs.URL); hints != nil {
		for _, svc := range schemas {
			if svcHints, ok := hints[svc.Name]; ok {
				for _, m := range svc.Methods {
					if svcHints[m.Name] {
						m.NeedsInteractiveStdin = true
					}
				}
			}
		}
	}

	// Step f2: DocumentationService — fetch structured docs and enrich schemas.
	pluginDocs := fetchPluginDocs(ctx, httpClient, hs.URL)
	enrichSchemasFromDocs(schemas, pluginDocs)

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
		PluginDocs: pluginDocs,
		Schemas:     schemas,
		Process:     procCmd.Process,
		SourceDir:   sourceDir,
		CacheDir:    cacheDir,
	}
	m.mu.Lock()
	m.plugins[name] = info
	m.mu.Unlock()

	sup := newSupervisor(name, binaryPath, sourceDir, cacheDir, m.CtxURL, m.CtxToken, sessionID)
	if err := sup.startAdopt(ctx, info); err != nil {
		procCmd.Process.Kill()
		m.mu.Lock()
		delete(m.plugins, name)
		m.mu.Unlock()
		return fmt.Errorf("supervisor start: %w", err)
	}
	m.mu.Lock()
	m.supervisors[name] = sup
	m.mu.Unlock()
	slog.Info("plugin registered with supervisor", "name", name, "services", services)

	return nil
}

// ReloadPlugins rescans the plugins directory, loading new plugins and
// unloading plugins whose directories no longer exist.
func (m *Manager) ReloadPlugins(ctx context.Context) (loaded, unloaded []string, _ error) {
	pluginsDir := expandHome(m.PluginsDir)
	cacheDir := expandHome(m.CacheDir)

	// Get current plugins.
	m.mu.RLock()
	current := make(map[string]bool)
	for name := range m.plugins {
		current[name] = true
	}
	m.mu.RUnlock()

	// Discover what's on disk now.
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read plugins dir: %w", err)
	}

	onDisk := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() && isGoPlugin(filepath.Join(pluginsDir, entry.Name())) {
			onDisk[entry.Name()] = true
		}
	}

	// Unload plugins no longer on disk.
	for name := range current {
		if !onDisk[name] {
			if err := m.UnloadPluginByName(name); err != nil {
				slog.Warn("unload failed during reload", "name", name, "error", err)
			} else {
				unloaded = append(unloaded, name)
			}
		}
	}

	// Load new plugins.
	pluginDeps, err := m.discoverPlugins(pluginsDir)
	if err != nil {
		return loaded, unloaded, fmt.Errorf("discover plugins: %w", err)
	}
	buildOrder := topologicalSort(pluginDeps)
	for _, name := range buildOrder {
		m.mu.RLock()
		_, exists := m.plugins[name]
		m.mu.RUnlock()
		if exists {
			continue
		}
		sourceDir := filepath.Join(pluginsDir, name)
		if err := m.loadPlugin(ctx, name, sourceDir, cacheDir); err != nil {
			slog.Warn("load failed during reload", "name", name, "error", err)
			continue
		}
		loaded = append(loaded, name)
	}

	return loaded, unloaded, nil
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

// fetchPluginDocs fetches structured documentation from the plugin's
// DocumentationService returning the Documentation proto if available.
func fetchPluginDocs(ctx context.Context, httpClient *http.Client, pluginURL string) *dotfilesdv1.Documentation {
	docsClient := dotfilesdv1connect.NewDocumentationServiceClient(httpClient, pluginURL)
	if resp, err := docsClient.GetDocumentation(ctx, connect.NewRequest(&dotfilesdv1.DocumentationRequest{})); err == nil {
		return resp.Msg.Documentation
	}
	return nil
}

// enrichSchemasFromDocs populates ServiceSchema descriptions from the
// structured Documentation proto returned by the plugin's DocumentationService.
// Recursively propagates service, method, message, and field descriptions.
func enrichSchemasFromDocs(schemas []*dotfilesdv1.ServiceSchema, doc *dotfilesdv1.Documentation) {
	if doc == nil {
		return
	}
	for _, svc := range schemas {
		for _, sd := range doc.Services {
			if sd.Name == svc.Name {
				svc.Description = sd.Description
				for _, m := range svc.Methods {
					for _, md := range sd.Methods {
						if md.Name == m.Name {
							m.Description = md.Description
							enrichMessageSchema(m.Request, md.Request)
							enrichMessageSchema(m.Response, md.Response)
							break
						}
					}
				}
				break
			}
		}
	}
}

// enrichMessageSchema recursively propagates MessageDoc descriptions
// (message, fields, nested messages) into a MessageSchema.
func enrichMessageSchema(ms *dotfilesdv1.MessageSchema, md *dotfilesdv1.MessageDoc) {
	if ms == nil || md == nil {
		return
	}
	ms.Description = md.Description
	for _, f := range ms.Fields {
		for _, fd := range md.Fields {
			if fd.Name == f.Name {
				f.Description = fd.Description
				break
			}
		}
	}
	for _, nested := range ms.Messages {
		for _, nd := range md.NestedMessages {
			if nd.Name == nested.Name {
				enrichMessageSchema(nested, nd)
				break
			}
		}
	}
}

// methodHintsResponse is the JSON structure returned by the plugin's
// /__dotfiles/method_hints endpoint.
type methodHintsResponse struct {
	Services []struct {
		Name              string   `json:"name"`
		InteractiveMethod []string `json:"interactive_methods"`
	} `json:"services"`
}

// fetchMethodHints calls the plugin's /__dotfiles/method_hints endpoint
// and returns a map of service_name → method_name → needs_interactive_stdin.
// Returns nil if the endpoint is not available or returns an error.
func fetchMethodHints(ctx context.Context, httpClient *http.Client, pluginURL string) map[string]map[string]bool {
	req, err := http.NewRequestWithContext(ctx, "GET", pluginURL+"/__dotfiles/method_hints", nil)
	if err != nil {
		slog.Debug("fetchMethodHints: create request failed", "url", pluginURL, "error", err)
		return nil
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Debug("fetchMethodHints: request failed", "url", pluginURL, "error", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug("fetchMethodHints: non-200", "url", pluginURL, "status", resp.StatusCode)
		return nil
	}

	var hints methodHintsResponse
	if err := json.NewDecoder(resp.Body).Decode(&hints); err != nil {
		slog.Debug("fetchMethodHints: decode failed", "url", pluginURL, "error", err)
		return nil
	}

	result := make(map[string]map[string]bool, len(hints.Services))
	for _, svc := range hints.Services {
		methods := make(map[string]bool, len(svc.InteractiveMethod))
		for _, m := range svc.InteractiveMethod {
			methods[m] = true
		}
		if len(methods) > 0 {
			result[svc.Name] = methods
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}
