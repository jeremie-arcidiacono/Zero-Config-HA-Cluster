package cluster

// Rescaling workflow: eviction, promotion and demotion.
//
// This workflow keeps the cluster in the right shape during its life.
//
// K3s never remove a machine that is durably gone by itself, so a cluster that lost a server stays
// degraded until someone intervenes. antsd make that manual intervention.
//
// Every server evaluates the cluster on each Serf event, but only the
// lowest-named alive stable_server acts (named the coordinator):
// When a deadline expires :
//  1. evicts (from K3s and from the Serf memberlist) the machines that have been Serf-failed longer
//     than the grace period
//  2. compares the number of K3s servers to what the population calls for, and, if they
//     differ for longer than the settle delay, designates one machine to change role.
//
// Eviction comes first, to avoid having leftover in the etcd membership that may still count in the quorum.
//
// The cluster-wide mutex is isEtcdMembershipChanging() to avoid multiple
// operation (joining, rescaling, ...) at the same time.
// Every coordinator step is idempotent: it can die mid-repair, and the next coordinator will redo the work.

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/admin"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
)

const (
	// rescaleConvertTimeout bounds a whole conversion: reinstalling K3s with the other role, then
	// waiting for it to report ready. See firstBootTimeout for more info.
	rescaleConvertTimeout = 10 * time.Minute
)

// eventRescaleConvert carries the coordinator's order to the machine that must change role.
// Payload: a JSON rescaleOrder.
const eventRescaleConvert = "antsd:rescale-convert"

// rescaleOrder designates the machine that changes role, and how it joins back.
type rescaleOrder struct {
	Target string `json:"target"`

	// The receiving node could deduce it's next role from its own role.
	// But the coordinator may be wrong, so the order is ignored if the node is not in the expected state.
	Role   node.Role `json:"role"`
	JoinIP string    `json:"join_ip"`
}

// rescaleProgress carries the rescaling state between run-loop iterations.
type rescaleProgress struct {
	// failedSince records when this node first saw each currently failed member.
	// (Serf does not expose failure timestamp).
	failedSince map[string]time.Time

	// imbalanceControlPlaneSince is when the control plane was first seen off its target size, zero while it
	// has the right one. Used for debounce.
	imbalanceControlPlaneSince time.Time

	// timer wakes the run loop at the next pending deadline.
	timer *time.Timer

	// order is the conversion in progress, used by both sides :
	//  - On the coordinator, the decision it made, cleared once broadcast
	//  - On the designated machine, the order it received, cleared once converted.
	// Zero during an eviction round.
	order rescaleOrder

	// evicting names the machines the current round removes. Empty outside an eviction round.
	evicting []string
}

func (r *rescaleProgress) stopTimer() {
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
}

// armTimer schedules the next evaluation. A zero deadline only cancels the pending timer.
func (r *rescaleProgress) armTimer(deadline, now time.Time, wake func()) {
	r.stopTimer()
	if deadline.IsZero() {
		return
	}
	r.timer = time.AfterFunc(max(deadline.Sub(now), 0), wake)
}

// trackFailures updates the local failure clock.
func (r *rescaleProgress) trackFailures(view clusterView, now time.Time) {
	if r.failedSince == nil {
		r.failedSince = make(map[string]time.Time)
	}

	failed := make(map[string]bool, len(r.failedSince))
	for _, name := range view.failedMembersNames() {
		failed[name] = true
		if _, seen := r.failedSince[name]; !seen {
			r.failedSince[name] = now
		}
	}

	// If a machine came back or was evicted, remove it from the failure tracking.
	maps.DeleteFunc(r.failedSince, func(name string, _ time.Time) bool { return !failed[name] })
}

// forget drops the decisions of a node that no longer holds the coordinator turn.
// The failure clock is kept: it is a useful and long-term observation.
func (r *rescaleProgress) forget() {
	r.stopTimer()
	r.imbalanceControlPlaneSince = time.Time{}
	r.order = rescaleOrder{}
	r.evicting = nil
}

