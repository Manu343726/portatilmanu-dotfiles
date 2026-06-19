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
	"github.com/spf13/cobra"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	verbose bool
	port    string
	client  dotfilesdv1connect.DotfilesServiceClient
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dotfilesctl",
		Short: "dotfiles runtime CLI",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			setupLogging(verbose)
			if port == "" {
				port = os.Getenv("DOTFILESD_PORT")
				if port == "" {
					port = "9105"
				}
			}
			baseURL := fmt.Sprintf("http://127.0.0.1:%s", port)
			client = dotfilesdv1connect.NewDotfilesServiceClient(http.DefaultClient, baseURL)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose logging to stderr")
	cmd.PersistentFlags().StringVarP(&port, "port", "p", "", "daemon port (default DOTFILESD_PORT env or 9105)")

	cmd.AddCommand(newPingCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newInfoCmd())
	cmd.AddCommand(newExecCmd())
	cmd.AddCommand(newReloadCmd())
	cmd.AddCommand(newGitCmd())
	cmd.AddCommand(newSudoCmd())
	cmd.AddCommand(newMCPCmd())

	return cmd
}

func setupLogging(v bool) {
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
	if v {
		writers = append(writers, os.Stderr)
	}

	var multi io.Writer
	if len(writers) == 1 {
		multi = writers[0]
	} else {
		multi = io.MultiWriter(writers...)
	}

	level := slog.LevelInfo
	if v {
		level = slog.LevelDebug
	}
	handler := slog.NewTextHandler(multi, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

func newPingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ping",
		Short: "check daemon is running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := client.Ping(context.Background(), connect.NewRequest(&dotfilesdv1.PingRequest{}))
			if err != nil {
				return fmt.Errorf("ping failed: %w", err)
			}
			s := resp.Msg
			fmt.Printf("dotfilesd v%s (pid %d, up %ds)\n", s.Version, s.Pid, s.UptimeSecs)
			return nil
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "show dotfiles repo status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := client.Status(context.Background(), connect.NewRequest(&dotfilesdv1.StatusRequest{}))
			if err != nil {
				return fmt.Errorf("status failed: %w", err)
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
			return nil
		},
	}
}

func newInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "detailed system information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := client.SystemInfo(context.Background(), connect.NewRequest(&dotfilesdv1.SystemInfoRequest{}))
			if err != nil {
				return fmt.Errorf("info failed: %w", err)
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
			return nil
		},
	}
}

func newExecCmd() *cobra.Command {
	var sudo bool

	cmd := &cobra.Command{
		Use:   "exec [--sudo] <command>",
		Short: "run a shell command",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			command := strings.Join(args, " ")
			resp, err := client.Exec(context.Background(), connect.NewRequest(&dotfilesdv1.ExecRequest{
				Command: command, Sudo: sudo,
			}))
			if err != nil {
				return fmt.Errorf("exec failed: %w", err)
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
			return nil
		},
	}

	cmd.Flags().BoolVar(&sudo, "sudo", false, "run with pkexec")
	return cmd
}

func newReloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reload [target]",
		Short: "reload configs (tmux, i3, kitty, all)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "all"
			if len(args) > 0 {
				target = args[0]
			}
			resp, err := client.Reload(context.Background(), connect.NewRequest(&dotfilesdv1.ReloadRequest{Target: target}))
			if err != nil {
				return fmt.Errorf("reload failed: %w", err)
			}
			for _, r := range resp.Msg.Results {
				status := "ok"
				if !r.Success {
					status = "error"
				}
				fmt.Printf("%-6s %s: %s\n", status, r.Target, r.Message)
			}
			return nil
		},
	}
}

func newGitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "git <action> [-- <paths>]",
		Short: "git operations (status|diff|add|commit|push|log)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			action := args[0]
			message, _ := cmd.Flags().GetString("message")
			paths, _ := cmd.Flags().GetString("paths")

			resp, err := client.Git(context.Background(), connect.NewRequest(&dotfilesdv1.GitRequest{
				Action: action, Message: message, Paths: paths,
			}))
			if err != nil {
				return fmt.Errorf("git failed: %w", err)
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
			return nil
		},
	}

	cmd.Flags().StringP("message", "m", "", "commit message")
	cmd.Flags().String("paths", "", "files to stage")
	return cmd
}

func newSudoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sudo",
		Short: "show available sudo methods",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := client.SudoMethods(context.Background(), connect.NewRequest(&dotfilesdv1.SudoMethodsRequest{}))
			if err != nil {
				return fmt.Errorf("sudo methods failed: %w", err)
			}
			fmt.Printf("current:  %s\n", resp.Msg.CurrentMethod)
			fmt.Printf("has sudo: %v\n", resp.Msg.HasElevation)
			fmt.Printf("available: %s\n", strings.Join(resp.Msg.AvailableMethods, ", "))
			return nil
		},
	}
}

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "start MCP stdio server (for AI agents)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runMCP()
			return nil
		},
	}
}
