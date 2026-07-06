package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"github.com/spf13/cobra"
	"golang.org/x/net/http2"
	"golang.org/x/term"
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
		Short:   descOr(firstLine(p.Description), disp),
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
				Short: descOr(firstLine(svc.Description), svc.Name),
				Long:  fmt.Sprintf("%s\n\n%s", svc.Name, svc.Description),
				RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
			}
			pluginCmd.AddCommand(svcCmd)
		}

		for _, m := range svc.Methods {
			runE := makeRunEProtoFromSchema(p.URL, p.DaemonURL, svc.Name, m)
			methodShort := descOr(firstLine(m.Description), fmt.Sprintf("%s.%s", shortSvcName(svc.Name), m.Name))

			enums := make(map[string]*dotfilesdv1.EnumSchema)
			if elideRPC {
				addFlagsFromSchema(svcCmd, m.Request, "", enums)
				svcCmd.RunE = runE
				if !elideSvc {
					svcCmd.Use = fmt.Sprintf("%s [flags]", svcCmd.Use)
					svcCmd.Short = methodShort
					svcCmd.Long = fmt.Sprintf("%s/%s\n\n%s", svc.Name, m.Name, m.Description)
				}
				if appendix := enumAppendix(enums); appendix != "" {
					svcCmd.Example = appendix
				}
			} else {
				rpcCmd := &cobra.Command{
					Use:   camelToKebab(m.Name),
					Short: methodShort,
					Long:  fmt.Sprintf("%s/%s\n\n%s", svc.Name, m.Name, m.Description),
					RunE:  runE,
				}
				addFlagsFromSchema(rpcCmd, m.Request, "", enums)
				if appendix := enumAppendix(enums); appendix != "" {
					rpcCmd.Example = appendix
				}
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

// seeEnumSuffix returns " (see <shortName>)" for an enum type.
func seeEnumSuffix(enumSchema *dotfilesdv1.EnumSchema) string {
	if enumSchema == nil {
		return ""
	}
	short := enumSchema.Name
	if idx := strings.LastIndex(short, "."); idx >= 0 {
		short = short[idx+1:]
	}
	return fmt.Sprintf(" (see %s)", short)
}

// enumAppendix formats all collected enums as a help appendix block.
func enumAppendix(enums map[string]*dotfilesdv1.EnumSchema) string {
	if len(enums) == 0 {
		return ""
	}
	names := make([]string, 0, len(enums))
	for name := range enums {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("Enums referenced by the flags above:\n")
	for _, name := range names {
		es := enums[name]
		short := name
		if idx := strings.LastIndex(name, "."); idx >= 0 {
			short = name[idx+1:]
		}
		desc := ""
		if es.Description != "" {
			desc = ": " + es.Description
		}
		fmt.Fprintf(&b, "  %s%s\n", short, desc)
		for _, v := range es.Values {
			if v.Description != "" {
				fmt.Fprintf(&b, "    %s  %s\n", v.Name, v.Description)
			} else {
				fmt.Fprintf(&b, "    %s\n", v.Name)
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// addFlagsFromSchema recursively adds cobra flags from a registry MessageSchema.
// enums collects referenced enum schemas keyed by fully-qualified type name.
func addFlagsFromSchema(cmd *cobra.Command, msg *dotfilesdv1.MessageSchema, prefix string, enums map[string]*dotfilesdv1.EnumSchema) {
	for _, fs := range msg.Fields {
		flagName := camelToKebab(prefix + fs.Name)
		desc := flagDescription(fs)

		// Track enum schemas for the help appendix.
		if fs.EnumSchema != nil && enums != nil {
			enums[fs.TypeName] = fs.EnumSchema
		}

		if fs.Label == dotfilesdv1.FieldLabel_FIELD_LABEL_REPEATED && fs.Kind == dotfilesdv1.FieldKind_FIELD_KIND_MESSAGE {
			cmd.Flags().StringToString(flagName, nil,
				"Repeated "+fs.TypeName+". Usage: --"+flagName+" [<idx>].<field>=<value>")
			continue
		}

		switch fs.Label {
		case dotfilesdv1.FieldLabel_FIELD_LABEL_REPEATED:
			addRepeatedFlag(cmd, flagName, fs, desc)

		default:
			addScalarFlag(cmd, flagName, fs, desc, msg, enums)
		}
	}
}

// addScalarFlag registers a single cobra flag from a FieldSchema.
func addScalarFlag(cmd *cobra.Command, flagName string, fs *dotfilesdv1.FieldSchema, desc string, parentMsg *dotfilesdv1.MessageSchema, enums map[string]*dotfilesdv1.EnumSchema) {
	fullDesc := desc
	if fs.Kind != dotfilesdv1.FieldKind_FIELD_KIND_ENUM && fs.TypeName != "" {
		fullDesc = desc + " (" + fs.TypeName + ")"
	}
	if fs.Kind == dotfilesdv1.FieldKind_FIELD_KIND_ENUM {
		fullDesc = desc + seeEnumSuffix(fs.EnumSchema)
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
			addFlagsFromSchema(cmd, nested, flagName+".", enums)
		} else {
			cmd.Flags().String(flagName, "", fullDesc+" (unknown message)")
		}

	default:
		cmd.Flags().String(flagName, "", fullDesc+" (unknown type)")
	}
}

// addRepeatedFlag registers a repeated cobra flag from a FieldSchema.
func addRepeatedFlag(cmd *cobra.Command, flagName string, fs *dotfilesdv1.FieldSchema, desc string) {
	suffix := " (repeated)"
	if fs.Kind == dotfilesdv1.FieldKind_FIELD_KIND_ENUM {
		// For enums, show " (see X)" instead of a bare type name.
		suffix = seeEnumSuffix(fs.EnumSchema) + " (repeated)"
	}
	switch fs.Kind {
	case dotfilesdv1.FieldKind_FIELD_KIND_STRING:
		cmd.Flags().StringSlice(flagName, nil, desc+suffix)
	case dotfilesdv1.FieldKind_FIELD_KIND_INT32, dotfilesdv1.FieldKind_FIELD_KIND_INT64,
		dotfilesdv1.FieldKind_FIELD_KIND_SINT32, dotfilesdv1.FieldKind_FIELD_KIND_SINT64,
		dotfilesdv1.FieldKind_FIELD_KIND_UINT32, dotfilesdv1.FieldKind_FIELD_KIND_UINT64,
		dotfilesdv1.FieldKind_FIELD_KIND_FIXED32, dotfilesdv1.FieldKind_FIELD_KIND_FIXED64,
		dotfilesdv1.FieldKind_FIELD_KIND_SFIXED32, dotfilesdv1.FieldKind_FIELD_KIND_SFIXED64:
		cmd.Flags().Int64Slice(flagName, nil, desc+suffix)
	case dotfilesdv1.FieldKind_FIELD_KIND_DOUBLE, dotfilesdv1.FieldKind_FIELD_KIND_FLOAT:
		cmd.Flags().Float64Slice(flagName, nil, desc+suffix)
	default:
		cmd.Flags().StringSlice(flagName, nil, desc+suffix)
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

		// Inject terminal dimensions into the request body for interactive
		// TUI methods (e.g. tuidiag's Watch). The plugin uses these to
		// size the PTY correctly from the start.
		if m.NeedsInteractiveStdin && term.IsTerminal(int(os.Stdin.Fd())) {
			if w, h, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
				if _, ok := body["terminalWidth"]; !ok {
					body["terminalWidth"] = int32(w)
				}
				if _, ok := body["terminalHeight"]; !ok {
					body["terminalHeight"] = int32(h)
				}
			}
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
		// Use a very long timeout so interactive TUI games work without
		// being killed mid-game. The caller (PersistentPostRunE) cleans
		// up after the command completes.
		ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
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

		// Set terminal to raw mode for interactive input when the method
		// needs it (e.g. TUI games, TUI diagnostics). For non-interactive
		// methods (the common case), stdin forwarding is skipped entirely
		// so the command exits cleanly when the plugin response arrives.
		var (
			oldTermState *term.State
			stdinDone    chan struct{}
		)

		if m.NeedsInteractiveStdin && term.IsTerminal(int(os.Stdin.Fd())) {
			s, err := term.MakeRaw(int(os.Stdin.Fd()))
			if err == nil {
				oldTermState = s
			}
		}
		defer func() {
			if oldTermState != nil {
				_ = term.Restore(int(os.Stdin.Fd()), oldTermState)
				oldTermState = nil
			}
		}()

		// Forward SIGWINCH (terminal resize) to the plugin's PTY.
		sigwinch := make(chan os.Signal, 1)
		if m.NeedsInteractiveStdin && oldTermState != nil {
			signal.Notify(sigwinch, syscall.SIGWINCH)
		}
		defer signal.Stop(sigwinch)
		go func() {
			for range sigwinch {
				w, h, err := term.GetSize(int(os.Stdin.Fd()))
				if err != nil {
					continue
				}
				_ = stream.Send(&dotfilesdv1.CallPluginMessage{
					WindowSize: &dotfilesdv1.WindowSize{
						Width:  int32(w),
						Height: int32(h),
					},
				})
			}
		}()

		if m.NeedsInteractiveStdin {
			// Forward local stdin to the plugin in a background goroutine.
			// No \r→\n conversion — TUI plugins (tview) need raw bytes,
			// and line-based plugins (games) handle this via the plugin
			// SDK's ReadStdin path which converts \r→\n internally.
			stdinDone = make(chan struct{}, 1)
			go func() {
				defer func() { stdinDone <- struct{}{} }()
				buf := make([]byte, 4096)
				for {
					n, err := os.Stdin.Read(buf)
					if n > 0 {
						data := buf[:n]

						// In raw mode, Ctrl+C (0x03) is sent as a regular byte
						// instead of generating SIGINT. Catch it here and exit.
						if bytes.Contains(data, []byte{0x03}) {
							if oldTermState != nil {
								_ = term.Restore(int(os.Stdin.Fd()), oldTermState)
								oldTermState = nil
							}
							os.Exit(130)
						}

						if err := stream.Send(&dotfilesdv1.CallPluginMessage{
							StdinChunk: data,
						}); err != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}()
		}

		// Receive streaming response.
		var streamErr error
	loop:
		for {
			msg, err := stream.Receive()
			if err != nil {
				// EOF is normal — the plugin's RPC returned successfully
				// and closed the stream. Treat it as a clean exit.
				if !errors.Is(err, io.EOF) {
					streamErr = err
				}
				break loop
			}
			if msg.Error != "" {
				streamErr = fmt.Errorf("plugin error: %s", msg.Error)
				break loop
			}
			if len(msg.StdoutChunk) > 0 {
				// No \n→\r\n conversion — PTY output from tview already
				// uses proper line endings for raw terminal mode.
				fmt.Print(string(msg.StdoutChunk))
			}
			if len(msg.StderrChunk) > 0 {
				out := msg.StderrChunk
				if oldTermState != nil {
					out = bytes.ReplaceAll(out, []byte{'\n'}, []byte{'\r', '\n'})
				}
				fmt.Fprint(os.Stderr, string(out))
			}
			// With --json, the plugin skips rendering and the CLI prints the
			// raw response body as prettified JSON instead.
			if len(msg.ResponseBody) > 0 && jsonOutput {
				var buf bytes.Buffer
				if err := json.Indent(&buf, msg.ResponseBody, "", "  "); err != nil {
					fmt.Println(string(msg.ResponseBody))
				} else {
					fmt.Println(buf.String())
				}
			}
			// Without --json, the plugin is responsible for writing output to
			// pc.Stdout() which arrives as StdoutChunks above. ResponseBody
			// is not printed — it's only used for JSON/programmatic access.
		}

		// Clean up: close the stream, restore terminal, kill stdin goroutine.
		_ = stream.CloseRequest()

		// Restore terminal BEFORE closing stdin (needs a valid fd).
		// Also print a newline to separate error output from the TUI content.
		if oldTermState != nil {
			_ = term.Restore(int(os.Stdin.Fd()), oldTermState)
			fmt.Fprintln(os.Stderr)
			oldTermState = nil // prevent defer from running again
		}

		// Immediately kill the stdin goroutine by closing stdin.
		// Without this, the goroutine blocks on os.Stdin.Read() and the
		// CLI won't exit until the user presses keys that drain through.
		if stdinDone != nil {
			os.Stdin.Close()
			<-stdinDone
		}

		// Log a detailed error if the stream failed, so the user sees
		// what happened (e.g. daemon restart, connection lost) instead
		// of a broken TUI display with mixed error text.
		if streamErr != nil {
			slog.Error("plugin call failed", "plugin", svcName, "method", m.Name, "error", streamErr)
			fmt.Fprintf(os.Stderr, "error: %v\n", streamErr)
			return streamErr
		}

		return nil
	}
}
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
		fieldName := fs.Name

		if fs.Kind == dotfilesdv1.FieldKind_FIELD_KIND_MESSAGE && fs.Label != dotfilesdv1.FieldLabel_FIELD_LABEL_REPEATED {
			// For message fields, recurse even if the parent flag wasn't
			// directly set — nested flags (e.g. --filter.status) set the
			// child flags, not the parent. The recursive call checks each
			// nested flag individually.
			nested := findNestedMessageInSchema(msg, fs.TypeName)
			if nested != nil {
				sub, err := buildJSONFromSchema(cmd, nested, flagName+".")
				if err != nil {
					return nil, fmt.Errorf("field %q: %w", fieldName, err)
				}
				if len(sub) > 0 {
					result[fieldName] = sub
				}
			}
			continue
		}

		if !cmd.Flags().Changed(flagName) {
			continue
		}

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
	if s == "" {
		return ""
	}
	runes := []rune(s)
	var result strings.Builder
	n := len(runes)

	for i, r := range runes {
		if r == '_' {
			result.WriteRune('-')
			continue
		}
		if r >= 'A' && r <= 'Z' {
			// Insert hyphen before uppercase that follows lowercase
			// or before the last uppercase in an acronym when followed by lowercase.
			if i > 0 {
				prevLower := runes[i-1] >= 'a' && runes[i-1] <= 'z'
				next := rune(0)
				if i+1 < n {
					next = runes[i+1]
				}
				nextLower := next >= 'a' && next <= 'z'
				if prevLower || (nextLower && i+1 < n) {
					result.WriteRune('-')
				}
			}
			result.WriteRune(r - 'A' + 'a')
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// firstLine returns the first line of s, or s itself if there's only one line.
func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
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

// descOr returns desc if non-empty, otherwise fallback.
func descOr(desc, fallback string) string {
	if desc != "" {
		return desc
	}
	return fallback
}

// flagDescription builds a human-readable description for a field schema flag.
// Uses the field's description when available, with the type name appended.
func flagDescription(fs *dotfilesdv1.FieldSchema) string {
	// For enum fields the type info is conveyed by the "see X enum below"
	// suffix added by addScalarFlag/addRepeatedFlag — never show it here.
	if fs.Kind == dotfilesdv1.FieldKind_FIELD_KIND_ENUM {
		if fs.Description != "" {
			return fs.Description
		}
		return fs.Name
	}
	if fs.Description != "" {
		if fs.TypeName != "" {
			return fs.Description + " (" + fs.TypeName + ")"
		}
		return fs.Description
	}
	if fs.TypeName != "" {
		return fs.Name + " (" + fs.TypeName + ")"
	}
	return fs.Name
}
