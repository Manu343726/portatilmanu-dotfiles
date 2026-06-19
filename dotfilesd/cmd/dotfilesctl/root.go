package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	buildHash  string
	verbose    bool
	noVerify   bool
	port       string
	sysClient  dotfilesdv1connect.SystemServiceClient
	dotClient  dotfilesdv1connect.DotfilesServiceClient
	execClient dotfilesdv1connect.ExecServiceClient
	cfgClient  dotfilesdv1connect.ConfigServiceClient
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dotfilesctl",
		Short: "dotfiles runtime CLI",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			checkBuildHash(noVerify, "dotfilesctl")

			viper.SetConfigName("config")
			viper.SetConfigType("yaml")
			viper.AddConfigPath("$HOME/.config/dotfilesctl")
			viper.AutomaticEnv()
			viper.SetEnvPrefix("DOTFILESCTL")

			if err := viper.ReadInConfig(); err != nil {
				if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
					fmt.Fprintf(os.Stderr, "config error: %v\n", err)
				}
			}

			if !cmd.Flags().Changed("port") {
				port = viper.GetString("port")
			}
			if !cmd.Flags().Changed("verbose") {
				verbose = viper.GetBool("verbose")
			}

			setupLogging(verbose)
			if port == "" {
				port = os.Getenv("DOTFILESD_PORT")
				if port == "" {
					port = "9105"
				}
			}
			baseURL := fmt.Sprintf("http://127.0.0.1:%s", port)
			sysClient = dotfilesdv1connect.NewSystemServiceClient(http.DefaultClient, baseURL)
			dotClient = dotfilesdv1connect.NewDotfilesServiceClient(http.DefaultClient, baseURL)
			execClient = dotfilesdv1connect.NewExecServiceClient(http.DefaultClient, baseURL)
			cfgClient = dotfilesdv1connect.NewConfigServiceClient(http.DefaultClient, baseURL)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose logging to stderr")
	cmd.PersistentFlags().BoolVar(&noVerify, "no-verify", false, "skip source version check")
	cmd.PersistentFlags().StringVarP(&port, "port", "p", "", "daemon port (default DOTFILESD_PORT env or 9105)")

	cmd.AddCommand(newSystemCmd())
	cmd.AddCommand(newDotfilesCmd())
	cmd.AddCommand(newExecCmd())
	cmd.AddCommand(newConfigCmd())
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

func checkBuildHash(noVerify bool, name string) {
	if buildHash == "" || buildHash == "dev" {
		return
	}
	srcDir := os.Getenv("HOME") + "/dotfilesd"
	out, err := exec.Command("git", "-C", srcDir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return
	}
	current := strings.TrimSpace(string(out))
	if current != buildHash && !noVerify {
		fmt.Fprintf(os.Stderr, "WARNING: %s source changed since build (built: %s, current: %s)\n", name, buildHash, current)
		fmt.Fprintf(os.Stderr, "  run 'make install' to rebuild, or use --no-verify to silence\n")
	}
}

// --- Enum parsing helpers ---------------------------------------------------

func parseLogLevel(s string) dotfilesdv1.LogLevel {
	key := "LOG_LEVEL_" + strings.ToUpper(s)
	if v, ok := dotfilesdv1.LogLevel_value[key]; ok {
		return dotfilesdv1.LogLevel(v)
	}
	if strings.ToLower(s) == "warn" {
		return dotfilesdv1.LogLevel_LOG_LEVEL_WARN
	}
	return dotfilesdv1.LogLevel_LOG_LEVEL_UNSPECIFIED
}

func parseGitAction(s string) dotfilesdv1.GitAction {
	key := "GIT_ACTION_" + strings.ToUpper(s)
	if v, ok := dotfilesdv1.GitAction_value[key]; ok {
		return dotfilesdv1.GitAction(v)
	}
	return dotfilesdv1.GitAction_GIT_ACTION_UNSPECIFIED
}

func parseReloadTarget(s string) dotfilesdv1.ReloadTarget {
	key := "RELOAD_TARGET_" + strings.ToUpper(s)
	if v, ok := dotfilesdv1.ReloadTarget_value[key]; ok {
		return dotfilesdv1.ReloadTarget(v)
	}
	return dotfilesdv1.ReloadTarget_RELOAD_TARGET_UNSPECIFIED
}

