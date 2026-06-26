package plugin

import (
	"fmt"
	"log/slog"
	"sync"

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

// PluginInfo holds the metadata and client for a loaded plugin.
type PluginInfo struct {
	Descriptor *dotfilesdv1.ExtensionDescriptor // plugin capabilities
	Client     *Client                         // RPC client for calling the plugin
	Process    *Process                        // running plugin subprocess
}

// Registry maintains a thread-safe map of loaded plugins.
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]*PluginInfo
}

// NewRegistry creates an empty plugin registry.
func NewRegistry() *Registry {
	return &Registry{
		plugins: make(map[string]*PluginInfo),
	}
}

// Register adds a plugin to the registry.
func (r *Registry) Register(name string, info *PluginInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.plugins[name]; exists {
		slog.Debug("registry: plugin already registered", "name", name)
		return fmt.Errorf("plugin %q already registered", name)
	}
	r.plugins[name] = info
	slog.Debug("registry: plugin registered", "name", name, "tools", len(info.Descriptor.Tools))
	return nil
}

// Unregister removes a plugin from the registry.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	slog.Debug("registry: unregistering plugin", "name", name)
	delete(r.plugins, name)
}

// Get returns the plugin info for the given name.
func (r *Registry) Get(name string) (*PluginInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.plugins[name]
	slog.Debug("registry: Get", "name", name, "found", ok)
	return info, ok
}

// List returns all registered plugins.
func (r *Registry) List() []PluginInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	slog.Debug("registry: List", "count", len(r.plugins))
	result := make([]PluginInfo, 0, len(r.plugins))
	for name, info := range r.plugins {
		slog.Debug("  registered plugin", "name", name, "tools", len(info.Descriptor.Tools))
		result = append(result, *info)
	}
	return result
}

// Len returns the number of registered plugins.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.plugins)
}

// Clear removes all plugins and shuts down their processes.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	slog.Debug("registry: clearing all plugins", "count", len(r.plugins))
	for name, info := range r.plugins {
		slog.Debug("  killing plugin", "name", name)
		if info.Process != nil {
			info.Process.Kill()
		}
		delete(r.plugins, name)
	}
	slog.Debug("registry: clear complete")
}
