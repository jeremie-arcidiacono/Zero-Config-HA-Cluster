package cluster

// First-boot bootstrap protocol.
//
// A node that discovered no existing cluster stays in fb_discovering until
// its user requests the creation of a new cluster.
// The confirmation is broadcast so every first-boot node enters fb_bootstrap_waiting and starts a timer.
// The first timer to expire broadcasts the start signal.
// Each node then derives its role from the sorted member list: rank 0 (N0) initializes K3s,
// the next ServerCount-1 nodes join as servers one at a time in rank order, the rest join as agents
// once the quorum is visible.
// All handlers are idempotent: events arriving in an unexpected state are ignored, which absorbs
// duplicates (e.g., two nodes broadcasting bootstrap-start almost simultaneously).

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/admin"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/k3s"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/serfnode"
)

// bootstrapWaitDelay is the grace period spent in fb_bootstrap_waiting so
// that the confirmation reaches every node before roles are computed.
const bootstrapWaitDelay = 10 * time.Second

// bootstrapReadyTimeout bounds the wait for a freshly installed K3s to report ready.
const bootstrapReadyTimeout = 5 * time.Minute

// Serf user event names of the bootstrap protocol.
const (
	// eventBootstrapRequested: a user confirmed the creation of a new
	// cluster: every first-boot node must enter fb_bootstrap_waiting.
	eventBootstrapRequested = "antsd:bootstrap-requested"

	// eventBootstrapStart: the waiting period is over: every node computes its role from the current member list.
	eventBootstrapStart = "antsd:bootstrap-start"
)

// memberStatusAlive is the Serf member status of a live node.
const memberStatusAlive = "alive"

// bootstrapProgress carries the first-boot protocol state between run-loop iterations.
type bootstrapProgress struct {
	// timer is the fb_bootstrap_waiting grace timer.
	timer *time.Timer

	// role is this node's role, assigned when bootstrap-start is received.
	// Empty until then.
	role node.Role

	// rank is this node's position in the sorted member list, assigned together with role.
	rank int

	// totalAliveMembers is the number of alive members when roles were computed, which
	// fixes the expected server count (quorum size).
	totalAliveMembers int
}

func (b *bootstrapProgress) stopTimer() {
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
}

// stateConflict builds the error returned to user actions that are not allowed in the current state.
func stateConflict(action string, state node.State) error {
	return fmt.Errorf("cannot %s in state %q: %w", action, state, admin.ErrConflict)
}

// onRequestBootstrap handles the user asking to create a new cluster
// (screen A -> C).
func (m *Manager) onRequestBootstrap() error {
	if m.state != node.StateDiscovering {
		return stateConflict("request bootstrap", m.state)
	}
	m.transition(node.StateBootstrapConfirm)
	return nil
}

// onCancelBootstrap handles the user going back to discovery (screen C -> A).
func (m *Manager) onCancelBootstrap() error {
	if m.state != node.StateBootstrapConfirm {
		return stateConflict("cancel bootstrap", m.state)
	}
	m.transition(node.StateDiscovering)
	return nil
}

// onConfirmBootstrap handles the user confirming the creation (screen C).
// It only broadcasts the decision: the local transition to fb_bootstrap_waiting happens when
// this node receives its own event, so every node follows the exact same path.
func (m *Manager) onConfirmBootstrap() error {
	if m.state != node.StateBootstrapConfirm {
		return stateConflict("confirm bootstrap", m.state)
	}
	if err := m.serf.SendUserEvent(eventBootstrapRequested, nil); err != nil {
		return fmt.Errorf("broadcast bootstrap request: %w", err)
	}
	return nil
}

// handleUserEvent dispatches a bootstrap protocol event.
func (m *Manager) handleUserEvent(e serfnode.Event) {
	switch e.Name {
	case eventBootstrapRequested:
		m.onBootstrapRequested()
	case eventBootstrapStart:
		m.onBootstrapStart()
	default:
		m.logger.Debug("ignoring unknown serf user event", "name", e.Name)
	}
}

