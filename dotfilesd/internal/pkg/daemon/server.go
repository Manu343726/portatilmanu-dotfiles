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

	"dotfilesd/internal/pkg/logging"
	"dotfilesd/internal/pkg/plugin"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"
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
}

type Daemon struct {
	config   Config
	server   *http.Server
	sessions *SessionStore
	scripts  *ScriptRegistry

	// Logging system.
	logger *logging.Manager

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

	sysSvc := &systemServer{startedAt: time.Now(), sessions: d.sessions, daemon: d}
	dotSvc := &dotfilesServer{sessions: d.sessions}
	execSvc := &execServer{sessions: d.sessions, bgTasks: newBackgroundTaskManager()}
	cfgSvc := &configServer{sessions: d.sessions}
	sessionSvc := newSessionServer(d.sessions)
	scriptSvc := newScriptServer(d.sessions, d.scripts)

	// Initialize plugin system.
	if err := d.InitPlugins(); err != nil {
		slog.Warn("plugin init (continuing)", "error", err)
	}

	mux := http.NewServeMux()
	{
		p, h := dotfilesdv1connect.NewSystemServiceHandler(sysSvc)
		mux.Handle(p, h)
	}
	{
		p, h := dotfilesdv1connect.NewDotfilesServiceHandler(dotSvc)
		mux.Handle(p, h)
	}
	{
		p, h := dotfilesdv1connect.NewExecServiceHandler(execSvc)
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
		p, h := dotfilesdv1connect.NewFeedbackServiceHandler(newFeedbackServer(d.sessions))
		mux.Handle(p, h)
	}
	{
		p, h := dotfilesdv1connect.NewLogServiceHandler(newLogServer(d))
		mux.Handle(p, h)
	}
	{
		p, h := dotfilesdv1connect.NewPluginRegistryServiceHandler(newRegistryServer(d.sessions, d))
		mux.Handle(p, h)
	}

	rpcAddr := fmt.Sprintf("127.0.0.1:%s", d.config.Port)
	d.server = &http.Server{
		Addr:    rpcAddr,
		Handler: mux,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("serving connect rpc", "addr", rpcAddr)
		if err := d.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sig:
		slog.Info("shutting down")
		if d.pluginMgr != nil {
			d.ShutdownPlugins()
		}
		return d.server.Close()
	case err := <-errCh:
		return err
	}
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
