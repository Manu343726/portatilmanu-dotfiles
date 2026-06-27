package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/fullstorydev/grpcurl"
	"github.com/jhump/protoreflect/desc"
	grpcreflect "github.com/jhump/protoreflect/grpcreflect"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/descriptorpb"
)

// BuildPluginCommand creates a cobra command for a plugin, generating
// subcommands and typed flags dynamically from the plugin's proto schema
// via grpcreflect. If the plugin is unreachable, falls back to a static
// info-only command.
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

	// Try to discover services via gRPC reflection against the plugin.
	source, err := newDescriptorSource(p.URL)
	if err != nil {
		slog.Debug("reflection failed for plugin, using static info", "plugin", name, "error", err)
		return buildStaticPluginCommand(p)
	}

	services, err := source.ListServices()
	if err != nil {
		slog.Debug("ListServices failed for plugin, using static info", "plugin", name, "error", err)
		return buildStaticPluginCommand(p)
	}

	// Filter out non-user services.
	var userServices []string
	for _, svc := range services {
		if svc == "grpc.reflection.v1.ServerReflection" ||
			svc == "grpc.reflection.v1alpha.ServerReflection" ||
			svc == "dotfilesd.v1.DocumentationService" {
			continue
		}
		userServices = append(userServices, svc)
	}

	if len(userServices) == 0 {
		return buildStaticPluginCommand(p)
	}

	elideSvc := len(userServices) == 1

	for _, svcName := range userServices {
		svcDesc, err := source.FindSymbol(svcName)
		if err != nil {
			continue
		}

		sd, ok := svcDesc.(*desc.ServiceDescriptor)
		if !ok {
			continue
		}

		svcCmd := pluginCmd
		if !elideSvc {
			shortName := shortSvcName(svcName)
			svcCmd = &cobra.Command{
				Use:   shortName,
				Short: svcName,
				RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
			}
			pluginCmd.AddCommand(svcCmd)
		}

		for _, md := range sd.GetMethods() {
			methodName := md.GetName()
			mdDesc := md.GetInputType()

			rpcCmd := &cobra.Command{
				Use:   camelToKebab(methodName),
				Short: fmt.Sprintf("%s.%s", shortSvcName(svcName), methodName),
				RunE:  makeRunE(svcName, methodName, mdDesc),
			}

			addFlagsFromMessage(rpcCmd, mdDesc, "")
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

// newDescriptorSource creates a grpcurl.DescriptorSource by connecting to
// the plugin at the given URL via gRPC reflection.
func newDescriptorSource(pluginURL string) (grpcurl.DescriptorSource, error) {
	u, err := url.Parse(pluginURL)
	if err != nil {
		return nil, fmt.Errorf("parse plugin URL: %w", err)
	}
	hostPort := u.Host

	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, hostPort,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.FailOnNonTempDialError(true),
	)
	if err != nil {
		return nil, fmt.Errorf("dial plugin: %w", err)
	}

	refClient := grpcreflect.NewClientAuto(ctx, conn)
	ds := grpcurl.DescriptorSourceFromServer(ctx, refClient)
	return ds, nil
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

// addFlagsFromMessage recursively adds cobra flags from a proto message descriptor.
func addFlagsFromMessage(cmd *cobra.Command, md *desc.MessageDescriptor, prefix string) {
	fields := md.GetFields()
	for _, fd := range fields {
		flagName := camelToKebab(prefix + fd.GetName())
		dsc := fd.GetFullyQualifiedName()

		if fd.IsMap() {
			vk := fd.GetMapValueType().GetType()
			cmd.Flags().StringSlice(flagName, nil,
				fmt.Sprintf("Map (%s → %s). Use --%s.<key>=<value>", fd.GetMapKeyType().GetName(), vk.String(), flagName))
			continue
		}

		if fd.IsRepeated() {
			switch fd.GetType() {
			case descriptorpb.FieldDescriptorProto_TYPE_STRING:
				cmd.Flags().StringSlice(flagName, nil, dsc+" (repeated)")
			case descriptorpb.FieldDescriptorProto_TYPE_INT32,
				descriptorpb.FieldDescriptorProto_TYPE_INT64,
				descriptorpb.FieldDescriptorProto_TYPE_UINT32,
				descriptorpb.FieldDescriptorProto_TYPE_UINT64,
				descriptorpb.FieldDescriptorProto_TYPE_SINT32,
				descriptorpb.FieldDescriptorProto_TYPE_SINT64,
				descriptorpb.FieldDescriptorProto_TYPE_FIXED32,
				descriptorpb.FieldDescriptorProto_TYPE_FIXED64,
				descriptorpb.FieldDescriptorProto_TYPE_SFIXED32,
				descriptorpb.FieldDescriptorProto_TYPE_SFIXED64:
				cmd.Flags().Int64Slice(flagName, nil, dsc+" (repeated ints)")
			case descriptorpb.FieldDescriptorProto_TYPE_DOUBLE,
				descriptorpb.FieldDescriptorProto_TYPE_FLOAT:
				cmd.Flags().Float64Slice(flagName, nil, dsc+" (repeated floats)")
			default:
				cmd.Flags().StringSlice(flagName, nil, dsc+" (repeated)")
			}
			continue
		}

		switch fd.GetType() {
		case descriptorpb.FieldDescriptorProto_TYPE_STRING:
			cmd.Flags().String(flagName, "", dsc)
		case descriptorpb.FieldDescriptorProto_TYPE_INT32,
			descriptorpb.FieldDescriptorProto_TYPE_SINT32,
			descriptorpb.FieldDescriptorProto_TYPE_SFIXED32:
			cmd.Flags().Int32(flagName, 0, dsc)
		case descriptorpb.FieldDescriptorProto_TYPE_INT64,
			descriptorpb.FieldDescriptorProto_TYPE_SINT64,
			descriptorpb.FieldDescriptorProto_TYPE_SFIXED64:
			cmd.Flags().Int64(flagName, 0, dsc)
		case descriptorpb.FieldDescriptorProto_TYPE_UINT32,
			descriptorpb.FieldDescriptorProto_TYPE_FIXED32:
			cmd.Flags().Uint32(flagName, 0, dsc)
		case descriptorpb.FieldDescriptorProto_TYPE_UINT64,
			descriptorpb.FieldDescriptorProto_TYPE_FIXED64:
			cmd.Flags().Uint64(flagName, 0, dsc)
		case descriptorpb.FieldDescriptorProto_TYPE_DOUBLE,
			descriptorpb.FieldDescriptorProto_TYPE_FLOAT:
			cmd.Flags().Float64(flagName, 0, dsc)
		case descriptorpb.FieldDescriptorProto_TYPE_BOOL:
			cmd.Flags().Bool(flagName, false, dsc)
		case descriptorpb.FieldDescriptorProto_TYPE_ENUM:
			ed := fd.GetEnumType()
			choices := make([]string, 0, len(ed.GetValues()))
			for _, v := range ed.GetValues() {
				choices = append(choices, v.GetName())
			}
			defVal := ""
			if len(choices) > 0 {
				defVal = choices[0]
			}
			cmd.Flags().String(flagName, defVal, dsc)
			cmd.RegisterFlagCompletionFunc(flagName, func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
				return choices, cobra.ShellCompDirectiveDefault
			})
		case descriptorpb.FieldDescriptorProto_TYPE_MESSAGE,
			descriptorpb.FieldDescriptorProto_TYPE_GROUP:
			nested := fd.GetMessageType()
			if nested != nil {
				addFlagsFromMessage(cmd, nested, flagName+".")
			}
		default:
			cmd.Flags().String(flagName, "", dsc+" (unknown type)")
		}
	}
}

// makeRunE returns the RunE function that builds a JSON body from cobra flags
// and invokes the RPC via HTTP POST to the Connect endpoint.
func makeRunE(svcName, methodName string, inputDesc *desc.MessageDescriptor) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		// Build a JSON map from cobra flags.
		body := buildJSONFromFlags(cmd, inputDesc, "")
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}

		// GET the daemon URL from env or use 9105 default.
		daemonPort := "9105"
		pluginURL := fmt.Sprintf("http://127.0.0.1:%s", daemonPort)
		if u := cmd.Flag("url"); u != nil && u.Changed {
			pluginURL = u.Value.String()
		}

		rpcURL := fmt.Sprintf("%s/%s/%s", pluginURL, svcName, methodName)
		req, err := http.NewRequest("POST", rpcURL, bytes.NewReader(jsonBytes))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
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

