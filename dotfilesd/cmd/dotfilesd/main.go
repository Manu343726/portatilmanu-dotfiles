package main

import (
	"fmt"
	"log/slog"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/natefinch/lumberjack.v2"
)

var buildHash string

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

			viper.BindPFlag("port", cmd.Flags().Lookup("port"))
			viper.BindPFlag("no_verify", cmd.Flags().Lookup("no-verify"))
			viper.BindPFlag("log.dir", cmd.Flags().Lookup("log-dir"))
			viper.BindPFlag("log.level", cmd.Flags().Lookup("log-level"))
			viper.BindPFlag("log.max_size_mb", cmd.Flags().Lookup("log-max-size"))
			viper.BindPFlag("log.max_backups", cmd.Flags().Lookup("log-max-backups"))
			viper.BindPFlag("log.max_age_days", cmd.Flags().Lookup("log-max-age"))

			if err := viper.ReadInConfig(); err != nil {
				if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
					fmt.Fprintf(os.Stderr, "config error: %v\n", err)
				}
			}

			if rpcPort == "" {
				rpcPort = viper.GetString("port")
				if rpcPort == "" {
					rpcPort = "9105"
				}
			}
			if logDir == "" {
				logDir = viper.GetString("log.dir")
				if logDir == "" {
					logDir = os.Getenv("HOME") + "/dotfilesd/logs"
				}
			}
			if logLevel == "" {
				logLevel = viper.GetString("log.level")
				if logLevel == "" {
					logLevel = "info"
				}
			}
			if logMaxMB == 0 {
				logMaxMB = viper.GetInt("log.max_size_mb")
				if logMaxMB == 0 {
					logMaxMB = 10
				}
			}
			if logBackup == 0 {
				logBackup = viper.GetInt("log.max_backups")
				if logBackup == 0 {
					logBackup = 5
				}
			}
			if logAge == 0 {
				logAge = viper.GetInt("log.max_age_days")
				if logAge == 0 {
					logAge = 30
				}
			}

			setupLogging(logDir, logLevel, logMaxMB, logBackup, logAge)

			svc := &dotfilesServer{startedAt: time.Now()}

			mux := http.NewServeMux()
			path, handler := dotfilesdv1connect.NewDotfilesServiceHandler(svc)
			mux.Handle(path, handler)

			rpcAddr := fmt.Sprintf("127.0.0.1:%s", rpcPort)
			rpcServer := &http.Server{
				Addr:    rpcAddr,
				Handler: withLogging(mux),
			}

			go func() {
				slog.Info("serving connect rpc", "addr", rpcAddr, "path", path)
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
	cmd.Flags().StringVar(&logLevel, "log-level", "", "log level: debug|info|warn|error (config: log.level)")
	cmd.Flags().IntVar(&logMaxMB, "log-max-size", 0, "max MB per log file (config: log.max_size_mb)")
	cmd.Flags().IntVar(&logBackup, "log-max-backups", 0, "max rotated files (config: log.max_backups)")
	cmd.Flags().IntVar(&logAge, "log-max-age", 0, "max days to keep logs (config: log.max_age_days)")

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
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

	var slogLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn", "warning":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	multi := io.MultiWriter(os.Stdout, fileWriter)
	handler := slog.NewJSONHandler(multi, &slog.HandlerOptions{Level: slogLevel})
	slog.SetDefault(slog.New(handler))
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

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration", time.Since(start),
		)
	})
}
