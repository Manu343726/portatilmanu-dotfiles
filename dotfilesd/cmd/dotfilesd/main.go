package main

import (
	"fmt"
	"log/slog"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"
	"github.com/spf13/cobra"
	"gopkg.in/natefinch/lumberjack.v2"
)

func main() {
	var rpcPort string

	cmd := &cobra.Command{
		Use:   "dotfilesd",
		Short: "dotfiles runtime daemon (Connect RPC)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if rpcPort == "" {
				rpcPort = getEnv("DOTFILESD_PORT", "9105")
			}
			return runDaemon(rpcPort)
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.Flags().StringVarP(&rpcPort, "port", "p", "", "RPC port (default DOTFILESD_PORT env or 9105)")

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runDaemon(port string) error {
	setupLogging()

	svc := &dotfilesServer{startedAt: time.Now()}

	mux := http.NewServeMux()
	path, handler := dotfilesdv1connect.NewDotfilesServiceHandler(svc)
	mux.Handle(path, handler)

	rpcAddr := fmt.Sprintf("127.0.0.1:%s", port)
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
}

func setupLogging() {
	logDir := os.Getenv("DOTFILESD_LOG_DIR")
	if logDir == "" {
		logDir = os.Getenv("HOME") + "/dotfilesd/logs"
	}
	os.MkdirAll(logDir, 0755)

	fileWriter := &lumberjack.Logger{
		Filename:   logDir + "/dotfilesd.log",
		MaxSize:    10,
		MaxBackups: 5,
		MaxAge:     30,
		Compress:   true,
	}

	multi := io.MultiWriter(os.Stdout, fileWriter)
	handler := slog.NewJSONHandler(multi, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(handler))
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
