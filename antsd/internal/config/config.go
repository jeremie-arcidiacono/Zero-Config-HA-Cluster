// Package config internal/config/config.go
package config

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
)

// Config holds all runtime parameters for antsd.
type Config struct {
	// Serf
	SerfBindAddr string
	SerfBindPort int

	// HTTP API
	HTTPPort int

	// Persistence
	StateFilePath string

	LogLevel string
}

// Default values, used when neither flag nor env var is set.
const (
	defaultSerfBindAddr = "0.0.0.0"
	defaultSerfBindPort = 7946
	defaultHTTPPort     = 80
	defaultStateFile    = "/var/lib/antsd/state.json"
	defaultLogLevel     = "debug"
)

// Load parses CLI flags and environment variables, and returns a validated Config.
func Load() (*Config, error) {
	c := &Config{}

	flag.StringVar(&c.SerfBindAddr, "serf-bind-addr", envOr("ANTSD_SERF_BIND_ADDR", defaultSerfBindAddr), "Serf bind address")
	flag.IntVar(&c.SerfBindPort, "serf-bind-port", envOrInt("ANTSD_SERF_BIND_PORT", defaultSerfBindPort), "Serf bind port")
	flag.IntVar(&c.HTTPPort, "http-port", envOrInt("ANTSD_HTTP_PORT", defaultHTTPPort), "HTTP administration (monitoring and control) port")
	flag.StringVar(&c.StateFilePath, "state-file", envOr("ANTSD_STATE_FILE", defaultStateFile), "Path to persistent state file")
	flag.StringVar(&c.LogLevel, "log-level", envOr("ANTSD_LOG_LEVEL", defaultLogLevel), "Log level (debug, info, warn, error)")

	flag.Parse()

	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	return c, nil
}

// validate checks that the configuration is valid.
func (c *Config) validate() error {
	if net.ParseIP(c.SerfBindAddr) == nil {
		return fmt.Errorf("invalid serf-bind-addr: %s", c.SerfBindAddr)
	}
	if c.SerfBindPort <= 0 || c.SerfBindPort > 65535 {
		return fmt.Errorf("serf-bind-port out of range: %d", c.SerfBindPort)
	}
	if c.HTTPPort <= 0 || c.HTTPPort > 65535 {
		return fmt.Errorf("http-port out of range: %d", c.HTTPPort)
	}
	if c.StateFilePath == "" {
		return fmt.Errorf("state-file must not be empty")
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		var i int
		if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
			return i
		}
	}
	return fallback
}

func (c *Config) String() string {
	return fmt.Sprintf("Config{SerfBindAddr: %s, SerfBindPort: %d, HTTPPort: %d, StateFilePath: %s, LogLevel: %s}",
		c.SerfBindAddr, c.SerfBindPort, c.HTTPPort, c.StateFilePath, c.LogLevel)
}

// GetLogLevel returns the slog.Level corresponding to the configured LogLevel string.
func (c *Config) GetLogLevel() slog.Level {
	switch strings.ToLower(c.LogLevel) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
