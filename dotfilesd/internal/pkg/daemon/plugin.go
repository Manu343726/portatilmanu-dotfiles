package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"

	"dotfilesd/internal/pkg/plugin"
)

// ------------------------------------------------
// Plugin system initialization
// ------------------------------------------------

// InitPlugins creates the plugin manager and loads plugins.
// Called during daemon startup.
func (d *Daemon) InitPlugins() error {
	pluginsDir := d.config.PluginsDir
	if pluginsDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home dir: %w", err)
		}
		pluginsDir = home + "/.config/dotfilesd/plugins"
	}

	cacheDir := d.config.PluginCacheDir
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home dir: %w", err)
		}
		cacheDir = home + "/.cache/dotfilesd/plugins"
	}

	ctxURL := fmt.Sprintf("http://127.0.0.1:%s", d.config.Port)
	ctxToken := generatePluginToken()
	d.pluginToken = ctxToken

	d.pluginMgr = plugin.NewManager(pluginsDir, cacheDir, ctxURL, ctxToken)

	slog.Info("loading plugins", "dir", pluginsDir)
	if err := d.pluginMgr.LoadPlugins(context.Background()); err != nil {
		return fmt.Errorf("load plugins: %w", err)
	}

	plugins := d.pluginMgr.ListPlugins()
	slog.Info("plugins loaded", "count", len(plugins))
	for _, p := range plugins {
		slog.Info("  plugin", "name", p.Name, "version", p.Version, "tools", len(p.Tools))
	}

	return nil
}

// generatePluginToken creates a random hex token for the Execution Context.
func generatePluginToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
