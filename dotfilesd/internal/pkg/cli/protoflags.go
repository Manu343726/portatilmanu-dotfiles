package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"github.com/spf13/cobra"
	"golang.org/x/net/http2"
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
			runE := makeRunEProtoFromSchema(p.URL, p.DaemonURL, svc.Name, m)
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
	// DaemonURL is the base URL of the daemon's RPC server, used to
	// invoke PluginExecutorService for proxied plugin calls.
	DaemonURL string
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
		if fs.TypeName != "" {
			desc = desc + " (" + fs.TypeName + ")"
		}

		if fs.Label == dotfilesdv1.FieldLabel_FIELD_LABEL_REPEATED && fs.Kind == dotfilesdv1.FieldKind_FIELD_KIND_MESSAGE {
			// Repeated message fields use StringToString with the pattern
			// --field [<index>].<subfield-path>=<value>, e.g. --a [0].b.c=1 --a [1].b.c=2
			cmd.Flags().StringToString(flagName, nil,
				"Repeated "+fs.TypeName+". Usage: --"+flagName+" [<idx>].<field>=<value>")
			continue
		}

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
// from cobra flags, invokes the plugin RPC via the daemon's PluginExecutorService,
// and streams stdout/stderr from the plugin to the terminal.
func makeRunEProtoFromSchema(pluginURL, daemonURL, svcName string, m *dotfilesdv1.MethodSchema) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		body, err := buildJSONFromSchema(cmd, m.Request, "")
		if err != nil {
			return fmt.Errorf("build request body: %w", err)
		}
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}

		jsonOutput, _ := cmd.Flags().GetBool("json")
		if !jsonOutput {
			if format, ok := body["format"]; ok {
				if s, ok := format.(string); ok && s == "json" {
					jsonOutput = true
				}
			}
		}

		// Open bidi stream to daemon's PluginExecutorService.
		// Use h2c transport since bidi streaming requires HTTP/2.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		h2cTransport := &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		}
		h2cClient := &http.Client{Transport: h2cTransport}
		execClient := dotfilesdv1connect.NewPluginExecutorServiceClient(h2cClient, daemonURL)
		stream := execClient.CallPlugin(ctx)

		// Send request header with render output preference.
		renderOutput := !jsonOutput
		clientID := fmt.Sprintf("cli_%d", time.Now().UnixNano())
		if DefaultSessionID != "" {
			clientID += "|" + DefaultSessionID
		}
		if err := stream.Send(&dotfilesdv1.CallPluginMessage{
			PluginName:   stripServiceSuffix(svcName),
			Service:      svcName,
			Method:       m.Name,
			RequestBody:  jsonBytes,
			ClientId:     clientID,
			RenderOutput: renderOutput,
		}); err != nil {
			return fmt.Errorf("send request: %w", err)
		}

		// Send stdin chunks from local stdin if piped, then close request.
		if isStdinAvailable() {
			buf := make([]byte, 4096)
			for {
				n, err := os.Stdin.Read(buf)
				if n > 0 {
					_ = stream.Send(&dotfilesdv1.CallPluginMessage{
						StdinChunk: buf[:n],
					})
				}
				if err != nil {
					break
				}
			}
		}
		if err := stream.CloseRequest(); err != nil {
			return fmt.Errorf("close request: %w", err)
		}

		// Receive streaming response.
		for {
			msg, err := stream.Receive()
			if err != nil {
				break
			}
			if msg.Error != "" {
				return fmt.Errorf("plugin error: %s", msg.Error)
			}
			if len(msg.StdoutChunk) > 0 {
				fmt.Print(string(msg.StdoutChunk))
			}
			if len(msg.StderrChunk) > 0 {
				fmt.Fprint(os.Stderr, string(msg.StderrChunk))
			}
			// Print JSON response body only when --json is set.
			if len(msg.ResponseBody) > 0 && jsonOutput {
				var buf bytes.Buffer
				if err := json.Indent(&buf, msg.ResponseBody, "", "  "); err != nil {
					fmt.Println(string(msg.ResponseBody))
				} else {
					fmt.Println(buf.String())
				}
			}
		}
		return nil
	}
}