// maybeRescale is the detection half of the workflow, run on every Serf event and timer expiry.
func (m *Manager) maybeRescale(view clusterView) {
	if !m.config.RescaleEnabled {
		return
	}

	// The failure clock must keep going on every node, so a new coordinator does not start without data.
	now := time.Now()
	m.rescale.trackFailures(view, now)

	if m.state != node.StateStableServer {
		return
	}
	if !view.isRescaleCoordinator(m.config.NodeName) {
		m.rescale.forget()
		return
	}

	// Prevent multiple concurrent operations (double rescaling, rescaling + joining, ...)
	if view.isEtcdMembershipChanging() {
		m.rescale.stopTimer()
		return
	}

	evictable, nextEvictionDeadline := view.durablyFailedMembersNames(m.rescale.failedSince, m.evictionGrace, now)
	if len(evictable) > 0 {
		m.startEviction(evictable)
		return
	}

	settled, imbalanceDeadline := m.trackImbalance(view, now)
	if settled && m.startConversion(view) {
		return
	}
	m.scheduleRescaleCheck(earliest(nextEvictionDeadline, imbalanceDeadline), now)
}

// trackImbalance updates the debounce clock and reports whether the control plane has been off its
// target size long enough to start rescaling, together with the instant at which it will have been.
func (m *Manager) trackImbalance(view clusterView, now time.Time) (settled bool, deadline time.Time) {
	if view.k3sServerCount() == view.desiredServerCount() {
		m.rescale.imbalanceControlPlaneSince = time.Time{}
		return false, time.Time{}
	}

	if m.rescale.imbalanceControlPlaneSince.IsZero() {
		m.logger.Info("control plane is off its target size",
			"servers", view.k3sServerCount(),
			"wanted", view.desiredServerCount(),
			"population", view.population())
		m.rescale.imbalanceControlPlaneSince = now
	}

	deadline = m.rescale.imbalanceControlPlaneSince.Add(m.rescaleSettleDelay)
	return !deadline.After(now), deadline
}

// startEviction removes from the cluster the machines that have been gone for too long.
func (m *Manager) startEviction(names []string) {
	m.logger.Info("evicting the machines that are durably gone",
		"nodes", names, "grace", m.evictionGrace)

	// Nodes are not drained: a machine Serf reports as failed answers nothing, so it cannot be asked to drain.

	m.rescale.stopTimer()
	m.rescale.evicting = names
	m.rescale.order = rescaleOrder{}
	m.transition(node.StateRescaleCoordinating)

	m.startK3sOperation(func(ctx context.Context) error {
		for _, name := range names {
			if err := m.clusterAdmin.DeleteNode(ctx, name); err != nil {
				return fmt.Errorf("delete the node object of %s: %w", name, err)
			}
		}
		return nil
	})
}

// finishEviction erases the evicted machines from the Serf memberlist, now that Kubernetes has forgotten them.
func (m *Manager) finishEviction() {
	for _, name := range m.rescale.evicting {
		if err := m.serf.RemoveFailedNode(name); err != nil {
			m.logger.Error("failed to erase an evicted machine from the memberlist",
				"node", name, "error", err)
			continue
		}
		delete(m.rescale.failedSince, name)
		m.logger.Info("machine evicted from the cluster", "node", name)
	}
	m.rescale.evicting = nil
	m.transition(node.StateStableServer)

	// The population just changed, maybe a conversion is needed.
	// No need to wait for Serf to gossip the removed nodes event :
	// only the coordinator acts, and its local memberlist is already updated.
	m.maybeRescale(m.observeCluster())
}

// startConversion designates the machine that changes role and prepares the cluster for it.
// It reports whether a conversion was actually started.
func (m *Manager) startConversion(view clusterView) bool {
	target, role, ok := m.chooseConversionTarget(view)
	if !ok {
		return false
	}

	m.logger.Info("rescaling the control plane",
		"target", target.Name, "new_role", role,
		"servers", view.k3sServerCount(), "wanted", view.desiredServerCount())

	m.rescale.stopTimer()
	m.rescale.imbalanceControlPlaneSince = time.Time{}
	m.rescale.evicting = nil
	m.rescale.order = rescaleOrder{Target: target.Name, Role: role, JoinIP: m.serf.LocalIP()}
	m.transition(node.StateRescaleCoordinating)

	name := target.Name
	m.startK3sOperation(func(ctx context.Context) error {
		return m.prepareConversion(ctx, name)
	})
	return true
}

