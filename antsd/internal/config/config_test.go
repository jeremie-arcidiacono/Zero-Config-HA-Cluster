package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
)

// validConfig is the configuration of a real node, used as the base every case below alters.
func validConfig() *Config {
	return &Config{
		NodeName:           "ants-01",
		SerfBindAddr:       "0.0.0.0",
		SerfBindPort:       7946,
		HTTPPort:           9000,
		StateFilePath:      "/var/lib/antsd/state.json",
		K3sInstaller:       InstallerExec,
		K3sToken:           "a-pre-shared-token",
		RescaleEnabled:     true,
		EvictionGrace:      time.Hour,
		RescaleSettleDelay: 10 * time.Second,
		LogLevel:           "info",
	}
}

func TestValidateAcceptsARealNodeConfiguration(t *testing.T) {
	if err := validConfig().validate(); err != nil {
		t.Fatalf("the base configuration must be valid: %v", err)
	}
}

// TestValidateRejects covers the settings antsd cannot run with. The one that matters most is the
// missing K3s token: it is only rejected for the exec installer, and a node that starts without
// it installs a K3s no other machine can join.
func TestValidateRejects(t *testing.T) {
	cases := map[string]func(*Config){
		"empty node name":        func(c *Config) { c.NodeName = "" },
		"node name k3s rewrites": func(c *Config) { c.NodeName = "Ants-01" },
		"serf address is no IP":  func(c *Config) { c.SerfBindAddr = "eth0" },
		"serf port out of range": func(c *Config) { c.SerfBindPort = 70000 },
		"serf port unset":        func(c *Config) { c.SerfBindPort = 0 },
		"http port out of range": func(c *Config) { c.HTTPPort = -1 },
		"empty state file":       func(c *Config) { c.StateFilePath = "" },
		"unknown installer":      func(c *Config) { c.K3sInstaller = "magic" },
		"exec without a token":   func(c *Config) { c.K3sToken = "" },
		"negative failure grace": func(c *Config) { c.EvictionGrace = -time.Minute },
		"zero settle delay":      func(c *Config) { c.RescaleSettleDelay = 0 },
		"fake role on exec":      func(c *Config) { c.K3sFakeInstalledRole = string(node.RoleServer) },
		"unknown fake role":      func(c *Config) { c.K3sInstaller, c.K3sToken, c.K3sFakeInstalledRole = InstallerFake, "", "overlord" },
	}

	for name, alter := range cases {
		t.Run(name, func(t *testing.T) {
			c := validConfig()
			alter(c)
			if err := c.validate(); err == nil {
				t.Errorf("validate accepted %s", name)
			}
		})
	}
}

// TestValidateFakeInstaller pins what the development installer relaxes: it needs no join token,
// and it is the only one that may be told which role to report as already installed.
func TestValidateFakeInstaller(t *testing.T) {
	c := validConfig()
	c.K3sInstaller = InstallerFake
	c.K3sToken = ""
	c.K3sFakeInstalledRole = string(node.RoleAgent)

	if err := c.validate(); err != nil {
		t.Errorf("validate refused the fake installer configuration: %v", err)
	}
}

func TestGetLogLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"INFO":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		// An unreadable level must not silence the daemon.
		"":       slog.LevelInfo,
		"louder": slog.LevelInfo,
	}

	for value, want := range cases {
		t.Run(value, func(t *testing.T) {
			c := &Config{LogLevel: value}
			if got := c.GetLogLevel(); got != want {
				t.Errorf("GetLogLevel(%q) = %v, want %v", value, got, want)
			}
		})
	}
}

// TestEnvFallbacks covers how the nodes are actually configured: the systemd unit deployed by
// Ansible sets environment variables, and a value antsd cannot read must leave the default in
// place rather than propagate a zero.
func TestEnvFallbacks(t *testing.T) {
	const key = "ANTSD_TEST_VALUE"

	t.Run("unset", func(t *testing.T) {
		if got := envOr(key, "fallback"); got != "fallback" {
			t.Errorf("envOr = %q, want the fallback", got)
		}
		if got := envOrBool(key, true); !got {
			t.Error("envOrBool did not fall back")
		}
		if got := envOrDuration(key, time.Hour); got != time.Hour {
			t.Errorf("envOrDuration = %s, want the fallback", got)
		}
	})

	t.Run("set", func(t *testing.T) {
		t.Setenv(key, "false")
		if got := envOrBool(key, true); got {
			t.Error("envOrBool ignored the environment")
		}

		t.Setenv(key, "2m")
		if got := envOrDuration(key, time.Hour); got != 2*time.Minute {
			t.Errorf("envOrDuration = %s, want 2m", got)
		}

		t.Setenv(key, "9100")
		if got := envOrInt(key, 9000); got != 9100 {
			t.Errorf("envOrInt = %d, want 9100", got)
		}
	})

	t.Run("unreadable", func(t *testing.T) {
		t.Setenv(key, "yes-please")
		if got := envOrBool(key, true); !got {
			t.Error("envOrBool did not fall back on an unreadable value")
		}
		if got := envOrDuration(key, time.Hour); got != time.Hour {
			t.Errorf("envOrDuration = %s, want the fallback on an unreadable value", got)
		}
		if got := envOrInt(key, 9000); got != 9000 {
			t.Errorf("envOrInt = %d, want the fallback on an unreadable value", got)
		}
	})
}

// TestStringHidesTheToken checks that the configuration dumped in the logs at startup does not
// carry the pre-shared cluster token.
func TestStringHidesTheToken(t *testing.T) {
	c := validConfig()
	c.K3sToken = "secret-join-token"

	if got := c.String(); strings.Contains(got, c.K3sToken) {
		t.Errorf("the K3s token leaked into the configuration dump: %s", got)
	}
}
