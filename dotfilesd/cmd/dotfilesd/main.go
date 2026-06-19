package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/natefinch/lumberjack.v2"
)

var buildHash string

const levelTrace = slog.Level(-8)

var logLevelVar slog.LevelVar

func main() {
	var (
		rpcPort   string
		noVerify  bool
		logDir    string
		logLevel  string
		logMaxMB  int
		logBackup int
		logAge    int
	)

	cmd := &cobra.Command{
		Use:   "dotfilesd",
		Short: "dotfiles runtime daemon (Connect RPC)",
		RunE: func(cmd *cobra.Command, args []string) error {
			checkBuildHash(noVerify, "dotfilesd")

			viper.SetConfigName("config")
			viper.SetConfigType("yaml")
			viper.AddConfigPath("$HOME/.config/dotfilesd")
			viper.AutomaticEnv()
			viper.SetEnvPrefix("DOTFILESD")

			if err := viper.ReadInConfig(); err != nil {
				if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
					fmt.Fprintf(os.Stderr, "config error: %v\n", err)
				}
			}

			rpcPort = firstNonEmpty(rpcPort, viper.GetString("port"), os.Getenv("DOTFILESD_PORT"), "9105")
			logDir = firstNonEmpty(logDir, viper.GetString("log.dir"), os.Getenv("DOTFILESD_LOG_DIR"), os.Getenv("HOME")+"/dotfilesd/logs")
			logDir = strings.Replace(logDir, "~", os.Getenv("HOME"), 1)

			logLevel = firstNonEmpty(logLevel, viper.GetString("log.level"), os.Getenv("DOTFILESD_LOG_LEVEL"), "info")
			logMaxMB = firstNonZeroInt(logMaxMB, viper.GetInt("log.max_size_mb"), 10)
			logBackup = firstNonZeroInt(logBackup, viper.GetInt("log.max_backups"), 5)
			logAge = firstNonZeroInt(logAge, viper.GetInt("log.max_age_days"), 30)

			setupLogging(logDir, logLevel, logMaxMB, logBackup, logAge)

			sysSvc := &systemServer{startedAt: time.Now()}
			dotSvc := &dotfilesServer{}
			execSvc := &execServer{}
			cfgSvc := &configServer{}

			mux := http.NewServeMux()
			{
				p, h := dotfilesdv1connect.NewSystemServiceHandler(sysSvc)
				mux.Handle(p, h)
			}
			{
				p, h := dotfilesdv1connect.NewDotfilesServiceHandler(dotSvc)
				mux.Handle(p, h)
			}
			{
				p, h := dotfilesdv1connect.NewExecServiceHandler(execSvc)
				mux.Handle(p, h)
			}
			{
				p, h := dotfilesdv1connect.NewConfigServiceHandler(cfgSvc)
				mux.Handle(p, h)
			}

			rpcAddr := fmt.Sprintf("127.0.0.1:%s", rpcPort)
			rpcServer := &http.Server{
				Addr:    rpcAddr,
				Handler: mux,
			}

			go func() {
				slog.Info("serving connect rpc", "addr", rpcAddr)
				if err := rpcServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					slog.Error("rpc server error", "error", err)
				}
			}()

			sig := make(chan os.Signal, 1)
			signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
			<-sig

			slog.Info("shutting down")
			rpcServer.Close()
			slog.Info("done")
			return nil
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.Flags().StringVarP(&rpcPort, "port", "p", "", "RPC port (env DOTFILESD_PORT, config: port)")
	cmd.Flags().BoolVar(&noVerify, "no-verify", false, "skip source version check")
	cmd.Flags().StringVar(&logDir, "log-dir", "", "log directory (config: log.dir)")
	cmd.Flags().StringVar(&logLevel, "log-level", "", "log level: trace|debug|info|warn|error (config: log.level)")
	cmd.Flags().IntVar(&logMaxMB, "log-max-size", 0, "max MB per log file (config: log.max_size_mb)")
	cmd.Flags().IntVar(&logBackup, "log-max-backups", 0, "max rotated files (config: log.max_backups)")
	cmd.Flags().IntVar(&logAge, "log-max-age", 0, "max days to keep logs (config: log.max_age_days)")

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonZeroInt(vals ...int) int {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}

func parseLogLevel(s string) (dotfilesdv1.LogLevel, slog.Level, bool) {
	key := "LOG_LEVEL_" + strings.ToUpper(s)
	v, ok := dotfilesdv1.LogLevel_value[key]
	if !ok {
		// Accept "warn" as alias for "warning"
		if strings.ToLower(s) == "warn" {
			return dotfilesdv1.LogLevel_LOG_LEVEL_WARN, slog.LevelWarn, true
		}
		return dotfilesdv1.LogLevel_LOG_LEVEL_UNSPECIFIED, slog.LevelInfo, false
	}
	enum := dotfilesdv1.LogLevel(v)
	return enum, logLevelToSlog(enum), true
}

func logLevelToSlog(l dotfilesdv1.LogLevel) slog.Level {
	switch l {
	case dotfilesdv1.LogLevel_LOG_LEVEL_TRACE:
		return levelTrace
	case dotfilesdv1.LogLevel_LOG_LEVEL_DEBUG:
		return slog.LevelDebug
	case dotfilesdv1.LogLevel_LOG_LEVEL_INFO:
		return slog.LevelInfo
	case dotfilesdv1.LogLevel_LOG_LEVEL_WARN:
		return slog.LevelWarn
	case dotfilesdv1.LogLevel_LOG_LEVEL_ERROR:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func setupLogging(logDir, level string, maxMB, backups, age int) {
	os.MkdirAll(logDir, 0755)

	fileWriter := &lumberjack.Logger{
		Filename:   logDir + "/dotfilesd.log",
		MaxSize:    maxMB,
		MaxBackups: backups,
		MaxAge:     age,
		Compress:   true,
	}

	_, lvl, ok := parseLogLevel(level)
	if !ok {
		lvl = slog.LevelInfo
	}
	logLevelVar.Set(lvl)

	multi := io.MultiWriter(os.Stdout, fileWriter)
	handler := slog.NewTextHandler(multi, &slog.HandlerOptions{
		Level: &logLevelVar,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
				level := a.Value.Any().(slog.Level)
				if level == levelTrace {
					a.Value = slog.StringValue("TRACE")
				}
			}
			return a
		},
	})
	slog.SetDefault(slog.New(handler))
}

func gracefulRestart(delay time.Duration) {
	slog.Warn("daemon restart requested, starting new instance", "delay_ms", delay.Milliseconds())
	time.Sleep(delay)

	binary, err := os.Executable()
	if err != nil {
		slog.Error("restart: cannot find binary", "error", err)
		os.Exit(1)
	}

	cmd := exec.Command(binary, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		slog.Error("restart: failed to start new instance", "error", err)
		os.Exit(1)
	}

	slog.Info("new instance started, exiting old instance")
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
