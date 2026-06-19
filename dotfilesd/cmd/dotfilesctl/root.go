package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"

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
