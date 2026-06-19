package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

type dotfilesServer struct {
	mu        sync.Mutex
	startedAt time.Time
	sudoMutex sync.Mutex
}

func (s *dotfilesServer) Ping(ctx context.Context, req *connect.Request[dotfilesdv1.PingRequest]) (*connect.Response[dotfilesdv1.PingResponse], error) {
	return connect.NewResponse(&dotfilesdv1.PingResponse{
		Version:    "0.1.0",
		Pid:        int64(os.Getpid()),
		UptimeSecs: int64(time.Since(s.startedAt).Seconds()),
	}), nil
}

func (s *dotfilesServer) Status(ctx context.Context, req *connect.Request[dotfilesdv1.StatusRequest]) (*connect.Response[dotfilesdv1.StatusResponse], error) {
	home := os.Getenv("HOME")
	hostname, _ := os.Hostname()
	uptimeRaw, _ := runCmd("uptime", "-p")

	gitBranch, _ := runCmd("git", "-C", home, "rev-parse", "--abbrev-ref", "HEAD")
	gitLog, _ := runCmd("git", "-C", home, "log", "--oneline", "-1")
	gitStatus, _ := runCmd("git", "-C", home, "status", "--porcelain")

	gitClean := strings.TrimSpace(gitStatus) == ""
	gitBranch = strings.TrimSpace(gitBranch)
	gitLog = strings.TrimSpace(gitLog)

	return connect.NewResponse(&dotfilesdv1.StatusResponse{
		GitClean:   gitClean,
		GitBranch:  gitBranch,
		LastCommit: gitLog,
		Uptime:     strings.TrimSpace(uptimeRaw),
		Hostname:   strings.TrimSpace(hostname),
	}), nil
}

func (s *dotfilesServer) Exec(ctx context.Context, req *connect.Request[dotfilesdv1.ExecRequest]) (*connect.Response[dotfilesdv1.ExecResponse], error) {
	cmdStr := req.Msg.Command
	sudo := req.Msg.Sudo

	if sudo {
		cmdStr = fmt.Sprintf("pkexec sh -c '%s'", strings.ReplaceAll(cmdStr, "'", "'\\''"))
	}

	stdout, stderr, exitCode := runCmdFull("sh", "-c", cmdStr)
	return connect.NewResponse(&dotfilesdv1.ExecResponse{
		ExitCode: int32(exitCode),
		Stdout:   stdout,
		Stderr:   stderr,
	}), nil
}

func (s *dotfilesServer) Reload(ctx context.Context, req *connect.Request[dotfilesdv1.ReloadRequest]) (*connect.Response[dotfilesdv1.ReloadResponse], error) {
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
	return connect.NewResponse(resp), nil
}

func (s *dotfilesServer) Git(ctx context.Context, req *connect.Request[dotfilesdv1.GitRequest]) (*connect.Response[dotfilesdv1.GitResponse], error) {
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
			return connect.NewResponse(&dotfilesdv1.GitResponse{ExitCode: 1, Stderr: "commit message required"}), nil
		}
		args = []string{"-C", home, "commit", "-m", req.Msg.Message}
	case "push":
		args = []string{"-C", home, "push"}
	case "log":
		args = []string{"-C", home, "log", "--oneline", "-10"}
	default:
		return connect.NewResponse(&dotfilesdv1.GitResponse{
			ExitCode: 1,
			Stderr:   fmt.Sprintf("unknown action: %s", action),
		}), nil
	}

	stdout, stderr, code := runCmdFull("git", args...)
	return connect.NewResponse(&dotfilesdv1.GitResponse{
		ExitCode: int32(code),
		Stdout:   stdout,
		Stderr:   stderr,
	}), nil
}

func (s *dotfilesServer) SystemInfo(ctx context.Context, req *connect.Request[dotfilesdv1.SystemInfoRequest]) (*connect.Response[dotfilesdv1.SystemInfoResponse], error) {
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

	return connect.NewResponse(&dotfilesdv1.SystemInfoResponse{
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
	}), nil
}

func (s *dotfilesServer) SudoMethods(ctx context.Context, req *connect.Request[dotfilesdv1.SudoMethodsRequest]) (*connect.Response[dotfilesdv1.SudoMethodsResponse], error) {
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

	return connect.NewResponse(&dotfilesdv1.SudoMethodsResponse{
		AvailableMethods: available,
		CurrentMethod:    current,
		HasElevation:     len(available) > 0,
	}), nil
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
