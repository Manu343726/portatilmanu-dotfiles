package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"github.com/spf13/cobra"
)

// BuildPluginCommand creates a cobra command for a plugin, generating
// subcommands and typed flags dynamically from the plugin's proto schemas
// (pre-populated by the daemon in PluginRegistryService). If no schemas
// are available, falls back to a static info-only command.
func BuildPluginCommand(p PluginRegistryInfo) *cobra.Command {
	name := p.Name
	disp := p.DisplayName
	if disp == "" {
		disp = name
	}

	pluginCmd := &cobra.Command{
		Use:     name,
		Short:   disp,
		Long:    fmt.Sprintf("Plugin: %s (%s v%s)\n%s", p.Name, p.DisplayName, p.Version, p.Description),
		GroupID: "plugins",
		RunE:    func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}

	// Add persistent --json flag for all plugin commands.
	pluginCmd.PersistentFlags().Bool("json", false, "output raw JSON instead of human-readable formatted output")

	// Use schemas from the registry — already populated by the daemon at
	// plugin load time via grpcreflect. No direct reflection needed.
	if len(p.Schemas) == 0 {
		slog.Debug("no schemas for plugin, using static info", "plugin", name)
		return buildStaticPluginCommand(p)
	}

	// Count non-system services for elision decisions.
	var nonSystemCount int
	for _, svc := range p.Schemas {
		if !isSystemService(svc.Name) && svc.Name != "dotfilesd.v1.DocumentationService" {
			nonSystemCount++
		}
	}
	elideSvc := nonSystemCount == 1

	for _, svc := range p.Schemas {
		if isSystemService(svc.Name) || svc.Name == "dotfilesd.v1.DocumentationService" {
			continue
		}

		elideRPC := len(svc.Methods) == 1

		svcCmd := pluginCmd
		if !elideSvc {
			shortName := shortSvcName(svc.Name)
			svcCmd = &cobra.Command{
				Use:   shortName,
				Short: svc.Name,
				RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
			}
			pluginCmd.AddCommand(svcCmd)
		}

		for _, m := range svc.Methods {
			runE := makeRunEProtoFromSchema(p.URL, svc.Name, m)
			shortDesc := fmt.Sprintf("%s.%s", shortSvcName(svc.Name), m.Name)

			if elideRPC {
				addFlagsFromSchema(svcCmd, m.Request, "")
				svcCmd.RunE = runE
				if !elideSvc {
					svcCmd.Use = fmt.Sprintf("%s [flags]", svcCmd.Use)
					svcCmd.Short = shortDesc
				}
			} else {
				rpcCmd := &cobra.Command{
					Use:   camelToKebab(m.Name),
					Short: shortDesc,
					RunE:  runE,
				}
				addFlagsFromSchema(rpcCmd, m.Request, "")
				svcCmd.AddCommand(rpcCmd)
			}
		}
	}

	return pluginCmd
}

// isSystemService returns true if the name is a gRPC reflection service.
func isSystemService(name string) bool {
	return name == "grpc.reflection.v1.ServerReflection" ||
		name == "grpc.reflection.v1alpha.ServerReflection"
}

// PluginRegistryInfo holds plugin info from the registry response.
type PluginRegistryInfo struct {
	Name        string
	DisplayName string
	Version     string
	Description string
	URL         string
	Services    []string
	// Schemas holds full introspection data (methods, fields, types, enums)
	// pre-populated by the daemon at plugin load time. Clients use these
	// instead of performing their own grpcreflect.
	Schemas []*dotfilesdv1.ServiceSchema
}

// buildStaticPluginCommand creates a simple info-only command (fallback).
func buildStaticPluginCommand(p PluginRegistryInfo) *cobra.Command {
	cmd := &cobra.Command{
		Use:     p.Name,
		Short:   p.DisplayName,
		GroupID: "plugins",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("Name:        %s\n", p.Name)
			fmt.Printf("Display:     %s\n", p.DisplayName)
			fmt.Printf("Version:     %s\n", p.Version)
			fmt.Printf("Description: %s\n", p.Description)
			if len(p.Services) > 0 {
				fmt.Println("Services:")
				for _, svc := range p.Services {
					fmt.Printf("  - %s\n", svc)
				}
			}
			return nil
		},
	}
	return cmd
}

// ─────────────────────────────────────────────
// Schema-based cobra flag generation
// ─────────────────────────────────────────────

