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

	"connectrpc.com/grpcreflect"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// methodSchema holds resolved RPC method information for a single method.
type methodSchema struct {
	svcFullName string // e.g. "weather.WeatherService"
	methodName  string // e.g. "Forecast"
	method      protoreflect.MethodDescriptor
	inputMsg    protoreflect.MessageDescriptor
}

// serviceSchema holds resolved service information.
type serviceSchema struct {
	fullName string // e.g. "weather.WeatherService"
	methods  []methodSchema
}

// BuildPluginCommand creates a cobra command for a plugin, generating
// subcommands and typed flags dynamically from the plugin's proto schema
// via grpcreflect over HTTP. If the plugin is unreachable, falls back
// to a static info-only command.
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

	// Try to discover services via HTTP-based grpcreflect.
	schemas, err := discoverPluginSchema(p.URL)
	if err != nil {
		slog.Debug("reflection failed for plugin, using static info", "plugin", name, "error", err)
		return buildStaticPluginCommand(p)
	}

	if len(schemas) == 0 {
		return buildStaticPluginCommand(p)
	}

	elideSvc := len(schemas) == 1

	for _, svc := range schemas {
		svcCmd := pluginCmd
		if !elideSvc {
			shortName := shortSvcName(svc.fullName)
			svcCmd = &cobra.Command{
				Use:   shortName,
				Short: svc.fullName,
				RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
			}
			pluginCmd.AddCommand(svcCmd)
		}

		for _, m := range svc.methods {
			rpcCmd := &cobra.Command{
				Use:   camelToKebab(m.methodName),
				Short: fmt.Sprintf("%s.%s", shortSvcName(svc.fullName), m.methodName),
				RunE:  makeRunEProto(p.URL, m),
			}

			addFlagsFromMessageDesc(rpcCmd, m.inputMsg, "")
			svcCmd.AddCommand(rpcCmd)
		}
	}

	return pluginCmd
}

// PluginRegistryInfo holds plugin info from the registry response.
type PluginRegistryInfo struct {
	Name        string
	DisplayName string
	Version     string
	Description string
	URL         string
	Services    []string
}

// discoverPluginSchema uses connectrpc grpcreflect over HTTP to get the
// plugin's service schema (services, methods, and input message descriptors).
func discoverPluginSchema(pluginURL string) ([]serviceSchema, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	httpClient := &http.Client{Timeout: 5 * time.Second}
	refClient := grpcreflect.NewClient(httpClient, pluginURL)
	stream := refClient.NewStream(ctx)
	defer stream.Close()

	svcNames, err := stream.ListServices()
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	var schemas []serviceSchema
	for _, fullName := range svcNames {
		s := string(fullName)
		if isSystemService(s) {
			continue
		}

		// Get file descriptor(s) containing this service.
		// The server returns the file plus its transitive dependencies.
		fdProtos, err := stream.FileContainingSymbol(fullName)
		if err != nil {
			slog.Debug("FileContainingSymbol failed", "service", s, "error", err)
			continue
		}

		// Build a *protoregistry.Files from all returned descriptors.
		svcDesc, err := findServiceDescriptor(fdProtos, s)
		if err != nil {
			slog.Debug("findServiceDescriptor failed", "service", s, "error", err)
			continue
		}

		var methods []methodSchema
		for i := 0; i < svcDesc.Methods().Len(); i++ {
			md := svcDesc.Methods().Get(i)
			methods = append(methods, methodSchema{
				svcFullName: s,
				methodName:  string(md.Name()),
				method:      md,
				inputMsg:    md.Input(),
			})
		}
		schemas = append(schemas, serviceSchema{fullName: s, methods: methods})
	}
	return schemas, nil
}

