package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

// --- SystemService ---------------------------------------------------------

type systemServer struct {
	mu        sync.Mutex
	startedAt time.Time
}

func (s *systemServer) Ping(ctx context.Context, req *connect.Request[dotfilesdv1.PingRequest]) (*connect.Response[dotfilesdv1.PingResponse], error) {
	slog.Log(ctx, levelTrace, "Ping", "request", req.Msg)

	resp := connect.NewResponse(&dotfilesdv1.PingResponse{
		Version:    "0.1.0",
		Pid:        int64(os.Getpid()),
		UptimeSecs: int64(time.Since(s.startedAt).Seconds()),
	})

	slog.Log(ctx, levelTrace, "Ping done", "response", resp.Msg)
	return resp, nil
}

func (s *systemServer) SystemInfo(ctx context.Context, req *connect.Request[dotfilesdv1.SystemInfoRequest]) (*connect.Response[dotfilesdv1.SystemInfoResponse], error) {
	slog.Log(ctx, levelTrace, "SystemInfo", "request", req.Msg)

	kernel, _ := runCmd("uname", "-r")
	shell := os.Getenv("SHELL")
	desktop := os.Getenv("XDG_CURRENT_DESKTOP")
	tmuxVer, _ := runCmd("tmux", "-V")
	kittyVer, _ := runCmd("kitty", "--version")
	i3Ver, _ := runCmd("i3", "--version")

	memTotal, _ := runCmd("awk", "/^MemTotal:/ {print $2}", "/proc/meminfo")
	memAvail, _ := runCmd("awk", "/^MemAvailable:/ {print $2}", "/proc/meminfo")
	load1, _ := runCmd("awk", "{print $1}", "/proc/loadavg")

	var memTotalKb, memAvailKb int64
	var cpuLoad float64
	fmt.Sscanf(strings.TrimSpace(memTotal), "%d", &memTotalKb)
	fmt.Sscanf(strings.TrimSpace(memAvail), "%d", &memAvailKb)
	fmt.Sscanf(strings.TrimSpace(load1), "%f", &cpuLoad)

	resp := connect.NewResponse(&dotfilesdv1.SystemInfoResponse{
		Os:            "linux",
		Kernel:        strings.TrimSpace(kernel),
		Shell:         shell,
		Desktop:       desktop,
		MemoryTotalKb: memTotalKb,
		MemoryAvailKb: memAvailKb,
		CpuLoad_1M:    cpuLoad,
		TmuxVersion:   strings.TrimSpace(tmuxVer),
		KittyVersion:  strings.TrimSpace(kittyVer),
		I3Version:     strings.TrimSpace(i3Ver),
	})

	slog.Log(ctx, levelTrace, "SystemInfo done", "response", resp.Msg)
	return resp, nil
}

func (s *systemServer) SudoMethods(ctx context.Context, req *connect.Request[dotfilesdv1.SudoMethodsRequest]) (*connect.Response[dotfilesdv1.SudoMethodsResponse], error) {
	slog.Log(ctx, levelTrace, "SudoMethods", "request", req.Msg)

	var available []string
	for _, name := range []string{"pkexec", "sudo"} {
		if _, err := exec.LookPath(name); err == nil {
			available = append(available, name)
		}
	}
	current := "auto"
	if _, err := exec.LookPath("pkexec"); err == nil {
		current = "pkexec"
	}

	resp := connect.NewResponse(&dotfilesdv1.SudoMethodsResponse{
		AvailableMethods: available,
		CurrentMethod:    current,
		HasElevation:     len(available) > 0,
	})

	slog.Log(ctx, levelTrace, "SudoMethods done", "response", resp.Msg)
	return resp, nil
}

// --- DotfilesService -------------------------------------------------------

type dotfilesServer struct{}

func (s *dotfilesServer) Status(ctx context.Context, req *connect.Request[dotfilesdv1.StatusRequest]) (*connect.Response[dotfilesdv1.StatusResponse], error) {
	slog.Log(ctx, levelTrace, "Dotfiles.Status", "request", req.Msg)

	home := os.Getenv("HOME")
	hostname, _ := os.Hostname()
	uptimeRaw, _ := runCmd("uptime", "-p")

	gitBranch, _ := runCmd("git", "-C", home, "rev-parse", "--abbrev-ref", "HEAD")
	gitLog, _ := runCmd("git", "-C", home, "log", "--oneline", "-1")
	gitStatus, _ := runCmd("git", "-C", home, "status", "--porcelain")

	gitClean := strings.TrimSpace(gitStatus) == ""
	gitBranch = strings.TrimSpace(gitBranch)
	gitLog = strings.TrimSpace(gitLog)

	resp := connect.NewResponse(&dotfilesdv1.StatusResponse{
		GitClean:   gitClean,
		GitBranch:  gitBranch,
		LastCommit: gitLog,
		Uptime:     strings.TrimSpace(uptimeRaw),
		Hostname:   strings.TrimSpace(hostname),
	})

	slog.Log(ctx, levelTrace, "Dotfiles.Status done", "response", resp.Msg)
	return resp, nil
}

