package daemon

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	"dotfilesd/internal/pkg/diagnostics"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

// scriptStep describes one parsed step from a script file.
type scriptStep struct {
	kind       string   // "exec", "confirm", "input", "choose"
	rawLine    string   // original source line for result reporting
	command    string   // shell command (exec only)
	message    string   // prompt text (confirm/input/choose)
	varName    string   // shell variable to export feedback value into
	options    []string // options list (choose only)
	defaultIdx int      // default option index (choose only)
}

// ScriptRunner parses and executes .dsh (dotfiles script) files.
type ScriptRunner struct {
	store    *SessionStore
	registry *ScriptRegistry
	diag     *diagnostics.Engine
}

func NewScriptRunner(store *SessionStore, registry *ScriptRegistry) *ScriptRunner {
	return &ScriptRunner{store: store, registry: registry}
}

// SetDiagEngine configures the diagnostics engine for script events.
func (r *ScriptRunner) SetDiagEngine(eng *diagnostics.Engine) {
	r.diag = eng
}

// ListScripts returns the registered script tree.
func (r *ScriptRunner) ListScripts(ctx context.Context, req *connect.Request[dotfilesdv1.ListScriptsRequest]) (*connect.Response[dotfilesdv1.ListScriptsResponse], error) {
	slog.Debug("ScriptService.ListScripts")
	r.store.ResolveSession(req.Msg.GetSession())

	entries, err := r.registry.ListScripts()
	if err != nil {
		return connect.NewResponse(&dotfilesdv1.ListScriptsResponse{}), nil
	}
	return connect.NewResponse(&dotfilesdv1.ListScriptsResponse{Entries: entries}), nil
}

