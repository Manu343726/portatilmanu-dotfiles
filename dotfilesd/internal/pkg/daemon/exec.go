package daemon

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

type execServer struct {
	sessions *SessionStore
	bgTasks  *backgroundTaskManager
}

func (s *execServer) Exec(ctx context.Context, req *connect.Request[dotfilesdv1.ExecRequest]) (*connect.Response[dotfilesdv1.ExecResponse], error) {
	session := s.sessions.ResolveSession(req.Msg.GetSession())
	slog.Log(ctx, levelTrace, "Exec", "session_id", session.id, "command", req.Msg.Command, "sudo", req.Msg.Sudo)

	if req.Msg.Sudo {
		vars := session.Variables()

		// 1. Elicitation — password prompt inside the MCP client's own UI
		//    (e.g. opencode form). The agent never sees the value.
		if vars["_cap_elicitation"] == "true" && session.HasCallbackURL() {
			return s.execSudoWithPassword(ctx, req.Msg.Command, session)
		}

		// 2. Graphical auth (pkexec) — desktop password dialog, no
		//    terminal UI interference.
		if vars["_cap_graphical"] == "true" && hasPkexec() {
			return s.ExecRaw(ctx, req.Msg.Command, true, session.id)
		}

		// 3. Terminal callback — fallback for headless terminal sessions.
		if vars["_cap_terminal"] == "true" && session.HasCallbackURL() {
			return s.execSudoWithPassword(ctx, req.Msg.Command, session)
		}

		// No viable auth method — return a clear error.
		slog.Warn("Exec sudo requested but no auth method available", "session_id", session.id, "caps", vars)
		return connect.NewResponse(&dotfilesdv1.ExecResponse{
			ExitCode: -1,
			Stderr:   "sudo requires authentication but no interactive method is available (no terminal or desktop detected)",
		}), nil
	}

	if session.id == "" {
		return s.ExecRaw(ctx, req.Msg.Command, false, session.id)
	}

	shell, err := session.ensureShell()
	if err != nil {
		slog.Error("failed to ensure session shell", "session_id", session.id, "error", err)
		return connect.NewResponse(&dotfilesdv1.ExecResponse{
			ExitCode: -1,
			Stderr:   fmt.Sprintf("session shell error: %v", err),
		}), nil
	}

	stdout, stderr, exitCode := shell.Exec(req.Msg.Command, session.Variables())

	if exitCode != 0 {
		slog.Warn("Exec command failed", "session_id", session.id, "command", req.Msg.Command, "exit_code", exitCode, "stderr", truncate(stderr, 200))
	}

	resp := connect.NewResponse(&dotfilesdv1.ExecResponse{
		ExitCode: int32(exitCode),
		Stdout:   stdout,
		Stderr:   stderr,
	})

	slog.Log(ctx, levelTrace, "Exec done", "session_id", session.id, "command", req.Msg.Command, "exit_code", exitCode)
	return resp, nil
}

// ExecStream runs a command and streams stdout/stderr chunks in real time.
func (s *execServer) ExecStream(
	ctx context.Context,
	req *connect.Request[dotfilesdv1.ExecStreamRequest],
	stream *connect.ServerStream[dotfilesdv1.ExecStreamResponse],
) error {
	session := s.sessions.ResolveSession(req.Msg.GetSession())
	slog.Log(ctx, levelTrace, "ExecStream", "session_id", session.id, "command", req.Msg.Command, "sudo", req.Msg.Sudo)

	if req.Msg.Sudo {
		// For streaming sudo, we use pkexec (graphical auth) or prompt
		// for password via the session, then run with the password.
		vars := session.Variables()

		if vars["_cap_graphical"] == "true" && hasPkexec() {
			return runCmdStreamWithSudo(ctx, stream, req.Msg.Command)
		}

		if (vars["_cap_elicitation"] == "true" || vars["_cap_terminal"] == "true") && session.HasCallbackURL() {
			return s.execStreamSudoWithPassword(ctx, req.Msg.Command, session, stream)
		}

		return stream.Send(&dotfilesdv1.ExecStreamResponse{
			Done:         true,
			ExitCode:     -1,
			ErrorMessage: "sudo requires authentication but no interactive method is available",
		})
	}

	// Non-sudo streaming: use raw exec or session shell.
	if session.id == "" {
		return runCmdStream(ctx, stream, req.Msg.Command)
	}

	shell, err := session.ensureShell()
	if err != nil {
		return stream.Send(&dotfilesdv1.ExecStreamResponse{
			Done:         true,
			ExitCode:     -1,
			ErrorMessage: fmt.Sprintf("session shell error: %v", err),
		})
	}

	// Session shell: run via shell and stream line-by-line.
	return shell.ExecStream(ctx, stream, req.Msg.Command, session.Variables())
}

