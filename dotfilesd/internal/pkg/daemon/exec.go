package daemon

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

type execServer struct {
	sessions *SessionStore
}

func (s *execServer) Exec(ctx context.Context, req *connect.Request[dotfilesdv1.ExecRequest]) (*connect.Response[dotfilesdv1.ExecResponse], error) {
	slog.Log(ctx, levelTrace, "Exec", "command", req.Msg.Command, "sudo", req.Msg.Sudo)

	session := s.sessions.ResolveSession(req.Msg.GetSession())

	if req.Msg.Sudo {
		if session.HasCallbackURL() {
			return s.execSudoWithPassword(ctx, req.Msg.Command, session)
		}
		return s.ExecRaw(ctx, req.Msg.Command, true)
	}

	if session.id == "" {
		return s.ExecRaw(ctx, req.Msg.Command, false)
	}

	shell, err := session.ensureShell()
	if err != nil {
		slog.Error("failed to ensure session shell", "error", err)
		return connect.NewResponse(&dotfilesdv1.ExecResponse{
			ExitCode: -1,
			Stderr:   fmt.Sprintf("session shell error: %v", err),
		}), nil
	}

	stdout, stderr, exitCode := shell.Exec(req.Msg.Command, session.Variables())

	if exitCode != 0 {
		slog.Warn("Exec command failed", "command", req.Msg.Command, "exit_code", exitCode, "stderr", truncate(stderr, 200))
	}

	resp := connect.NewResponse(&dotfilesdv1.ExecResponse{
		ExitCode: int32(exitCode),
		Stdout:   stdout,
		Stderr:   stderr,
	})

	slog.Log(ctx, levelTrace, "Exec done", "command", req.Msg.Command, "exit_code", exitCode)
	return resp, nil
}

func (s *execServer) execSudoWithPassword(ctx context.Context, command string, session *Session) (*connect.Response[dotfilesdv1.ExecResponse], error) {
	slog.Log(ctx, levelTrace, "Exec sudo requesting password")

	user := os.Getenv("USER")
	if user == "" {
		user = "unknown"
	}
	prompt := fmt.Sprintf("[sudo] password for %s: ", user)

	password, err := session.RequestInput(ctx, prompt, "", true)
	if err != nil {
		slog.Warn("Exec sudo password request failed", "error", err)
		return connect.NewResponse(&dotfilesdv1.ExecResponse{
			ExitCode: -1,
			Stderr:   fmt.Sprintf("password prompt failed: %v", err),
		}), nil
	}

	// Copy password to byte slice we can zero after use.
	pwd := []byte(password + "\n")
	defer zeroBytes(pwd)

	var stdout, stderr bytes.Buffer
	cmd := exec.Command("sudo", "-S", "sh", "-c", command)
	cmd.Stdin = bytes.NewReader(pwd)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	if exitCode != 0 {
		slog.Warn("Exec sudo command failed", "command", command, "exit_code", exitCode, "stderr", truncate(stderr.String(), 200))
	}

	resp := connect.NewResponse(&dotfilesdv1.ExecResponse{
		ExitCode: int32(exitCode),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	})

	slog.Log(ctx, levelTrace, "Exec sudo done", "command", command, "exit_code", exitCode)
	return resp, nil
}

func (s *execServer) ExecRaw(ctx context.Context, command string, sudo bool) (*connect.Response[dotfilesdv1.ExecResponse], error) {
	cmdStr := command
	if sudo {
		cmdStr = fmt.Sprintf("pkexec sh -c '%s'", strings.ReplaceAll(cmdStr, "'", "'\\''"))
		slog.Debug("Exec wrapping with pkexec", "original_command", command)
	}

	stdout, stderr, exitCode := runCmdFull("sh", "-c", cmdStr)

	if exitCode != 0 {
		slog.Warn("Exec command failed", "command", command, "sudo", sudo, "exit_code", exitCode, "stderr", truncate(stderr, 200))
	}

	resp := connect.NewResponse(&dotfilesdv1.ExecResponse{
		ExitCode: int32(exitCode),
		Stdout:   stdout,
		Stderr:   stderr,
	})

	slog.Log(ctx, levelTrace, "Exec done", "command", command, "exit_code", exitCode)
	return resp, nil
}

