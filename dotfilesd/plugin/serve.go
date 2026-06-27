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

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"connectrpc.com/connect"
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
// The server automatically exposes:
//   - PluginBaseService (standard protocol for discovery)
//   - All custom services from Config.Services
//
// It performs the handshake with the daemon and blocks until SIGTERM/SIGINT.
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

	// 1. PluginBaseService — standard protocol, always served.
	baseSvc := &pluginBaseServiceServer{
		name:        cfg.Name,
		displayName: cfg.DisplayName,
		version:     cfg.Version,
		description: cfg.Description,
		author:      cfg.Author,
		pluginType:  cfg.Type,
		services:    cfg.Services,
	}
	bPath, bHandler := dotfilesdv1connect.NewPluginBaseServiceHandler(baseSvc)
	mux.Handle(bPath, bHandler)

	// 2. Custom services (type-safe plugin-to-plugin RPC).
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

	srv := &http.Server{Handler: mux}
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

// pluginBaseServiceServer implements PluginBaseService.
type pluginBaseServiceServer struct {
	name, displayName, version, description, author, pluginType string
	services                                                    []Service
}

func (s *pluginBaseServiceServer) GetInfo(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.GetInfoRequest],
) (*connect.Response[dotfilesdv1.GetInfoResponse], error) {
	return connect.NewResponse(&dotfilesdv1.GetInfoResponse{
		Name:        s.name,
		DisplayName: s.displayName,
		Version:     s.version,
		Description: s.description,
		Author:      s.author,
		Type:        s.pluginType,
	}), nil
}

func (s *pluginBaseServiceServer) ListServices(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.ListServicesRequest],
) (*connect.Response[dotfilesdv1.ListServicesResponse], error) {
	descs := make([]*dotfilesdv1.ServiceDescriptor, len(s.services))
	for i, svc := range s.services {
		descs[i] = &dotfilesdv1.ServiceDescriptor{
			Name:             svc.Name,
			Description:      svc.Description,
			PluginAccessible: svc.PluginAccessible,
		}
	}
	return connect.NewResponse(&dotfilesdv1.ListServicesResponse{
		Services: descs,
	}), nil
}

func (s *pluginBaseServiceServer) GetDocumentation(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.GetDocumentationRequest],
) (*connect.Response[dotfilesdv1.GetDocumentationResponse], error) {
	// Default: return not implemented. Plugins can override this.
	return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("documentation not implemented"))
}
