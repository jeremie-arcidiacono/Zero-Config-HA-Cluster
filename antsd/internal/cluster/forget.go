package cluster

// Forget-me protocol: a virgin machine has the cluster erase what it still knows under its name,
// before installing anything.
//
// A factory reset only wipes the local disk: the machine cannot ask the cluster anything on its way
// out, because the reset is a physical button independent of antsd and K3s.
// So the cluster keeps a ghost of that machine: its node object and, if
// it was a server, its etcd member. The machine then comes back with the same name (derived from
// its MAC address):
//   - K3s refuses the registration of a node whose password no longer matches the stored secret,
//   - a ghost etcd member keeps its seat in the quorum forever, which blocks every later promotion,
//   - and the rescaling eviction can never clean it up, since it only removes machines Serf reports
//     as failed, while this one is back and alive.
//
// The machine therefore asks first and installs second:
//
//	joiner       antsd:forget-me {name}   ->  every node
//	coordinator  deletes the K3s node object (which also removes its etcd member and its secret)
//	coordinator  antsd:forgotten {name}   ->  every node
//	joiner       installs K3s
//
// The joiner keeps asking until it gets an answer, and never installs without one (Serf user events are best-effort
// broadcasts, and the coordinator may be busy).
// Waiting is safer.
//
// This protocol covers the joining path only. A ghost implies a surviving cluster, and a virgin
// machine that sees one always takes the joining path rather than the bootstrap one.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
)

// Events of the protocol. Both payloads are a JSON forgetOrder.
const (
	// eventForgetMe is the request, broadcast by the virgin machine.
	eventForgetMe = "antsd:forget-me"

	// eventForgotten is the confirmation, broadcast by the coordinator that did the cleanup.
	eventForgotten = "antsd:forgotten"
)

const (
	// forgetRetryInterval is the delay between two requests, while no confirmation comes back.
	forgetRetryInterval = 15 * time.Second

	// forgetLogEvery is the number of unanswered requests between two warnings, so a machine
	// waiting forever don't flood the logs.
	forgetLogEvery = 8

	// forgetProbeTimeout bounds the K3s lookup answering a request.
	// Needed because a stuck kubectl would leak one goroutine per request.
	forgetProbeTimeout = 30 * time.Second
)

// forgetOrder names the machine the cluster must forget.
type forgetOrder struct {
	Name string `json:"name"`
}

// --- Joiner side ---

// startForgetMe begins the cleanup phase.
func (m *Manager) startForgetMe() {
	m.transition(node.StateJoiningCleanup)
	m.startK3sOperation(m.ensureK3sIsNotInstalled)
}

// onJoiningCleanupChecked runs once this machine is known to hold no K3s installation.
func (m *Manager) onJoiningCleanupChecked() {
	m.logger.Info("asking the cluster to forget any node registered under this name",
		"node", m.config.NodeName)

	m.joining.forgetAttempts = 0
	m.sendForgetMe()
}

// sendForgetMe broadcasts one request and schedules the next one.
func (m *Manager) sendForgetMe() {
	m.joining.forgetAttempts++

	payload, err := json.Marshal(forgetOrder{Name: m.config.NodeName})
	if err != nil {
		m.failJoining(fmt.Errorf("encode the forget-me request: %w", err))
		return
	}
	if err := m.serf.SendUserEvent(eventForgetMe, payload); err != nil {
		// Not fatal: the next attempt may go through.
		m.logger.Warn("failed to broadcast the forget-me request", "error", err)
	}

	if m.joining.forgetAttempts%forgetLogEvery == 0 {
		m.logger.Warn("still waiting for the cluster to confirm this name is free",
			"node", m.config.NodeName, "attempts", m.joining.forgetAttempts)
	}

	m.joining.stopTimer()
	m.joining.timer = time.AfterFunc(m.forgetRetryInterval, func() {
		_ = m.submit(command{typ: cmdForgetRetry})
	})
}

// onForgetRetry asks again, as long as no confirmation arrived.
func (m *Manager) onForgetRetry() {
	if m.state != node.StateJoiningCleanup {
		return
	}
	m.sendForgetMe()
}

