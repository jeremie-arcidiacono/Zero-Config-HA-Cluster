package main

import (
	"antsd/internal/cluster"
	"antsd/internal/config"
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	startedAt := time.Now()

	// temporary logger for early startup messages (before config is loaded)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	conf, err := config.Load()
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}
	logger.Info("loaded configuration", "config", conf)

	// replace logger with one configured at the desired log level
	lvl := conf.GetLogLevel()
	logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mgr := cluster.New(conf, logger, startedAt)
	if err := mgr.Run(ctx); err != nil {
		logger.Error("exited with error", "error", err)
		os.Exit(1)
	}
}