// addFlagsFromSchema recursively adds cobra flags from a registry MessageSchema.
func addFlagsFromSchema(cmd *cobra.Command, msg *dotfilesdv1.MessageSchema, prefix string) {
	for _, fs := range msg.Fields {
		flagName := camelToKebab(prefix + fs.Name)
		desc := fs.Name

		switch fs.Label {
		case dotfilesdv1.FieldLabel_FIELD_LABEL_REPEATED:
			addRepeatedFlag(cmd, flagName, fs, desc)

		default:
			addScalarFlag(cmd, flagName, fs, desc, msg)
		}
	}
}

// addScalarFlag registers a single cobra flag from a FieldSchema.
func addScalarFlag(cmd *cobra.Command, flagName string, fs *dotfilesdv1.FieldSchema, desc string, parentMsg *dotfilesdv1.MessageSchema) {
	fullDesc := desc
	if fs.TypeName != "" {
		fullDesc = desc + " (" + fs.TypeName + ")"
	}

	switch fs.Kind {
	case dotfilesdv1.FieldKind_FIELD_KIND_STRING, dotfilesdv1.FieldKind_FIELD_KIND_BYTES:
		cmd.Flags().String(flagName, "", fullDesc)

	case dotfilesdv1.FieldKind_FIELD_KIND_INT32, dotfilesdv1.FieldKind_FIELD_KIND_SINT32, dotfilesdv1.FieldKind_FIELD_KIND_SFIXED32:
		cmd.Flags().Int32(flagName, 0, fullDesc)
	case dotfilesdv1.FieldKind_FIELD_KIND_INT64, dotfilesdv1.FieldKind_FIELD_KIND_SINT64, dotfilesdv1.FieldKind_FIELD_KIND_SFIXED64:
		cmd.Flags().Int64(flagName, 0, fullDesc)
	case dotfilesdv1.FieldKind_FIELD_KIND_UINT32, dotfilesdv1.FieldKind_FIELD_KIND_FIXED32:
		cmd.Flags().Uint32(flagName, 0, fullDesc)
	case dotfilesdv1.FieldKind_FIELD_KIND_UINT64, dotfilesdv1.FieldKind_FIELD_KIND_FIXED64:
		cmd.Flags().Uint64(flagName, 0, fullDesc)
	case dotfilesdv1.FieldKind_FIELD_KIND_DOUBLE, dotfilesdv1.FieldKind_FIELD_KIND_FLOAT:
		cmd.Flags().Float64(flagName, 0, fullDesc)
	case dotfilesdv1.FieldKind_FIELD_KIND_BOOL:
		cmd.Flags().Bool(flagName, false, fullDesc)

	case dotfilesdv1.FieldKind_FIELD_KIND_ENUM:
		if fs.EnumSchema != nil {
			choices := make([]string, len(fs.EnumSchema.Values))
			for i, ev := range fs.EnumSchema.Values {
				choices[i] = ev.Name
			}
			defVal := ""
			if len(choices) > 0 {
				defVal = choices[0]
			}
			cmd.Flags().String(flagName, defVal, fullDesc)
			name := flagName
			cmd.RegisterFlagCompletionFunc(name, func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
				return choices, cobra.ShellCompDirectiveDefault
			})
		} else {
			cmd.Flags().String(flagName, "", fullDesc)
		}

	case dotfilesdv1.FieldKind_FIELD_KIND_MESSAGE:
		nested := findNestedMessageInSchema(parentMsg, fs.TypeName)
		if nested != nil {
			addFlagsFromSchema(cmd, nested, flagName+".")
		} else {
			cmd.Flags().String(flagName, "", fullDesc+" (unknown message)")
		}

	default:
		cmd.Flags().String(flagName, "", fullDesc+" (unknown type)")
	}
}

// addRepeatedFlag registers a repeated cobra flag from a FieldSchema.
func addRepeatedFlag(cmd *cobra.Command, flagName string, fs *dotfilesdv1.FieldSchema, desc string) {
	switch fs.Kind {
	case dotfilesdv1.FieldKind_FIELD_KIND_STRING:
		cmd.Flags().StringSlice(flagName, nil, desc+" (repeated)")
	case dotfilesdv1.FieldKind_FIELD_KIND_INT32, dotfilesdv1.FieldKind_FIELD_KIND_INT64,
		dotfilesdv1.FieldKind_FIELD_KIND_SINT32, dotfilesdv1.FieldKind_FIELD_KIND_SINT64,
		dotfilesdv1.FieldKind_FIELD_KIND_UINT32, dotfilesdv1.FieldKind_FIELD_KIND_UINT64,
		dotfilesdv1.FieldKind_FIELD_KIND_FIXED32, dotfilesdv1.FieldKind_FIELD_KIND_FIXED64,
		dotfilesdv1.FieldKind_FIELD_KIND_SFIXED32, dotfilesdv1.FieldKind_FIELD_KIND_SFIXED64:
		cmd.Flags().Int64Slice(flagName, nil, desc+" (repeated ints)")
	case dotfilesdv1.FieldKind_FIELD_KIND_DOUBLE, dotfilesdv1.FieldKind_FIELD_KIND_FLOAT:
		cmd.Flags().Float64Slice(flagName, nil, desc+" (repeated floats)")
	default:
		cmd.Flags().StringSlice(flagName, nil, desc+" (repeated)")
	}
}

