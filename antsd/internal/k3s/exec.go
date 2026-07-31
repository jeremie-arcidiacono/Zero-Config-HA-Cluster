package k3s

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
)

const (
	// installScriptPath is the K3s install script bundled in the ants-os image.
	installScriptPath = "/usr/local/bin/install-k3s.sh"

	// binPath is the K3s binary bundled in the ants-os image.
	binPath = "/usr/local/bin/k3s"

	// agentKubeconfigPath is the kubelet credential written by the K3s agent.
	agentKubeconfigPath = "/var/lib/rancher/k3s/agent/kubelet.kubeconfig"

	// systemdUnitDir is where the K3s install script writes its service unit.
	systemdUnitDir = "/etc/systemd/system"

	// systemdServerUnit and systemdAgentUnit are the service units created by the K3s install script.
	// Their presence shows the role of an existing installation.
	systemdServerUnit = "k3s.service"
	systemdAgentUnit  = "k3s-agent.service"

	// readyPollInterval is the delay between two readiness probes.
	readyPollInterval = 3 * time.Second

	// readyLogEvery is the number of failed probes between two warnings, so
	// that an unbounded wait stays visible in the logs without flooding them.
	readyLogEvery = 20
)

// ExecInstaller drives the K3s installation by executing the install script.
type ExecInstaller struct {
	logger *slog.Logger

	// token is the pre-shared cluster join token (identical on every node).
	token string

	// unitDir is the systemd unit directory.
	unitDir string
}

// NewExecInstaller returns an Installer that runs the install script.
func NewExecInstaller(token string, logger *slog.Logger) *ExecInstaller {
	return &ExecInstaller{logger: logger, token: token, unitDir: systemdUnitDir}
}

func (i *ExecInstaller) InstallServerInit(ctx context.Context) error {
	return i.runInstallScript(ctx, []string{
		"INSTALL_K3S_EXEC=server --cluster-init",
	})
}

func (i *ExecInstaller) InstallServerJoin(ctx context.Context, serverIP string) error {
	return i.runInstallScript(ctx, []string{
		fmt.Sprintf("INSTALL_K3S_EXEC=server --server %s", joinURL(serverIP)),
	})
}

func (i *ExecInstaller) InstallAgent(ctx context.Context, serverIP string) error {
	return i.runInstallScript(ctx, []string{
		"INSTALL_K3S_EXEC=agent",
		"K3S_URL=" + joinURL(serverIP),
	})
}

// runInstallScript executes the install script with the air-gap environment
// plus the mode-specific variables in extraEnv.
func (i *ExecInstaller) runInstallScript(ctx context.Context, extraEnv []string) error {
	cmd := exec.CommandContext(ctx, installScriptPath)
	cmd.Env = append(os.Environ(),
		"INSTALL_K3S_SKIP_DOWNLOAD=true",
		"K3S_TOKEN="+i.token,
	)
	cmd.Env = append(cmd.Env, extraEnv...)

	i.logger.Info("running k3s install script", "mode_env", extraEnv)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("k3s install script failed: %w (output: %s)", err, tail(output))
	}

	i.logger.Debug("k3s install script completed", "output", string(output))
	return nil
}

// WaitServerReady polls the local Kubernetes API readiness endpoint through
// "k3s kubectl" until it answers OK or ctx is canceled.
func (i *ExecInstaller) WaitServerReady(ctx context.Context) error {
	return i.pollUntilReady(ctx, "server", func(ctx context.Context) error {
		probe := exec.CommandContext(ctx, binPath, "kubectl", "get", "--raw=/readyz")
		if output, err := probe.CombinedOutput(); err != nil {
			return fmt.Errorf("%w (output: %s)", err, tail(output))
		}
		return nil
	})
}

// WaitAgentReady polls the cluster through "k3s kubectl"
// until it reports this node as Ready or ctx is canceled.
//
// An agent hosts no API server, so readiness has to be asked of the control plane.
func (i *ExecInstaller) WaitAgentReady(ctx context.Context) error {
	// todo : use same name in config, serf and k3s
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("resolve hostname used as the k3s node name: %w", err)
	}
	nodeName := strings.ToLower(strings.TrimSpace(hostname)) // DNS RFC-1123 name rule

	const readyConditionPath = `jsonpath={.status.conditions[?(@.type=="Ready")].status}`

	return i.pollUntilReady(ctx, "agent", func(ctx context.Context) error {
		probe := exec.CommandContext(ctx, binPath, "kubectl",
			"--kubeconfig", agentKubeconfigPath,
			"get", "node", nodeName, // Limit the scope to this node only, because an agent is not authorized to get all nodes
			"-o", readyConditionPath)

		output, err := probe.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w (output: %s)", err, tail(output))
		}
		if condition := strings.TrimSpace(string(output)); condition != "True" {
			return fmt.Errorf("node %q is not Ready yet (condition: %q)", nodeName, condition)
		}
		return nil
	})
}

// InstalledRole recovers the role of an existing installation from the name of
// the systemd unit created by the installation script.
//
// Finding both units means a previous installation was not cleaned up before
// another one ran: the node is in an ambiguous state.
func (i *ExecInstaller) InstalledRole(context.Context) (node.Role, error) {
	hasServer, err := systemdUnitExists(filepath.Join(i.unitDir, systemdServerUnit))
	if err != nil {
		return "", err
	}
	hasAgent, err := systemdUnitExists(filepath.Join(i.unitDir, systemdAgentUnit))
	if err != nil {
		return "", err
	}

	switch {
	case hasServer && hasAgent:
		return "", fmt.Errorf("ambiguous k3s installation: both %s and %s exist in %s",
			systemdServerUnit, systemdAgentUnit, i.unitDir)
	case hasServer:
		return node.RoleServer, nil
	case hasAgent:
		return node.RoleAgent, nil
	default:
		return "", fmt.Errorf("no k3s unit found in %s: %w", i.unitDir, ErrNotInstalled)
	}
}

// systemdUnitExists reports whether a systemd unit file is present.
func systemdUnitExists(path string) (bool, error) {
	switch _, err := os.Stat(path); {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
}

// pollUntilReady runs probe every readyPollInterval until it succeeds or the
// context is canceled.
// role only labels the logs.
// The wait is unbounded: the deadline belongs to the caller.
func (i *ExecInstaller) pollUntilReady(ctx context.Context, role string, probe func(context.Context) error) error {
	ticker := time.NewTicker(readyPollInterval)
	defer ticker.Stop()

	started := time.Now()
	for attempt := 1; ; attempt++ {
		err := probe(ctx)
		if err == nil {
			i.logger.Info("local k3s is ready", "role", role, "waited", time.Since(started).Round(time.Second))
			return nil
		}

		// The first failures are expected (K3s is still starting).
		// Report periodically so that a node stuck waiting forever still remains visible.
		if attempt%readyLogEvery == 0 {
			i.logger.Warn("k3s still not ready",
				"role", role, "attempts", attempt,
				"waited", time.Since(started).Round(time.Second), "error", err)
		} else {
			// todo remove this ?
			i.logger.Debug("k3s not ready yet", "role", role, "attempt", attempt, "error", err)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("gave up waiting for k3s %s readiness after %s: %w (last probe error: %v)",
				role, time.Since(started).Round(time.Second), ctx.Err(), err)
		case <-ticker.C:
		}
	}
}

// tail returns the last part of a command output.
// Used to keep error messages short.
func tail(output []byte) string {
	const maxLen = 1024
	output = bytes.TrimSpace(output)
	if len(output) > maxLen {
		output = output[len(output)-maxLen:]
	}
	return string(output)
}