// onBootstrapRequested moves a first-boot node to fb_bootstrap_waiting and starts the timer.
func (m *Manager) onBootstrapRequested() {
	if m.state != node.StateDiscovering && m.state != node.StateBootstrapConfirm {
		m.logger.Debug("ignoring bootstrap request", "state", m.state)
		return
	}

	m.transition(node.StateBootstrapWaiting)
	m.bootstrap.timer = time.AfterFunc(m.bootstrapWaitDelay, func() {
		// Notify through a command.
		_ = m.submit(command{typ: cmdWaitTimerExpired})
	})
}

// onWaitTimerExpired broadcasts the start signal.
// Several nodes may do so near-simultaneously: receivers deduplicate by state.
func (m *Manager) onWaitTimerExpired() {
	if m.state != node.StateBootstrapWaiting {
		return
	}
	m.logger.Info("bootstrap waiting period over, broadcasting start signal")
	if err := m.serf.SendUserEvent(eventBootstrapStart, nil); err != nil {
		m.failBootstrap(fmt.Errorf("broadcast bootstrap start: %w", err))
	}
}

// onBootstrapStart computes this node's role from the alive member list and starts the K3s installation.
func (m *Manager) onBootstrapStart() {
	if m.state != node.StateBootstrapWaiting {
		m.logger.Debug("ignoring bootstrap start", "state", m.state)
		return
	}
	m.bootstrap.stopTimer()

	names := m.aliveMemberNames()
	rank, err := node.Rank(names, m.config.NodeName)
	if err != nil {
		m.failBootstrap(fmt.Errorf("compute rank: %w", err))
		return
	}

	m.bootstrap.totalAliveMembers = len(names)
	m.bootstrap.rank = rank
	m.bootstrap.role = node.RoleForRank(rank, len(names))
	m.logger.Info("bootstrap role computed",
		"rank", rank, "total", len(names), "role", m.bootstrap.role)

	if rank == 0 {
		// TODO : add a guard to prevent the init of a cluster if any stable_* node is found => it would cause double cluster.
		// This node is N0: initialize the cluster.
		m.transition(node.StateBootstrapInstallInit)
		m.startK3sOperation(func(ctx context.Context) error {
			return m.installThenWaitReady(ctx, m.installer.InstallServerInit, m.installer.WaitServerReady)
		})
		return
	}

	// We are not N0: wait until a server is up.
	// It may already be, if this node missed the tag change.
	m.maybeInstallServer()
	m.maybeInstallAgent()
}

// maybeInstallServer starts the K3s server installation once this node has
// a server role (ranks 1..ServerCount-1), a server to join, and it is its turn.
func (m *Manager) maybeInstallServer() {
	if m.state != node.StateBootstrapWaiting || m.bootstrap.role != node.RoleServer {
		return
	}

	if m.stableServerCount() < m.bootstrap.rank {
		return // Servers join one at a time, in rank order.
	}
	serverIP := m.joinTargetIP()
	if serverIP == "" {
		return
	}

	m.transition(node.StateBootstrapInstallServer)
	m.startK3sOperation(func(ctx context.Context) error {
		return m.installThenWaitReady(
			ctx,
			func(ctx context.Context) error { return m.installer.InstallServerJoin(ctx, serverIP) },
			m.installer.WaitServerReady)
	})
}

