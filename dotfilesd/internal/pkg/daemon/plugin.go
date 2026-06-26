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

// pluginBackend implements plugin.ContextBackend by routing to the daemon's
// exec server and the session's feedback methods. This is the bridge between
// the Execution Context service and the daemon's core capabilities.
type pluginBackend struct {
	daemon  *Daemon
	execSvc *execServer
}

func newPluginBackend(d *Daemon, execSvc *execServer) *pluginBackend {
	return &pluginBackend{daemon: d, execSvc: execSvc}
}

func (b *pluginBackend) Exec(ctx context.Context, sessionID, command string) (int32, string, string, error) {
	// Session tracking: ensure a session exists for this plugin if needed.
	if sessionID != "" && b.daemon.sessions.Get(sessionID) == nil {
		b.daemon.sessions.CreateEphemeral()
	}

	resp, err := b.execSvc.ExecRaw(ctx, command, false)
	if err != nil {
		return 0, "", "", err
	}
	return resp.Msg.ExitCode, resp.Msg.Stdout, resp.Msg.Stderr, nil
}

func (b *pluginBackend) SudoExec(ctx context.Context, sessionID, command string) (int32, string, string, error) {
	resp, err := b.execSvc.ExecRaw(ctx, command, true)
	if err != nil {
		return 0, "", "", err
	}
	return resp.Msg.ExitCode, resp.Msg.Stdout, resp.Msg.Stderr, nil
}

func (b *pluginBackend) RequestInput(ctx context.Context, sessionID, prompt, defaultVal string, sensitive bool) (string, error) {
	session := b.daemon.sessions.Get(sessionID)
	if session == nil {
		return "", fmt.Errorf("session %q not found", sessionID)
	}
	return session.RequestInput(ctx, prompt, defaultVal, sensitive)
}

func (b *pluginBackend) RequestConfirm(ctx context.Context, sessionID, msg string, defaultConfirm bool) (bool, error) {
	session := b.daemon.sessions.Get(sessionID)
	if session == nil {
		return false, fmt.Errorf("session %q not found", sessionID)
	}
	return session.RequestConfirm(ctx, msg, defaultConfirm)
}

func (b *pluginBackend) RequestChoose(ctx context.Context, sessionID, prompt string, options []string, defaultIndex int) (int32, string, error) {
	session := b.daemon.sessions.Get(sessionID)
	if session == nil {
		return 0, "", fmt.Errorf("session %q not found", sessionID)
	}
	idx, opt, err := session.RequestChoose(ctx, prompt, options, defaultIndex)
	if err != nil {
		return 0, "", err
	}
	return int32(idx), opt, nil
}

// ------------------------------------------------
// Plugin system initialization
// ------------------------------------------------

// InitPlugins creates the plugin manager, context server, and loads plugins.
// Called during daemon startup.
func (d *Daemon) InitPlugins(execSvc *execServer) error {
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

	backend := newPluginBackend(d, execSvc)
	ctxPath, ctxHandler := plugin.NewContextServer(plugin.ContextServerOptions{
		Backend: backend,
		Token:   ctxToken,
	})
	d.pluginCtxPath = ctxPath
	d.pluginCtxHandler = ctxHandler

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
