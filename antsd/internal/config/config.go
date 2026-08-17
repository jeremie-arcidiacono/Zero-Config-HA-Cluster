// Package config internal/config/config.go
package config

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
)

// Config holds all runtime parameters for antsd.
type Config struct {
	// NodeName is the cluster-wide unique node identifier, also used for role election (lowest name).
	// Defaults to a name derived from the MAC address.
	NodeName string

	// Serf
	SerfBindAddr string
	SerfBindPort int

	// HTTP API
	HTTPPort int

	// Persistence
	StateFilePath string

	// K3s
	K3sInstaller string // InstallerExec or InstallerFake
	K3sToken     string // pre-shared cluster join token

	// K3sFakeInstalledRole makes the fake installer report K3s as already
	// installed with this role ("server", "agent", or empty for "not installed").
	// For development only, to test the rejoin path.
	K3sFakeInstalledRole string

	// RescaleEnabled turns the rescaling workflow on.
	RescaleEnabled bool

	// EvictionGrace is how long a machine must stay continuously Serf-failed before it is evicted.
	// It is long by default: a reboot or a basic maintenance must never trigger one.
	EvictionGrace time.Duration

	// RescaleSettleDelay debounce the control-plane size decision, so a membership still
	// settling down does not trigger a rescaling.
	RescaleSettleDelay time.Duration

	LogLevel string
}

// Supported values for Config.K3sInstaller.
const (
	// InstallerExec runs the real K3s install script.
	InstallerExec = "exec"
	// InstallerFake simulates installations (local development and tests).
	InstallerFake = "fake"
)

// Default values, used when neither flag nor env var is set.
const (
	defaultSerfBindAddr = "0.0.0.0"
	defaultSerfBindPort = 7946
	defaultHTTPPort     = 9000
	defaultStateFile    = "/var/lib/antsd/state.json"
	defaultLogLevel     = "debug"
	defaultK3sInstaller = InstallerExec

	defaultRescaleEnabled     = true
	defaultEvictionGrace      = 12 * time.Hour
	defaultRescaleSettleDelay = 30 * time.Second
)

// Load parses CLI flags and environment variables, and returns a validated Config.
func Load() (*Config, error) {
	c := &Config{}

	derivedName, derivedNameErr := defaultNodeName()

	flag.StringVar(&c.NodeName, "node-name", envOr("ANTSD_NODE_NAME", derivedName), "Unique node name (defaults to a name derived from the MAC address)")
	flag.StringVar(&c.SerfBindAddr, "serf-bind-addr", envOr("ANTSD_SERF_BIND_ADDR", defaultSerfBindAddr), "Serf bind address")
	flag.IntVar(&c.SerfBindPort, "serf-bind-port", envOrInt("ANTSD_SERF_BIND_PORT", defaultSerfBindPort), "Serf bind port")
	flag.IntVar(&c.HTTPPort, "http-port", envOrInt("ANTSD_HTTP_PORT", defaultHTTPPort), "HTTP administration (monitoring and control) port")
	flag.StringVar(&c.StateFilePath, "state-file", envOr("ANTSD_STATE_FILE", defaultStateFile), "Path to persistent state file")
	flag.StringVar(&c.K3sInstaller, "k3s-installer", envOr("ANTSD_K3S_INSTALLER", defaultK3sInstaller), "K3s installer implementation (exec or fake)")
	flag.StringVar(&c.K3sToken, "k3s-token", envOr("ANTSD_K3S_TOKEN", ""), "Pre-shared K3s cluster join token")
	flag.StringVar(&c.K3sFakeInstalledRole, "k3s-fake-installed-role", envOr("ANTSD_K3S_FAKE_INSTALLED_ROLE", ""), "Role the fake installer reports as already installed, to replay the rejoin path locally (server or agent)")
	flag.BoolVar(&c.RescaleEnabled, "rescale-enabled", envOrBool("ANTSD_RESCALE_ENABLED", defaultRescaleEnabled), "Let the cluster repair its control plane automatically: eviction, promotion, demotion (turn off with -rescale-enabled=false)")
	flag.DurationVar(&c.EvictionGrace, "rescale-eviction-grace", envOrDuration("ANTSD_RESCALE_EVICTION_GRACE", defaultEvictionGrace), "How long a machine must stay unreachable before it is evicted from the cluster")
	flag.DurationVar(&c.RescaleSettleDelay, "rescale-settle-delay", envOrDuration("ANTSD_RESCALE_SETTLE_DELAY", defaultRescaleSettleDelay), "Debounce period before acting on a control plane that is off its target size")
	flag.StringVar(&c.LogLevel, "log-level", envOr("ANTSD_LOG_LEVEL", defaultLogLevel), "Log level (debug, info, warn, error)")

	flag.Parse()

	if c.NodeName == "" && derivedNameErr != nil {
		return nil, fmt.Errorf("cannot derive a node name from this machine, pass -node-name: %w", derivedNameErr)
	}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	return c, nil
}

// validate checks that the configuration is valid.
func (c *Config) validate() error {
	if err := validateNodeName(c.NodeName); err != nil {
		return err
	}
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
	if c.K3sInstaller != InstallerExec && c.K3sInstaller != InstallerFake {
		return fmt.Errorf("k3s-installer must be %q or %q, got %q", InstallerExec, InstallerFake, c.K3sInstaller)
	}
	if c.K3sInstaller == InstallerExec && c.K3sToken == "" {
		return fmt.Errorf("k3s-token is required with the %q installer", InstallerExec)
	}
	if c.EvictionGrace <= 0 {
		return fmt.Errorf("rescale-eviction-grace must be positive, got %s", c.EvictionGrace)
	}
	if c.RescaleSettleDelay <= 0 {
		return fmt.Errorf("rescale-settle-delay must be positive, got %s", c.RescaleSettleDelay)
	}
	if c.K3sFakeInstalledRole != "" {
		if c.K3sInstaller != InstallerFake {
			return fmt.Errorf("k3s-fake-installed-role only applies to the %q installer", InstallerFake)
		}
		role := node.Role(c.K3sFakeInstalledRole)
		if role != node.RoleServer && role != node.RoleAgent {
			return fmt.Errorf("k3s-fake-installed-role must be %q or %q, got %q",
				node.RoleServer, node.RoleAgent, c.K3sFakeInstalledRole)
		}
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

func envOrBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func envOrDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func (c *Config) String() string {
	// The K3s token is a secret: report only whether it is set.
	tokenInfo := "UNSET"
	if c.K3sToken != "" {
		tokenInfo = "set"
	}
	return fmt.Sprintf("Config{NodeName: %s, SerfBindAddr: %s, SerfBindPort: %d, HTTPPort: %d, StateFilePath: %s, "+
		"K3sInstaller: %s, K3sToken: %s, RescaleEnabled: %t, EvictionGrace: %s, RescaleSettleDelay: %s, LogLevel: %s}",
		c.NodeName, c.SerfBindAddr, c.SerfBindPort, c.HTTPPort, c.StateFilePath,
		c.K3sInstaller, tokenInfo,
		c.RescaleEnabled, c.EvictionGrace, c.RescaleSettleDelay,
		c.LogLevel)
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
