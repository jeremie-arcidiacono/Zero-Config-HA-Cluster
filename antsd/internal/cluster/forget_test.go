package cluster

// Tests of the forget-me protocol.
//
// The fake bus, installers and helpers come from bootstrap_test.go and rescale_test.go.

import (
	"context"
	"testing"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
)

// noRescaling turns the repair workflow off on a node, so that only the forget-me protocol can
// touch the cluster. The protocol is deliberately independent of that switch: it is a prerequisite
// for joining, not a topology decision, and the switch never froze joining either.
func noRescaling(m *Manager) { m.config.RescaleEnabled = false }

// TestJoinerHasItsLeftoversErasedFirst is the nominal case: the cluster still holds a node object
// under the newcomer's name, and the newcomer installs only once it is gone.
func TestJoinerHasItsLeftoversErasedFirst(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	coordinator := newRunningNode(ctx, t, bus, "ants-01", "10.0.0.1", node.RoleServer)

	// What the previous life of the newcomer left in the cluster.
	const ghost = "ants-11"
	coordinator.controlPlane.AddNode(ghost)

	installer := newRecordingInstaller()
	installer.beforeInstall = func() {
		exists, err := coordinator.controlPlane.NodeExists(ctx, ghost)
		if err != nil {
			t.Errorf("NodeExists: %v", err)
		}
		if exists {
			t.Error("the machine installed K3s while the cluster still knew a node under its name")
		}
	}

	joiner := newTestManagerWithInstaller(t, bus, ghost, "10.0.1.1", installer)
	go func() { _ = joiner.Run(ctx) }()

	waitForState(t, joiner, node.StateStableAgent)

	if !coordinator.controlPlane.calledOn("DeleteNode", ghost) {
		t.Errorf("the coordinator did not erase the leftover node object (calls: %v)",
			coordinator.controlPlane.calledMethods())
	}
	if coordinator.controlPlane.called("DrainNode") {
		t.Errorf("the coordinator drained a machine that hosts nothing (calls: %v)",
			coordinator.controlPlane.calledMethods())
	}
	if !installer.called("InstallAgent") {
		t.Errorf("the machine never joined (calls: %v)", installer.calledMethods())
	}
}

// TestJoinerWithNothingToEraseIsNotDelayed pins the property that keeps the joining path free of
// the cluster-wide turn: a newcomer the cluster knows nothing about must not wait for an etcd
// operation to finish.
//
// The coordinator answers such a request from a read-only lookup, without taking the turn. Only a
// machine that really has leftovers waits, which is the case below.
func TestJoinerWithNothingToEraseIsNotDelayed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	coordinator := newRunningNode(ctx, t, bus, "ants-01", "10.0.0.1", node.RoleServer)
	// A machine frozen mid-conversion holds the cluster-wide turn for the whole test.
	seedMember(t, bus, "ants-02", "10.0.0.2", node.StateRescaleDemoting)

	installer := newRecordingInstaller()
	joiner := newTestManagerWithInstaller(t, bus, "ants-11", "10.0.1.1", installer)
	go func() { _ = joiner.Run(ctx) }()

	waitForState(t, joiner, node.StateStableAgent)

	if coordinator.controlPlane.called("DeleteNode") {
		t.Errorf("the coordinator took the cluster-wide turn for a machine with nothing to erase "+
			"(calls: %v)", coordinator.controlPlane.calledMethods())
	}
}