func parseSudoMethod(s string) dotfilesdv1.SudoMethod {
	key := "SUDO_METHOD_" + strings.ToUpper(s)
	if v, ok := dotfilesdv1.SudoMethod_value[key]; ok {
		return dotfilesdv1.SudoMethod(v)
	}
	return dotfilesdv1.SudoMethod_SUDO_METHOD_UNSPECIFIED
}

// --- system subcommand group ------------------------------------------------

func newSystemCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "system",
		Short: "daemon health and system information",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "ping",
		Short: "check daemon is running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := sysClient.Ping(context.Background(), connect.NewRequest(&dotfilesdv1.PingRequest{}))
			if err != nil {
				return fmt.Errorf("ping failed: %w", err)
			}
			s := resp.Msg
			fmt.Printf("dotfilesd v%s (pid %d, up %ds)\n", s.Version, s.Pid, s.UptimeSecs)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "info",
		Short: "detailed system information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := sysClient.SystemInfo(context.Background(), connect.NewRequest(&dotfilesdv1.SystemInfoRequest{}))
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
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "sudo",
		Short: "show available sudo methods",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := sysClient.SudoMethods(context.Background(), connect.NewRequest(&dotfilesdv1.SudoMethodsRequest{}))
			if err != nil {
				return fmt.Errorf("sudo methods failed: %w", err)
			}
			fmt.Printf("current:  %s\n", resp.Msg.CurrentMethod)
			fmt.Printf("has sudo: %v\n", resp.Msg.HasElevation)
			fmt.Printf("available: %s\n", strings.Join(resp.Msg.AvailableMethods, ", "))
			return nil
		},
	})
	return cmd
}

// --- dotfiles subcommand group ---------------------------------------------

func newDotfilesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dotfiles",
		Short: "dotfiles repository management",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "show dotfiles repo status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := dotClient.Status(context.Background(), connect.NewRequest(&dotfilesdv1.StatusRequest{}))
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
	})
	cmd.AddCommand(newGitCmd())
	return cmd
}

func newGitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "git <action> [-- <paths>]",
		Short: "git operations (status|diff|add|commit|push|log)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			action := parseGitAction(args[0])
			if action == dotfilesdv1.GitAction_GIT_ACTION_UNSPECIFIED {
				return fmt.Errorf("unknown git action: %s (valid: status, diff, add, commit, push, log)", args[0])
			}
			message, _ := cmd.Flags().GetString("message")
			paths, _ := cmd.Flags().GetString("paths")

			resp, err := dotClient.Git(context.Background(), connect.NewRequest(&dotfilesdv1.GitRequest{
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

// --- exec subcommand -------------------------------------------------------

func newExecCmd() *cobra.Command {
	var sudo bool

	cmd := &cobra.Command{
		Use:   "exec [--sudo] <command>",
		Short: "run a shell command",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			command := strings.Join(args, " ")
			if !sudo {
				// Simple unary exec.
				resp, err := execClient.Exec(context.Background(), connect.NewRequest(&dotfilesdv1.ExecRequest{
					Command: command,
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
			}

			return sudoExec(context.Background(), command)
		},
	}

	cmd.Flags().BoolVar(&sudo, "sudo", false, "run with sudo (interactive password prompt in terminal)")
	return cmd
}

func sudoExec(ctx context.Context, command string) error {
	method := dotfilesdv1.SudoMethod_SUDO_METHOD_TERMINAL
	if _, err := os.Stat("/dev/tty"); os.IsNotExist(err) {
		method = dotfilesdv1.SudoMethod_SUDO_METHOD_GRAPHICAL
	}

	// First call — no password, see what daemon says.
	resp, err := execClient.SudoExec(ctx, connect.NewRequest(&dotfilesdv1.SudoExecRequest{
		Command: command, PreferredMethod: method,
	}))
	if err != nil {
		return fmt.Errorf("sudo exec: %w", err)
	}

	switch o := resp.Msg.Outcome.(type) {
	case *dotfilesdv1.SudoExecResponse_Result:
		r := o.Result
		if r.AuthCancelled {
			return fmt.Errorf("sudo failed: %s", r.Stderr)
		}
		if r.Stdout != "" {
			fmt.Print(r.Stdout)
		}
		if r.Stderr != "" {
			fmt.Fprint(os.Stderr, r.Stderr)
		}
		if r.ExitCode != 0 {
			os.Exit(int(r.ExitCode))
		}
		return nil

	case *dotfilesdv1.SudoExecResponse_AuthChallenge:
		challenge := o.AuthChallenge
		fmt.Fprint(os.Stderr, challenge.Prompt)
		var password string
		if fd := int(os.Stdin.Fd()); term.IsTerminal(fd) {
			raw, err := term.ReadPassword(fd)
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}
			password = string(raw)
			fmt.Fprintln(os.Stderr)
		} else if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
			raw, err := term.ReadPassword(int(tty.Fd()))
			tty.Close()
			if err != nil {
				return fmt.Errorf("read password from tty: %w", err)
			}
			password = string(raw)
			fmt.Fprintln(os.Stderr)
		} else {
			reader := bufio.NewReader(os.Stdin)
			password, err = reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}
			password = strings.TrimRight(password, "\n\r")
		}

		// Retry with password.
		resp, err = execClient.SudoExec(ctx, connect.NewRequest(&dotfilesdv1.SudoExecRequest{
			Command: command, Password: password, PreferredMethod: dotfilesdv1.SudoMethod_SUDO_METHOD_TERMINAL,
		}))
		if err != nil {
			return fmt.Errorf("sudo exec with password: %w", err)
		}

		r := resp.Msg.GetResult()
		if r == nil {
			return fmt.Errorf("unexpected response after auth")
		}
		if r.AuthCancelled {
			return fmt.Errorf("sudo failed: %s", r.Stderr)
		}
		if r.Stdout != "" {
			fmt.Print(r.Stdout)
		}
		if r.Stderr != "" {
			fmt.Fprint(os.Stderr, r.Stderr)
		}
		if r.ExitCode != 0 {
			os.Exit(int(r.ExitCode))
		}
		return nil

	default:
		return fmt.Errorf("unexpected response type from daemon")
	}
}

// --- config subcommand group -----------------------------------------------

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "dotfiles configuration reload, daemon reconfiguration, restart",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "reload [target]",
		Short: "reload configs (tmux, i3, kitty, all)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := dotfilesdv1.ReloadTarget_RELOAD_TARGET_ALL
			if len(args) > 0 {
				target = parseReloadTarget(args[0])
				if target == dotfilesdv1.ReloadTarget_RELOAD_TARGET_UNSPECIFIED {
					return fmt.Errorf("unknown target: %s (valid: tmux, i3, kitty, all)", args[0])
				}
			}
			resp, err := cfgClient.Reload(context.Background(), connect.NewRequest(&dotfilesdv1.ReloadRequest{Target: target}))
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
	})
	var reconfigureCmd = &cobra.Command{
		Use:   "reconfigure --log-level <level>",
		Short: "change daemon runtime configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			levelStr, _ := cmd.Flags().GetString("log-level")
			if levelStr == "" {
				return fmt.Errorf("--log-level is required (trace, debug, info, warn, error)")
			}
			logLevel := parseLogLevel(levelStr)
			if logLevel == dotfilesdv1.LogLevel_LOG_LEVEL_UNSPECIFIED {
				return fmt.Errorf("invalid log level: %s (valid: trace, debug, info, warn, error)", levelStr)
			}
			resp, err := cfgClient.Reconfigure(context.Background(), connect.NewRequest(&dotfilesdv1.ReconfigureRequest{
				LogLevel: logLevel,
			}))
			if err != nil {
				return fmt.Errorf("reconfigure failed: %w", err)
			}
			fmt.Println(resp.Msg.Message)
			if !resp.Msg.Success {
				os.Exit(1)
			}
			return nil
		},
	}
	reconfigureCmd.Flags().String("log-level", "", "new log level (trace, debug, info, warn, error)")
	cmd.AddCommand(reconfigureCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "restart",
		Short: "gracefully restart the daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := cfgClient.Restart(context.Background(), connect.NewRequest(&dotfilesdv1.RestartRequest{}))
			if err != nil {
				return fmt.Errorf("restart failed: %w", err)
			}
			fmt.Println(resp.Msg.Message)
			return nil
		},
	})
	return cmd
}

// --- mcp subcommand --------------------------------------------------------

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
