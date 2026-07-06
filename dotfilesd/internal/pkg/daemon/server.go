package daemon

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"dotfilesd/internal/pkg/diagnostics"
	"dotfilesd/internal/pkg/logging"
	"dotfilesd/internal/pkg/plugin"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type Config struct {
	Port           string
	LogDir         string
	LogLevel       string
	LogMaxMB       int
	LogBackup      int
	LogAge         int
	ScriptsDir     string
	PluginsDir     string `yaml:"plugins_dir"`
	PluginCacheDir string `yaml:"plugin_cache_dir"`
	SudoTimeout   time.Duration
}

type Daemon struct {
	config   Config
	server   *http.Server
	sessions *SessionStore
	scripts  *ScriptRegistry

	// Logging system.
	logger *logging.Manager

	// Diagnostics engine.
	diag *diagnostics.Engine

	// Plugin system.
	pluginMgr   *plugin.Manager
	pluginToken string

	// Background tasks.
	bgTasks *backgroundTaskManager
}

func New(cfg Config) *Daemon {
	SetDaemonPort(cfg.Port)
	scriptsDir := cfg.ScriptsDir
	if scriptsDir == "" {
		home, _ := os.UserHomeDir()
		scriptsDir = home + "/.config/dotfilesd/scripts"
	}
	return &Daemon{
		config:   cfg,
		sessions: NewSessionStore(),
		scripts:  NewScriptRegistry(scriptsDir),
		diag:     diagnostics.New(),
	}
}

// PluginManager returns the daemon's plugin manager.
func (d *Daemon) PluginManager() *plugin.Manager {
	return d.pluginMgr
}

// ScriptsRegistry returns the daemon's script registry.
func (d *Daemon) ScriptsRegistry() *ScriptRegistry {
	return d.scripts
}