// stripServiceSuffix extracts the plugin name from a fully-qualified
// service name like "resources.ResourcesService" → "resources".
func stripServiceSuffix(fullName string) string {
	parts := strings.Split(fullName, ".")
	if len(parts) > 0 {
		return parts[0]
	}
	return fullName
}

// buildJSONFromSchema recursively builds a JSON-compatible map from cobra flags,
// driven by a registry MessageSchema.
func buildJSONFromSchema(cmd *cobra.Command, msg *dotfilesdv1.MessageSchema, prefix string) (map[string]any, error) {
	result := make(map[string]any)
	for _, fs := range msg.Fields {
		flagName := camelToKebab(prefix + fs.Name)
		if !cmd.Flags().Changed(flagName) {
			continue
		}

		fieldName := fs.Name

		if fs.Label == dotfilesdv1.FieldLabel_FIELD_LABEL_REPEATED && fs.Kind == dotfilesdv1.FieldKind_FIELD_KIND_MESSAGE {
			arr, err := buildRepeatedMessageFromSchema(cmd, flagName, fs, msg)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", fieldName, err)
			}
			result[fieldName] = arr
			continue
		}

		if fs.Label == dotfilesdv1.FieldLabel_FIELD_LABEL_REPEATED {
			result[fieldName] = buildRepeatedFromSchema(cmd, flagName, fs)
			continue
		}

		v, err := buildScalarFromSchema(cmd, flagName, fs, msg)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", fieldName, err)
		}
		result[fieldName] = v
	}
	return result, nil
}

// buildRepeatedMessageFromSchema builds a JSON array from a StringToString flag
// where each key is "<index>.<field-path>" and value is the typed field value.
//
// Example: --a [0].b.c=1 --a [1].b.c=2 produces [{"b":{"c":1}},{"b":{"c":2}}]
//
// Indices must be consecutive from 0 to N-1 with no gaps.
func buildRepeatedMessageFromSchema(cmd *cobra.Command, flagName string, fs *dotfilesdv1.FieldSchema, parentMsg *dotfilesdv1.MessageSchema) ([]any, error) {
	rawMap, err := cmd.Flags().GetStringToString(flagName)
	if err != nil {
		return nil, fmt.Errorf("invalid flag value for %q: %w", flagName, err)
	}
	if len(rawMap) == 0 {
		return nil, nil
	}

	// Find the nested message schema for this field type.
	nestedMsg := findNestedMessageInSchema(parentMsg, fs.TypeName)
	if nestedMsg == nil {
		return nil, fmt.Errorf("unknown message type %q", fs.TypeName)
	}

	// Parse entries: group by array index.
	// Key format: "[<idx>].<rest>" where <rest> may contain multiple dots.
	// Example: [0].b.c=1, [1].b.c=2
	type entry struct {
		idx   int
		path  string // field path after the index, e.g. "b.c"
		value string
	}
	var entries []entry
	seenIndices := make(map[int]bool)
	maxIdx := -1
	for key, val := range rawMap {
		if len(key) == 0 || key[0] != '[' {
			return nil, fmt.Errorf("invalid key %q: expected [<index>].<field-path>, got no opening bracket", key)
		}
		closeBracket := strings.IndexByte(key, ']')
		if closeBracket < 0 {
			return nil, fmt.Errorf("invalid key %q: expected [<index>].<field-path>, got no closing bracket", key)
		}
		if closeBracket+1 >= len(key) || key[closeBracket+1] != '.' {
			return nil, fmt.Errorf("invalid key %q: expected [<index>].<field-path>, got no dot after bracket", key)
		}
		idxStr := key[1:closeBracket]
		rest := key[closeBracket+2:] // skip "]."

		var parsedIdx int
		if n, _ := fmt.Sscanf(idxStr, "%d", &parsedIdx); n == 0 || parsedIdx < 0 {
			return nil, fmt.Errorf("invalid key %q: expected numeric index, got %q", key, idxStr)
		}

		if seenIndices[parsedIdx] {
			return nil, fmt.Errorf("duplicate index %d for field %q", parsedIdx, flagName)
		}
		seenIndices[parsedIdx] = true
		if parsedIdx > maxIdx {
			maxIdx = parsedIdx
		}
		entries = append(entries, entry{idx: parsedIdx, path: rest, value: val})
	}

	// Validate consecutive indices.
	for i := 0; i <= maxIdx; i++ {
		if !seenIndices[i] {
			return nil, fmt.Errorf("non-consecutive indices for %q: missing index %d (got %d entries, max=%d)",
				flagName, i, len(entries), maxIdx)
		}
	}

	// Build array: for each index, apply all paths for that index.
	result := make([]any, maxIdx+1)
	for i := 0; i <= maxIdx; i++ {
		result[i] = make(map[string]any)
	}

	// Group entries by index.
	byIdx := make(map[int][]entry)
	for _, e := range entries {
		byIdx[e.idx] = append(byIdx[e.idx], e)
	}

	for idx, idxEntries := range byIdx {
		obj := make(map[string]any)
		for _, e := range idxEntries {
			if err := setNestedField(obj, e.path, e.value, nestedMsg); err != nil {
				return nil, fmt.Errorf("index %d: %w", idx, err)
			}
		}
		result[idx] = obj
	}

	return result, nil
}