func (s *execServer) SudoExec(ctx context.Context, req *connect.Request[dotfilesdv1.SudoExecRequest]) (*connect.Response[dotfilesdv1.SudoExecResponse], error) {
	r := req.Msg
	s.sessions.ResolveSession(req.Msg.GetSession())
	password := r.Password
	method := r.PreferredMethod

	hasPassword := password != ""
	slog.Log(ctx, levelTrace, "SudoExec", "command", r.Command, "method", method, "has_password", hasPassword)

	if hasPassword {
		if !hasSudo() {
			slog.Warn("SudoExec: sudo not available for password auth")
			return connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_Result{
				Result: &dotfilesdv1.SudoResult{AuthCancelled: true, Stderr: "sudo not available"},
			}}), nil
		}
		var stdout, stderr strings.Builder
		cmd := exec.Command("sudo", "-S", "sh", "-c", r.Command)
		cmd.Stdin = strings.NewReader(password + "\n")
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}
		if exitCode != 0 {
			slog.Warn("SudoExec password auth failed", "command", r.Command, "exit_code", exitCode, "stderr", truncate(stderr.String(), 200))
		}
		resp := connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_Result{
			Result: &dotfilesdv1.SudoResult{ExitCode: int32(exitCode), Stdout: stdout.String(), Stderr: stderr.String()},
		}})
		slog.Log(ctx, levelTrace, "SudoExec done", "command", r.Command, "exit_code", exitCode)
		return resp, nil
	}

	switch method {
	case dotfilesdv1.SudoMethod_SUDO_METHOD_NOPASS:
		if !hasSudo() {
			slog.Warn("SudoExec: sudo not available for nopass method")
			return connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_Result{
				Result: &dotfilesdv1.SudoResult{AuthCancelled: true, Stderr: "sudo not available"},
			}}), nil
		}
		stdout, stderr, code := runCmdFull("sudo", "-n", "sh", "-c", r.Command)
		if code != 0 {
			slog.Warn("SudoExec nopass failed", "command", r.Command, "exit_code", code, "stderr", truncate(stderr, 200))
		}
		resp := connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_Result{
			Result: &dotfilesdv1.SudoResult{ExitCode: int32(code), Stdout: stdout, Stderr: stderr},
		}})
		slog.Log(ctx, levelTrace, "SudoExec done", "command", r.Command, "exit_code", code)
		return resp, nil

	case dotfilesdv1.SudoMethod_SUDO_METHOD_GRAPHICAL:
		if !hasPkexec() {
			slog.Warn("SudoExec: pkexec not available for graphical method")
			return connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_Result{
				Result: &dotfilesdv1.SudoResult{AuthCancelled: true, Stderr: "pkexec not available"},
			}}), nil
		}
		cmdStr := fmt.Sprintf("pkexec sh -c '%s'", strings.ReplaceAll(r.Command, "'", "'\\''"))
		stdout, stderr, code := runCmdFull("sh", "-c", cmdStr)
		if code == -1 {
			slog.Warn("SudoExec graphical auth cancelled or failed", "command", r.Command, "exit_code", code)
		} else if code != 0 {
			slog.Warn("SudoExec graphical command failed", "command", r.Command, "exit_code", code, "stderr", truncate(stderr, 200))
		}
		resp := connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_Result{
			Result: &dotfilesdv1.SudoResult{ExitCode: int32(code), Stdout: stdout, Stderr: stderr},
		}})
		slog.Log(ctx, levelTrace, "SudoExec done", "command", r.Command, "exit_code", code)
		return resp, nil
	}

	var methods []string
	if hasSudo() {
		methods = append(methods, "terminal")
	}
	if hasPkexec() {
		methods = append(methods, "graphical")
	}
	user := os.Getenv("USER")
	if user == "" {
		user = "unknown"
	}
	prompt := fmt.Sprintf("[sudo] password for %s: ", user)

	slog.Debug("SudoExec issued auth challenge", "command", r.Command, "methods", methods)

	return connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_AuthChallenge{
		AuthChallenge: &dotfilesdv1.AuthChallenge{Methods: methods, Prompt: prompt},
	}}), nil
}