// RunScript parses the script, executes every step in order and returns the
// combined result. Each step runs in the session's persistent shell so that
// variables set by @input / @choose are available to later commands.
func (r *ScriptRunner) RunScript(ctx context.Context, req *connect.Request[dotfilesdv1.RunScriptRequest]) (*connect.Response[dotfilesdv1.RunScriptResponse], error) {
	rpcReq := req.Msg
	session := r.store.ResolveSession(req.Msg.GetSession())
	slog.Debug("ScriptService.RunScript", "session_id", session.id)
	if session == nil {
		return connect.NewResponse(&dotfilesdv1.RunScriptResponse{
			AllSucceeded: false,
			Error:        "no session available",
		}), nil
	}

	// Ensure session has a shell.
	shell, err := session.ensureShell()
	if err != nil {
		slog.Error("failed to ensure session shell", "error", err)
		return connect.NewResponse(&dotfilesdv1.RunScriptResponse{
			AllSucceeded: false,
			Error:        fmt.Sprintf("session shell error: %v", err),
		}), nil
	}

	// Read script content.
	var script string
	switch src := rpcReq.Source.(type) {
	case *dotfilesdv1.RunScriptRequest_Script:
		script = src.Script
	case *dotfilesdv1.RunScriptRequest_ScriptPath:
		data, err := os.ReadFile(src.ScriptPath)
		if err != nil {
			slog.Error("read script file", "path", src.ScriptPath, "error", err)
			return connect.NewResponse(&dotfilesdv1.RunScriptResponse{
				AllSucceeded: false,
				Error:        fmt.Sprintf("read script file: %v", err),
			}), nil
		}
		script = string(data)
	case *dotfilesdv1.RunScriptRequest_RegisteredScript:
		content, _, err := r.registry.ReadScriptContent(src.RegisteredScript)
		if err != nil {
			slog.Error("registered script not found", "name", src.RegisteredScript, "error", err)
			return connect.NewResponse(&dotfilesdv1.RunScriptResponse{
				AllSucceeded: false,
				Error:        fmt.Sprintf("registered script not found: %v", err),
			}), nil
		}
		script = content
	default:
		return connect.NewResponse(&dotfilesdv1.RunScriptResponse{
			AllSucceeded: false,
			Error:        "no script source provided (use 'script' or 'script_path')",
		}), nil
	}

	// Parse.
	steps, err := parseScript(script)
	if err != nil {
		return connect.NewResponse(&dotfilesdv1.RunScriptResponse{
			AllSucceeded: false,
			Error:        fmt.Sprintf("parse error: %v", err),
		}), nil
	}

	// Determine a human-readable label for this script.
	scriptLabel := "script"
	switch src := rpcReq.Source.(type) {
	case *dotfilesdv1.RunScriptRequest_RegisteredScript:
		scriptLabel = src.RegisteredScript
	case *dotfilesdv1.RunScriptRequest_ScriptPath:
		// Extract a short name from the path (last path component).
		if idx := strings.LastIndexByte(src.ScriptPath, '/'); idx >= 0 {
			scriptLabel = src.ScriptPath[idx+1:]
		} else {
			scriptLabel = src.ScriptPath
		}
	}
	scriptID := "script:" + scriptLabel + "_" + fmt.Sprintf("%x", time.Now().UnixNano())
	scriptStart := time.Now()

	// Push script_start.
	if r.diag != nil {
		r.diag.PushEvent(diagnostics.Event{
			Type:      diagnostics.EventScriptStart,
			Resource:  scriptID,
			Parent:    "session:" + session.id,
			Timestamp: scriptStart,
			Message:   scriptLabel,
		})
	}

	// Execute.
	var results []*dotfilesdv1.StepResult
	allOK := true

	for i, step := range steps {
		slog.Log(ctx, levelTrace, "script step", "step", i, "kind", step.kind, "line", truncate(step.rawLine, 120))

		result := &dotfilesdv1.StepResult{
			StepNumber: int32(i + 1),
			SourceLine: step.rawLine,
			StepKind:   stepKindFromString(step.kind),
		}

		switch step.kind {
		case "exec":
			execID := "exec:" + step.command + "_" + fmt.Sprintf("%x", time.Now().UnixNano())
			execStart := time.Now()

			if r.diag != nil {
				r.diag.PushEvent(diagnostics.Event{
					Type:      diagnostics.EventExecStart,
					Resource:  execID,
					Parent:    scriptID,
					Timestamp: execStart,
					Message:   step.command,
				})
			}

			stdout, stderr, exitCode := shell.Exec(step.command, session.Variables())

			if r.diag != nil {
				execDur := time.Since(execStart)
				r.diag.PushEvent(diagnostics.Event{
					Type:      diagnostics.EventExecStop,
					Resource:  execID,
					Parent:    scriptID,
					Timestamp: time.Now(),
					Message:   step.command,
					Attrs: map[string]string{
						"exit_code":   fmt.Sprintf("%d", exitCode),
						"duration_ns": fmt.Sprintf("%d", execDur.Nanoseconds()),
					},
				})
			}

			result.Stdout = stdout
			result.Stderr = stderr
			result.ExitCode = int32(exitCode)
			if exitCode != 0 {
				allOK = false
			}

		case "confirm":
			if !session.HasCallbackURL() {
				result.ExitCode = -1
				result.Stderr = "no callback URL available for feedback"
				allOK = false
				break
			}
			defaultVal := false
			confirmed, err := session.RequestConfirm(ctx, step.message, defaultVal)
			if err != nil {
				result.ExitCode = -1
				result.Stderr = fmt.Sprintf("confirm request failed: %v", err)
				allOK = false
				break
			}
			val := "false"
			if confirmed {
				val = "true"
			}
			result.FeedbackValue = val
			result.Stdout = val + "\n"
			if step.varName != "" {
				shell.Exec(fmt.Sprintf("%s=%s", step.varName, val), nil)
			}

		case "input":
			if !session.HasCallbackURL() {
				result.ExitCode = -1
				result.Stderr = "no callback URL available for feedback"
				allOK = false
				break
			}
			val, err := session.RequestInput(ctx, step.message, "", false)
			if err != nil {
				result.ExitCode = -1
				result.Stderr = fmt.Sprintf("input request failed: %v", err)
				allOK = false
				break
			}
			result.FeedbackValue = val
			result.Stdout = val + "\n"
			if step.varName != "" {
				// Export as a shell variable for subsequent commands.
				shell.Exec(fmt.Sprintf("%s=%q", step.varName, val), nil)
			}

		case "choose":
			if !session.HasCallbackURL() {
				result.ExitCode = -1
				result.Stderr = "no callback URL available for feedback"
				allOK = false
				break
			}
			idx, option, err := session.RequestChoose(ctx, step.message, step.options, step.defaultIdx)
			if err != nil {
				result.ExitCode = -1
				result.Stderr = fmt.Sprintf("choose request failed: %v", err)
				allOK = false
				break
			}
			if idx < 0 {
				result.ExitCode = -1
				result.Stderr = "user cancelled choice"
				allOK = false
				break
			}
			result.FeedbackValue = option
			result.Stdout = option + "\n"
			if step.varName != "" {
				shell.Exec(fmt.Sprintf("%s=%q", step.varName, option), nil)
			}

		default:
			result.ExitCode = -1
			result.Stderr = fmt.Sprintf("unknown step kind: %s", step.kind)
			allOK = false
		}

		results = append(results, result)
	}

	errMsg := ""
	if !allOK {
		errMsg = "one or more steps failed"
	}

	if r.diag != nil {
		r.diag.PushEvent(diagnostics.Event{
			Type:      diagnostics.EventScriptStop,
			Resource:  scriptID,
			Parent:    "session:" + session.id,
			Timestamp: time.Now(),
			Message:   scriptLabel,
			Attrs: map[string]string{
				"steps":       fmt.Sprintf("%d", len(steps)),
				"all_ok":      fmt.Sprintf("%t", allOK),
				"duration_ns": fmt.Sprintf("%d", time.Since(scriptStart).Nanoseconds()),
			},
		})
	}

	return connect.NewResponse(&dotfilesdv1.RunScriptResponse{
		Steps:        results,
		AllSucceeded: allOK,
		Error:        errMsg,
	}), nil
}

