package k3s

// Cluster-wide Kubernetes operations, using kubectl.
//
// Difference with the Installer:
// Installer manage the local node's K3s installation, while ControlPlane acts on the cluster as a whole and can only
// run on a server.

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

const (
	// serverKubeconfigPath is the admin kubeconfig file written by K3s on server nodes.
	serverKubeconfigPath = "/etc/rancher/k3s/k3s.yaml"

	// drainTimeout bounds the eviction of a node's workloads.
	drainTimeout = 2 * time.Minute
)

// ControlPlane performs Kubernetes-level operations.
type ControlPlane interface {
	// DrainNode evicts the workloads hosted by a node, so they are rescheduled before it
	// leaves the cluster or changes role.
	DrainNode(ctx context.Context, name string) error

	// DeleteNode removes a node from K3s the cluster.
	// When the node is a server, it's removed from the etcd membership.
	// Deleting a node the cluster does not know is not an error.
	DeleteNode(ctx context.Context, name string) error

	// NodeExists reports whether the K3s cluster knows a node by that name.
	NodeExists(ctx context.Context, name string) (bool, error)
}

// ExecControlPlane runs the operations through kubectl.
type ExecControlPlane struct {
	logger *slog.Logger

	binPath        string
	kubeconfigPath string
	drainTimeout   time.Duration
}

// NewExecControlPlane returns a ControlPlane driving the local K3s server.
func NewExecControlPlane(logger *slog.Logger) *ExecControlPlane {
	return &ExecControlPlane{
		logger:         logger,
		binPath:        binPath,
		kubeconfigPath: serverKubeconfigPath,
		drainTimeout:   drainTimeout,
	}
}

func (a *ExecControlPlane) DrainNode(ctx context.Context, name string) error {
	a.logger.Info("draining node", "node", name, "timeout", a.drainTimeout)

	_, err := a.kubectl(ctx, "drain", name,
		"--ignore-daemonsets",    //
		"--delete-emptydir-data", // The data is local and disposable by definition
		"--force",                // Evict the pods not managed by a controller
		"--timeout", a.drainTimeout.String())
	return err
}

func (a *ExecControlPlane) DeleteNode(ctx context.Context, name string) error {
	a.logger.Info("deleting node object", "node", name)

	_, err := a.kubectl(ctx, "delete", "node", name, "--ignore-not-found")
	return err
}

func (a *ExecControlPlane) NodeExists(ctx context.Context, name string) (bool, error) {
	output, err := a.kubectl(ctx, "get", "node", name, "--ignore-not-found", "-o", "name")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) != "", nil
}

// kubectl runs one kubectl command on the local K3s server.
func (a *ExecControlPlane) kubectl(ctx context.Context, args ...string) (string, error) {
	// If kubectl command is not the one brought by K3s, the --kubeconfig flag is required.
	full := append([]string{"kubectl", "--kubeconfig", a.kubeconfigPath}, args...)

	output, err := exec.CommandContext(ctx, a.binPath, full...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("kubectl %s: %w (output: %s)", strings.Join(args, " "), err, tail(output))
	}

	a.logger.Debug("kubectl completed", "args", args, "output", string(output))
	return string(output), nil
}