// ─────────────────────────────────────────────
// Schema-based JSON body builder (RunE)
// ─────────────────────────────────────────────

// makeRunEProtoFromSchema returns the RunE function that builds a JSON body
// from cobra flags using registry MethodSchema and invokes the RPC.
func makeRunEProtoFromSchema(pluginURL, svcName string, m *dotfilesdv1.MethodSchema) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		body := buildJSONFromSchema(cmd, m.Request, "")
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}

		// Build headers: CLI defaults to RenderOutput=true (human-readable).
		headers := map[string]string{
			"X-Dotfiles-Render-Output": "true",
		}
		if jsonFlag, _ := cmd.Flags().GetBool("json"); jsonFlag {
			headers["X-Dotfiles-Render-Output"] = "false"
		}
		if format, ok := body["format"]; ok {
			if s, ok := format.(string); ok && s == "json" {
				headers["X-Dotfiles-Render-Output"] = "false"
			}
		}

		// Build the RPC URL: {baseURL}/{svcName}/{methodName}
		rpcURL := fmt.Sprintf("%s/%s/%s", pluginURL, svcName, m.Name)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		httpReq, err := http.NewRequestWithContext(ctx, "POST", rpcURL, bytes.NewReader(jsonBytes))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			httpReq.Header.Set(k, v)
		}

		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			return fmt.Errorf("rpc call: %w", err)
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read response: %w", err)
		}
		if resp.StatusCode >= 400 {
			return fmt.Errorf("RPC failed (HTTP %d): %s", resp.StatusCode, string(respBody))
		}

		var pretty bytes.Buffer
		if err := json.Indent(&pretty, respBody, "", "  "); err != nil {
			fmt.Println(string(respBody))
		} else {
			fmt.Println(pretty.String())
		}
		return nil
	}
}

// buildJSONFromSchema recursively builds a JSON-compatible map from cobra flags,
// driven by a registry MessageSchema.
func buildJSONFromSchema(cmd *cobra.Command, msg *dotfilesdv1.MessageSchema, prefix string) map[string]any {
	result := make(map[string]any)
	for _, fs := range msg.Fields {
		flagName := camelToKebab(prefix + fs.Name)
		if !cmd.Flags().Changed(flagName) {
			continue
		}

		fieldName := fs.Name

		if fs.Label == dotfilesdv1.FieldLabel_FIELD_LABEL_REPEATED && fs.Kind == dotfilesdv1.FieldKind_FIELD_KIND_MESSAGE {
			// Repeated message: not directly supported via flags — skip.
			continue
		}

		if fs.Label == dotfilesdv1.FieldLabel_FIELD_LABEL_REPEATED {
			result[fieldName] = buildRepeatedFromSchema(cmd, flagName, fs)
			continue
		}

		result[fieldName] = buildScalarFromSchema(cmd, flagName, fs, msg)
	}
	return result
}