// execStreamSudoWithPassword prompts for the sudo password, then runs the
// command with sudo -S and streams output.
func (s *execServer) execStreamSudoWithPassword(
	ctx context.Context,
	command string,
	session *Session,
	stream *connect.ServerStream[dotfilesdv1.ExecStreamResponse],
) error {
	user := os.Getenv("USER")
	if user == "" {
		user = "unknown"
	}
	prompt := fmt.Sprintf("[sudo] password for %s: ", user)

	password, err := session.RequestInput(ctx, prompt, "", true)
	if err != nil {
		return stream.Send(&dotfilesdv1.ExecStreamResponse{
			Done:         true,
			ExitCode:     -1,
			ErrorMessage: fmt.Sprintf("password prompt failed: %v", err),
		})
	}

	pwd := []byte(password + "\n")
	defer zeroBytes(pwd)

	cmd := exec.Command("sudo", "-S", "sh", "-c", command)
	cmd.Stdin = bytes.NewReader(pwd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return stream.Send(&dotfilesdv1.ExecStreamResponse{
			Done:         true,
			ExitCode:     -1,
			ErrorMessage: fmt.Sprintf("start sudo command: %v", err),
		})
	}

	reader := bufio.NewReader(stdout)
	buf := make([]byte, 4096)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if err := stream.Send(&dotfilesdv1.ExecStreamResponse{
				StdoutChunk: chunk,
			}); err != nil {
				_ = cmd.Process.Kill()
				return err
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			_ = stream.Send(&dotfilesdv1.ExecStreamResponse{
				Done:         true,
				ExitCode:     -1,
				ErrorMessage: readErr.Error(),
			})
			_ = cmd.Process.Kill()
			return nil
		}
	}

	err = cmd.Wait()
	exitCode := int32(0)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = int32(exitErr.ExitCode())
		} else {
			exitCode = -1
		}
	}

	return stream.Send(&dotfilesdv1.ExecStreamResponse{
		Done:     true,
		ExitCode: exitCode,
	})
}

func (s *execServer) execSudoWithPassword(ctx context.Context, command string, session *Session) (*connect.Response[dotfilesdv1.ExecResponse], error) {
	slog.Log(ctx, levelTrace, "Exec sudo requesting password", "session_id", session.id)

	user := os.Getenv("USER")
	if user == "" {
		user = "unknown"
	}
	prompt := fmt.Sprintf("[sudo] password for %s: ", user)

	password, err := session.RequestInput(ctx, prompt, "", true)
	if err != nil {
		slog.Warn("Exec sudo password request failed", "session_id", session.id, "error", err)
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
		slog.Warn("Exec sudo command failed", "session_id", session.id, "command", command, "exit_code", exitCode, "stderr", truncate(stderr.String(), 200))
	}

	resp := connect.NewResponse(&dotfilesdv1.ExecResponse{
		ExitCode: int32(exitCode),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	})

	slog.Log(ctx, levelTrace, "Exec sudo done", "session_id", session.id, "command", command, "exit_code", exitCode)
	return resp, nil
}