// buildJSONFromFlags recursively builds a JSON-compatible map from cobra flags.
func buildJSONFromFlags(cmd *cobra.Command, md *desc.MessageDescriptor, prefix string) map[string]interface{} {
	result := make(map[string]interface{})
	fields := md.GetFields()
	for _, fd := range fields {
		flagName := camelToKebab(prefix + fd.GetName())
		if !cmd.Flags().Changed(flagName) {
			// Use proto field name (snake_case) for JSON structure.
			continue
		}

		protoName := string(fd.GetName())
		if fd.IsMap() {
			vals, _ := cmd.Flags().GetStringSlice(flagName)
			m := make(map[string]interface{})
			for _, entry := range vals {
				if eq := strings.Index(entry, "="); eq >= 0 {
					m[entry[:eq]] = parseMapValueStr(entry[eq+1:], fd.GetMapValueType().GetType())
				}
			}
			result[protoName] = m
			continue
		}
		if fd.IsRepeated() {
			result[protoName] = buildRepeatedValue(cmd, flagName, fd)
			continue
		}

		result[protoName] = buildScalarValue(cmd, flagName, fd)
	}
	return result
}

func buildScalarValue(cmd *cobra.Command, flagName string, fd *desc.FieldDescriptor) interface{} {
	switch fd.GetType() {
	case descriptorpb.FieldDescriptorProto_TYPE_STRING:
		v, _ := cmd.Flags().GetString(flagName)
		return v
	case descriptorpb.FieldDescriptorProto_TYPE_INT32,
		descriptorpb.FieldDescriptorProto_TYPE_SINT32,
		descriptorpb.FieldDescriptorProto_TYPE_SFIXED32:
		v, _ := cmd.Flags().GetInt32(flagName)
		return v
	case descriptorpb.FieldDescriptorProto_TYPE_INT64,
		descriptorpb.FieldDescriptorProto_TYPE_SINT64,
		descriptorpb.FieldDescriptorProto_TYPE_SFIXED64:
		v, _ := cmd.Flags().GetInt64(flagName)
		return v
	case descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED32:
		v, _ := cmd.Flags().GetUint32(flagName)
		return v
	case descriptorpb.FieldDescriptorProto_TYPE_UINT64,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED64:
		v, _ := cmd.Flags().GetUint64(flagName)
		return v
	case descriptorpb.FieldDescriptorProto_TYPE_DOUBLE,
		descriptorpb.FieldDescriptorProto_TYPE_FLOAT:
		v, _ := cmd.Flags().GetFloat64(flagName)
		return v
	case descriptorpb.FieldDescriptorProto_TYPE_BOOL:
		v, _ := cmd.Flags().GetBool(flagName)
		return v
	case descriptorpb.FieldDescriptorProto_TYPE_ENUM:
		v, _ := cmd.Flags().GetString(flagName)
		return v
	case descriptorpb.FieldDescriptorProto_TYPE_MESSAGE,
		descriptorpb.FieldDescriptorProto_TYPE_GROUP:
		nested := fd.GetMessageType()
		if nested != nil {
			return buildJSONFromFlags(cmd, nested, flagName+".")
		}
		return nil
	default:
		return nil
	}
}

