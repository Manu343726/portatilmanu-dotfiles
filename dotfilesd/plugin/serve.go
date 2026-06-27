package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

// Service is a Connect RPC service a plugin exposes.
// Plugins define their own protobuf services and register them here.
type Service struct {
	// Fully-qualified service name (e.g. "dotfilesd.v1.WeatherService").
	Name string
	// Human-readable description.
	Description string
	// HTTP path prefix (e.g. "/dotfilesd.v1.WeatherService/").
	Path string
	// Connect RPC handler.
	Handler http.Handler
	// Whether this service is accessible to other plugins.
	PluginAccessible bool
}

// Config configures a plugin.
type Config struct {
	// Plugin metadata.
	Name, DisplayName, Version, Description, Author string
	// Plugin type: "server" (long-lived) or "command" (ephemeral).
	Type string

	// Background worker (optional). Runs after the server starts.
	Background func(ctx Context, stop <-chan struct{})

	// Custom Connect RPC services this plugin exposes.
	// Other plugins can call these after discovering them via the registry.
	Services []Service
}

// Serve starts the plugin server.
//
// The server mounts all custom services from Config.Services and performs
// the handshake with the daemon, blocking until SIGTERM/SIGINT.
func Serve(cfg Config) {
	ctxURL := os.Getenv("EXECUTION_CONTEXT_URL")
	ctxToken := os.Getenv("EXECUTION_CONTEXT_TOKEN")
	sessionID := os.Getenv("SESSION_ID")

	if ctxURL == "" {
		fmt.Fprintf(os.Stderr, "plugin: EXECUTION_CONTEXT_URL not set\n")
		os.Exit(1)
	}

	ctxClient := newContextClient(ctxURL, ctxToken, sessionID, cfg.Name)

	mux := http.NewServeMux()
	ctxWrappedMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(WithContext(r.Context(), ctxClient))
		mux.ServeHTTP(w, r)
	})

	// Mount custom services (type-safe plugin-to-plugin RPC).
	for _, svc := range cfg.Services {
		mux.Handle(svc.Path, svc.Handler)
	}

	// Listen on a random available port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "plugin: failed to listen: %v\n", err)
		os.Exit(1)
	}

	addr := listener.Addr().(*net.TCPAddr)
	pluginURL := fmt.Sprintf("http://127.0.0.1:%d", addr.Port)

	// Write handshake JSON to stdout so the daemon can discover us.
	handshake := map[string]string{
		"protocol":   "dotfilesd-plugin-v1",
		"url":        pluginURL,
		"session_id": sessionID,
	}
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(handshake); err != nil {
		fmt.Fprintf(os.Stderr, "plugin: handshake encode failed: %v\n", err)
		os.Exit(1)
	}

	// Signal handler for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	srv := &http.Server{Handler: ctxWrappedMux}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "plugin: serve error: %v\n", err)
		}
	}()

	// Start background worker if provided.
	if cfg.Background != nil {
		stopCh := make(chan struct{})
		go cfg.Background(ctxClient, stopCh)
		<-sigCh
		close(stopCh)
	} else {
		<-sigCh
	}
	_ = srv.Shutdown(context.Background())
}