// buildScalarFromSchema extracts a single flag value from cobra flags,
// typed according to the field's FieldKind.
func buildScalarFromSchema(cmd *cobra.Command, flagName string, fs *dotfilesdv1.FieldSchema, parentMsg *dotfilesdv1.MessageSchema) any {
	switch fs.Kind {
	case dotfilesdv1.FieldKind_FIELD_KIND_STRING, dotfilesdv1.FieldKind_FIELD_KIND_BYTES:
		v, _ := cmd.Flags().GetString(flagName)
		return v
	case dotfilesdv1.FieldKind_FIELD_KIND_INT32, dotfilesdv1.FieldKind_FIELD_KIND_SINT32, dotfilesdv1.FieldKind_FIELD_KIND_SFIXED32:
		v, _ := cmd.Flags().GetInt32(flagName)
		return v
	case dotfilesdv1.FieldKind_FIELD_KIND_INT64, dotfilesdv1.FieldKind_FIELD_KIND_SINT64, dotfilesdv1.FieldKind_FIELD_KIND_SFIXED64:
		v, _ := cmd.Flags().GetInt64(flagName)
		return v
	case dotfilesdv1.FieldKind_FIELD_KIND_UINT32, dotfilesdv1.FieldKind_FIELD_KIND_FIXED32:
		v, _ := cmd.Flags().GetUint32(flagName)
		return v
	case dotfilesdv1.FieldKind_FIELD_KIND_UINT64, dotfilesdv1.FieldKind_FIELD_KIND_FIXED64:
		v, _ := cmd.Flags().GetUint64(flagName)
		return v
	case dotfilesdv1.FieldKind_FIELD_KIND_DOUBLE, dotfilesdv1.FieldKind_FIELD_KIND_FLOAT:
		v, _ := cmd.Flags().GetFloat64(flagName)
		return v
	case dotfilesdv1.FieldKind_FIELD_KIND_BOOL:
		v, _ := cmd.Flags().GetBool(flagName)
		return v
	case dotfilesdv1.FieldKind_FIELD_KIND_ENUM:
		v, _ := cmd.Flags().GetString(flagName)
		return v
	case dotfilesdv1.FieldKind_FIELD_KIND_MESSAGE:
		nested := findNestedMessageInSchema(parentMsg, fs.TypeName)
		if nested != nil {
			return buildJSONFromSchema(cmd, nested, flagName+".")
		}
		return nil
	default:
		return nil
	}
}

// buildRepeatedFromSchema extracts a repeated flag value from cobra flags.
func buildRepeatedFromSchema(cmd *cobra.Command, flagName string, fs *dotfilesdv1.FieldSchema) any {
	switch fs.Kind {
	case dotfilesdv1.FieldKind_FIELD_KIND_STRING:
		v, _ := cmd.Flags().GetStringSlice(flagName)
		return v
	case dotfilesdv1.FieldKind_FIELD_KIND_INT32, dotfilesdv1.FieldKind_FIELD_KIND_INT64,
		dotfilesdv1.FieldKind_FIELD_KIND_SINT32, dotfilesdv1.FieldKind_FIELD_KIND_SINT64,
		dotfilesdv1.FieldKind_FIELD_KIND_UINT32, dotfilesdv1.FieldKind_FIELD_KIND_UINT64,
		dotfilesdv1.FieldKind_FIELD_KIND_FIXED32, dotfilesdv1.FieldKind_FIELD_KIND_FIXED64,
		dotfilesdv1.FieldKind_FIELD_KIND_SFIXED32, dotfilesdv1.FieldKind_FIELD_KIND_SFIXED64:
		v, _ := cmd.Flags().GetInt64Slice(flagName)
		return v
	case dotfilesdv1.FieldKind_FIELD_KIND_DOUBLE, dotfilesdv1.FieldKind_FIELD_KIND_FLOAT:
		v, _ := cmd.Flags().GetFloat64Slice(flagName)
		return v
	default:
		v, _ := cmd.Flags().GetStringSlice(flagName)
		return v
	}
}

// ─────────────────────────────────────────────
// Schema navigation helpers
// ─────────────────────────────────────────────

// findNestedMessageInSchema finds a nested MessageSchema by fully-qualified
// type name, searching recursively through nested messages.
func findNestedMessageInSchema(parent *dotfilesdv1.MessageSchema, typeName string) *dotfilesdv1.MessageSchema {
	if parent.Name == typeName {
		return parent
	}
	for _, nested := range parent.Messages {
		if nested.Name == typeName {
			return nested
		}
		if found := findNestedMessageInSchema(nested, typeName); found != nil {
			return found
		}
	}
	return nil
}

// ─────────────────────────────────────────────
// Shared helpers
// ─────────────────────────────────────────────

// shortSvcName returns a short name from a fully-qualified service name.
func shortSvcName(fullName string) string {
	parts := strings.Split(fullName, ".")
	if len(parts) == 0 {
		return fullName
	}
	last := parts[len(parts)-1]
	return strings.TrimSuffix(last, "Service")
}

// camelToKebab converts CamelCase and snake_case to kebab-case.
func camelToKebab(s string) string {
	var result strings.Builder
	for i, r := range s {
		if r == '_' {
			result.WriteRune('-')
			continue
		}
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				result.WriteRune('-')
			}
			result.WriteRune(r - 'A' + 'a')
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}
