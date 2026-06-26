package plugin

import dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

// ToolInputSchema describes the expected input parameters for a tool.
// This mirrors the proto ToolInputSchema but uses plain Go types for
// a nicer authoring experience.
type ToolInputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]PropertySchema `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

// PropertySchema describes a single input parameter.
type PropertySchema struct {
	Type        string   `json:"type"`
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

// ---------------------------------------------------------------------------
// Conversion helpers (SDK types → proto types)
// ---------------------------------------------------------------------------

func toProtoInput(in ToolInputSchema) *dotfilesdv1.ToolInputSchema {
	props := make(map[string]*dotfilesdv1.PropertySchema, len(in.Properties))
	for k, v := range in.Properties {
		props[k] = &dotfilesdv1.PropertySchema{
			Type:        v.Type,
			Description: v.Description,
			Enum:        v.Enum,
			Default:     v.Default,
			HasDefault:  v.HasDefault,
		}
	}

	return &dotfilesdv1.ToolInputSchema{
		Type:       in.Type,
		Properties: props,
		Required:   in.Required,
	}
}

func toProtoCLI(cli CLIHints) *dotfilesdv1.CLIHints {
	if cli.CommandPath == "" && cli.Category == "" && len(cli.FlagMapping) == 0 {
		return nil
	}
	return &dotfilesdv1.CLIHints{
		CommandPath: cli.CommandPath,
		Category:    cli.Category,
		FlagMapping: cli.FlagMapping,
	}
}