// setNestedField walks a dot-separated field path (e.g. "b.c") into a nested
// message schema, setting the leaf value in the target map with the correct
// Go type according to the field's schema kind. Intermediate path components
// must be message fields; the final component must be a scalar or enum field.
func setNestedField(obj map[string]any, path, value string, msg *dotfilesdv1.MessageSchema) error {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return fmt.Errorf("empty field path")
	}

	// Walk all but the last part.
	current := msg
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		var found bool
		for _, f := range current.Fields {
			if f.Name == part {
				if f.Kind != dotfilesdv1.FieldKind_FIELD_KIND_MESSAGE {
					return fmt.Errorf("field %q is not a message but path continues after it", part)
				}
				nested := findNestedMessageInSchema(current, f.TypeName)
				if nested == nil {
					return fmt.Errorf("unknown message type %q for field %q", f.TypeName, part)
				}
				// Create the nested map if it doesn't exist.
				if _, ok := obj[part]; !ok {
					obj[part] = make(map[string]any)
				}
				obj = obj[part].(map[string]any)
				current = nested
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown field %q in message %q", part, current.Name)
		}
	}

	// Set the leaf value.
	lastPart := parts[len(parts)-1]
	for _, f := range current.Fields {
		if f.Name == lastPart {
			typed, err := parseScalarValue(value, f.Kind)
			if err != nil {
				return fmt.Errorf("field %q: %w", lastPart, err)
			}
			obj[lastPart] = typed
			return nil
		}
	}
	return fmt.Errorf("unknown field %q in message %q (for path %q)", lastPart, current.Name, path)
}

