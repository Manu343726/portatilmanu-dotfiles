// Package plugin is the public SDK for writing dotfilesd extensions (plugins).
//
// A plugin is a standalone Go program that uses this SDK to:
//   - Expose tools (commands) that the daemon can discover via the Extension API
//   - Use the Execution Context to interact with the host system (run commands,
//     prompt the user, etc.) through the daemon's controlled interface
//
// Usage (in a plugin's main.go):
//
//	func main() {
//	    plugin.Serve("hello", "Hello World", "1.0.0", "A sample plugin",
//	        plugin.NewTool("greet", "Greet someone",
//	            &dotfilesdv1.ToolInputSchema{
//	                Properties: map[string]*dotfilesdv1.PropertySchema{
//	                    "name": {Type: "string", Description: "Name to greet"},
//	                },
//	                Required: []string{"name"},
//	            },
//	            &dotfilesdv1.CLIHints{CommandPath: "greet"},
//	            func(ctx plugin.Context, args map[string]string) error {
//	                fmt.Fprintf(ctx.Stdout(), "Hello, %s!", args["name"])
//	                return nil
//	            },
//	        ),
//	    )
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

// Serve starts the plugin's Connect RPC server and performs the handshake
// with the daemon. It blocks until the process receives SIGTERM or SIGINT.
//
// The daemon launches the plugin binary with these environment variables:
//   - EXECUTION_CONTEXT_URL — base URL of the daemon's Execution Context service
//   - EXECUTION_CONTEXT_TOKEN — secret token for authenticating context requests
//   - SESSION_ID — plugin's session ID for grouping requests
//
// Serve writes a JSON handshake to stdout:
//
//	{"protocol":"dotfilesd-extension-v1","url":"http://127.0.0.1:PORT","session_id":"..."}
//
// The daemon reads this line to learn the plugin's URL and then calls
// GetDescriptor to discover the plugin's capabilities.
//
// Tools execute in their own goroutine when the daemon calls CallTool. Each
// tool receives a plugin.Context that can be used to run system commands,
// prompt the user, or stream output through the daemon.
func Serve(name, displayName, version, description string, tools ...Tool) {
	serve(name, displayName, version, description, tools, nil)
}

// ServeWithBackground is like Serve but also starts a background goroutine
// with access to the execution context. This is useful for plugins that
// need to perform continuous work (polling, monitoring, aggregation) using
// the daemon's exec context throughout the plugin's lifetime.
//
// The background function receives the plugin's Context and a stop channel.
// It should return when the stop channel is closed (the daemon is shutting
// down or the plugin was killed). The background goroutine is started after
// the plugin server is fully initialized and the handshake has completed.
//
// Usage:
//
//	func main() {
//	    plugin.ServeWithBackground("ram", "RAM Monitor", "1.0.0",
//	        "Live RAM monitoring",
//	        func(ctx plugin.Context, stop <-chan struct{}) {
//	            // background loop: collect, poll, etc.
//	            for {
//	                select {
//	                case <-stop:
//	                    return
//	                case <-time.After(5 * time.Second):
//	                    // use ctx.Exec(...), etc.
//	                }
//	            }
//	        },
//	        plugin.NewTool("current", "Get current RAM usage", ...),
//	        plugin.NewTool("history", "Show RAM usage history", ...),
//	    )
//	}
func ServeWithBackground(
	name, displayName, version, description string,
	background func(ctx Context, stop <-chan struct{}),
	tools ...Tool,
) {
	serve(name, displayName, version, description, tools, background)
}

// serve is the shared implementation for Serve and ServeWithBackground.
func serve(
	name, displayName, version, description string,
	tools []Tool,
	background func(ctx Context, stop <-chan struct{}),
) {
	ctxURL := os.Getenv("EXECUTION_CONTEXT_URL")
	ctxToken := os.Getenv("EXECUTION_CONTEXT_TOKEN")
	sessionID := os.Getenv("SESSION_ID")

	if ctxURL == "" {
		fmt.Fprintf(os.Stderr, "plugin: EXECUTION_CONTEXT_URL not set\n")
		os.Exit(1)
	}

	// Build the extension server with tool handlers.
	ctxClient := newContextClient(ctxURL, ctxToken, sessionID)

	mux := http.NewServeMux()

	svc := &extensionSvcServer{
		name:        name,
		displayName: displayName,
		version:     version,
		description: description,
		tools:       tools,
		ctxClient:   ctxClient,
	}

	path, handler := dotfilesdv1connect.NewExtensionServiceHandler(svc)
	mux.Handle(path, handler)

	// Listen on a random available port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "plugin: failed to listen: %v\n", err)
		os.Exit(1)
	}

	addr := listener.Addr().(*net.TCPAddr)

	// Write handshake JSON to stdout so the daemon can discover us.
	handshake := map[string]string{
		"protocol":   "dotfilesd-extension-v1",
		"url":        fmt.Sprintf("http://127.0.0.1:%d", addr.Port),
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

	// Start the background worker if provided.
	if background != nil {
		stopCh := make(chan struct{})
		go background(ctxClient, stopCh)
		<-sigCh
		close(stopCh) // signal the background worker to stop
	} else {
		<-sigCh
	}
	_ = srv.Shutdown(context.Background())
}

// extensionSvcServer implements the ExtensionService Connect RPC handlers.
type extensionSvcServer struct {
	name, displayName, version, description string
	tools                                   []Tool
	ctxClient                               Context
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
			Name:        s.name,
			DisplayName: s.displayName,
			Version:     s.version,
			Description: s.description,
			Tools:       toolsPB,
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
			// Create stdout/stderr writers that tunnel output via the RPC stream.
			stdout := &streamWriter{stream: stream}
			stderr := &streamWriter{stream: stream, isStderr: true}

			// Wrap context with streaming writers so tool output is sent
			// back to the caller in real time.
			toolCtx := &streamingContext{
				Context: s.ctxClient,
				stdout:  stdout,
				stderr:  stderr,
			}

			// Run the tool. It writes to ctx.Stdout()/Stderr() and may
			// return an error.
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

// streamWriter implements io.Writer by sending each Write call as a chunk
// on the Connect server stream.
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