func (d *Daemon) Start() error {
	d.setupLogging()

	// Push daemon start event.
	d.diag.PushEvent(diagnostics.Event{
		Type:      diagnostics.EventDaemonStart,
		Resource:  "daemon",
		Timestamp: time.Now(),
		Message:   fmt.Sprintf("dotfilesd v0.1.0 (pid %d, port %s)", os.Getpid(), d.config.Port),
		Attrs: map[string]string{
			"pid":  fmt.Sprintf("%d", os.Getpid()),
			"port": d.config.Port,
		},
	})

	sysSvc := &systemServer{startedAt: time.Now(), sessions: d.sessions, daemon: d}
	dotSvc := &dotfilesServer{sessions: d.sessions}
	bgTasks := newBackgroundTaskManager()
	bgTasks.SetDiagEngine(d.diag)
	sudoTimeout := d.config.SudoTimeout
	if sudoTimeout <= 0 {
		sudoTimeout = 15 * time.Minute
	}
	execSvc := &execServer{sessions: d.sessions, bgTasks: bgTasks, diag: d.diag, sudoTimeout: sudoTimeout}
	keySvc := &keyServer{sessions: d.sessions}
	cfgSvc := &configServer{sessions: d.sessions}
	sessionSvc := newSessionServer(d.sessions)
	scriptSvc := newScriptServer(d.sessions, d.scripts)
	scriptSvc.SetDiagEngine(d.diag)
	diagPostSvc := newDiagnosticsPostServer(d.diag)
	diagQuerySvc := newDiagnosticsQueryServer(d.diag)

	// Wire diagnostics engine into session store.
	d.sessions.SetDiagEngine(d.diag)

	// Build the mux with all service handlers BEFORE starting plugins.
	// This ensures PluginRegistryService is available when plugins try
	// to discover each other during their initialization.
	mux := http.NewServeMux()

	// —— Services accessible without auth (CLI, MCP, agent) ——
	{
		p, h := dotfilesdv1connect.NewSystemServiceHandler(sysSvc)
		mux.Handle(p, h)
	}
	{
		p, h := dotfilesdv1connect.NewDotfilesServiceHandler(dotSvc)
		mux.Handle(p, h)
	}
	{
		p, h := dotfilesdv1connect.NewConfigServiceHandler(cfgSvc)
		mux.Handle(p, h)
	}
	{
		p, h := dotfilesdv1connect.NewSessionServiceHandler(sessionSvc)
		mux.Handle(p, h)
	}
	{
		p, h := dotfilesdv1connect.NewScriptServiceHandler(scriptSvc)
		mux.Handle(p, h)
	}
	{
		p, h := dotfilesdv1connect.NewDiagnosticsPostServiceHandler(diagPostSvc)
		mux.Handle(p, h)
	}
	{
		p, h := dotfilesdv1connect.NewDiagnosticsQueryServiceHandler(diagQuerySvc)
		mux.Handle(p, h)
	}
	{
		p, h := dotfilesdv1connect.NewExecServiceHandler(execSvc)
		mux.Handle(p, h)
	}
	{
		p, h := dotfilesdv1connect.NewKeyServiceHandler(keySvc)
		mux.Handle(p, h)
	}

	// —— Services accessible with X-Dotfiles-Context-Token auth ——
	auth := d.tokenAuthMiddleware
	{
		p, h := dotfilesdv1connect.NewFeedbackServiceHandler(newFeedbackServer(d.sessions))
		mux.Handle(p, auth(h))
	}
	{
		p, h := dotfilesdv1connect.NewIOServiceHandler(newIOServer(d))
		mux.Handle(p, auth(h))
	}

	// SecretsService — accessible with auth so only plugins with the
	// context token can request secrets.
	{
		secretsPath := expandHome(secretsFilePath)
		secretsData, err := loadSecretsFile(secretsPath)
		if err != nil {
			slog.Warn("failed to load secrets file", "path", secretsPath, "error", err)
			secretsData = make(map[string]map[string]string)
		}
		p, h := dotfilesdv1connect.NewSecretsServiceHandler(newSecretsServer(d.sessions, secretsData))
		mux.Handle(p, auth(h))
	}

	// PluginRegistryService is accessible WITHOUT auth — both plugins and CLI/MCP
	// use it to discover plugin metadata. It is a read-only query service.
	{
		p, h := dotfilesdv1connect.NewPluginRegistryServiceHandler(newRegistryServer(d.sessions, d))
		mux.Handle(p, h)
	}

	// PluginExecutorService proxies calls to plugins and streams output back.
	// Accessible without auth so CLI/MCP can invoke plugin RPCs.
	{
		p, h := dotfilesdv1connect.NewPluginExecutorServiceHandler(newExecutorServer(d))
		mux.Handle(p, h)
	}

	rpcAddr := fmt.Sprintf("127.0.0.1:%s", d.config.Port)
	d.server = &http.Server{
		Addr:    rpcAddr,
		Handler: h2c.NewHandler(mux, &http2.Server{}),
	}

	// Start the HTTP server in a goroutine BEFORE loading plugins so that
	// PluginRegistryService is immediately available for cross-plugin discovery.
	errCh := make(chan error, 1)
	go func() {
		slog.Info("serving connect rpc", "addr", rpcAddr)
		if err := d.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Small delay to let the server socket accept connections.
	time.Sleep(50 * time.Millisecond)

	// Initialize plugin system (discovers, builds, launches plugins).
	if err := d.InitPlugins(); err != nil {
		slog.Warn("plugin init (continuing)", "error", err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sig:
		slog.Info("shutting down")
		d.diag.PushEvent(diagnostics.Event{
			Type:      diagnostics.EventDaemonStop,
			Resource:  "daemon",
			Timestamp: time.Now(),
			Message:   "shutdown",
		})
		if d.pluginMgr != nil {
			d.ShutdownPlugins()
		}
		return d.server.Close()
	case err := <-errCh:
		return err
	}
}

// tokenAuthHeader is the header that plugins MUST include on daemon-facing
// RPCs. The value must match the token generated at daemon startup.
const tokenAuthHeader = "X-Dotfiles-Context-Token"

// tokenAuthMiddleware wraps an http.Handler to require a valid plugin token.
// If the token is missing or wrong, it returns 401.
func (d *Daemon) tokenAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if d.pluginToken == "" {
			http.Error(w, `{"code":"unauthenticated","message":"daemon has no plugin token"}`, http.StatusInternalServerError)
			return
		}
		token := r.Header.Get(tokenAuthHeader)
		if token == "" {
			http.Error(w, `{"code":"unauthenticated","message":"missing X-Dotfiles-Context-Token header"}`, http.StatusUnauthorized)
			return
		}
		if token != d.pluginToken {
			http.Error(w, `{"code":"unauthenticated","message":"invalid plugin token"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// restartDaemon is a function variable that can be replaced in tests.
var restartDaemon = gracefulRestart

func gracefulRestart(delay time.Duration) {
	slog.Warn("daemon restart requested, starting new instance", "delay_ms", delay.Milliseconds())
	time.Sleep(delay)

	binary, err := os.Executable()
	if err != nil {
		slog.Error("restart: cannot find binary", "error", err)
		os.Exit(1)
	}

	cmd := exec.Command(binary, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		slog.Error("restart: failed to start new instance", "error", err)
		os.Exit(1)
	}

	slog.Info("new instance started, exiting old instance")
	os.Exit(1)
}