// chooseConversionTarget picks the machine that must change role, and which role it takes.
func (m *Manager) chooseConversionTarget(view clusterView) (admin.Member, node.Role, bool) {
	switch {
	case view.needsAnotherK3sServer():
		target, found := view.findPromotionTarget()
		if !found {
			m.logger.Debug("control plane too small but no agent available to promote",
				"servers", view.k3sServerCount(), "wanted", view.desiredServerCount())
			return admin.Member{}, "", false
		}
		return target, node.RoleServer, true

	case view.hasTooManyK3sServers():
		target, found := view.findDemotionTarget()
		if !found {
			m.logger.Debug("control plane too large but no server available to demote",
				"servers", view.k3sServerCount(), "wanted", view.desiredServerCount())
			return admin.Member{}, "", false
		}
		if target.Name == m.config.NodeName { // Make sure to not destruct ourselves.
			m.logger.Error("refusing to demote the coordinator itself",
				"servers", view.k3sServerCount(), "wanted", view.desiredServerCount())
			return admin.Member{}, "", false
		}
		return target, node.RoleAgent, true

	default:
		return admin.Member{}, "", false
	}
}

// prepareConversion drain the designated machine and removes it from the K3s cluster, so it can
// register again with its new role.
func (m *Manager) prepareConversion(ctx context.Context, name string) error {
	if err := m.clusterAdmin.DrainNode(ctx, name); err != nil {
		// A pod that refuses to move must not block a degraded cluster, and the DeleteNode evicts it anyway.
		m.logger.Warn("draining the machine before its conversion failed, deleting it anyway",
			"node", name, "error", err)
	}
	if err := m.clusterAdmin.DeleteNode(ctx, name); err != nil {
		return fmt.Errorf("delete the node object of %s: %w", name, err)
	}

	exists, err := m.clusterAdmin.NodeExists(ctx, name)
	if err != nil {
		return fmt.Errorf("check that %s left the cluster: %w", name, err)
	}
	if exists {
		return fmt.Errorf("node %s is still known to the cluster after its deletion", name)
	}
	return nil
}

// onRescaleCoordinationDone finishes a coordination round whose cluster-side work succeeded.
func (m *Manager) onRescaleCoordinationDone() {
	if m.rescale.order.Target == "" { // The round was an eviction, not a conversion.
		m.finishEviction()
		return
	}

	// The round is a conversion:
	// the node as been deleted from K3s, now it must be told to change its local K3s installation.
	order := m.rescale.order
	m.rescale.order = rescaleOrder{}

	payload, err := json.Marshal(order)
	if err != nil {
		m.abandonCoordination(fmt.Errorf("encode the conversion order: %w", err))
		return
	}
	if err := m.serf.SendUserEvent(eventRescaleConvert, payload); err != nil {
		m.abandonCoordination(fmt.Errorf("broadcast the conversion order: %w", err))
		return
	}

	m.logger.Info("conversion order broadcast", "target", order.Target, "new_role", order.Role)
	m.transition(node.StateStableServer)

	// Re-arm rather than wait for a Serf update: the order may never be executed (e.g., target
	// died between the decision and the delivery)
	now := time.Now()
	m.scheduleRescaleCheck(now.Add(m.rescaleSettleDelay), now)
}

