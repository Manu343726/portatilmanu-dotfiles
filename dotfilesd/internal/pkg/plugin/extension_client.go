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

// ToProtoDescriptor converts an internal ExtensionDescriptor to the proto type.
func ToProtoDescriptor(d *ExtensionDescriptor) *dotfilesdv1.ExtensionDescriptor {
	if d == nil {
		return nil
	}
	tools := make([]*dotfilesdv1.ToolDescriptor, len(d.Tools))
	for i, t := range d.Tools {
		protoInput := &dotfilesdv1.ToolInputSchema{
			Type:       t.Input.Type,
			Properties: make(map[string]*dotfilesdv1.PropertySchema, len(t.Input.Properties)),
			Required:   t.Input.Required,
		}
		for k, v := range t.Input.Properties {
			protoInput.Properties[k] = &dotfilesdv1.PropertySchema{
				Type:        v.Type,
				Description: v.Description,
				Enum:        v.Enum,
				Default:     v.Default,
				HasDefault:  v.HasDefault,
			}
		}

		protoCLI := &dotfilesdv1.CLIHints{
			CommandPath: t.CLI.CommandPath,
			Category:    t.CLI.Category,
			FlagMapping: t.CLI.FlagMapping,
		}

		tools[i] = &dotfilesdv1.ToolDescriptor{
			Name:        t.Name,
			Description: t.Description,
			Input:       protoInput,
			Cli:         protoCLI,
		}
	}
	return &dotfilesdv1.ExtensionDescriptor{
		Name:        d.Name,
		DisplayName: d.DisplayName,
		Version:     d.Version,
		Description: d.Description,
		Tools:       tools,
	}
}

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
func (c *Client) GetDescriptor(ctx context.Context) (*ExtensionDescriptor, error) {
	slog.Debug("client GetDescriptor: calling plugin extension API")
	resp, err := c.client.GetDescriptor(ctx, connect.NewRequest(&dotfilesdv1.GetDescriptorRequest{}))
	if err != nil {
		slog.Debug("client GetDescriptor failed", "error", err)
		return nil, fmt.Errorf("get descriptor: %w", err)
	}
	slog.Debug("client GetDescriptor response received", "name", resp.Msg.Descriptor_)
	return fromProtoDescriptor(resp.Msg.Descriptor_), nil
}

// CallTool invokes a named tool on the plugin with the given arguments.
func (c *Client) CallTool(ctx context.Context, toolName string, args map[string]string) (text string, isError bool, structuredData string, err error) {
	slog.Debug("client CallTool", "tool", toolName, "args", args)
	req := connect.NewRequest(&dotfilesdv1.CallToolRequest{
		ToolName:  toolName,
		Arguments: args,
	})

	resp, err := c.client.CallTool(ctx, req)
	if err != nil {
		slog.Debug("client CallTool failed", "tool", toolName, "error", err)
		return "", false, "", fmt.Errorf("call tool %q: %w", toolName, err)
	}

	slog.Debug("client CallTool succeeded", "tool", toolName, "is_error", resp.Msg.IsError, "text_len", len(resp.Msg.Text))
	return resp.Msg.Text, resp.Msg.IsError, resp.Msg.StructuredData, nil
}

// fromProtoDescriptor converts a proto ExtensionDescriptor to the internal Go type.
func fromProtoDescriptor(pb *dotfilesdv1.ExtensionDescriptor) *ExtensionDescriptor {
	if pb == nil {
		return nil
	}

	tools := make([]ToolDescriptor, len(pb.Tools))
	for i, t := range pb.Tools {
		tools[i] = toolFromProto(t)
	}

	return &ExtensionDescriptor{
		Name:        pb.Name,
		DisplayName: pb.DisplayName,
		Version:     pb.Version,
		Description: pb.Description,
		Tools:       tools,
	}
}

func toolFromProto(t *dotfilesdv1.ToolDescriptor) ToolDescriptor {
	input := ToolInputSchema{}
	if t.Input != nil {
		input.Type = t.Input.Type
		input.Required = t.Input.Required
		input.Properties = make(map[string]PropertySchema, len(t.Input.Properties))
		for k, v := range t.Input.Properties {
			input.Properties[k] = PropertySchema{
				Type:        v.Type,
				Description: v.Description,
				Enum:        v.Enum,
				Default:     v.Default,
				HasDefault:  v.HasDefault,
			}
		}
	}

	cli := CLIHints{}
	if t.Cli != nil {
		cli.CommandPath = t.Cli.CommandPath
		cli.Category = t.Cli.Category
		cli.FlagMapping = t.Cli.FlagMapping
	}

	return ToolDescriptor{
		Name:        t.Name,
		Description: t.Description,
		Input:       input,
		CLI:         cli,
	}
}