// ---- Parser -----------------------------------------------------------------

// directiveRe matches @confirm, @input, @choose directives.
var directiveRe = regexp.MustCompile(`^@(\w+)\s+(.*)`)

// parseScript parses a script string into a list of steps.
//
// Syntax:
//
//	# comment (ignored)
//	<empty line> (ignored)
//	shell command              -> exec step
//	@confirm "message"         -> confirm step (var: $_confirm)
//	@input "prompt"            -> input step (var: $_input)
//	@input "prompt" as VAR     -> input step (var: VAR)
//	@choose "p" "o1" "o2" ... -> choose step (var: $_choose)
//	@choose "p" "o1" ... as V -> choose step (var: V)
func parseScript(content string) ([]scriptStep, error) {
	var steps []scriptStep
	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if !strings.HasPrefix(line, "@") {
			// Plain shell command.
			steps = append(steps, scriptStep{
				kind:    "exec",
				rawLine: line,
				command: line,
			})
			continue
		}

		// Directive.
		step, err := parseDirective(line, lineNum)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}

	return steps, scanner.Err()
}

// parseDirective parses one @-prefixed line.
func parseDirective(line string, lineNum int) (scriptStep, error) {
	matches := directiveRe.FindStringSubmatch(line)
	if matches == nil {
		return scriptStep{}, fmt.Errorf("line %d: malformed directive: %s", lineNum, line)
	}

	directive := matches[1]
	args := matches[2]

	switch directive {
	case "confirm":
		msg, err := extractQuotedString(args)
		if err != nil {
			return scriptStep{}, fmt.Errorf("line %d: @confirm: %w", lineNum, err)
		}
		return scriptStep{
			kind:    "confirm",
			rawLine: line,
			message: msg,
			varName: "_confirm",
		}, nil

	case "input":
		msg, rest, err := extractFirstQuoted(args)
		if err != nil {
			return scriptStep{}, fmt.Errorf("line %d: @input: %w", lineNum, err)
		}
		varName := "_input"
		if rest != "" {
			// Expect: as VARNAME
			rest = strings.TrimSpace(rest)
			if strings.HasPrefix(strings.ToLower(rest), "as ") {
				varName = strings.TrimSpace(rest[3:])
			}
		}
		return scriptStep{
			kind:    "input",
			rawLine: line,
			message: msg,
			varName: varName,
		}, nil

	case "choose":
		// Extract quoted arguments until "as VAR" or end.
		msg, options, varName, err := parseChooseArgs(args)
		if err != nil {
			return scriptStep{}, fmt.Errorf("line %d: @choose: %w", lineNum, err)
		}
		if len(options) == 0 {
			return scriptStep{}, fmt.Errorf("line %d: @choose: at least one option required", lineNum)
		}
		return scriptStep{
			kind:       "choose",
			rawLine:    line,
			message:    msg,
			options:    options,
			defaultIdx: 0,
			varName:    varName,
		}, nil

	default:
		return scriptStep{}, fmt.Errorf("line %d: unknown directive @%s", lineNum, directive)
	}
}