// findServiceDescriptor builds a *protoregistry.Files from one or more
// FileDescriptorProtos and looks up the service by its fully-qualified name.
func findServiceDescriptor(fdProtos []*descriptorpb.FileDescriptorProto, svcFullName string) (protoreflect.ServiceDescriptor, error) {
	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: fdProtos})
	if err != nil {
		return nil, fmt.Errorf("build file descriptors: %w", err)
	}

	d, err := files.FindDescriptorByName(protoreflect.FullName(svcFullName))
	if err != nil {
		return nil, fmt.Errorf("find service %q: %w", svcFullName, err)
	}

	svcDesc, ok := d.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("symbol %q is not a service", svcFullName)
	}
	return svcDesc, nil
}

// isSystemService returns true for built-in services that should not be
// exposed as CLI commands.
func isSystemService(name string) bool {
	return name == "grpc.reflection.v1.ServerReflection" ||
		name == "grpc.reflection.v1alpha.ServerReflection" ||
		name == "dotfilesd.v1.DocumentationService"
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

// addFlagsFromMessageDesc recursively adds cobra flags from a
// protoreflect.MessageDescriptor.
func addFlagsFromMessageDesc(cmd *cobra.Command, msg protoreflect.MessageDescriptor, prefix string) {
	fields := msg.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		flagName := camelToKebab(prefix + string(fd.Name()))
		fullDesc := string(fd.FullName())

		switch {
		case fd.IsMap():
			mapValKind := fd.MapValue().Kind()
			cmd.Flags().StringSlice(flagName, nil,
				fmt.Sprintf("Map (string → %s). Use --%s.<key>=<value>", mapValKind, flagName))

		case fd.IsList():
			switch fd.Kind() {
			case protoreflect.StringKind:
				cmd.Flags().StringSlice(flagName, nil, fullDesc+" (repeated)")
			case protoreflect.Int32Kind, protoreflect.Int64Kind,
				protoreflect.Sint32Kind, protoreflect.Sint64Kind,
				protoreflect.Uint32Kind, protoreflect.Uint64Kind,
				protoreflect.Fixed32Kind, protoreflect.Fixed64Kind,
				protoreflect.Sfixed32Kind, protoreflect.Sfixed64Kind:
				cmd.Flags().Int64Slice(flagName, nil, fullDesc+" (repeated ints)")
			case protoreflect.FloatKind, protoreflect.DoubleKind:
				cmd.Flags().Float64Slice(flagName, nil, fullDesc+" (repeated floats)")
			default:
				cmd.Flags().StringSlice(flagName, nil, fullDesc+" (repeated)")
			}

		default:
			switch fd.Kind() {
			case protoreflect.StringKind:
				cmd.Flags().String(flagName, "", fullDesc)
			case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
				cmd.Flags().Int32(flagName, 0, fullDesc)
			case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
				cmd.Flags().Int64(flagName, 0, fullDesc)
			case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
				cmd.Flags().Uint32(flagName, 0, fullDesc)
			case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
				cmd.Flags().Uint64(flagName, 0, fullDesc)
			case protoreflect.FloatKind, protoreflect.DoubleKind:
				cmd.Flags().Float64(flagName, 0, fullDesc)
			case protoreflect.BoolKind:
				cmd.Flags().Bool(flagName, false, fullDesc)
			case protoreflect.EnumKind:
				enumDesc := fd.Enum()
				choices := make([]string, enumDesc.Values().Len())
				for j := 0; j < enumDesc.Values().Len(); j++ {
					choices[j] = string(enumDesc.Values().Get(j).Name())
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
			case protoreflect.MessageKind, protoreflect.GroupKind:
				nested := fd.Message()
				if nested != nil {
					addFlagsFromMessageDesc(cmd, nested, flagName+".")
				}
			default:
				cmd.Flags().String(flagName, "", fullDesc+" (unknown type)")
			}
		}
	}
}

// makeRunEProto returns the RunE function that builds a JSON body from
// cobra flags and invokes the RPC via HTTP POST to the Connect endpoint.
func makeRunEProto(pluginURL string, m methodSchema) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		// Build a JSON map from cobra flags.
		body := buildJSONFromMessage(cmd, m.inputMsg, "")
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}

		rpcURL := fmt.Sprintf("%s/%s/%s", pluginURL, m.svcFullName, m.methodName)
		req, err := http.NewRequest("POST", rpcURL, bytes.NewReader(jsonBytes))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		httpClient := &http.Client{Timeout: 10 * time.Second}
		resp, err := httpClient.Do(req)
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