func (s *dotfilesServer) Git(ctx context.Context, req *connect.Request[dotfilesdv1.GitRequest]) (*connect.Response[dotfilesdv1.GitResponse], error) {
	slog.Log(ctx, levelTrace, "Dotfiles.Git", "action", req.Msg.Action, "paths", req.Msg.Paths)

	home := os.Getenv("HOME")
	action := req.Msg.Action

	var args []string
	switch action {
	case "status":
		args = []string{"-C", home, "status"}
	case "diff":
		args = []string{"-C", home, "diff"}
	case "add":
		if req.Msg.Paths != "" {
			args = append([]string{"-C", home, "add"}, strings.Fields(req.Msg.Paths)...)
		} else {
			args = []string{"-C", home, "add", "-A"}
		}
	case "commit":
		if req.Msg.Message == "" {
			resp := connect.NewResponse(&dotfilesdv1.GitResponse{ExitCode: 1, Stderr: "commit message required"})
			slog.Log(ctx, levelTrace, "Dotfiles.Git done", "response", resp.Msg)
			return resp, nil
		}
		args = []string{"-C", home, "commit", "-m", req.Msg.Message}
	case "push":
		args = []string{"-C", home, "push"}
	case "log":
		args = []string{"-C", home, "log", "--oneline", "-10"}
	default:
		resp := connect.NewResponse(&dotfilesdv1.GitResponse{
			ExitCode: 1,
			Stderr:   fmt.Sprintf("unknown action: %s", action),
		})
		slog.Log(ctx, levelTrace, "Dotfiles.Git done", "response", resp.Msg)
		return resp, nil
	}

	stdout, stderr, code := runCmdFull("git", args...)
	resp := connect.NewResponse(&dotfilesdv1.GitResponse{
		ExitCode: int32(code),
		Stdout:   stdout,
		Stderr:   stderr,
	})

	slog.Log(ctx, levelTrace, "Dotfiles.Git done", "action", action, "exit_code", code, "stderr_truncated", truncate(stderr, 200))
	return resp, nil
}

// --- ExecService -----------------------------------------------------------

type execServer struct{}

func (s *execServer) Exec(ctx context.Context, req *connect.Request[dotfilesdv1.ExecRequest]) (*connect.Response[dotfilesdv1.ExecResponse], error) {
	slog.Log(ctx, levelTrace, "Exec", "command", req.Msg.Command, "sudo", req.Msg.Sudo)
	return s.ExecRaw(ctx, req.Msg.Command, req.Msg.Sudo)
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
	case "nopass":
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

	case "graphical":
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

// --- ConfigService ---------------------------------------------------------

type configServer struct{}

func (s *configServer) Reload(ctx context.Context, req *connect.Request[dotfilesdv1.ReloadRequest]) (*connect.Response[dotfilesdv1.ReloadResponse], error) {
	slog.Log(ctx, levelTrace, "Config.Reload", "target", req.Msg.Target)

	target := req.Msg.Target

	type result struct {
		target  string
		success bool
		message string
	}
	var results []result

	do := func(t, cmd string, args ...string) {
		out, err := runCmd(cmd, args...)
		msg := strings.TrimSpace(out)
		if err != nil {
			msg = fmt.Sprintf("%s (non-fatal)", err)
		}
		results = append(results, result{target: t, success: err == nil, message: msg})
	}

	switch target {
	case "tmux":
		do("tmux", "tmux", "source-file", os.Getenv("HOME")+"/.tmux.conf")
	case "i3":
		do("i3", "i3-msg", "reload")
	case "kitty":
		do("kitty", "kitty", "@", "load-config")
	case "all":
		do("tmux", "tmux", "source-file", os.Getenv("HOME")+"/.tmux.conf")
		do("i3", "i3-msg", "reload")
		do("kitty", "kitty", "@", "load-config")
	default:
		results = append(results, result{target: target, success: false, message: fmt.Sprintf("unknown target: %s", target)})
	}

	resp := &dotfilesdv1.ReloadResponse{}
	for _, r := range results {
		resp.Results = append(resp.Results, &dotfilesdv1.ReloadResponse_ReloadResult{
			Target:  r.target,
			Success: r.success,
			Message: r.message,
		})
	}

	slog.Log(ctx, levelTrace, "Config.Reload done", "results", resp.Results)
	return connect.NewResponse(resp), nil
}

func (s *configServer) Reconfigure(ctx context.Context, req *connect.Request[dotfilesdv1.ReconfigureRequest]) (*connect.Response[dotfilesdv1.ReconfigureResponse], error) {
	r := req.Msg
	slog.Log(ctx, levelTrace, "Config.Reconfigure", "log_level", r.LogLevel)

	newLevel, ok := parseLogLevel(r.LogLevel)
	if !ok {
		msg := fmt.Sprintf("invalid log level: %q (valid: trace, debug, info, warn, error)", r.LogLevel)
		slog.Warn("Reconfigure: invalid log level", "log_level", r.LogLevel)
		return connect.NewResponse(&dotfilesdv1.ReconfigureResponse{
			Success: false,
			Message: msg,
		}), nil
	}

	logLevelVar.Set(newLevel)
	msg := fmt.Sprintf("log level changed to %s", r.LogLevel)
	slog.Warn("Reconfigure applied", "log_level", r.LogLevel)

	return connect.NewResponse(&dotfilesdv1.ReconfigureResponse{
		Success: true,
		Message: msg,
	}), nil
}

func (s *configServer) Restart(ctx context.Context, req *connect.Request[dotfilesdv1.RestartRequest]) (*connect.Response[dotfilesdv1.RestartResponse], error) {
	slog.Warn("Restart requested")

	go gracefulRestart(500 * time.Millisecond)

	return connect.NewResponse(&dotfilesdv1.RestartResponse{
		Message: "daemon restarting in 500ms, reconnect after ~3s",
	}), nil
}

// --- Shared helpers --------------------------------------------------------

func hasSudo() bool {
	_, err := exec.LookPath("sudo")
	return err == nil
}

func hasPkexec() bool {
	_, err := exec.LookPath("pkexec")
	return err == nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func runCmd(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

func runCmdFull(name string, args ...string) (string, string, int) {
	var stdout, stderr strings.Builder
	cmd := exec.Command(name, args...)
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
	return stdout.String(), stderr.String(), exitCode
}
