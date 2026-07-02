package main

import (
	"antsd/internal/config"
	"log/slog"
	"os"
)

func main() {
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


	// todo : start manager
}
