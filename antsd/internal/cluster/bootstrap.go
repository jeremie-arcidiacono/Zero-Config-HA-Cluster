package cluster

// First-boot bootstrap protocol.
//
// A node that discovered no existing cluster stays in fb_discovering until
// its user requests the creation of a new cluster.
// The confirmation is broadcast so every first-boot node enters fb_bootstrap_waiting and starts a timer.
// The first timer to expire broadcasts the start signal.
// Each node then derives its role from the sorted member list: rank 0 (N0) initializes K3s,
// the next ServerCount-1 nodes join as servers once N0 is ready, the rest join as agents once the quorum is visible.
// All handlers are idempotent: events arriving in an unexpected state are ignored, which absorbs
// duplicates (e.g., two nodes broadcasting bootstrap-start almost simultaneously).

import (
	"context"
	"fmt"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/admin"
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

	// eventServerReady: N0's K3s is initialized and ready to be joined.
	// Payload: N0's IP address.
	eventServerReady = "antsd:server-ready"
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

	// totalAliveMembers is the number of alive members when roles were computed, which
	// fixes the expected server count (quorum size).
	totalAliveMembers int

	// serverIP is N0's address, learned from the server-ready event.
	serverIP string
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
	case eventServerReady:
		m.onServerReady(string(e.Payload))
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
	m.bootstrap.role = node.RoleForRank(rank, len(names))
	m.logger.Info("bootstrap role computed",
		"rank", rank, "total", len(names), "role", m.bootstrap.role)

	if rank == 0 {
		// This node is N0: initialize the cluster.
		m.transition(node.StateBootstrapInstallInit)
		m.startK3sOperation(func(ctx context.Context) error {
			if err := m.installer.InstallServerInit(ctx); err != nil {
				return err
			}
			return waitBootstrapReady(ctx, m.installer.WaitServerReady)
		})
		return
	}

	// We are not N0: wait for the server-ready signal.
	// It may already have been received if events were delivered out of order (really unlikely ?)
	m.maybeInstallServer()
	m.maybeInstallAgent()
}

// onServerReady records N0's address and unblocks the joining nodes.
func (m *Manager) onServerReady(serverIP string) {
	if serverIP == "" {
		m.logger.Warn("ignoring server-ready event with empty payload")
		return
	}
	m.bootstrap.serverIP = serverIP
	m.maybeInstallServer()
	m.maybeInstallAgent()
}

// maybeInstallServer starts the K3s server installation once this node has
// a server role (ranks 1..ServerCount-1) and knows N0's address.
func (m *Manager) maybeInstallServer() {
	if m.state != node.StateBootstrapWaiting ||
		m.bootstrap.role != node.RoleServer ||
		m.bootstrap.serverIP == "" {
		return
	}

	serverIP := m.bootstrap.serverIP
	m.transition(node.StateBootstrapInstallServer)
	m.startK3sOperation(func(ctx context.Context) error {
		if err := m.installer.InstallServerJoin(ctx, serverIP); err != nil {
			return err
		}
		return waitBootstrapReady(ctx, m.installer.WaitServerReady)
	})
}

// maybeInstallAgent starts the K3s agent installation once this node has an
// agent role, knows N0's address, and observes the full server quorum.
func (m *Manager) maybeInstallAgent() {
	if m.state != node.StateBootstrapWaiting ||
		m.bootstrap.role != node.RoleAgent ||
		m.bootstrap.serverIP == "" {
		return
	}
	if m.stableServerCount() < node.DesiredServerCount(m.bootstrap.totalAliveMembers) {
		return
	}

	serverIP := m.bootstrap.serverIP
	m.transition(node.StateBootstrapInstallAgent)
	m.startK3sOperation(func(ctx context.Context) error {
		if err := m.installer.InstallAgent(ctx, serverIP); err != nil {
			return err
		}
		return waitBootstrapReady(ctx, m.installer.WaitAgentReady)
	})
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
		// Become stable first, so the state tag update precedes the ready
		// signal: joining nodes then count this node in the quorum.
		m.becomeStable(m.buildFirstBootState(node.RoleServer))
		if err := m.serf.SendUserEvent(eventServerReady, []byte(m.serf.LocalIP())); err != nil {
			// The cluster cannot progress without this signal: other nodes would stay in fb_bootstrap_waiting.
			m.logger.Error("failed to broadcast server-ready", "error", err)
		}
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