// onForgotten handles the coordinator's confirmation on the machine that asked.
func (m *Manager) onForgotten(payload []byte) {
	var order forgetOrder
	if err := json.Unmarshal(payload, &order); err != nil {
		m.logger.Warn("ignoring an unreadable forget confirmation", "error", err)
		return
	}
	if order.Name != m.config.NodeName || m.state != node.StateJoiningCleanup {
		return // Not for us, or we are not waiting for one.
	}

	m.logger.Info("the cluster confirmed this name is free, resuming the join", "node", order.Name)
	m.joining.stopTimer()
	m.joining.cleaned = true

	m.transition(node.StateJoiningWaiting)
	m.maybeJoinCluster(m.observeCluster()) // The server this node was going to join may have changed while the cleanup ran.
}

// --- Coordinator side ---

// onForgetMe handles a request on the machine that holds the coordinator turn.
// It only looks the name up (read-only).
// A machine with nothing to erase is confirmed straight away, even while the
// cluster is busy with another etcd operation. Only a real leftover makes the sender wait.
func (m *Manager) onForgetMe(payload []byte) {
	var order forgetOrder
	if err := json.Unmarshal(payload, &order); err != nil {
		m.logger.Warn("ignoring an unreadable forget-me request", "error", err)
		return
	}
	if !m.canForget(m.observeCluster(), order.Name) {
		return
	}

	// Lookup run in a goroutine: kubectl must not block the run loop.
	name := order.Name
	ctx, cancel := context.WithTimeout(m.ctx, forgetProbeTimeout)
	go func() {
		defer cancel()

		found, err := m.controlPlane.NodeExists(ctx, name)
		if err != nil {
			// Not fatal: the sender will ask again.
			m.logger.Warn("failed to look up a machine in the K3s cluster", "node", name, "error", err)
			return
		}
		_ = m.submit(command{typ: cmdForgetProbed, name: name, found: found})
	}()
}

// canForget reports whether this node may act on a request naming the given machine.
func (m *Manager) canForget(view clusterView, name string) bool {
	if m.state != node.StateStableServer {
		return false // Only a server runs cluster-wide operations, and only an idle one.
	}
	if !view.isRescaleCoordinator(m.config.NodeName) {
		return false // One machine answers, the same one that drives the rescaling.
	}

	// GUARD: only a machine that is virgin can be erased.
	if !view.isVirginMember(name) {
		m.logger.Warn("refusing to forget a machine that is not on its first boot", "node", name)
		return false
	}
	return true
}

// onForgetProbed acts on what K3s answered about a machine that asked to be forgotten.
func (m *Manager) onForgetProbed(name string, found bool) {
	view := m.observeCluster()
	if !m.canForget(view, name) {
		return // Membership could have changed while the lookup ran.
	}

	if !found {
		m.logger.Debug("K3s knows nothing under this name, nothing to erase", "node", name)
		m.confirmForgotten(name)
		return
	}

	// If it was a server, an etcd member deletion will happen.
	if view.isEtcdMembershipChanging() {
		m.logger.Info("a machine on its first boot must be erased from K3s, waiting for the cluster to be free",
			"node", name)
		return
	}

	m.logger.Info("erasing what K3s still knows about a machine on its first boot", "node", name)

	m.rescale.resetProgress()
	m.rescale.cleaning = name

	m.startCoordinationRound(func(ctx context.Context) error {
		return m.controlPlane.DeleteNode(ctx, name)
	})
}

// finishForgetting finish the forgetting round by sending the confirmation to the machine that asked.
func (m *Manager) finishForgetting() {
	name := m.rescale.cleaning
	m.rescale.cleaning = ""

	m.transition(node.StateStableServer)
	m.confirmForgotten(name)

	// Run now the others actions that may have been delayed by the forgetting round.
	m.maybeRescale(m.observeCluster())
}

// confirmForgotten tells the machine that asked that K3s no longer knows its name.
func (m *Manager) confirmForgotten(name string) {
	payload, err := json.Marshal(forgetOrder{Name: name})
	if err != nil {
		m.logger.Error("failed to encode a forget confirmation", "node", name, "error", err)
		return
	}
	if err := m.serf.SendUserEvent(eventForgotten, payload); err != nil {
		// Not fatal: the sender asks again.
		m.logger.Warn("failed to broadcast a forget confirmation", "node", name, "error", err)
		return
	}
	m.logger.Info("machine confirmed unknown to K3s", "node", name)
}
