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
	"time"

	"dotfilesd/internal/pkg/diagnostics"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

type execServer struct {
	sessions    *SessionStore
	bgTasks     *backgroundTaskManager
	diag        *diagnostics.Engine
	sudoTimeout time.Duration
}

func (s *execServer) Exec(ctx context.Context, req *connect.Request[dotfilesdv1.ExecRequest]) (*connect.Response[dotfilesdv1.ExecResponse], error) {
	session := s.sessions.ResolveSession(req.Msg.GetSession())
	slog.Log(ctx, levelTrace, "Exec", "session_id", session.id, "command", req.Msg.Command, "sudo", req.Msg.Sudo)

	execID := fmt.Sprintf("exec:%s_%d", req.Msg.Command, time.Now().UnixNano())
	execStart := time.Now()

	// Determine exec parent: check for _diag_parent in the incoming request's
	// session variables (set by plugin SDK for plugin-to-plugin traceability).
	// Fall back to "session:<id>". We read from req.Msg directly to avoid
	// permanently mutating the stored session's variables.
	execParent := "session:" + session.id
	if sm := req.Msg.GetSession(); sm != nil {
		if dp, ok := sm.GetVariables()["_diag_parent"]; ok && dp != "" {
			execParent = dp
		}
	}

	if s.diag != nil {
		s.diag.PushEvent(diagnostics.Event{
			Type:      diagnostics.EventExecStart,
			Resource:  execID,
			Parent:    execParent,
			Timestamp: execStart,
			Message:   req.Msg.Command,
			Attrs:     map[string]string{"sudo": fmt.Sprintf("%t", req.Msg.Sudo)},
		})
	}

	// defer exec_stop so we always capture it regardless of early returns
	var execExitCode int32 = -1
	defer func() {
		if s.diag != nil {
			execDur := time.Since(execStart)
			attrs := map[string]string{
				"exit_code":   fmt.Sprintf("%d", execExitCode),
				"duration_ns": fmt.Sprintf("%d", execDur.Nanoseconds()),
			}
			s.diag.PushEvent(diagnostics.Event{
				Type:      diagnostics.EventExecStop,
				Resource:  execID,
				Parent:    execParent,
				Timestamp: time.Now(),
				Message:   req.Msg.Command,
				Attrs:     attrs,
			})
		}
	}()

	// --- start of original function body ---

	if req.Msg.Sudo {
		var timeout time.Duration
		if req.Msg.SudoTimeoutSeconds > 0 {
			timeout = time.Duration(req.Msg.SudoTimeoutSeconds) * time.Second
		}

		// Try cached sudo first (sudo -n probe, then session cache).
		if stdout, stderr, code, ok := s.tryCachedSudo(req.Msg.Command, session); ok {
			return connect.NewResponse(&dotfilesdv1.ExecResponse{
				ExitCode: int32(code),
				Stdout:   stdout,
				Stderr:   stderr,
			}), nil
		}

		vars := session.Variables()

		// 1. Elicitation — password prompt inside the MCP client's own UI
		//    (e.g. opencode form). The agent never sees the value.
		if vars["_cap_elicitation"] == "true" && session.HasCallbackURL() {
			return s.execSudoWithPassword(ctx, req.Msg.Command, session, timeout)
		}

		// 2. Graphical auth (pkexec) — desktop password dialog, no
		//    terminal UI interference.
		if vars["_cap_graphical"] == "true" && hasPkexec() {
			return s.ExecRaw(ctx, req.Msg.Command, true, session.id)
		}

		// 3. Terminal callback — fallback for headless terminal sessions.
		if vars["_cap_terminal"] == "true" && session.HasCallbackURL() {
			return s.execSudoWithPassword(ctx, req.Msg.Command, session, timeout)
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
	execExitCode = int32(exitCode)

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

	execID := fmt.Sprintf("exec:%s_%d", req.Msg.Command, time.Now().UnixNano())
	execStart := time.Now()

	// Determine exec parent: check _diag_parent in the incoming request's
	// session variables (plugin-to-plugin traceability).
	execParent := "session:" + session.id
	if sm := req.Msg.GetSession(); sm != nil {
		if dp, ok := sm.GetVariables()["_diag_parent"]; ok && dp != "" {
			execParent = dp
		}
	}

	pushStart := func() {
		if s.diag == nil {
			return
		}
		s.diag.PushEvent(diagnostics.Event{
			Type:      diagnostics.EventExecStart,
			Resource:  execID,
			Parent:    execParent,
			Timestamp: execStart,
			Message:   req.Msg.Command,
			Attrs:     map[string]string{"sudo": fmt.Sprintf("%t", req.Msg.Sudo)},
		})
	}
	pushStop := func(exitCode int32) {
		if s.diag == nil {
			return
		}
		execDur := time.Since(execStart)
		s.diag.PushEvent(diagnostics.Event{
			Type:      diagnostics.EventExecStop,
			Resource:  execID,
			Parent:    execParent,
			Timestamp: time.Now(),
			Message:   req.Msg.Command,
			Attrs: map[string]string{
				"exit_code":   fmt.Sprintf("%d", exitCode),
				"duration_ns": fmt.Sprintf("%d", execDur.Nanoseconds()),
			},
		})
	}

	if req.Msg.Sudo {
		var timeout time.Duration
		if req.Msg.SudoTimeoutSeconds > 0 {
			timeout = time.Duration(req.Msg.SudoTimeoutSeconds) * time.Second
		}

		// Try cached sudo first.
		if stdout, stderr, code, ok := s.tryCachedSudo(req.Msg.Command, session); ok {
			pushStart()
			_ = stream.Send(&dotfilesdv1.ExecStreamResponse{StdoutChunk: []byte(stdout)})
			if stderr != "" {
				_ = stream.Send(&dotfilesdv1.ExecStreamResponse{StderrChunk: []byte(stderr)})
			}
			pushStop(int32(code))
			return stream.Send(&dotfilesdv1.ExecStreamResponse{Done: true, ExitCode: int32(code)})
		}

		vars := session.Variables()

		if vars["_cap_graphical"] == "true" && hasPkexec() {
			pushStart()
			err := runCmdStreamWithSudo(ctx, stream, req.Msg.Command)
			ec := int32(-1)
			if err == nil {
				ec = 0
			}
			pushStop(ec)
			return err
		}

		if (vars["_cap_elicitation"] == "true" || vars["_cap_terminal"] == "true") && session.HasCallbackURL() {
			pushStart()
			err := s.execStreamSudoWithPassword(ctx, req.Msg.Command, session, stream, timeout)
			ec := int32(-1)
			if err == nil {
				ec = 0
			}
			pushStop(ec)
			return err
		}

		pushStart()
		pushStop(-1)
		return stream.Send(&dotfilesdv1.ExecStreamResponse{
			Done:         true,
			ExitCode:     -1,
			ErrorMessage: "sudo requires authentication but no interactive method is available",
		})
	}

	// Non-sudo streaming: use raw exec or session shell.
	if session.id == "" {
		pushStart()
		err := runCmdStream(ctx, stream, req.Msg.Command)
		ec := int32(-1)
		if err == nil {
			ec = 0
		}
		pushStop(ec)
		return err
	}

	shell, err := session.ensureShell()
	if err != nil {
		pushStart()
		pushStop(-1)
		return stream.Send(&dotfilesdv1.ExecStreamResponse{
			Done:         true,
			ExitCode:     -1,
			ErrorMessage: fmt.Sprintf("session shell error: %v", err),
		})
	}

	// Session shell: run via shell and stream line-by-line.
	pushStart()
	err = shell.ExecStream(ctx, stream, req.Msg.Command, session.Variables())
	ec := int32(-1)
	if err == nil {
		ec = 0
	}
	pushStop(ec)
	return err
}

// resolveSudoTimeout returns the effective sudo cache timeout, preferring
// a per-call override over the daemon default.
func (s *execServer) resolveSudoTimeout(reqTimeoutSec int32) time.Duration {
	if reqTimeoutSec > 0 {
		return time.Duration(reqTimeoutSec) * time.Second
	}
	return s.sudoTimeout
}

// cacheSudoAfterSuccess stores the password in the session cache on success.
// pwd is the raw password bytes (will be zeroed after use).
func (s *execServer) cacheSudoAfterSuccess(session *Session, pwd []byte, timeout time.Duration, command string, exitCode int) {
	if exitCode == 0 && len(pwd) > 0 {
		session.SetSudoCache(string(pwd), timeout)
		slog.Debug("sudo password cached", "session_id", session.id, "command", command)
	}
}

// tryCachedSudo attempts to run a command with sudo using cached credentials.
// It returns (stdout, stderr, exitCode, ok). If ok is false, the caller
// should fall through to the password prompt path.
func (s *execServer) tryCachedSudo(command string, session *Session) (string, string, int, bool) {
	// 1. Try sudo -n first (sudo's built-in credential cache, e.g. timestamp_timeout).
	stdout, stderr, code := runCmdFull("sudo", "-n", "sh", "-c", command)
	if code == 0 {
		slog.Debug("sudo -n cache hit", "session_id", session.id, "command", command)
		return stdout, stderr, code, true
	}
	slog.Log(context.TODO(), levelTrace, "sudo -n miss, trying session cache", "session_id", session.id)

	// 2. Try our session-level cache.
	cachedPwd, ok := session.GetSudoCache()
	if !ok {
		return "", "", -1, false
	}
	defer zeroBytes(cachedPwd)

	stdout, stderr, code = runCmdFullWithStdin(string(cachedPwd)+"\n", "sudo", "-S", "sh", "-c", command)
	if code == 0 {
		slog.Debug("sudo session cache hit", "session_id", session.id, "command", command)
		return stdout, stderr, code, true
	}

	// Cache entry is stale or wrong — clear it.
	slog.Warn("sudo session cache miss (stale or wrong password)", "session_id", session.id, "exit_code", code, "stderr", truncate(stderr, 200))
	session.ClearSudoCache()
	return "", "", -1, false
}

// execStreamSudoWithPassword prompts for the sudo password, then runs the
// command with sudo -S and streams output.
func (s *execServer) execStreamSudoWithPassword(
	ctx context.Context,
	command string,
	session *Session,
	stream *connect.ServerStream[dotfilesdv1.ExecStreamResponse],
	timeout time.Duration,
) error {
	// If timeout is zero, use daemon default.
	if timeout <= 0 {
		timeout = s.sudoTimeout
	}

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

	// Cache the password on success.
	if exitCode == 0 {
		session.SetSudoCache(password, timeout)
		slog.Debug("ExecStream sudo cached", "session_id", session.id)
	}

	return stream.Send(&dotfilesdv1.ExecStreamResponse{
		Done:     true,
		ExitCode: exitCode,
	})
}

func (s *execServer) execSudoWithPassword(ctx context.Context, command string, session *Session, timeout time.Duration) (*connect.Response[dotfilesdv1.ExecResponse], error) {
	// If timeout is zero, use daemon default.
	if timeout <= 0 {
		timeout = s.sudoTimeout
	}

	slog.Log(ctx, levelTrace, "Exec sudo requesting password", "session_id", session.id, "timeout", timeout)

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
	} else {
		// Cache the password on success.
		session.SetSudoCache(password, timeout)
		slog.Debug("Exec sudo cached", "session_id", session.id)
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

	timeout := s.resolveSudoTimeout(r.SudoTimeoutSeconds)

	// Decrypt encrypted_password if key_id is set.
	passwordToUse := password
	if r.KeyId != "" && len(r.EncryptedPassword) > 0 {
		key, ok := session.GetSharedKey(r.KeyId)
		if !ok {
			slog.Warn("SudoExec: shared key not found or expired", "session_id", session.id, "key_id", r.KeyId)
			return connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_Result{
				Result: &dotfilesdv1.SudoResult{AuthCancelled: true, Stderr: "encryption key not found or expired — re-negotiate key"},
			}}), nil
		}
		dec, err := decryptWithKey(r.EncryptedPassword, key)
		zeroBytes(key)
		if err != nil {
			slog.Error("SudoExec: failed to decrypt password", "session_id", session.id, "error", err)
			return connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_Result{
				Result: &dotfilesdv1.SudoResult{AuthCancelled: true, Stderr: "failed to decrypt password"},
			}}), nil
		}
		passwordToUse = string(dec)
		zeroBytes(dec)
	}

	if hasPassword || (r.KeyId != "" && len(r.EncryptedPassword) > 0) {
		if !hasSudo() {
			slog.Warn("SudoExec: sudo not available for password auth", "session_id", session.id)
			return connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_Result{
				Result: &dotfilesdv1.SudoResult{AuthCancelled: true, Stderr: "sudo not available"},
			}}), nil
		}
		var stdout, stderr strings.Builder
		cmd := exec.Command("sudo", "-S", "sh", "-c", r.Command)
		cmd.Stdin = strings.NewReader(passwordToUse + "\n")
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
		} else {
			// Cache the password on success.
			session.SetSudoCache(passwordToUse, timeout)
			slog.Debug("SudoExec cached", "session_id", session.id)
		}
		resp := connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_Result{
			Result: &dotfilesdv1.SudoResult{ExitCode: int32(exitCode), Stdout: stdout.String(), Stderr: stderr.String()},
		}})
		slog.Log(ctx, levelTrace, "SudoExec done", "session_id", session.id, "command", r.Command, "exit_code", exitCode)
		return resp, nil
	}

	// No password provided — try cached sudo first.
	if stdout, stderr, code, ok := s.tryCachedSudo(r.Command, session); ok {
		slog.Debug("SudoExec cache hit", "session_id", session.id, "command", r.Command)
		return connect.NewResponse(&dotfilesdv1.SudoExecResponse{Outcome: &dotfilesdv1.SudoExecResponse_Result{
			Result: &dotfilesdv1.SudoResult{ExitCode: int32(code), Stdout: stdout, Stderr: stderr},
		}}), nil
	}

	// Try available methods based on session capabilities.
	vars := session.Variables()

	// 1. Elicitation — MCP client UI prompt.
	if vars["_cap_elicitation"] == "true" && session.HasCallbackURL() {
		slog.Log(ctx, levelTrace, "SudoExec delegating to secure feedback path (elicitation)", "session_id", session.id)
		execResp, err := s.execSudoWithPassword(ctx, r.Command, session, timeout)
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
		execResp, err := s.execSudoWithPassword(ctx, r.Command, session, timeout)
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

	var methods []dotfilesdv1.SudoMethod
	if hasSudo() {
		methods = append(methods, dotfilesdv1.SudoMethod_SUDO_METHOD_GRAPHICAL)
	}
	if hasPkexec() {
		methods = append(methods, dotfilesdv1.SudoMethod_SUDO_METHOD_GRAPHICAL)
	}
	// Deduplicate.
	if len(methods) > 1 && methods[0] == methods[1] {
		methods = methods[:1]
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
		// Resolve timeout from per-call override.
		var timeout time.Duration
		if start.SudoTimeoutSeconds > 0 {
			timeout = time.Duration(start.SudoTimeoutSeconds) * time.Second
		}
		if timeout <= 0 {
			timeout = s.sudoTimeout
		}

		// Try cached sudo first.
		if _, _, _, ok := s.tryCachedSudo(command, session); ok {
			slog.Debug("BackgroundExec cache hit", "session_id", session.id)
			cmd = exec.Command("sudo", "-n", "sh", "-c", command)
		} else {
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
				// Cache optimistically — password was just accepted by sudo -S.
				session.SetSudoCache(password, timeout)
				slog.Debug("BackgroundExec sudo cached", "session_id", session.id)
			} else {
				return connect.NewError(connect.CodeFailedPrecondition,
					fmt.Errorf("no sudo method available"))
			}
		}
	} else {
		cmd = exec.Command("sh", "-c", command)
	}

	// Hand off to the task manager — it owns the stream from here.
	s.bgTasks.start(ctx, stream, cmd)
	return nil
}
