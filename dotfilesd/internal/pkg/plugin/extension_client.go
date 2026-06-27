package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"connectrpc.com/connect"
)

// Client is a Connect RPC client for calling a plugin's Extension API.
// It's used by the daemon to discover and invoke plugin tools.
type Client struct {
	client dotfilesdv1connect.ExtensionServiceClient
}

// NewClient creates a new Client connected to a plugin's Extension Service.
func NewClient(pluginURL string) *Client {
	return &Client{
		client: dotfilesdv1connect.NewExtensionServiceClient(&http.Client{}, pluginURL),
	}
}

// GetDescriptor retrieves the plugin's capabilities.
func (c *Client) GetDescriptor(ctx context.Context) (*dotfilesdv1.ExtensionDescriptor, error) {
	slog.Debug("client GetDescriptor: calling plugin extension API")
	resp, err := c.client.GetDescriptor(ctx, connect.NewRequest(&dotfilesdv1.GetDescriptorRequest{}))
	if err != nil {
		slog.Debug("client GetDescriptor failed", "error", err)
		return nil, fmt.Errorf("get descriptor: %w", err)
	}
	slog.Debug("client GetDescriptor response received", "name", resp.Msg.Descriptor_)
	return resp.Msg.Descriptor_, nil
}

// CallTool invokes a named tool on the plugin with the given arguments.
// Returns a server stream that the caller can iterate over to receive
// stdout/stderr chunks, followed by a final done message.
func (c *Client) CallTool(ctx context.Context, toolName string, args map[string]string) (*connect.ServerStreamForClient[dotfilesdv1.CallToolResponse], error) {
	slog.Debug("client CallTool", "tool", toolName, "args", args)
	req := connect.NewRequest(&dotfilesdv1.CallToolRequest{
		ToolName:  toolName,
		Arguments: args,
	})

	stream, err := c.client.CallTool(ctx, req)
	if err != nil {
		slog.Debug("client CallTool failed", "tool", toolName, "error", err)
		return nil, fmt.Errorf("call tool %q: %w", toolName, err)
	}

	slog.Debug("client CallTool stream opened", "tool", toolName)
	return stream, nil
}
