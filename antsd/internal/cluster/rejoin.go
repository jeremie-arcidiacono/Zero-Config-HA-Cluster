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
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
)

// rejoinTimeout bounds the wait for the already-installed K3s to report ready again.
//
// It is generous because if the whole cluster is restarting, we wait on the rest of the cluster to come back too.
const rejoinTimeout = 10 * time.Minute

// startRejoin begins the rejoin workflow from the state left by a previous boot.
func (m *Manager) startRejoin() {
	persisted := *m.persistedState
	m.logger.Info("persisted state found, rejoining the cluster",
		"path", m.config.StateFilePath,
		"role", persisted.Role,
		"boot_count", persisted.BootCount,
		"first_boot_completed_at", persisted.FirstBootCompletedAt)

	// GUARD: a rename between two boots leaves the previous name behind in the K3s cluster
	if persisted.NodeName != m.config.NodeName {
		m.failRejoin(fmt.Errorf("this node is named %q but its persisted state was written by %q: "+
			"a renamed node must be factory reset", m.config.NodeName, persisted.NodeName))
		return
	}

	m.transition(node.StateRejoinCluster)

	m.startK3sOperation(func(ctx context.Context) error {
		// One timeout for the whole sequence
		ctx, cancel := context.WithTimeout(ctx, m.rejoinTimeout)
		defer cancel()

		if err := m.checkInstalledRole(ctx, persisted.Role); err != nil {
			return err
		}

		waitReady := m.installer.WaitAgentReady
		if persisted.Role == node.RoleServer {
			waitReady = m.installer.WaitServerReady
		}
		if err := waitReady(ctx); err != nil {
			return fmt.Errorf("k3s did not report ready within %s: %w", m.rejoinTimeout, err)
		}
		return nil
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