// TestJoinerWaitsForTheClusterToBeFree covers the case that does have to wait: leftovers exist, so
// erasing them may take an etcd member with it, and that cannot run beside another membership
// change. The machine keeps asking and installs nothing until the cluster is free.
func TestJoinerWaitsForTheClusterToBeFree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	// Rescaling is off on the coordinator: once the turn is free below, this cluster is short of
	// servers and would start a promotion, which also deletes a node object.
	coordinator := newRunningNodeWith(ctx, t, bus, "ants-01", "10.0.0.1", node.RoleServer, noRescaling)

	const ghost = "ants-11"
	coordinator.controlPlane.AddNode(ghost)

	busy := seedMember(t, bus, "ants-02", "10.0.0.2", node.StateRescaleDemoting)

	installer := newRecordingInstaller()
	joiner := newTestManagerWithInstaller(t, bus, ghost, "10.0.1.1", installer)
	go func() { _ = joiner.Run(ctx) }()

	waitForState(t, joiner, node.StateJoiningCleanup)

	// Far more than the retry interval and the erasure would need.
	time.Sleep(300 * time.Millisecond)

	if coordinator.controlPlane.called("DeleteNode") {
		t.Errorf("the leftovers were erased during another etcd membership change (calls: %v)",
			coordinator.controlPlane.calledMethods())
	}
	if calls := installer.calledMethods(); len(calls) != 0 {
		t.Errorf("the machine installed K3s before the cluster could erase its name (calls: %v)", calls)
	}

	// The conversion ends, so the turn is free again.
	if err := busy.SetState(node.StateStableAgent); err != nil {
		t.Fatalf("SetState on the frozen member: %v", err)
	}

	waitForState(t, joiner, node.StateStableAgent)
	if !coordinator.controlPlane.calledOn("DeleteNode", ghost) {
		t.Errorf("the leftovers were never erased (calls: %v)", coordinator.controlPlane.calledMethods())
	}
}

// TestForgetMeIsRefusedForAMachineInTheCluster is the guard that makes the protocol safe to run on
// a name read from a best-effort request.
// Only a machine Serf shows on its first boot may have its name erased.
func TestForgetMeIsRefusedForAMachineInTheCluster(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	// Rescaling is off on the coordinator: this cluster is short of servers, and a promotion also
	// drains and deletes a node. Leaving it on would make the assertions below ambiguous.
	coordinator := newRunningNodeWith(ctx, t, bus, "ants-01", "10.0.0.1", node.RoleServer, noRescaling)
	agent := newRunningNode(ctx, t, bus, "ants-02", "10.0.0.2", node.RoleAgent)
	coordinator.controlPlane.AddNode(agent.manager.config.NodeName)

	// A machine asks the cluster to forget a working member instead of itself.
	liar := seedMember(t, bus, "ants-11", "10.0.1.1", node.StateJoiningCleanup)
	if err := liar.SendUserEvent(eventForgetMe, []byte(`{"name":"ants-02"}`)); err != nil {
		t.Fatalf("SendUserEvent: %v", err)
	}

	// Far more time than the erasure would need.
	time.Sleep(300 * time.Millisecond)

	if coordinator.controlPlane.called("DeleteNode") {
		t.Errorf("the coordinator erased a machine that belongs to the cluster (calls: %v)",
			coordinator.controlPlane.calledMethods())
	}
	if got := agent.manager.State(); got != string(node.StateStableAgent) {
		t.Errorf("agent state = %q, want %q", got, node.StateStableAgent)
	}
}

// TestMachineWithK3sNeverAsksToBeForgotten covers the machine the protocol must never serve: one
// with no state file but a K3s installation on disk, which runs the first-boot protocol again
// after a reboot while its K3s is alive and registered.
//
// Serf shows it on its first boot, so the coordinator would accept the request and delete the node
// object of a running member. It is stopped locally instead, before it asks: such a machine needs
// a factory reset, which is what the terminal state tells its user.
func TestMachineWithK3sNeverAsksToBeForgotten(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	coordinator := newRunningNode(ctx, t, bus, "ants-01", "10.0.0.1", node.RoleServer)

	const name = "ants-11"
	coordinator.controlPlane.AddNode(name) // Its own live installation, not a leftover.

	installer := newRecordingInstaller()
	installer.SetInstalledRole(node.RoleAgent)
	joiner := newTestManagerWithInstaller(t, bus, name, "10.0.1.1", installer)
	go func() { _ = joiner.Run(ctx) }()

	waitForState(t, joiner, node.StateJoiningFailed)
	assertNoInstall(t, installer)

	if coordinator.controlPlane.called("DeleteNode") {
		t.Errorf("a machine with a live K3s had its node object deleted (calls: %v)",
			coordinator.controlPlane.calledMethods())
	}
}