// onRescaleConvert handles the coordinator's order on the machine it designates.
func (m *Manager) onRescaleConvert(payload []byte) {
	var order rescaleOrder
	if err := json.Unmarshal(payload, &order); err != nil {
		m.logger.Warn("ignoring an unreadable conversion order", "error", err)
		return
	}
	if order.Target != m.config.NodeName {
		return // The order is for another machine, ignore it.
	}

	// GUARD: makes the order safe to repeat
	if m.state != order.Role.Other().StableState() {
		m.logger.Debug("ignoring a conversion order", "state", m.state, "new_role", order.Role)
		return
	}

	if m.persistedState == nil {
		m.logger.Error("refusing to convert a node with no persisted state", "new_role", order.Role)
		return
	}
	if order.JoinIP == "" {
		m.logger.Error("ignoring a conversion order that carries no server address", "new_role", order.Role)
		return
	}

	m.logger.Info("converting to the role the cluster needs",
		"new_role", order.Role, "server_ip", order.JoinIP)

	m.rescale.order = order
	if order.Role == node.RoleServer {
		m.transition(node.StateRescalePromoting)
	} else {
		m.transition(node.StateRescaleDemoting)
	}

	m.startK3sOperation(func(ctx context.Context) error {
		return m.convertThenWaitReady(ctx, order)
	})
}

// convertThenWaitReady reinstall the local K3s with its new role and waits for it to be ready.
func (m *Manager) convertThenWaitReady(ctx context.Context, order rescaleOrder) error {
	// One timeout for the whole sequence
	ctx, cancel := context.WithTimeout(ctx, rescaleConvertTimeout)
	defer cancel()

	if err := m.installer.Convert(ctx, order.Role, order.JoinIP); err != nil {
		return err
	}
	// Still distrust of the K3s scripts (see installThenWaitReady doc).
	installed, err := m.installer.InstalledRole(ctx)
	if err != nil {
		return fmt.Errorf("read the installed k3s role: %w", err)
	}
	if installed != order.Role {
		return fmt.Errorf("k3s is installed as %q but should be %q", installed, order.Role)
	}

	waitReady := m.installer.WaitAgentReady
	if order.Role == node.RoleServer {
		waitReady = m.installer.WaitServerReady
	}
	if err := waitReady(ctx); err != nil {
		return fmt.Errorf("k3s was not converted to %q and ready within %s: %w",
			order.Role, rescaleConvertTimeout, err)
	}
	return nil
}

// onRescaleConverted completes a conversion: the node runs its new role and rejoins the stable set.
func (m *Manager) onRescaleConverted() {
	updated := *m.persistedState
	updated.Role = m.rescale.order.Role
	updated.RoleChangedAt = time.Now()
	m.rescale.order = rescaleOrder{}

	m.logger.Info("conversion completed", "role", updated.Role)
	m.becomeStable(updated)
}

// onRescaleCheck re-runs the evaluation when a deadline expired with no Serf event to trigger it.
func (m *Manager) onRescaleCheck() {
	m.maybeRescale(m.observeCluster())
}

// scheduleRescaleCheck re-arms the evaluation timer.
func (m *Manager) scheduleRescaleCheck(deadline, now time.Time) {
	m.rescale.armTimer(deadline, now, func() {
		_ = m.submit(command{typ: cmdRescaleCheck})
	})
}

// abandonCoordination gives up on a round, and put back the node in the stable state.
func (m *Manager) abandonCoordination(err error) {
	m.logger.Warn("rescaling round abandoned", "error", err, "state", m.state)

	m.rescale.order = rescaleOrder{}
	m.rescale.evicting = nil
	m.rescale.imbalanceControlPlaneSince = time.Time{}

	// A coordination round changes nothing on the coordinator itself, so no terminal state is needed.
	m.transition(node.StateStableServer)

	// A round that keeps failing is retried by the same node forever (because it's still an alive stable_server).
	// In the future: a coordinator that cannot reach its own API server is an unhealthy node:
	// the failure detection should catch it.

	now := time.Now()
	m.scheduleRescaleCheck(now.Add(m.rescaleSettleDelay), now)
}

// failRescale logs the error and halts a node whose conversion failed.
func (m *Manager) failRescale(err error) {
	m.logger.Error("rescaling failed", "error", err, "state", m.state)
	m.rescale.stopTimer()
	m.rescale.order = rescaleOrder{}
	m.transition(node.StateRescaleFailed)
}

// earliest returns the nearest of two deadlines, ignoring the zero ones (meaning "no deadline").
func earliest(a, b time.Time) time.Time {
	switch {
	case a.IsZero():
		return b
	case b.IsZero():
		return a
	case b.Before(a):
		return b
	default:
		return a
	}
}
