package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"
	"gopkg.in/natefinch/lumberjack.v2"
)

var client dotfilesdv1connect.DotfilesServiceClient

func main() {
	verbose := false
	args := os.Args[1:]
	for i, a := range args {
		if a == "--verbose" || a == "-v" {
			verbose = true
			args = append(args[:i], args[i+1:]...)
			break
		}
	}

	setupLogging(verbose)

	port := os.Getenv("DOTFILESD_PORT")
	if port == "" {
		port = "9105"
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%s", port)
	client = dotfilesdv1connect.NewDotfilesServiceClient(http.DefaultClient, baseURL)

	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "ping":
		doPing()
	case "status":
		doStatus()
	case "info":
		doInfo()
	case "exec":
		doExec(cmdArgs)
	case "reload":
		doReload(cmdArgs)
	case "git":
		doGit(cmdArgs)
	case "sudo":
		doSudo()
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func setupLogging(verbose bool) {
	logDir := os.Getenv("DOTFILESD_LOG_DIR")
	if logDir == "" {
		logDir = os.Getenv("HOME") + "/dotfilesd/logs"
	}
	os.MkdirAll(logDir, 0755)

	fileWriter := &lumberjack.Logger{
		Filename:   logDir + "/dotfilesctl.log",
		MaxSize:    10,
		MaxBackups: 5,
		MaxAge:     30,
		Compress:   true,
	}

	var writers []io.Writer
	writers = append(writers, fileWriter)
	if verbose {
		writers = append(writers, os.Stderr)
	}

	var multi io.Writer
	if len(writers) == 1 {
		multi = writers[0]
	} else {
		multi = io.MultiWriter(writers...)
	}

	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	handler := slog.NewTextHandler(multi, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
}

func doPing() {
	resp, err := client.Ping(context.Background(), connect.NewRequest(&dotfilesdv1.PingRequest{}))
	if err != nil {
		fatalf("ping failed: %v", err)
	}
	s := resp.Msg
	fmt.Printf("dotfilesd v%s (pid %d, up %ds)\n", s.Version, s.Pid, s.UptimeSecs)
}

func doStatus() {
	resp, err := client.Status(context.Background(), connect.NewRequest(&dotfilesdv1.StatusRequest{}))
	if err != nil {
		fatalf("status failed: %v", err)
	}
	s := resp.Msg
	clean := "clean"
	if !s.GitClean {
		clean = "dirty"
	}
	fmt.Printf("branch: %s (%s)\n", s.GitBranch, clean)
	fmt.Printf("last:   %s\n", s.LastCommit)
	fmt.Printf("host:   %s\n", s.Hostname)
	fmt.Printf("uptime: %s\n", s.Uptime)
}

func doInfo() {
	resp, err := client.SystemInfo(context.Background(), connect.NewRequest(&dotfilesdv1.SystemInfoRequest{}))
	if err != nil {
		fatalf("info failed: %v", err)
	}
	s := resp.Msg
	fmt.Printf("OS:      %s\n", s.Os)
	fmt.Printf("Kernel:  %s\n", s.Kernel)
	fmt.Printf("Shell:   %s\n", s.Shell)
	fmt.Printf("Desktop: %s\n", s.Desktop)
	fmt.Printf("Memory:  %d MB total / %d MB avail\n", s.MemoryTotalKb/1024, s.MemoryAvailKb/1024)
	fmt.Printf("CPU:     %.2f load\n", s.CpuLoad_1M)
	fmt.Printf("Tmux:    %s\n", s.TmuxVersion)
	fmt.Printf("Kitty:   %s\n", s.KittyVersion)
	fmt.Printf("I3:      %s\n", s.I3Version)
}

func doExec(args []string) {
	command := ""
	sudo := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--sudo":
			sudo = true
		case "--help", "-h":
			fmt.Println("usage: dotfilesctl exec [--sudo] <command>")
			fmt.Println("       dotfilesctl exec --sudo pacman -Syu")
			return
		default:
			command = strings.Join(args[i:], " ")
			goto run
		}
	}
run:

	resp, err := client.Exec(context.Background(), connect.NewRequest(&dotfilesdv1.ExecRequest{
		Command: command, Sudo: sudo,
	}))
	if err != nil {
		fatalf("exec failed: %v", err)
	}
	if resp.Msg.Stdout != "" {
		fmt.Print(resp.Msg.Stdout)
	}
	if resp.Msg.Stderr != "" {
		fmt.Fprint(os.Stderr, resp.Msg.Stderr)
	}
	if resp.Msg.ExitCode != 0 {
		os.Exit(int(resp.Msg.ExitCode))
	}
}

func doReload(args []string) {
	target := "all"
	if len(args) > 0 {
		target = args[0]
	}
	resp, err := client.Reload(context.Background(), connect.NewRequest(&dotfilesdv1.ReloadRequest{Target: target}))
	if err != nil {
		fatalf("reload failed: %v", err)
	}
	for _, r := range resp.Msg.Results {
		status := "ok"
		if !r.Success {
			status = "error"
		}
		fmt.Printf("%-6s %s: %s\n", status, r.Target, r.Message)
	}
}

func doGit(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: dotfilesctl git <action> [options]")
		fmt.Println("actions: status, diff, add, commit, push, log")
		fmt.Println("options:")
		fmt.Println("  -m <msg>  commit message")
		fmt.Println("  -- <paths> files to stage (for add)")
		os.Exit(1)
	}

	action := args[0]
	message := ""
	paths := ""

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "-m":
			if i+1 < len(args) {
				i++
				message = args[i]
			}
		case "--":
			i++
			paths = strings.Join(args[i:], " ")
			i = len(args)
		default:
			paths = strings.Join(args[i:], " ")
			i = len(args)
		}
	}

	resp, err := client.Git(context.Background(), connect.NewRequest(&dotfilesdv1.GitRequest{
		Action: action, Message: message, Paths: paths,
	}))
	if err != nil {
		fatalf("git failed: %v", err)
	}
	if resp.Msg.Stderr != "" {
		fmt.Fprint(os.Stderr, resp.Msg.Stderr)
	}
	if resp.Msg.Stdout != "" {
		fmt.Print(resp.Msg.Stdout)
	}
	if resp.Msg.ExitCode != 0 {
		os.Exit(int(resp.Msg.ExitCode))
	}
}

func doSudo() {
	resp, err := client.SudoMethods(context.Background(), connect.NewRequest(&dotfilesdv1.SudoMethodsRequest{}))
	if err != nil {
		fatalf("sudo methods failed: %v", err)
	}
	fmt.Printf("current:  %s\n", resp.Msg.CurrentMethod)
	fmt.Printf("has sudo: %v\n", resp.Msg.HasElevation)
	fmt.Printf("available: %s\n", strings.Join(resp.Msg.AvailableMethods, ", "))
}

func printUsage() {
	fmt.Println("dotfilesctl - dotfiles runtime CLI")
	fmt.Println("")
	fmt.Println("usage:")
	fmt.Println("  dotfilesctl ping              check daemon is running")
	fmt.Println("  dotfilesctl status            show dotfiles repo status")
	fmt.Println("  dotfilesctl info              detailed system info")
	fmt.Println("  dotfilesctl exec [--sudo] <cmd>  run a command")
	fmt.Println("  dotfilesctl reload [target]   reload configs (tmux|i3|kitty|all)")
	fmt.Println("  dotfilesctl git <action>      git operations")
	fmt.Println("  dotfilesctl sudo              show sudo methods")
	fmt.Println("  -v, --verbose                 verbose logging to stderr")
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