func buildRepeatedValue(cmd *cobra.Command, flagName string, fd *desc.FieldDescriptor) interface{} {
	switch fd.GetType() {
	case descriptorpb.FieldDescriptorProto_TYPE_STRING:
		v, _ := cmd.Flags().GetStringSlice(flagName)
		return v
	case descriptorpb.FieldDescriptorProto_TYPE_INT32,
		descriptorpb.FieldDescriptorProto_TYPE_INT64,
		descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		descriptorpb.FieldDescriptorProto_TYPE_UINT64:
		v, _ := cmd.Flags().GetInt64Slice(flagName)
		return v
	case descriptorpb.FieldDescriptorProto_TYPE_DOUBLE,
		descriptorpb.FieldDescriptorProto_TYPE_FLOAT:
		v, _ := cmd.Flags().GetFloat64Slice(flagName)
		return v
	default:
		v, _ := cmd.Flags().GetStringSlice(flagName)
		return v
	}
}

func parseMapValueStr(val string, kind descriptorpb.FieldDescriptorProto_Type) interface{} {
	switch kind {
	case descriptorpb.FieldDescriptorProto_TYPE_INT32,
		descriptorpb.FieldDescriptorProto_TYPE_INT64,
		descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		descriptorpb.FieldDescriptorProto_TYPE_UINT64:
		var v int64
		fmt.Sscanf(val, "%d", &v)
		return v
	case descriptorpb.FieldDescriptorProto_TYPE_DOUBLE,
		descriptorpb.FieldDescriptorProto_TYPE_FLOAT:
		var v float64
		fmt.Sscanf(val, "%f", &v)
		return v
	case descriptorpb.FieldDescriptorProto_TYPE_BOOL:
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

