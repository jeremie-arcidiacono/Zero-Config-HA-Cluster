package cluster

// First-boot workflow: bootstrap path.
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
	"fmt"
	"slices"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/admin"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
)

// bootstrapWaitDelay is the grace period spent in fb_bootstrap_waiting so
// that the confirmation reaches every node before roles are computed.
const bootstrapWaitDelay = 10 * time.Second

// Serf user event names of the bootstrap protocol.
const (
	// eventBootstrapRequested: a user confirmed the creation of a new
	// cluster: every first-boot node must enter fb_bootstrap_waiting.
	eventBootstrapRequested = "antsd:bootstrap-requested"

	// eventBootstrapStart: the waiting period is over: every node computes its role from the current member list.
	eventBootstrapStart = "antsd:bootstrap-start"
)

// bootstrapProgress carries the bootstrap path state between run-loop iterations.
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

	// members is the cohort this node builds the new cluster with, assigned together with role.
	// It tells the cluster this node is creating apart from any other one on the LAN.
	members []string
}

func (b *bootstrapProgress) stopTimer() {
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
}

// isCohortMember reports whether the named node builds the new cluster with this one.
// The cohort is empty until roles are computed, so before that every cluster is a foreign one.
func (b *bootstrapProgress) isCohortMember(name string) bool {
	return slices.Contains(b.members, name)
}

// reset drops the protocol progress when the node leaves the bootstrap path.
func (b *bootstrapProgress) reset() {
	b.stopTimer()
	*b = bootstrapProgress{}
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

	// (GUARD) If a cluster already exists on the LAN : refuse the request.
	if member, found := m.observeCluster().findExistingK3sClusterMember(); found {
		return fmt.Errorf("cannot create a new cluster: node %q already belongs to one (state %q, %s): %w",
			member.Name, member.Tags["state"], member.Status, admin.ErrConflict)
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

// onBootstrapRequested moves a first-boot node to fb_bootstrap_waiting and starts the timer.
func (m *Manager) onBootstrapRequested() {
	if m.state != node.StateDiscovering && m.state != node.StateBootstrapConfirm {
		m.logger.Debug("ignoring bootstrap request", "state", m.state)
		return
	}

	m.transition(node.StateBootstrapWaiting)
	m.bootstrap.timer = time.AfterFunc(m.bootstrapWaitDelay, func() {
		// Notify through a command.
		_ = m.submit(command{typ: cmdBootstrapWaitExpired})
	})
}

// onBootstrapWaitExpired broadcasts the bootstrap start signal.
// Several nodes may do so near-simultaneously: receivers deduplicate by state.
func (m *Manager) onBootstrapWaitExpired() {
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

	view := m.observeCluster()

	names := view.aliveMemberNames()
	rank, err := node.Rank(names, m.config.NodeName)
	if err != nil {
		m.failBootstrap(fmt.Errorf("compute rank: %w", err))
		return
	}

	m.bootstrap.members = names
	m.bootstrap.totalAliveMembers = len(names)
	m.bootstrap.rank = rank
	m.bootstrap.role = node.RoleForRank(rank, len(names))
	m.logger.Info("bootstrap role computed",
		"rank", rank, "total", len(names), "role", m.bootstrap.role)

	if rank == 0 {
		// This node is N0: initialize the cluster, unless one already exists on the LAN (GUARD).
		if member, found := view.findExistingK3sClusterMember(); found {
			m.failBootstrap(fmt.Errorf("refusing to initialize a new cluster: node %q already belongs to one (state %q, %s)",
				member.Name, member.Tags["state"], member.Status))
			return
		}

		m.transition(node.StateBootstrapInstallInit)
		m.startK3sOperation(func(ctx context.Context) error {
			return m.installThenWaitReady(ctx, m.installer.InstallServerInit, m.installer.WaitServerReady)
		})
		return
	}

	// We are not N0: wait until a server is up.
	// It may already be, if this node missed the tag change.
	m.maybeInstallServer(view)
	m.maybeInstallAgent(view)
}

// maybeInstallServer starts the K3s server installation once this node has
// a server role (ranks 1..ServerCount-1), a server to join, and it is its turn.
func (m *Manager) maybeInstallServer(view clusterView) {
	if m.state != node.StateBootstrapWaiting || m.bootstrap.role != node.RoleServer {
		return
	}

	if view.stableServerCount() < m.bootstrap.rank {
		return // Servers join one at a time, in rank order.
	}
	target, found := view.findK3sJoinTarget()
	if !found {
		return
	}

	m.transition(node.StateBootstrapInstallServer)
	m.startK3sOperation(func(ctx context.Context) error {
		return m.installThenWaitReady(
			ctx,
			func(ctx context.Context) error { return m.installer.InstallServerJoin(ctx, target.IP) },
			m.installer.WaitServerReady)
	})
}

// maybeInstallAgent starts the K3s agent installation once this node has an
// agent role, a server to join, and observes the full server quorum.
func (m *Manager) maybeInstallAgent(view clusterView) {
	if m.state != node.StateBootstrapWaiting || m.bootstrap.role != node.RoleAgent {
		return
	}
	if view.stableServerCount() < node.DesiredServerCount(m.bootstrap.totalAliveMembers) {
		return
	}
	target, found := view.findK3sJoinTarget()
	if !found {
		return
	}

	m.transition(node.StateBootstrapInstallAgent)
	m.startK3sOperation(func(ctx context.Context) error {
		return m.installThenWaitReady(
			ctx,
			func(ctx context.Context) error { return m.installer.InstallAgent(ctx, target.IP) },
			m.installer.WaitAgentReady)
	})
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

// failBootstrap logs the error and halts the first-boot protocol.
func (m *Manager) failBootstrap(err error) {
	m.logger.Error("bootstrap failed", "error", err, "state", m.state)
	m.bootstrap.stopTimer()
	m.transition(node.StateBootstrapFailed)
}
