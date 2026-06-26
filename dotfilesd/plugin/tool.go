package plugin

import dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

// Tool is the interface that every plugin tool must implement.
//
// The daemon calls GetDescriptor to enumerate all tools from a plugin, then
// uses the metadata to auto-generate CLI commands and MCP tool definitions.
// When a user or agent invokes a tool, the daemon calls CallTool with the
// tool name and arguments.
//
// Tools write their output to ctx.Stdout() and ctx.Stderr() during Run()
// (streamed back in real time via RPC). Run returns a Go error: nil for
// success, non-nil for failure.
type Tool interface {
	// Name returns the tool name (e.g. "forecast"). Must be unique within
	// the plugin.
	Name() string

	// Description returns a human-readable description of what the tool does.
	Description() string

	// Input returns the schema describing expected input parameters.
	Input() *dotfilesdv1.ToolInputSchema

	// CLI returns CLI generation hints for this tool.
	CLI() *dotfilesdv1.CLIHints

	// Run executes the tool with the given arguments.
	//
	// Arguments are a map of parameter names to their JSON-encoded string
	// values. The tool is responsible for deserializing them as needed.
	//
	// Write output to ctx.Stdout() / ctx.Stderr() as the tool executes — it
	// is streamed back to the caller in real time via RPC streaming.
	// Return nil for success, or an error describing the failure.
	Run(ctx Context, args map[string]string) error
}

// SimpleFuncTool is a Tool implementation backed by a simple function.
// Most plugin authors will use NewTool to create these.
type SimpleFuncTool struct {
	name        string
	description string
	input       *dotfilesdv1.ToolInputSchema
	cli         *dotfilesdv1.CLIHints
	fn          func(ctx Context, args map[string]string) error
}

var _ Tool = (*SimpleFuncTool)(nil)

// Name returns the tool name.
func (t *SimpleFuncTool) Name() string { return t.name }

// Description returns the tool description.
func (t *SimpleFuncTool) Description() string { return t.description }

// Input returns the tool's input schema.
func (t *SimpleFuncTool) Input() *dotfilesdv1.ToolInputSchema { return t.input }

// CLI returns the tool's CLI hints.
func (t *SimpleFuncTool) CLI() *dotfilesdv1.CLIHints { return t.cli }

// Run executes the tool function.
func (t *SimpleFuncTool) Run(ctx Context, args map[string]string) error {
	return t.fn(ctx, args)
}

// NewTool creates a new Tool backed by the given function.
//
// Example:
//
//	plugin.NewTool("greet", "Greet someone",
//	    &dotfilesdv1.ToolInputSchema{
//	        Properties: map[string]*dotfilesdv1.PropertySchema{
//	            "name": {Type: "string", Description: "Name to greet"},
//	        },
//	        Required: []string{"name"},
//	    },
//	    &dotfilesdv1.CLIHints{CommandPath: "greet"},
//	    func(ctx plugin.Context, args map[string]string) error {
//	        name := args["name"]
//	        if name == "" {
//	            name = "World"
//	        }
//	        fmt.Fprintf(ctx.Stdout(), "Hello, %s!", name)
//	        return nil
//	    },
//	)
func NewTool(name, description string, input *dotfilesdv1.ToolInputSchema, cli *dotfilesdv1.CLIHints, fn func(ctx Context, args map[string]string) error) Tool {
	return &SimpleFuncTool{
		name:        name,
		description: description,
		input:       input,
		cli:         cli,
		fn:          fn,
	}
}