func (s *execServer) ExecRaw(ctx context.Context, command string, sudo bool, sessionID string) (*connect.Response[dotfilesdv1.ExecResponse], error) {
	cmdStr := command
	if sudo {
		cmdStr = fmt.Sprintf("pkexec sh -c '%s'", strings.ReplaceAll(cmdStr, "'", "'\\''"))
		slog.Debug("Exec wrapping with pkexec", "session_id", sessionID, "original_command", command)
	}

	stdout, stderr, exitCode := runCmdFull("sh", "-c", cmdStr)

	if exitCode != 0 {
		slog.Warn("Exec command failed", "session_id", sessionID, "command", command, "sudo", sudo, "exit_code", exitCode, "stderr", truncate(stderr, 200))
	}

	resp := connect.NewResponse(&dotfilesdv1.ExecResponse{
		ExitCode: int32(exitCode),
		Stdout:   stdout,
		Stderr:   stderr,
	})

	slog.Log(ctx, levelTrace, "Exec done", "session_id", sessionID, "command", command, "exit_code", exitCode)
	return resp, nil
}

func (s *execServer) SudoExec(ctx context.Context, req *connect.Request[dotfilesdv1.SudoExecRequest]) (*connect.Response[dotfilesdv1.SudoExecResponse], error) {
	r := req.Msg
	session := s.sessions.ResolveSession(req.Msg.GetSession())
	password := r.Password
	method := r.PreferredMethod

	hasPassword := password != ""
	slog.Log(ctx, levelTrace, "SudoExec", "session_id", session.id, "command", r.Command, "method", method, "has_password", hasPassword)

	if hasPassword {
		if !hasSudo() {
			slog.Warn("SudoExec: sudo not available for password auth", "session_id", session.id)
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
			slog.Warn("SudoExec password auth failed", "session_id", session.id, "command", r.Command, "exit_code", exitCode, "stderr", truncate(stderr.String(), 200))
		}
		resp := connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_Result{
			Result: &dotfilesdv1.SudoResult{ExitCode: int32(exitCode), Stdout: stdout.String(), Stderr: stderr.String()},
		}})
		slog.Log(ctx, levelTrace, "SudoExec done", "session_id", session.id, "command", r.Command, "exit_code", exitCode)
		return resp, nil
	}

	// No password provided — try available methods based on session capabilities.
	vars := session.Variables()

	// 1. Elicitation — MCP client UI prompt.
	if vars["_cap_elicitation"] == "true" && session.HasCallbackURL() {
		slog.Log(ctx, levelTrace, "SudoExec delegating to secure feedback path (elicitation)", "session_id", session.id)
		execResp, err := s.execSudoWithPassword(ctx, r.Command, session)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_Result{
			Result: &dotfilesdv1.SudoResult{
				ExitCode: execResp.Msg.ExitCode,
				Stdout:   execResp.Msg.Stdout,
				Stderr:   execResp.Msg.Stderr,
			},
		}}), nil
	}

	// 2. Graphical auth (pkexec).
	if vars["_cap_graphical"] == "true" && hasPkexec() {
		return s.SudoExec(ctx, connect.NewRequest(&dotfilesdv1.SudoExecRequest{
			Command:         r.Command,
			Session:         req.Msg.Session,
			PreferredMethod: dotfilesdv1.SudoMethod_SUDO_METHOD_GRAPHICAL,
		}))
	}

	// 3. Terminal callback fallback.
	if vars["_cap_terminal"] == "true" && session.HasCallbackURL() {
		slog.Log(ctx, levelTrace, "SudoExec delegating to secure feedback path (terminal)", "session_id", session.id)
		execResp, err := s.execSudoWithPassword(ctx, r.Command, session)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_Result{
			Result: &dotfilesdv1.SudoResult{
				ExitCode: execResp.Msg.ExitCode,
				Stdout:   execResp.Msg.Stdout,
				Stderr:   execResp.Msg.Stderr,
			},
		}}), nil
	}

	switch method {
	case dotfilesdv1.SudoMethod_SUDO_METHOD_NOPASS:
		if !hasSudo() {
			slog.Warn("SudoExec: sudo not available for nopass method", "session_id", session.id)
			return connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_Result{
				Result: &dotfilesdv1.SudoResult{AuthCancelled: true, Stderr: "sudo not available"},
			}}), nil
		}
		stdout, stderr, code := runCmdFull("sudo", "-n", "sh", "-c", r.Command)
		if code != 0 {
			slog.Warn("SudoExec nopass failed", "session_id", session.id, "command", r.Command, "exit_code", code, "stderr", truncate(stderr, 200))
		}
		resp := connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_Result{
			Result: &dotfilesdv1.SudoResult{ExitCode: int32(code), Stdout: stdout, Stderr: stderr},
		}})
		slog.Log(ctx, levelTrace, "SudoExec done", "session_id", session.id, "command", r.Command, "exit_code", code)
		return resp, nil

	case dotfilesdv1.SudoMethod_SUDO_METHOD_GRAPHICAL:
		if !hasPkexec() {
			slog.Warn("SudoExec: pkexec not available for graphical method", "session_id", session.id)
			return connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_Result{
				Result: &dotfilesdv1.SudoResult{AuthCancelled: true, Stderr: "pkexec not available"},
			}}), nil
		}
		cmdStr := fmt.Sprintf("pkexec sh -c '%s'", strings.ReplaceAll(r.Command, "'", "'\\''"))
		stdout, stderr, code := runCmdFull("sh", "-c", cmdStr)
		if code == -1 {
			slog.Warn("SudoExec graphical auth cancelled or failed", "session_id", session.id, "command", r.Command, "exit_code", code)
		} else if code != 0 {
			slog.Warn("SudoExec graphical command failed", "session_id", session.id, "command", r.Command, "exit_code", code, "stderr", truncate(stderr, 200))
		}
		resp := connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_Result{
			Result: &dotfilesdv1.SudoResult{ExitCode: int32(code), Stdout: stdout, Stderr: stderr},
		}})
		slog.Log(ctx, levelTrace, "SudoExec done", "session_id", session.id, "command", r.Command, "exit_code", code)
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

	slog.Debug("SudoExec issued auth challenge", "session_id", session.id, "command", r.Command, "methods", methods)

	return connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_AuthChallenge{
		AuthChallenge: &dotfilesdv1.AuthChallenge{Methods: methods, Prompt: prompt},
	}}), nil
}

