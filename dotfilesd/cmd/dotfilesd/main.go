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
	"gopkg.in/natefinch/lumberjack.v2"
)

func main() {
	setupLogging()

	rpcPort := getEnv("DOTFILESD_PORT", "9105")

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
