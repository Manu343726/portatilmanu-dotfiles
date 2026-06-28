package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"time"

	"dotfilesd/internal/pkg/diagnostics"
	"dotfilesd/internal/pkg/plugin"
)

// InitPlugins creates the plugin manager and loads plugins.
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

	if err := d.pluginMgr.LoadPlugins(context.Background()); err != nil {
		return fmt.Errorf("load plugins: %w", err)
	}

	plugins := d.pluginMgr.ListPlugins()
	slog.Info("plugins loaded", "count", len(plugins))
	for _, p := range plugins {
		slog.Info("  plugin", "name", p.Name, "version", p.Version, "display", p.DisplayName, "services", p.Services)
		now := time.Now()
		d.diag.PushEvent(diagnostics.Event{
			Type:      diagnostics.EventPluginSpawn,
			Resource:  "plugin:" + p.Name,
			Parent:    "daemon",
			Timestamp: now,
			Message:   fmt.Sprintf("%s v%s", p.DisplayName, p.Version),
			Attrs: map[string]string{
				"pid":      fmt.Sprintf("%d", p.Process.Pid),
				"url":      p.URL,
				"services": fmt.Sprintf("%d", len(p.Services)),
			},
		})
	}

	return nil
}

// generatePluginToken creates a random hex token.
func generatePluginToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ShutdownPlugins shuts down the plugin manager and all supervisors.
func (d *Daemon) ShutdownPlugins() {
	if d.pluginMgr != nil {
		plugins := d.pluginMgr.ListPlugins()
		for _, p := range plugins {
			d.diag.PushEvent(diagnostics.Event{
				Type:      diagnostics.EventPluginStop,
				Resource:  "plugin:" + p.Name,
				Timestamp: time.Now(),
				Message:   fmt.Sprintf("%s v%s", p.DisplayName, p.Version),
			})
		}
		d.pluginMgr.Shutdown()
	}
}
