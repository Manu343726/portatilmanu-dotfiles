// Package plugin is the public SDK for writing dotfilesd extensions (plugins).
//
// A plugin is a standalone Go program that uses this SDK to:
//   - Expose tools (commands) that the daemon discovers via DescriptorService
//   - Expose type-safe Connect RPC services for plugin-to-plugin calls
//   - Use the plugin Context to interact with the host system
//
// Usage (simple tool-based plugin):
//
//	func main() {
//	    plugin.Serve(plugin.Config{
//	        Name:        "weather",
//	        DisplayName: "Weather Plugin",
//	        Version:     "1.0.0",
//	        Description: "Fetches weather data from wttr.in",
//	        Tools: []plugin.Tool{
//	            plugin.NewTool("forecast", "Get weather forecast",
//	                &dotfilesdv1.ToolInputSchema{...},
//	                nil,
//	                func(ctx plugin.Context, args map[string]string) error {
//	                    result, _ := ctx.Exec("curl wttr.in")
//	                    fmt.Fprintln(ctx.Stdout(), result.Stdout)
//	                    return nil
//	                },
//	            ),
//	        },
//	    })
//	}
//
// Usage (with custom RPC service for type-safe plugin-to-plugin calls):
//
//	func main() {
//	    weatherSvc := &myWeatherService{}
//	    path, handler := weatherconnect.NewWeatherServiceHandler(weatherSvc)
//
//	    plugin.Serve(plugin.Config{
//	        Name:        "weather",
//	        DisplayName: "Weather Plugin",
//	        Version:     "1.0.0",
//	        Description: "Fetches weather data from wttr.in",
//	        Services: []plugin.ServiceRegistration{
//	            {Path: path, Handler: handler,
//	             Info: &dotfilesdv1.ServiceInfo{
//	                 Name: "dotfilesd.v1.WeatherService",
//	                 Description: "Type-safe weather API for other plugins",
//	                 PluginAccessible: true,
//	             }},
//	        },
//	    })
//	}
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

// Config configures a plugin server.
type Config struct {
	// Basic plugin metadata.
	Name, DisplayName, Version, Description string

	// Legacy tool-based API (wrapped into ExtensionService.CallTool).
	Tools []Tool

	// Background worker (optional). Runs after the server starts.
	Background func(ctx Context, stop <-chan struct{})

	// Type-safe Connect RPC services this plugin exposes.
	// Other plugins can call these after discovering them via the registry.
	Services []ServiceRegistration
}

// ServiceRegistration registers a custom Connect service on the plugin.
type ServiceRegistration struct {
	Path    string                    // HTTP path prefix (e.g. "/dotfilesd.v1.WeatherService/")
	Handler http.Handler
	Info    *dotfilesdv1.ServiceInfo  // metadata for the registry
}

// ServePlugin starts the plugin server with the given config.
//
// The server automatically exposes:
//   - DescriptorService (daemon-known protocol for discovery)
//   - ExtensionService (backward compat for legacy tool dispatch)
//   - All custom services from Config.Services
//
// It performs the handshake with the daemon and blocks until SIGTERM/SIGINT.
// This is the new-style entry point. Use Serve() for simple tool-only plugins.
func ServePlugin(cfg Config) {
	serve(cfg)
}

// Serve is the traditional convenience wrapper for simple tool plugins.
// It calls ServePlugin with a Config built from the positional arguments.
func Serve(name, displayName, version, description string, tools ...Tool) {
	serve(Config{
		Name:        name,
		DisplayName: displayName,
		Version:     version,
		Description: description,
		Tools:       tools,
	})
}

// ServeWithBackground is the traditional wrapper for plugins with a background
// worker. It calls ServePlugin with a Config built from the positional args.
func ServeWithBackground(
	name, displayName, version, description string,
	background func(ctx Context, stop <-chan struct{}),
	tools ...Tool,
) {
	serve(Config{
		Name:        name,
		DisplayName: displayName,
		Version:     version,
		Description: description,
		Tools:       tools,
		Background:  background,
	})
}

