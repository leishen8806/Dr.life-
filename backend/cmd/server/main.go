package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/config"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/db"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/logx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		os.Exit(1)
	}

	logger := logx.NewLogger(cfg.LogLevel, os.Stdout)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.NewPool(ctx, cfg)
	if err != nil {
		logger.Error("database pool failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		logger.Error("http listener failed", "error", err)
		os.Exit(1)
	}

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.NewHandler(logger, db.Ping(database)),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		// WriteTimeout stays unset so future real-time streams can remain open.
	}
	logger.Info("http server starting", "addr", cfg.HTTPAddr, "environment", cfg.AppEnv)
	if err := server.Serve(ctx, httpServer, listener, cfg.ShutdownTimeout); err != nil {
		logger.Error("http server stopped with error", "error", err)
		os.Exit(1)
	}
}
