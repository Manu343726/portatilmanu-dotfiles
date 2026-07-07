package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

// RunScript sends a script (inline content) to the daemon for execution.
func RunScript(clients *Clients, sessionID, script string) error {
	slog.Info("script run", "session_id", sessionID, "script_len", len(script))
	req := connect.NewRequest(&dotfilesdv1.RunScriptRequest{
		Source: &dotfilesdv1.RunScriptRequest_Script{
			Script: script,
		},
		Session: sessionProto(sessionID),
	})
	resp, err := clients.Script.RunScript(context.Background(), req)
	if err != nil {
		slog.Error("script run failed", "error", err)
		return fmt.Errorf("script run failed: %w", err)
	}
	slog.Debug("script completed", "all_succeeded", resp.Msg.AllSucceeded, "steps", len(resp.Msg.Steps))
	return printScriptResult(resp.Msg)
}

// RunScriptFile reads a script file from the daemon host.
func RunScriptFile(clients *Clients, sessionID, path string) error {
	slog.Info("script file run", "path", path, "session_id", sessionID)
	req := connect.NewRequest(&dotfilesdv1.RunScriptRequest{
		Source: &dotfilesdv1.RunScriptRequest_ScriptPath{
			ScriptPath: path,
		},
		Session: sessionProto(sessionID),
	})
	resp, err := clients.Script.RunScript(context.Background(), req)
	if err != nil {
		slog.Error("script file run failed", "path", path, "error", err)
		return fmt.Errorf("script run failed: %w", err)
	}
	slog.Debug("script file completed", "path", path, "all_succeeded", resp.Msg.AllSucceeded)
	return printScriptResult(resp.Msg)
}

// RunListScripts fetches the registered script tree from the daemon and prints it.
func RunListScripts(clients *Clients, sessionID string) error {
	slog.Debug("listing scripts", "session_id", sessionID)
	req := connect.NewRequest(&dotfilesdv1.ListScriptsRequest{Session: sessionProto(sessionID)})
	resp, err := clients.Script.ListScripts(context.Background(), req)
	if err != nil {
		slog.Error("list scripts failed", "error", err)
		return fmt.Errorf("list scripts failed: %w", err)
	}
	if len(resp.Msg.Entries) == 0 {
		fmt.Println("no registered scripts found")
		return nil
	}
	printScriptEntries(resp.Msg.Entries, "")
	return nil
}

func printScriptEntries(entries []*dotfilesdv1.ScriptEntry, indent string) {
	for _, e := range entries {
		desc := e.Description
		if desc == "" {
			desc = e.Name
		}
		if e.IsDirectory {
			fmt.Printf("%s%s/\t%s\n", indent, e.Name, desc)
			printScriptEntries(e.Children, indent+"  ")
		} else {
			status := ""
			if !e.Enabled {
				status = " [disabled]"
			}
			fmt.Printf("%s%s\t%s%s\n", indent, e.Name, desc, status)
		}
	}
}

// RunRegisteredScript runs a registered script by its relative path (e.g. "git/commit").
func RunRegisteredScript(clients *Clients, sessionID, relPath string) error {
	slog.Info("registered script run", "path", relPath, "session_id", sessionID)
	req := connect.NewRequest(&dotfilesdv1.RunScriptRequest{
		Source: &dotfilesdv1.RunScriptRequest_RegisteredScript{
			RegisteredScript: relPath,
		},
		Session: sessionProto(sessionID),
	})
	resp, err := clients.Script.RunScript(context.Background(), req)
	if err != nil {
		slog.Error("registered script run failed", "path", relPath, "error", err)
		return fmt.Errorf("script run failed: %w", err)
	}
	slog.Debug("registered script completed", "path", relPath, "all_succeeded", resp.Msg.AllSucceeded)
	return printScriptResult(resp.Msg)
}

func printScriptResult(resp *dotfilesdv1.RunScriptResponse) error {
	if resp.Error != "" {
		fmt.Fprint(os.Stderr, "Script error: ", resp.Error, "\n")
	}

	for _, step := range resp.Steps {
		prefix := fmt.Sprintf("[%d]", step.StepNumber)
		switch step.StepKind {
		case dotfilesdv1.StepKind_STEP_KIND_EXEC:
			fmt.Printf("%s $ %s\n", prefix, step.SourceLine)
			if step.Stdout != "" {
				fmt.Print(step.Stdout)
			}
			if step.Stderr != "" {
				fmt.Fprint(os.Stderr, step.Stderr)
			}
			if step.ExitCode != 0 {
				fmt.Fprintf(os.Stderr, "%s exit code: %d\n", prefix, step.ExitCode)
			}
		case dotfilesdv1.StepKind_STEP_KIND_CONFIRM:
			fmt.Printf("%s @confirm %s → %s\n", prefix, step.SourceLine, step.FeedbackValue)
		case dotfilesdv1.StepKind_STEP_KIND_INPUT:
			fmt.Printf("%s @input → %s\n", prefix, step.FeedbackValue)
		case dotfilesdv1.StepKind_STEP_KIND_CHOOSE:
			fmt.Printf("%s @choose → %s\n", prefix, step.FeedbackValue)
		default:
			fmt.Printf("%s %s\n", prefix, step.SourceLine)
		}
	}

	if !resp.AllSucceeded {
		return fmt.Errorf("script completed with errors")
	}
	return nil
}