// BackgroundExec handles the bidirectional background execution stream.
// The first client message must contain a start action; after that the
// client may send stdin chunks or cancel. The server streams stdout/stderr
// chunks and a final exit event.
func (s *execServer) BackgroundExec(
	ctx context.Context,
	stream *connect.BidiStream[dotfilesdv1.BackgroundExecRequest, dotfilesdv1.BackgroundExecResponse],
) error {
	// Read the start message.
	msg, err := stream.Receive()
	if err != nil {
		return err
	}

	start := msg.GetStart()
	if start == nil {
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("first BackgroundExec message must be a start action"))
	}

	session := s.sessions.ResolveSession(msg.GetSession())
	command := start.Command
	sudo := start.Sudo

	slog.Log(ctx, levelTrace, "BackgroundExec", "session_id", session.id, "command", command, "sudo", sudo)

	var cmd *exec.Cmd

	if sudo {
		vars := session.Variables()

		if vars["_cap_graphical"] == "true" && hasPkexec() {
			cmd = exec.Command("pkexec", "sh", "-c", command)
		} else if vars["_cap_elicitation"] == "true" || vars["_cap_terminal"] == "true" {
			if !session.HasCallbackURL() {
				return connect.NewError(connect.CodeFailedPrecondition,
					fmt.Errorf("sudo requires callback URL for password prompt"))
			}
			user := os.Getenv("USER")
			if user == "" {
				user = "unknown"
			}
			prompt := fmt.Sprintf("[sudo] password for %s: ", user)
			password, err := session.RequestInput(ctx, prompt, "", true)
			if err != nil {
				return connect.NewError(connect.CodeInternal,
					fmt.Errorf("password prompt: %w", err))
			}
			pwd := []byte(password + "\n")
			defer zeroBytes(pwd)
			cmd = exec.Command("sudo", "-S", "sh", "-c", command)
			cmd.Stdin = strings.NewReader(string(pwd))
		} else {
			return connect.NewError(connect.CodeFailedPrecondition,
				fmt.Errorf("no sudo method available"))
		}
	} else {
		cmd = exec.Command("sh", "-c", command)
	}

	// Hand off to the task manager — it owns the stream from here.
	s.bgTasks.start(ctx, stream, cmd)
	return nil
}