// parseScalarValue converts a string value to the appropriate Go type
// for the given FieldKind.
func parseScalarValue(val string, kind dotfilesdv1.FieldKind) (any, error) {
	switch kind {
	case dotfilesdv1.FieldKind_FIELD_KIND_STRING, dotfilesdv1.FieldKind_FIELD_KIND_BYTES,
		dotfilesdv1.FieldKind_FIELD_KIND_ENUM:
		return val, nil

	case dotfilesdv1.FieldKind_FIELD_KIND_INT32, dotfilesdv1.FieldKind_FIELD_KIND_SINT32,
		dotfilesdv1.FieldKind_FIELD_KIND_SFIXED32:
		var v int32
		if n, _ := fmt.Sscanf(val, "%d", &v); n == 0 {
			return nil, fmt.Errorf("expected int32, got %q", val)
		}
		return v, nil

	case dotfilesdv1.FieldKind_FIELD_KIND_INT64, dotfilesdv1.FieldKind_FIELD_KIND_SINT64,
		dotfilesdv1.FieldKind_FIELD_KIND_SFIXED64:
		var v int64
		if n, _ := fmt.Sscanf(val, "%d", &v); n == 0 {
			return nil, fmt.Errorf("expected int64, got %q", val)
		}
		return v, nil

	case dotfilesdv1.FieldKind_FIELD_KIND_UINT32, dotfilesdv1.FieldKind_FIELD_KIND_FIXED32:
		var v uint32
		if n, _ := fmt.Sscanf(val, "%d", &v); n == 0 {
			return nil, fmt.Errorf("expected uint32, got %q", val)
		}
		return v, nil

	case dotfilesdv1.FieldKind_FIELD_KIND_UINT64, dotfilesdv1.FieldKind_FIELD_KIND_FIXED64:
		var v uint64
		if n, _ := fmt.Sscanf(val, "%d", &v); n == 0 {
			return nil, fmt.Errorf("expected uint64, got %q", val)
		}
		return v, nil

	case dotfilesdv1.FieldKind_FIELD_KIND_DOUBLE, dotfilesdv1.FieldKind_FIELD_KIND_FLOAT:
		var v float64
		if n, _ := fmt.Sscanf(val, "%f", &v); n == 0 {
			return nil, fmt.Errorf("expected float, got %q", val)
		}
		return v, nil

	case dotfilesdv1.FieldKind_FIELD_KIND_BOOL:
		switch val {
		case "true", "1", "yes":
			return true, nil
		case "false", "0", "no":
			return false, nil
		default:
			return nil, fmt.Errorf("expected bool, got %q", val)
		}

	default:
		return val, nil
	}
}

// buildScalarFromSchema extracts a single flag value from cobra flags,
// typed according to the field's FieldKind.
func buildScalarFromSchema(cmd *cobra.Command, flagName string, fs *dotfilesdv1.FieldSchema, parentMsg *dotfilesdv1.MessageSchema) (any, error) {
	switch fs.Kind {
	case dotfilesdv1.FieldKind_FIELD_KIND_STRING, dotfilesdv1.FieldKind_FIELD_KIND_BYTES:
		v, _ := cmd.Flags().GetString(flagName)
		return v, nil
	case dotfilesdv1.FieldKind_FIELD_KIND_INT32, dotfilesdv1.FieldKind_FIELD_KIND_SINT32, dotfilesdv1.FieldKind_FIELD_KIND_SFIXED32:
		v, _ := cmd.Flags().GetInt32(flagName)
		return v, nil
	case dotfilesdv1.FieldKind_FIELD_KIND_INT64, dotfilesdv1.FieldKind_FIELD_KIND_SINT64, dotfilesdv1.FieldKind_FIELD_KIND_SFIXED64:
		v, _ := cmd.Flags().GetInt64(flagName)
		return v, nil
	case dotfilesdv1.FieldKind_FIELD_KIND_UINT32, dotfilesdv1.FieldKind_FIELD_KIND_FIXED32:
		v, _ := cmd.Flags().GetUint32(flagName)
		return v, nil
	case dotfilesdv1.FieldKind_FIELD_KIND_UINT64, dotfilesdv1.FieldKind_FIELD_KIND_FIXED64:
		v, _ := cmd.Flags().GetUint64(flagName)
		return v, nil
	case dotfilesdv1.FieldKind_FIELD_KIND_DOUBLE, dotfilesdv1.FieldKind_FIELD_KIND_FLOAT:
		v, _ := cmd.Flags().GetFloat64(flagName)
		return v, nil
	case dotfilesdv1.FieldKind_FIELD_KIND_BOOL:
		v, _ := cmd.Flags().GetBool(flagName)
		return v, nil
	case dotfilesdv1.FieldKind_FIELD_KIND_ENUM:
		v, _ := cmd.Flags().GetString(flagName)
		return v, nil
	case dotfilesdv1.FieldKind_FIELD_KIND_MESSAGE:
		nested := findNestedMessageInSchema(parentMsg, fs.TypeName)
		if nested != nil {
			return buildJSONFromSchema(cmd, nested, flagName+".")
		}
		return nil, nil
	default:
		return nil, nil
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

// isStdinAvailable returns true if os.Stdin has data available to read
// (i.e. it's not a terminal or has been redirected).
func isStdinAvailable() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	// Check if stdin is a pipe or socket (not a terminal).
	return (stat.Mode() & os.ModeCharDevice) == 0
}