// maybeInstallAgent starts the K3s agent installation once this node has an
// agent role, a server to join, and observes the full server quorum.
func (m *Manager) maybeInstallAgent() {
	if m.state != node.StateBootstrapWaiting || m.bootstrap.role != node.RoleAgent {
		return
	}
	if m.stableServerCount() < node.DesiredServerCount(m.bootstrap.totalAliveMembers) {
		return
	}
	serverIP := m.joinTargetIP()
	if serverIP == "" {
		return
	}

	m.transition(node.StateBootstrapInstallAgent)
	m.startK3sOperation(func(ctx context.Context) error {
		return m.installThenWaitReady(
			ctx,
			func(ctx context.Context) error { return m.installer.InstallAgent(ctx, serverIP) },
			m.installer.WaitAgentReady)
	})
}

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

	if err := install(ctx); err != nil {
		// roleErr check if the installation left a systemd unit (it also covers an ambiguous double role installation)
		if _, roleErr := m.installer.InstalledRole(ctx); roleErr != nil {
			return fmt.Errorf("k3s install script failed, leaving no usable installation (%v): %w", roleErr, err)
		}
		m.logger.Warn("k3s install script reported a failure but wrote its systemd unit, "+
			"letting the readiness probe decide", "error", err)
	}
	return waitBootstrapReady(ctx, waitReady)
}

// waitBootstrapReady runs a readiness probe with the first-boot deadline.
func waitBootstrapReady(ctx context.Context, waitReady func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, bootstrapReadyTimeout)
	defer cancel()

	if err := waitReady(ctx); err != nil {
		return fmt.Errorf("k3s did not become ready within %s: %w", bootstrapReadyTimeout, err)
	}
	return nil
}

// onBootstrapInstallSucceeded finalizes the installation operation that just completed.
func (m *Manager) onBootstrapInstallSucceeded() {
	switch m.state {
	case node.StateBootstrapInstallInit:
		// todo : add a delay to ensure k3s is not too busy while finishing is own install ?
		m.becomeStable(m.buildFirstBootState(node.RoleServer))
	case node.StateBootstrapInstallServer:
		m.becomeStable(m.buildFirstBootState(node.RoleServer))
	case node.StateBootstrapInstallAgent:
		m.becomeStable(m.buildFirstBootState(node.RoleAgent))
	}
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

// failBootstrap logs the error and halts the first-boot protocol.
func (m *Manager) failBootstrap(err error) {
	m.logger.Error("bootstrap failed", "error", err, "state", m.state)
	m.bootstrap.stopTimer()
	m.transition(node.StateBootstrapFailed)
}

// becomeStable enters the stable state of the node's role and persists state on disk.
// todo : move to a new shared file, because it's used in both bootstrap and rejoin workflows ?
func (m *Manager) becomeStable(state node.PersistedState) {
	if err := state.Save(m.config.StateFilePath); err != nil {
		m.logger.Error("failed to persist node state", "path", m.config.StateFilePath, "error", err)
	} else {
		m.logger.Debug("node state persisted",
			"path", m.config.StateFilePath, "role", state.Role, "boot_count", state.BootCount)
	}

	m.persistedState = &state
	m.transition(state.Role.StableState())
}

// aliveMemberNames returns the names of all alive Serf members, this node included.
// todo : better to put this in serfnode ?
func (m *Manager) aliveMemberNames() []string {
	snapshot := m.serf.Snapshot()
	names := make([]string, 0, len(snapshot.Members))
	for _, member := range snapshot.Members {
		if member.Status == memberStatusAlive {
			names = append(names, member.Name)
		}
	}
	return names
}

// joinTargetIP returns the address of the K3s server this node should join,
// or an empty string while no server is up yet.
// The address is read from Serf membership.
// When multiple servers are available: the lowest name wins.
// todo : better to use a random server instead of the lowest name ? (to avoid overloading IO on the same server ?)
func (m *Manager) joinTargetIP() string {
	snapshot := m.serf.Snapshot()
	target := admin.Member{}
	for _, member := range snapshot.Members {
		if member.Status != memberStatusAlive ||
			member.Tags["state"] != string(node.StateStableServer) {
			continue
		}
		if target.Name == "" || member.Name < target.Name {
			target = member
		}
	}
	return target.IP
}

// stableServerCount returns how many alive members currently expose the stable_server state.
func (m *Manager) stableServerCount() int {
	snapshot := m.serf.Snapshot()
	count := 0
	for _, member := range snapshot.Members {
		if member.Status == memberStatusAlive &&
			member.Tags["state"] == string(node.StateStableServer) {
			count++
		}
	}
	return count
}
