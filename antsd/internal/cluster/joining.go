package cluster

// First-boot workflow: joining path.
//
// A virgin machine that starts beside a running cluster must never create a second one:
// it joins the existing cluster without any user interaction.
//
// The node moves to fb_joining_waiting as soon as it sees a joinable server, and lets a grace
// timer run so the membership (and its tags) reaches it before the join target is picked.
// It then always installs K3s as an agent: sizing the control plane belongs to the rescaling
// workflow, which promotes this machine afterward if the population needs it.
// An agent never touches the etcd membership, so joining machines install in parallel and take no
// cluster-wide turn.
//
// The decision is re-evaluated on every Serf event: a node that sees no reachable
// server simply waits, it never falls back to the bootstrap protocol.

import (
	"context"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
)

// joinWaitDelay is the grace period spent in fb_joining_waiting so that the
// membership settles before the join target is picked.
const joinWaitDelay = 10 * time.Second

// joiningProgress carries the joining path state between run-loop iterations.
type joiningProgress struct {
	// timer is the fb_joining_waiting grace timer, then the forget-me retry timer.
	timer *time.Timer

	// settled tells whether the grace period is over, meaning the node can install.
	settled bool

	// cleaned tells whether the cluster confirmed it knows no K3s node under this name.
	cleaned bool

	// forgetAttempts counts the forget-me requests sent, only to space out the log warnings.
	forgetAttempts int
}

func (j *joiningProgress) stopTimer() {
	if j.timer != nil {
		j.timer.Stop()
		j.timer = nil
	}
}

// maybeStartJoiningPath diverts a first-boot node to the joining path as soon as a
// cluster it is not building itself becomes joinable.
//
// The decision is made with the join target rather than "any node already in a cluster ?": a cluster whose
// servers are all restarting or dead cannot be joined.
// Creating a cluster next to a recovering one is refused separately, by onRequestBootstrap
// and by the cluster-init guard.
func (m *Manager) maybeStartJoiningPath(view clusterView) {
	if m.state != node.StateDiscovering &&
		m.state != node.StateBootstrapConfirm &&
		m.state != node.StateBootstrapWaiting {
		return
	}

	target, found := view.findK3sJoinTarget()
	if !found || m.bootstrap.isCohortMember(target.Name) {
		return // The server is part of the current bootstrap, so keep bootstrapping instead of joining.
	}

	m.logger.Info("existing cluster discovered, joining it instead of bootstrapping",
		"server", target.Name, "server_ip", target.IP)
	m.bootstrap.reset()
	m.transition(node.StateJoiningWaiting)

	m.joining.timer = time.AfterFunc(m.joinWaitDelay, func() {
		// Notify through a command.
		_ = m.submit(command{typ: cmdJoinWaitExpired})
	})
}

// onJoinWaitExpired ends the grace period, in case no further Serf event comes.
func (m *Manager) onJoinWaitExpired() {
	if m.state != node.StateJoiningWaiting {
		return
	}
	m.joining.settled = true
	m.joining.stopTimer()
	m.maybeJoinCluster(m.observeCluster())
}

// maybeJoinCluster starts the K3s installation once the grace period is over and a server is reachable.
func (m *Manager) maybeJoinCluster(view clusterView) {
	if m.state != node.StateJoiningWaiting || !m.joining.settled {
		return
	}

	// Read again: the server seen when the divert happened may be gone.
	target, found := view.findK3sJoinTarget()
	if !found {
		return
	}

	if !m.joining.cleaned {
		m.startForgetMe()
		return
	}
	m.joinAsAgent(target.IP)
}

// joinAsAgent installs K3s as an agent of the discovered cluster.
func (m *Manager) joinAsAgent(serverIP string) {
	m.logger.Info("joining the existing cluster as an agent", "server_ip", serverIP)

	m.transition(node.StateJoiningAgent)
	m.startK3sOperation(func(ctx context.Context) error {
		return m.installThenWaitReady(
			ctx,
			func(ctx context.Context) error { return m.installer.InstallAgent(ctx, serverIP) },
			m.installer.WaitAgentReady)
	})
}

// onJoiningInstallSucceeded finalizes the installation that just completed.
func (m *Manager) onJoiningInstallSucceeded() {
	if m.state == node.StateJoiningAgent {
		m.becomeStable(m.buildFirstBootState(node.RoleAgent))
	}
}

// failJoining logs the error and halts the joining path.
func (m *Manager) failJoining(err error) {
	m.logger.Error("joining an existing cluster failed", "error", err, "state", m.state)
	m.joining.stopTimer()
	m.transition(node.StateJoiningFailed)
}
