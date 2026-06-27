package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"dotfilesd/internal/pkg/plugin"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
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

func (b *pluginBackend) Log(ctx context.Context, pluginName, level, msg string, attrs map[string]string) error {
	// Convert map attrs to key-value pairs.
	kv := make([]any, 0, len(attrs)*2)
	for k, v := range attrs {
		kv = append(kv, k, v)
	}

	// Use the daemon's new logging package if available, otherwise slog.
	if b.daemon.logger != nil {
		log := b.daemon.logger.Logger("plugin." + pluginName)
		switch level {
		case "trace":
			log.Trace(msg, kv...)
		case "debug":
			log.Debug(msg, kv...)
		case "info":
			log.Info(msg, kv...)
		case "warn":
			log.Warn(msg, kv...)
		case "error":
			log.Error(msg, kv...)
		case "fatal":
			log.Fatal(msg, kv...)
		default:
			log.Info(msg, kv...)
		}
	} else {
		// Fallback to slog.
		slogLevel := slog.LevelInfo
		switch level {
		case "trace":
			slogLevel = levelTrace
		case "debug":
			slogLevel = slog.LevelDebug
		case "info":
			slogLevel = slog.LevelInfo
		case "warn":
			slogLevel = slog.LevelWarn
		case "error":
			slogLevel = slog.LevelError
		}
		slog.Log(ctx, slogLevel, msg, "plugin", pluginName, "attrs", attrs)
	}
	return nil
}

func (b *pluginBackend) CallPlugin(ctx context.Context, pluginName, toolName string, args map[string]string) (int32, string, string, string, error) {
	// Route through the plugin manager's CallTool which opens a streaming
	// connection to the target plugin. We buffer the entire response.
	if b.daemon.pluginMgr == nil {
		return 0, "", "", "", fmt.Errorf("plugin system not initialized")
	}

	stream, err := b.daemon.pluginMgr.CallTool(ctx, pluginName, toolName, args)
	if err != nil {
		return 0, "", "", "", fmt.Errorf("call plugin %q tool %q: %w", pluginName, toolName, err)
	}

	var stdoutBuf, stderrBuf strings.Builder
	var errMsg string
	for stream.Receive() {
		chunk := stream.Msg()
		if len(chunk.StdoutChunk) > 0 {
			stdoutBuf.Write(chunk.StdoutChunk)
		}
		if len(chunk.StderrChunk) > 0 {
			stderrBuf.Write(chunk.StderrChunk)
		}
		if chunk.Done {
			errMsg = chunk.ErrorMessage
			break
		}
	}
	if err := stream.Err(); err != nil {
		return 0, stdoutBuf.String(), stderrBuf.String(), "", fmt.Errorf("plugin tool stream: %w", err)
	}

	exitCode := int32(0)
	if errMsg != "" {
		exitCode = 1
	}

	return exitCode, stdoutBuf.String(), stderrBuf.String(), errMsg, nil
}

func (b *pluginBackend) RunScript(ctx context.Context, sessionID string, req *dotfilesdv1.RunScriptViaContextRequest) (bool, []*dotfilesdv1.StepResult, string, error) {
	// Convert the context-style request to the internal ScriptRunner request.
	runReq := connect.NewRequest(&dotfilesdv1.RunScriptRequest{
		Session: &dotfilesdv1.Session{Id: sessionID},
	})
	switch src := req.Source.(type) {
	case *dotfilesdv1.RunScriptViaContextRequest_Script:
		runReq.Msg.Source = &dotfilesdv1.RunScriptRequest_Script{Script: src.Script}
	case *dotfilesdv1.RunScriptViaContextRequest_ScriptPath:
		runReq.Msg.Source = &dotfilesdv1.RunScriptRequest_ScriptPath{ScriptPath: src.ScriptPath}
	case *dotfilesdv1.RunScriptViaContextRequest_RegisteredScript:
		runReq.Msg.Source = &dotfilesdv1.RunScriptRequest_RegisteredScript{RegisteredScript: src.RegisteredScript}
	default:
		return false, nil, "no script source provided", nil
	}

	runner := NewScriptRunner(b.daemon.sessions, b.daemon.scripts)
	resp, err := runner.RunScript(ctx, runReq)
	if err != nil {
		return false, nil, "", fmt.Errorf("run script: %w", err)
	}

	return resp.Msg.AllSucceeded, resp.Msg.Steps, resp.Msg.Error, nil
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
