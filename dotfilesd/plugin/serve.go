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

	dotfilesdv1connect "dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"connectrpc.com/grpcreflect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
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
// The server mounts grpcreflect handlers (for daemon discovery),
// a default DocumentationService, and all custom services from
// Config.Services. It performs the handshake with the daemon and
// blocks until SIGTERM/SIGINT.
func Serve(cfg Config) {
	ctxURL := os.Getenv("EXECUTION_CONTEXT_URL")
	ctxToken := os.Getenv("EXECUTION_CONTEXT_TOKEN")
	sessionID := os.Getenv("SESSION_ID")

	if ctxURL == "" {
		fmt.Fprintf(os.Stderr, "plugin: EXECUTION_CONTEXT_URL not set\n")
		os.Exit(1)
	}

	ctxClient := newContextClient(ctxURL, ctxToken, sessionID, cfg.Name, "")

	mux := http.NewServeMux()

	// Collect all service names for the grpcreflect reflector.
	names := []string{}
	hasDocsSvc := false
	for _, svc := range cfg.Services {
		names = append(names, svc.Name)
		if svc.Name == "dotfilesd.v1.DocumentationService" {
			hasDocsSvc = true
		}
	}
	if !hasDocsSvc {
		names = append(names, "dotfilesd.v1.DocumentationService")
	}

	// Mount grpcreflect handlers — daemon discovers ALL services via this.
	reflector := grpcreflect.NewStaticReflector(names...)
	mux.Handle(grpcreflect.NewHandlerV1(reflector))
	mux.Handle(grpcreflect.NewHandlerV1Alpha(reflector))

	// Mount default DocumentationService (SKIP if plugin provides its own).
	if !hasDocsSvc {
		docsSvc := &documentationServiceServer{
			name:        cfg.Name,
			displayName: cfg.DisplayName,
			version:     cfg.Version,
			description: cfg.Description,
			services:    cfg.Services,
		}
		docsPath, docsHandler := dotfilesdv1connect.NewDocumentationServiceHandler(docsSvc)
		mux.Handle(docsPath, docsHandler)
	}

	// Mount custom services (type-safe plugin-to-plugin RPC).
	for _, svc := range cfg.Services {
		mux.Handle(svc.Path, svc.Handler)
	}

	// Context injection middleware: wraps every request so handlers can
	// call plugin.ExtractContext(ctx) to get daemon access.
	// Reads X-Dotfiles-Render-Output and X-Client-ID from incoming requests
	// and propagates them into the plugin Context for stream multiplexing.
	ctxWrappedMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientID := r.Header.Get("X-Client-ID")
		ro := r.Header.Get("X-Dotfiles-Render-Output") == "true"

		var ctx Context = ctxClient
		if clientID != "" || ro {
			// Create a per-request context with the client-specific settings.
			c := ctxClient
			if clientID != "" {
				c.clientID = clientID
			}
			if ro {
				c.renderOutput = true
			}
			ctx = c
		}
		r = r.WithContext(WithContext(r.Context(), ctx))
		mux.ServeHTTP(w, r)
	})

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
		"protocol":     "dotfilesd-plugin-v1",
		"url":          pluginURL,
		"session_id":   sessionID,
		"name":         cfg.Name,
		"display_name": cfg.DisplayName,
		"version":      cfg.Version,
		"description":  cfg.Description,
	}
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(handshake); err != nil {
		fmt.Fprintf(os.Stderr, "plugin: handshake encode failed: %v\n", err)
		os.Exit(1)
	}

	// Signal handler for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	srv := &http.Server{Handler: h2c.NewHandler(ctxWrappedMux, &http2.Server{})}
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
