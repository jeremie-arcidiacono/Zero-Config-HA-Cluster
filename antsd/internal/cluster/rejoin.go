package cluster

// Rejoin-cluster workflow.
//
// A node that finds a state file on startup already belongs to a cluster: it
// rebooted, crashed, or lost power. K3s is installed and enabled as a systemd
// service, so it is already restarting on its own and reconnecting to its
// peers. antsd only checks that what is on disk is coherent, waits for the local
// K3s to report ready again, and goes back to its stable state.
//
// This path never falls back to the first-boot protocol: doing so
// would rerun the K3s installation script over existing data.

import (
	"context"
	"fmt"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
)

// startRejoin begins the rejoin workflow from the state left by a previous boot.
func (m *Manager) startRejoin() {
	persisted := *m.persistedState
	m.logger.Info("persisted state found, rejoining the cluster",
		"path", m.config.StateFilePath,
		"role", persisted.Role,
		"boot_count", persisted.BootCount,
		"first_boot_completed_at", persisted.FirstBootCompletedAt)

	if persisted.NodeName != m.config.NodeName {
		// todo : this should be fatal after we unify the nodeName between config, serf and k3s. nodeName should not change
		m.logger.Warn("persisted node name differs from the configured one",
			"persisted", persisted.NodeName, "configured", m.config.NodeName)
	}

	m.transition(node.StateRejoinCluster)

	m.startK3sOperation(func(ctx context.Context) error {
		// No timeout here
		if err := m.checkInstalledRole(ctx, persisted.Role); err != nil {
			return err
		}
		if persisted.Role == node.RoleServer {
			return m.installer.WaitServerReady(ctx)
		}
		return m.installer.WaitAgentReady(ctx)
	})
}

// checkInstalledRole verifies that the K3s installation on disk is the one the persisted state describes.
func (m *Manager) checkInstalledRole(ctx context.Context, want node.Role) error {
	installed, err := m.installer.InstalledRole(ctx)
	if err != nil {
		return fmt.Errorf("read the installed k3s role: %w", err)
	}
	if installed != want {
		return fmt.Errorf("k3s is installed as %q but the persisted state says %q", installed, want)
	}
	return nil
}

// onRejoinReady completes the rejoin: the local K3s is ready.
func (m *Manager) onRejoinReady() {
	updatedState := *m.persistedState
	updatedState.BootCount++

	m.logger.Info("k3s is ready again, node back in the cluster", "role", updatedState.Role)
	m.becomeStable(updatedState)
}

// failRejoin logs the error and puts the node in the terminal failed rejoin state.
func (m *Manager) failRejoin(err error) {
	m.logger.Error("rejoin failed", "error", err, "state", m.state)
	m.transition(node.StateRejoinFailed)
}
