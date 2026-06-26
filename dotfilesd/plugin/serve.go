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
//	            plugin.ToolInputSchema{
//	                Properties: map[string]plugin.PropertySchema{
//	                    "name": {Type: "string", Description: "Name to greet"},
//	                },
//	                Required: []string{"name"},
//	            },
//	            plugin.CLIHints{CommandPath: "greet"},
//	            func(ctx plugin.Context, args map[string]string) (string, bool, string) {
//	                return "Hello, " + args["name"] + "!", false, ""
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
func Serve(name, displayName, version, description string, tools ...Tool) {
	ctxURL := os.Getenv("EXECUTION_CONTEXT_URL")
	ctxToken := os.Getenv("EXECUTION_CONTEXT_TOKEN")
	sessionID := os.Getenv("SESSION_ID")

	if ctxURL == "" {
		fmt.Fprintf(os.Stderr, "plugin: EXECUTION_CONTEXT_URL not set\n")
		os.Exit(1)
	}

	// Build the extension server with tool handlers.
	mux := http.NewServeMux()

	svc := &extensionSvcServer{
		name:        name,
		displayName: displayName,
		version:     version,
		description: description,
		tools:       tools,
		ctxClient:   newContextClient(ctxURL, ctxToken, sessionID),
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

	// Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "plugin: serve error: %v\n", err)
		}
	}()

	<-sigCh
	_ = srv.Shutdown(context.Background())
}

// extensionSvcServer implements the ExtensionService Connect RPC handlers.
type extensionSvcServer struct {
	name, displayName, version, description string
	tools                                    []Tool
	ctxClient                                Context
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
			Input:       toProtoInput(t.Input()),
			Cli:         toProtoCLI(t.CLI()),
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
) (*connect.Response[dotfilesdv1.CallToolResponse], error) {
	for _, t := range s.tools {
		if t.Name() == req.Msg.ToolName {
			text, isErr, structured := t.Run(s.ctxClient, req.Msg.Arguments)
			return connect.NewResponse(&dotfilesdv1.CallToolResponse{
				Text:           text,
				IsError:        isErr,
				StructuredData: structured,
			}), nil
		}
	}
	return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("tool %q not found", req.Msg.ToolName))
}
