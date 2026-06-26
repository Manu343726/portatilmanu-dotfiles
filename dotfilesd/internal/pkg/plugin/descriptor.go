// Package plugin provides Go types that mirror the proto ExtensionDescriptor
// for use in the plugin manager and SDK without direct proto dependency at the
// SDK boundary. The top-level plugin/ package (public SDK) re-exports the types
// plugin authors need.
package plugin

// ToolDescriptor describes a single tool exposed by a plugin.
type ToolDescriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Input       ToolInputSchema `json:"input"`
	CLI         CLIHints        `json:"cli"`
}

// ToolInputSchema describes the expected input parameters for a tool.
type ToolInputSchema struct {
	Type       string                    `json:"type"` // "object"
	Properties map[string]PropertySchema `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

// PropertySchema describes a single input parameter.
type PropertySchema struct {
	Type        string   `json:"type"` // "string", "integer", "boolean", "number", "enum"
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Default     string   `json:"default,omitempty"`
	HasDefault  bool     `json:"has_default,omitempty"`
}

// CLIHints provides CLI generation hints for a tool.
type CLIHints struct {
	CommandPath string            `json:"command_path,omitempty"`
	Category    string            `json:"category,omitempty"`
	FlagMapping map[string]string `json:"flag_mapping,omitempty"`
}

// ExtensionDescriptor describes a plugin's capabilities.
type ExtensionDescriptor struct {
	Name        string          `json:"name"`
	DisplayName string          `json:"display_name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	Tools       []ToolDescriptor `json:"tools"`
}
