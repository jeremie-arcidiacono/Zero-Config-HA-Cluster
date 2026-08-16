package cluster

// Steps shared by the first-boot workflows.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/k3s"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
)

// firstBootTimeout bounds a whole first-boot installation: running the K3s install script and then waiting
// for it to report ready.
//
// It is generous: expiring here put a terminal state (a factory reset is the only way out).
// The bound exists to guarantee the node stops blocking every other etcd membership change.
const firstBootTimeout = 10 * time.Minute

// ensureK3sIsNotInstalled refuses a first-boot installation on a node that already runs K3s.
//
// A node that failed its first boot has no state file but may have a working K3s: (e.g., the readiness probe timed out).
// Rebooting it would replay the first boot and reinstall over a live etcd.
func (m *Manager) ensureK3sIsNotInstalled(ctx context.Context) error {
	role, err := m.installer.InstalledRole(ctx)
	switch {
	case errors.Is(err, k3s.ErrNotInstalled):
		return nil
	case err != nil:
		return fmt.Errorf("cannot check for an existing k3s installation: %w", err)
	default:
		// todo : instead of a factory reset, antsd should manually delete the node inside the k3s cluster ?
		// otherwise, it could have a "duplicate name" during the next first boot.
		return fmt.Errorf("k3s is already installed as %q while this node has no persisted state: "+
			"a factory reset is required", role)
	}
}

// installThenWaitReady runs an installation step, then waits for K3s readiness.
//
// The installation script reports a failure when K3s do not come up on its first attempt.
// But the systemd unit has Restart=always, so K3s retries on its own.
// That's why we use the readiness probe instead of the exit code.
// A script error is only fatal when it left no systemd unit behind, meaning the
// installation itself never happened.
func (m *Manager) installThenWaitReady(
	ctx context.Context,
	install func(context.Context) error,
	waitReady func(context.Context) error,
) error {
	// GUARD: refuse to install over an existing K3s, even if the node has no persisted state.
	if err := m.ensureK3sIsNotInstalled(ctx); err != nil {
		return err
	}

	// One timeout for the whole sequence
	ctx, cancel := context.WithTimeout(ctx, firstBootTimeout)
	defer cancel()

	if err := install(ctx); err != nil {
		// roleErr check if the installation left a systemd unit (it also covers an ambiguous double role installation)
		if _, roleErr := m.installer.InstalledRole(ctx); roleErr != nil {
			return fmt.Errorf("k3s install script failed, leaving no usable installation (%v): %w", roleErr, err)
		}
		m.logger.Warn("k3s install script reported a failure but wrote its systemd unit, "+
			"letting the readiness probe decide", "error", err)
	}

	if err := waitReady(ctx); err != nil {
		return fmt.Errorf("k3s was not installed and ready within %s: %w", firstBootTimeout, err)
	}
	return nil
}

// buildFirstBootState builds the state persisted at the end of a first boot.
func (m *Manager) buildFirstBootState(role node.Role) node.PersistedState {
	return node.PersistedState{
		NodeName:             m.config.NodeName,
		Role:                 role,
		BootCount:            1,
		FirstBootCompletedAt: time.Now(),
	}
}