// ---- Server adapter --------------------------------------------------------

type scriptServer struct {
	runner *ScriptRunner
}

func newScriptServer(store *SessionStore, registry *ScriptRegistry) *scriptServer {
	runner := NewScriptRunner(store, registry)
	return &scriptServer{runner: runner}
}

// SetDiagEngine passes the diagnostics engine through to the runner.
func (s *scriptServer) SetDiagEngine(eng *diagnostics.Engine) {
	s.runner.SetDiagEngine(eng)
}

func (s *scriptServer) RunScript(ctx context.Context, req *connect.Request[dotfilesdv1.RunScriptRequest]) (*connect.Response[dotfilesdv1.RunScriptResponse], error) {
	return s.runner.RunScript(ctx, req)
}

func (s *scriptServer) ListScripts(ctx context.Context, req *connect.Request[dotfilesdv1.ListScriptsRequest]) (*connect.Response[dotfilesdv1.ListScriptsResponse], error) {
	return s.runner.ListScripts(ctx, req)
}

// ---- Argument extraction helpers -------------------------------------------

// extractQuotedString extracts one double-quoted string from s. The entire
// input must be a single quoted string.
func extractQuotedString(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '"' {
		return "", fmt.Errorf("expected quoted string, got: %s", s)
	}
	var buf strings.Builder
	i := 1
	found := false
	for i < len(s) {
		ch := s[i]
		if ch == '"' {
			i++
			found = true
			// Allow trailing content after closing quote.
			break
		}
		if ch == '\\' && i+1 < len(s) {
			i++
			buf.WriteByte(s[i])
		} else {
			buf.WriteByte(ch)
		}
		i++
	}
	if !found {
		return "", fmt.Errorf("unclosed quoted string")
	}
	return buf.String(), nil
}

// extractFirstQuoted extracts the first double-quoted string from s and
// returns the remainder (after the closing quote).
func extractFirstQuoted(s string) (string, string, error) {
	s = strings.TrimSpace(s)
	idx := strings.IndexByte(s, '"')
	if idx < 0 {
		return "", "", fmt.Errorf("expected quoted string, got: %s", s)
	}
	rest := s[idx:]
	val, err := extractQuotedString(rest)
	if err != nil {
		return "", "", err
	}
	// Find end of this quoted string.
	endIdx := idx + 2 // opening quote
	for endIdx < len(s) {
		if s[endIdx] == '"' {
			endIdx++
			// Skip escaped quotes.
			if endIdx < len(s) && s[endIdx] == '"' {
				continue
			}
			break
		}
		if s[endIdx] == '\\' {
			endIdx++ // skip next char
		}
		endIdx++
	}
	remainder := strings.TrimSpace(s[endIdx:])
	return val, remainder, nil
}

// parseChooseArgs parses: "prompt" "opt1" "opt2" ... [as VARNAME]
func parseChooseArgs(args string) (prompt string, options []string, varName string, err error) {
	args = strings.TrimSpace(args)

	// Extract prompt (first quoted string).
	prompt, rest, err := extractFirstQuoted(args)
	if err != nil {
		return "", nil, "", err
	}

	// Extract option strings until "as" or end.
	rest = strings.TrimSpace(rest)
	for rest != "" {
		// Check for "as VARNAME" terminator.
		lower := strings.ToLower(rest)
		if strings.HasPrefix(lower, "as ") {
			varName = strings.TrimSpace(rest[3:])
			rest = ""
			break
		}

		val, rem, err := extractFirstQuoted(rest)
		if err != nil {
			break // no more quoted strings
		}
		options = append(options, val)
		rest = strings.TrimSpace(rem)
	}

	if varName == "" {
		varName = "_choose"
	}

	return prompt, options, varName, nil
}

func stepKindFromString(s string) dotfilesdv1.StepKind {
	switch s {
	case "exec":
		return dotfilesdv1.StepKind_STEP_KIND_EXEC
	case "confirm":
		return dotfilesdv1.StepKind_STEP_KIND_CONFIRM
	case "input":
		return dotfilesdv1.StepKind_STEP_KIND_INPUT
	case "choose":
		return dotfilesdv1.StepKind_STEP_KIND_CHOOSE
	default:
		return dotfilesdv1.StepKind_STEP_KIND_UNSPECIFIED
	}
}