// serve is the shared implementation.
func serve(cfg Config) {
	ctxURL := os.Getenv("EXECUTION_CONTEXT_URL")
	ctxToken := os.Getenv("EXECUTION_CONTEXT_TOKEN")
	sessionID := os.Getenv("SESSION_ID")

	if ctxURL == "" {
		fmt.Fprintf(os.Stderr, "plugin: EXECUTION_CONTEXT_URL not set\n")
		os.Exit(1)
	}

	ctxClient := newContextClient(ctxURL, ctxToken, sessionID, cfg.Name)

	mux := http.NewServeMux()

	// 1. DescriptorService — daemon-known protocol, always served.
	descSvc := &descriptorServiceServer{
		name:        cfg.Name,
		displayName: cfg.DisplayName,
		version:     cfg.Version,
		description: cfg.Description,
		tools:       cfg.Tools,
		services:    cfg.Services,
	}
	dPath, dHandler := dotfilesdv1connect.NewDescriptorServiceHandler(descSvc)
	mux.Handle(dPath, dHandler)

	// 2. Legacy ExtensionService — backward compat tool dispatch.
	if len(cfg.Tools) > 0 {
		extSvc := &extensionSvcServer{
			name:      cfg.Name,
			tools:     cfg.Tools,
			ctxClient: ctxClient,
		}
		ePath, eHandler := dotfilesdv1connect.NewExtensionServiceHandler(extSvc)
		mux.Handle(ePath, eHandler)
	}

	// 3. Custom services (type-safe plugin-to-plugin RPC).
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
		"protocol":   "dotfilesd-extension-v1",
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

// descriptorServiceServer implements the DescriptorService.
type descriptorServiceServer struct {
	name, displayName, version, description string
	tools                                   []Tool
	services                                []ServiceRegistration
}

func (s *descriptorServiceServer) GetDescriptor(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.GetDescriptorRequest],
) (*connect.Response[dotfilesdv1.GetDescriptorResponse], error) {
	toolsPB := make([]*dotfilesdv1.ToolDescriptor, len(s.tools))
	for i, t := range s.tools {
		toolsPB[i] = &dotfilesdv1.ToolDescriptor{
			Name:        t.Name(),
			Description: t.Description(),
			Input:       t.Input(),
			Cli:         t.CLI(),
		}
	}

	return connect.NewResponse(&dotfilesdv1.GetDescriptorResponse{
		Descriptor_: &dotfilesdv1.ExtensionDescriptor{
			Name:        s.name,
			DisplayName: s.displayName,
			Version:     s.version,
			Description: s.description,
			Tools:       toolsPB,
		},
	}), nil
}

func (s *descriptorServiceServer) ListServices(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.ListServicesRequest],
) (*connect.Response[dotfilesdv1.ListServicesResponse], error) {
	svcInfos := make([]*dotfilesdv1.ServiceInfo, len(s.services))
	for i, svc := range s.services {
		svcInfos[i] = svc.Info
	}
	return connect.NewResponse(&dotfilesdv1.ListServicesResponse{
		Services: svcInfos,
	}), nil
}

// extensionSvcServer implements the legacy ExtensionService for backward compat.
type extensionSvcServer struct {
	name      string
	tools     []Tool
	ctxClient *contextClient
}

func (s *extensionSvcServer) GetDescriptor(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.GetDescriptorRequest],
) (*connect.Response[dotfilesdv1.GetDescriptorResponse], error) {
	toolsPB := make([]*dotfilesdv1.ToolDescriptor, len(s.tools))
	for i, t := range s.tools {
		toolsPB[i] = &dotfilesdv1.ToolDescriptor{
			Name:        t.Name(),
			Description: t.Description(),
			Input:       t.Input(),
			Cli:         t.CLI(),
		}
	}

	return connect.NewResponse(&dotfilesdv1.GetDescriptorResponse{
		Descriptor_: &dotfilesdv1.ExtensionDescriptor{
			Name:  s.name,
			Tools: toolsPB,
		},
	}), nil
}

func (s *extensionSvcServer) CallTool(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.CallToolRequest],
	stream *connect.ServerStream[dotfilesdv1.CallToolResponse],
) error {
	for _, t := range s.tools {
		if t.Name() == req.Msg.ToolName {
			stdout := &streamWriter{stream: stream}
			stderr := &streamWriter{stream: stream, isStderr: true}

			toolCtx := &streamingContext{
				Context: s.ctxClient,
				client:  s.ctxClient,
				stdout:  stdout,
				stderr:  stderr,
			}

			err := t.Run(toolCtx, req.Msg.Arguments)
			doneMsg := &dotfilesdv1.CallToolResponse{Done: true}
			if err != nil {
				doneMsg.ErrorMessage = err.Error()
			}
			return stream.Send(doneMsg)
		}
	}
	return connect.NewError(connect.CodeNotFound, fmt.Errorf("tool %q not found", req.Msg.ToolName))
}

// streamWriter implements io.Writer by sending each Write as a chunk.
type streamWriter struct {
	stream   *connect.ServerStream[dotfilesdv1.CallToolResponse]
	isStderr bool
}

func (w *streamWriter) Write(p []byte) (int, error) {
	msg := &dotfilesdv1.CallToolResponse{}
	if w.isStderr {
		msg.StderrChunk = p
	} else {
		msg.StdoutChunk = p
	}
	if err := w.stream.Send(msg); err != nil {
		return 0, err
	}
	return len(p), nil
}
