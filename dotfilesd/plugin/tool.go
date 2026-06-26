package plugin

// Tool is the interface that every plugin tool must implement.
//
// The daemon calls GetDescriptor to enumerate all tools from a plugin, then
// uses the metadata to auto-generate CLI commands and MCP tool definitions.
// When a user or agent invokes a tool, the daemon calls CallTool with the
// tool name and arguments.
type Tool interface {
	// Name returns the tool name (e.g. "forecast"). Must be unique within
	// the plugin.
	Name() string

	// Description returns a human-readable description of what the tool does.
	Description() string

	// Input returns the schema describing expected input parameters.
	Input() ToolInputSchema

	// CLI returns CLI generation hints for this tool.
	CLI() CLIHints

	// Run executes the tool with the given arguments.
	//
	// Arguments are a map of parameter names to their JSON-encoded string
	// values. The tool is responsible for deserializing them as needed.
	//
	// Returns:
	//   - text: human-readable text output (stdout-style)
	//   - isError: whether the tool encountered an error
	//   - structuredData: optional JSON string with structured result data
	Run(ctx Context, args map[string]string) (text string, isError bool, structuredData string)
}

// SimpleFuncTool is a Tool implementation backed by a simple function.
// Most plugin authors will use NewTool to create these.
type SimpleFuncTool struct {
	name        string
	description string
	input       ToolInputSchema
	cli         CLIHints
	fn          func(ctx Context, args map[string]string) (string, bool, string)
}

var _ Tool = (*SimpleFuncTool)(nil)

// Name returns the tool name.
func (t *SimpleFuncTool) Name() string { return t.name }

// Description returns the tool description.
func (t *SimpleFuncTool) Description() string { return t.description }

// Input returns the tool's input schema.
func (t *SimpleFuncTool) Input() ToolInputSchema { return t.input }

// CLI returns the tool's CLI hints.
func (t *SimpleFuncTool) CLI() CLIHints { return t.cli }

// Run executes the tool function.
func (t *SimpleFuncTool) Run(ctx Context, args map[string]string) (string, bool, string) {
	return t.fn(ctx, args)
}

// NewTool creates a new Tool backed by the given function.
//
// Example:
//
//	plugin.NewTool("greet", "Greet someone",
//	    plugin.ToolInputSchema{
//	        Properties: map[string]plugin.PropertySchema{
//	            "name": {Type: "string", Description: "Name to greet"},
//	        },
//	        Required: []string{"name"},
//	    },
//	    plugin.CLIHints{CommandPath: "greet"},
//	    func(ctx plugin.Context, args map[string]string) (string, bool, string) {
//	        name := args["name"]
//	        if name == "" {
//	            name = "World"
//	        }
//	        return fmt.Sprintf("Hello, %s!", name), false, ""
//	    },
//	)
func NewTool(name, description string, input ToolInputSchema, cli CLIHints, fn func(ctx Context, args map[string]string) (string, bool, string)) Tool {
	return &SimpleFuncTool{
		name:        name,
		description: description,
		input:       input,
		cli:         cli,
		fn:          fn,
	}
}
