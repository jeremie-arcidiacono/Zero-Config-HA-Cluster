package k3s

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	// installScriptPath is the K3s install script bundled in the ants-os image.
	installScriptPath = "/usr/local/bin/install-k3s.sh"

	// binPath is the K3s binary bundled in the ants-os image.
	binPath = "/usr/local/bin/k3s"

	// agentKubeconfigPath is the kubelet credential written by the K3s agent.
	agentKubeconfigPath = "/var/lib/rancher/k3s/agent/kubelet.kubeconfig"

	// readyTimeout bounds the readiness probes: K3s can take a while on the first start.
	readyTimeout = 5 * time.Minute // todo : use smaller timeout ?

	// readyPollInterval is the delay between two readiness probes.
	readyPollInterval = 3 * time.Second
)

// ExecInstaller drives the K3s installation by executing the install script.
type ExecInstaller struct {
	logger *slog.Logger

	// token is the pre-shared cluster join token (identical on every node).
	token string
}

// NewExecInstaller returns an Installer that runs the install script.
func NewExecInstaller(token string, logger *slog.Logger) *ExecInstaller {
	return &ExecInstaller{logger: logger, token: token}
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
// "k3s kubectl" until it answers ok, and gives up after readyTimeout.
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
// until it reports this node as Ready, and gives up after readyTimeout.
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

// pollUntilReady runs probe every readyPollInterval until it succeeds, the
// context is canceled, or readyTimeout expires. role only labels the logs.
func (i *ExecInstaller) pollUntilReady(ctx context.Context, role string, probe func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()

	ticker := time.NewTicker(readyPollInterval)
	defer ticker.Stop()

	for {
		err := probe(ctx)
		if err == nil {
			i.logger.Info("local k3s is ready", "role", role)
			return nil
		}
		i.logger.Debug("k3s not ready yet", "role", role, "error", err)

		select {
		case <-ctx.Done():
			return fmt.Errorf("k3s %s did not become ready within %s: %w", role, readyTimeout, ctx.Err())
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