// buildJSONFromMessage recursively builds a JSON-compatible map from
// cobra flags, driven by a protoreflect.MessageDescriptor.
func buildJSONFromMessage(cmd *cobra.Command, msg protoreflect.MessageDescriptor, prefix string) map[string]any {
	result := make(map[string]any)
	fields := msg.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		flagName := camelToKebab(prefix + string(fd.Name()))
		if !cmd.Flags().Changed(flagName) {
			continue
		}

		protoName := string(fd.Name())

		if fd.IsMap() {
			vals, _ := cmd.Flags().GetStringSlice(flagName)
			m := make(map[string]any)
			for _, entry := range vals {
				if eq := strings.Index(entry, "="); eq >= 0 {
					m[entry[:eq]] = parseMapValueKind(entry[eq+1:], fd.MapValue().Kind())
				}
			}
			result[protoName] = m
			continue
		}

		if fd.IsList() {
			result[protoName] = buildRepeatedValueKind(cmd, flagName, fd)
			continue
		}

		result[protoName] = buildScalarValueKind(cmd, flagName, fd)
	}
	return result
}

// buildScalarValueKind extracts a single flag value from cobra flags,
// typed according to the field's protoreflect.Kind.
func buildScalarValueKind(cmd *cobra.Command, flagName string, fd protoreflect.FieldDescriptor) any {
	switch fd.Kind() {
	case protoreflect.StringKind:
		v, _ := cmd.Flags().GetString(flagName)
		return v
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		v, _ := cmd.Flags().GetInt32(flagName)
		return v
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		v, _ := cmd.Flags().GetInt64(flagName)
		return v
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		v, _ := cmd.Flags().GetUint32(flagName)
		return v
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		v, _ := cmd.Flags().GetUint64(flagName)
		return v
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		v, _ := cmd.Flags().GetFloat64(flagName)
		return v
	case protoreflect.BoolKind:
		v, _ := cmd.Flags().GetBool(flagName)
		return v
	case protoreflect.EnumKind:
		v, _ := cmd.Flags().GetString(flagName)
		return v
	case protoreflect.MessageKind, protoreflect.GroupKind:
		nested := fd.Message()
		if nested != nil {
			return buildJSONFromMessage(cmd, nested, flagName+".")
		}
		return nil
	default:
		return nil
	}
}

// buildRepeatedValueKind extracts a repeated flag value from cobra flags.
func buildRepeatedValueKind(cmd *cobra.Command, flagName string, fd protoreflect.FieldDescriptor) any {
	switch fd.Kind() {
	case protoreflect.StringKind:
		v, _ := cmd.Flags().GetStringSlice(flagName)
		return v
	case protoreflect.Int32Kind, protoreflect.Int64Kind,
		protoreflect.Sint32Kind, protoreflect.Sint64Kind,
		protoreflect.Uint32Kind, protoreflect.Uint64Kind,
		protoreflect.Fixed32Kind, protoreflect.Fixed64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Sfixed64Kind:
		v, _ := cmd.Flags().GetInt64Slice(flagName)
		return v
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		v, _ := cmd.Flags().GetFloat64Slice(flagName)
		return v
	default:
		v, _ := cmd.Flags().GetStringSlice(flagName)
		return v
	}
}

// parseMapValueKind parses a map value string according to the protoreflect.Kind.
func parseMapValueKind(val string, kind protoreflect.Kind) any {
	switch kind {
	case protoreflect.Int32Kind, protoreflect.Int64Kind,
		protoreflect.Uint32Kind, protoreflect.Uint64Kind:
		var v int64
		fmt.Sscanf(val, "%d", &v)
		return v
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		var v float64
		fmt.Sscanf(val, "%f", &v)
		return v
	case protoreflect.BoolKind:
		return val == "true" || val == "1"
	default:
		return val
	}
}

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
