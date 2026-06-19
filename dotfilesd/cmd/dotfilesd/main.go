package main

import (
	"fmt"
	"log/slog"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"
	"gopkg.in/natefinch/lumberjack.v2"
)

func main() {
	setupLogging(false)

	rpcPort := getEnv("DOTFILESD_PORT", "9105")
	mcpPort := getEnv("DOTFILESD_MCP_PORT", "9106")

	svc := &dotfilesServer{startedAt: time.Now()}
	mcpSrv := NewMCPServer(svc, 9106)

	mux := http.NewServeMux()
	path, handler := dotfilesdv1connect.NewDotfilesServiceHandler(svc)
	mux.Handle(path, handler)

	rpcAddr := fmt.Sprintf("127.0.0.1:%s", rpcPort)
	rpcServer := &http.Server{
		Addr:    rpcAddr,
		Handler: withLogging(mux),
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		slog.Info("serving connect rpc", "addr", rpcAddr, "path", path)
		if err := rpcServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("rpc server error", "error", err)
		}
	}()

	go func() {
		defer wg.Done()
		slog.Info("serving mcp", "addr", fmt.Sprintf("127.0.0.1:%s", mcpPort))
		if err := mcpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("mcp server error", "error", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	slog.Info("shutting down")
	rpcServer.Close()
	wg.Wait()
	slog.Info("done")
}

func setupLogging(_ bool) {
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
