package cluster

// Tests of the rescaling workflow.
//
// The fake bus, installers and helpers come from bootstrap_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/k3s"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
)

// Delays short enough to watch a whole repair inside a test, long enough that a decision taken
// too early still shows up as one.
const (
	testFailureGrace = 100 * time.Millisecond
	testSettleDelay  = 50 * time.Millisecond
)

// runningNode is a machine already in the cluster, with the handles a test asserts on.
type runningNode struct {
	manager      *Manager
	installer    *recordingInstaller
	controlPlane *recordingControlPlane
}

// newRunningNode brings a manager up in the stable state of a role, the way a machine that
// completed its first boot comes back after a reboot. This is how a test gets a cluster that is
// already running without replaying a bootstrap.
func newRunningNode(ctx context.Context, t *testing.T, bus *fakeBus, name, ip string, role node.Role) *runningNode {
	t.Helper()
	return newRunningNodeWith(ctx, t, bus, name, ip, role, nil)
}

// newRunningNodeWith is newRunningNode with a hook to adjust the manager. The hook runs before
// the run loop starts: the fields it reaches are owned by that loop afterwards.
func newRunningNodeWith(
	ctx context.Context,
	t *testing.T,
	bus *fakeBus,
	name, ip string,
	role node.Role,
	configure func(*Manager),
) *runningNode {
	t.Helper()

	installer := newRecordingInstaller()
	installer.SetInstalledRole(role)
	controlPlane := newRecordingControlPlane()

	m := newTestManagerWith(t, bus, name, ip, installer, controlPlane)
	writePersistedState(t, m, node.PersistedState{
		NodeName:             name,
		Role:                 role,
		BootCount:            1,
		FirstBootCompletedAt: firstBootDate,
	})
	m.evictionGrace = testFailureGrace
	m.rescaleSettleDelay = testSettleDelay
	if configure != nil {
		configure(m)
	}

	go func() { _ = m.Run(ctx) }()
	waitForState(t, m, role.StableState())

	return &runningNode{manager: m, installer: installer, controlPlane: controlPlane}
}

// seedMember puts a synthetic machine on the bus, advertising a state but running no manager.
// Used for the peers a test only needs as a membership fact.
func seedMember(t *testing.T, bus *fakeBus, name, ip string, state node.State) *fakeSerf {
	t.Helper()

	peer := bus.addNode(name, ip)
	if err := peer.SetState(state); err != nil {
		t.Fatalf("SetState on the seeded member %s: %v", name, err)
	}
	return peer
}

// waitForMemberGone polls until a machine has disappeared from the memberlist.
func waitForMemberGone(t *testing.T, observer *Manager, name string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, found := memberOf(observer, name); !found {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("machine %s is still in the memberlist observed by %s", name, observer.config.NodeName)
}

func memberOf(observer *Manager, name string) (string, bool) {
	for _, m := range observer.serf.Snapshot().Members {
		if m.Name == name {
			return m.Status, true
		}
	}
	return "", false
}

// convertFailingInstaller behaves like the fake one until a conversion is asked of it, which
// fails the way a node whose uninstall left K3s behind does.
type convertFailingInstaller struct {
	*k3s.FakeInstaller
}

func newConvertFailingInstaller(installed node.Role) *convertFailingInstaller {
	installer := &convertFailingInstaller{FakeInstaller: newFastFakeInstaller()}
	installer.SetInstalledRole(installed)
	return installer
}

func (c *convertFailingInstaller) Convert(context.Context, node.Role, string) error {
	return errors.New("k3s uninstall left an installation behind")
}

// TestRescaleEvictsDeadServerThenPromotesAgent is the nominal repair :
// a server that is durably gone is removed from Kubernetes and from the
// memberlist, and the agent takes its place in the control plane.
func TestRescaleEvictsDeadServerThenPromotesAgent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Four machines call for three servers, which is what the cluster runs: nothing to repair yet.
	bus := newFakeBus()
	coordinator := newRunningNode(ctx, t, bus, "ants-01", "10.0.0.1", node.RoleServer)
	seedMember(t, bus, "ants-02", "10.0.0.2", node.StateStableServer)
	dying := seedMember(t, bus, "ants-03", "10.0.0.3", node.StateStableServer)
	agent := newRunningNode(ctx, t, bus, "ants-04", "10.0.0.4", node.RoleAgent)

	bus.markFailed(dying)

	// The dead machine leaves Kubernetes, then the memberlist.
	waitForMemberGone(t, coordinator.manager, "ants-03")
	if !coordinator.controlPlane.calledOn("DeleteNode", "ants-03") {
		t.Errorf("the dead server's node object was not deleted (calls: %v)",
			coordinator.controlPlane.calledMethods())
	}

	// Three machines now call for three servers, and only two remain: the agent is promoted.
	waitForState(t, agent.manager, node.StateStableServer)

	if !agent.installer.called("Convert:server") {
		t.Errorf("the agent did not convert to a server (calls: %v)", agent.installer.calledMethods())
	}
	if !agent.installer.called("WaitServerReady") {
		t.Errorf("the promoted node did not wait on the server readiness probe (calls: %v)",
			agent.installer.calledMethods())
	}
	if got := agent.installer.lastJoinTarget(); got != "10.0.0.1" {
		t.Errorf("the promoted node joined %q, want the coordinator's IP 10.0.0.1", got)
	}
	if !coordinator.controlPlane.calledOn("DrainNode", "ants-04") {
		t.Errorf("the promoted node was not drained first (calls: %v)",
			coordinator.controlPlane.calledMethods())
	}

	persisted := readPersistedState(t, agent.manager.config.StateFilePath)
	if persisted.Role != node.RoleServer {
		t.Errorf("persisted role = %q, want %q", persisted.Role, node.RoleServer)
	}
	if persisted.RoleChangedAt.IsZero() {
		t.Error("the role change was not dated: the report needs the trace")
	}
	if !persisted.FirstBootCompletedAt.Equal(firstBootDate) {
		t.Errorf("first boot date rewritten by the conversion: got %s, want %s",
			persisted.FirstBootCompletedAt, firstBootDate)
	}
	if persisted.BootCount != 2 {
		t.Errorf("boot count = %d, want 2: a conversion is not a boot", persisted.BootCount)
	}
}

// TestRescaleWaitsOutTheGracePeriod covers the machine that is merely rebooting. Eviction is
// irreversible for it (its K3s is removed from the cluster), so a failure that ends before the
// grace period must leave no trace at all.
func TestRescaleWaitsOutTheGracePeriod(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// A grace period long enough for the machine to come back well inside it.
	const grace = 500 * time.Millisecond

	bus := newFakeBus()
	coordinator := newRunningNodeWith(ctx, t, bus, "ants-01", "10.0.0.1", node.RoleServer,
		func(m *Manager) { m.evictionGrace = grace })
	seedMember(t, bus, "ants-02", "10.0.0.2", node.StateStableServer)
	rebooting := seedMember(t, bus, "ants-03", "10.0.0.3", node.StateStableServer)

	bus.markFailed(rebooting)
	time.Sleep(grace / 5)
	bus.markAlive(rebooting)

	// Well past the instant the failure would have expired at, had it not ended.
	time.Sleep(grace + 200*time.Millisecond)

	if coordinator.controlPlane.called("DeleteNode") {
		t.Errorf("a machine that came back was evicted anyway (calls: %v)",
			coordinator.controlPlane.calledMethods())
	}
	if status, found := memberOf(coordinator.manager, "ants-03"); !found || status != memberStatusAlive {
		t.Errorf("ants-03 status = %q (found: %t), want alive", status, found)
	}
	if got := coordinator.manager.State(); got != string(node.StateStableServer) {
		t.Errorf("coordinator state = %q, want %q", got, node.StateStableServer)
	}
}

// TestRescaleRetriesAfterAFailedCoordination checks that a repair round which failed on the
// cluster side is retried instead of latching the coordinator.
func TestRescaleRetriesAfterAFailedCoordination(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Evicting the agent leaves three machines calling for three servers, so no conversion follows
	// and every DeleteNode the test counts belongs to the eviction.
	bus := newFakeBus()
	coordinator := newRunningNode(ctx, t, bus, "ants-01", "10.0.0.1", node.RoleServer)
	seedMember(t, bus, "ants-02", "10.0.0.2", node.StateStableServer)
	seedMember(t, bus, "ants-03", "10.0.0.3", node.StateStableServer)
	dying := seedMember(t, bus, "ants-04", "10.0.0.4", node.StateStableAgent)

	coordinator.controlPlane.failNextDeletes(1)
	bus.markFailed(dying)

	waitForMemberGone(t, coordinator.manager, "ants-04")

	if got := coordinator.controlPlane.callCount("DeleteNode"); got != 2 {
		t.Errorf("DeleteNode ran %d times, want 2 (the refused attempt and its retry)", got)
	}
	if got := coordinator.manager.State(); got != string(node.StateStableServer) {
		t.Errorf("coordinator state = %q, want %q", got, node.StateStableServer)
	}
}

// TestRescaleRefusesToActWithoutQuorum covers the minority side of a network partition. A machine
// cut off from the others is the only alive server of its own view, so it takes itself for the
// coordinator and reads the whole surviving majority as durably gone. Eviction being irreversible,
// it must change nothing as long as it cannot prove it still speaks for the cluster.
func TestRescaleRefusesToActWithoutQuorum(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	isolated := newRunningNode(ctx, t, bus, "ants-01", "10.0.0.1", node.RoleServer)
	majority := []*fakeSerf{
		seedMember(t, bus, "ants-02", "10.0.0.2", node.StateStableServer),
		seedMember(t, bus, "ants-03", "10.0.0.3", node.StateStableServer),
	}

	isolated.controlPlane.loseQuorum()
	for _, peer := range majority {
		bus.markFailed(peer)
	}

	// Well past the instant the eviction would have run at.
	time.Sleep(testFailureGrace + 200*time.Millisecond)

	if isolated.controlPlane.called("DeleteNode") {
		t.Errorf("a node with no quorum evicted machines anyway (calls: %v)",
			isolated.controlPlane.calledMethods())
	}
	for _, name := range []string{"ants-02", "ants-03"} {
		if _, found := memberOf(isolated.manager, name); !found {
			t.Errorf("a node with no quorum erased %s from its memberlist", name)
		}
	}
}

// TestRescaleIgnoresAnOrderItMustNotFollow checks the guards that let the conversion order carry
// no sequence number: a duplicate delivered after the conversion, or one addressed elsewhere,
// must leave the machine alone. Acting on any of them would tear down a K3s that is serving.
func TestRescaleIgnoresAnOrderItMustNotFollow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	server := newRunningNode(ctx, t, bus, "ants-01", "10.0.0.1", node.RoleServer)

	orders := map[string]rescaleOrder{
		"the conversion already happened": {Target: "ants-01", Role: node.RoleServer, JoinIP: "10.0.0.9"},
		"another machine is designated":   {Target: "ants-02", Role: node.RoleAgent, JoinIP: "10.0.0.9"},
		"no server to join back through":  {Target: "ants-01", Role: node.RoleAgent},
	}
	for name, order := range orders {
		payload, err := json.Marshal(order)
		if err != nil {
			t.Fatalf("encode the %s order: %v", name, err)
		}
		bus.broadcastUser(eventRescaleConvert, payload)
	}
	bus.broadcastUser(eventRescaleConvert, []byte("not an order at all"))

	// Far more time than a conversion would need.
	time.Sleep(200 * time.Millisecond)

	if got := server.manager.State(); got != string(node.StateStableServer) {
		t.Errorf("state = %q, want %q", got, node.StateStableServer)
	}
	assertNoInstall(t, server.installer)
}

// TestRescaleDisabledFreezesTheTopology covers the kill switch used during a failure-injection
// campaign: a machine can then be unplugged without the cluster repairing itself underneath the
// measurement.
func TestRescaleDisabledFreezesTheTopology(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	coordinator := newRunningNodeWith(ctx, t, bus, "ants-01", "10.0.0.1", node.RoleServer,
		func(m *Manager) { m.config.RescaleEnabled = false })
	seedMember(t, bus, "ants-02", "10.0.0.2", node.StateStableServer)
	dying := seedMember(t, bus, "ants-03", "10.0.0.3", node.StateStableServer)

	bus.markFailed(dying)

	// Far more time than the grace period and the repair would need.
	time.Sleep(500 * time.Millisecond)

	if coordinator.controlPlane.called("DeleteNode") {
		t.Errorf("a repair ran with the rescaling turned off (calls: %v)",
			coordinator.controlPlane.calledMethods())
	}
	if status, found := memberOf(coordinator.manager, "ants-03"); !found || status != memberStatusFailed {
		t.Errorf("ants-03 status = %q (found: %t), want it left in the memberlist as failed", status, found)
	}
}

// TestRescaleDemotesHighestNamedServer covers the situation the odd target exists to create:
// five servers lose one durably, four remain, and four machines call for three servers.
//
// Without the odd target this scenario had no answer at all: the desired count could never be
// below the number of machines, so a demotion was unreachable.
func TestRescaleDemotesHighestNamedServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	coordinator := newRunningNode(ctx, t, bus, "ants-01", "10.0.0.1", node.RoleServer)
	seedMember(t, bus, "ants-02", "10.0.0.2", node.StateStableServer)
	seedMember(t, bus, "ants-03", "10.0.0.3", node.StateStableServer)
	demoted := newRunningNode(ctx, t, bus, "ants-04", "10.0.0.4", node.RoleServer)
	dying := seedMember(t, bus, "ants-05", "10.0.0.5", node.StateStableServer)

	bus.markFailed(dying)
	waitForMemberGone(t, coordinator.manager, "ants-05")

	// The highest-named survivor steps down, never the coordinator.
	waitForState(t, demoted.manager, node.StateStableAgent)

	if !demoted.installer.called("Convert:agent") {
		t.Errorf("the highest-named server did not convert to an agent (calls: %v)",
			demoted.installer.calledMethods())
	}
	if !demoted.installer.called("WaitAgentReady") {
		t.Errorf("the demoted node did not wait on the agent readiness probe (calls: %v)",
			demoted.installer.calledMethods())
	}
	if coordinator.controlPlane.calledOn("DrainNode", "ants-01") {
		t.Error("the coordinator drained itself")
	}

	persisted := readPersistedState(t, demoted.manager.config.StateFilePath)
	if persisted.Role != node.RoleAgent {
		t.Errorf("persisted role = %q, want %q", persisted.Role, node.RoleAgent)
	}
}

// TestRescaleDemotesDownToASingleServer is the no-agent case of the 2026-08-03 journal entry:
// three servers lose one, and two machines are better served by one writable server than by a
// two-member etcd that tolerates nothing.
func TestRescaleDemotesDownToASingleServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	coordinator := newRunningNode(ctx, t, bus, "ants-01", "10.0.0.1", node.RoleServer)
	demoted := newRunningNode(ctx, t, bus, "ants-02", "10.0.0.2", node.RoleServer)
	dying := seedMember(t, bus, "ants-03", "10.0.0.3", node.StateStableServer)

	bus.markFailed(dying)
	waitForMemberGone(t, coordinator.manager, "ants-03")

	waitForState(t, demoted.manager, node.StateStableAgent)
	if got := coordinator.manager.State(); got != string(node.StateStableServer) {
		t.Errorf("coordinator state = %q, want %q: it keeps the last server slot",
			got, node.StateStableServer)
	}
}

// TestRescaleHasASingleCoordinator checks that a repair is driven by one machine only.
//
// Every server evaluates the cluster and reaches the same conclusion, so nothing but the
// lowest-name rule stops three of them from deleting the same node object at once.
func TestRescaleHasASingleCoordinator(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	servers := make([]*runningNode, 0, 3)
	for i := 1; i <= 3; i++ {
		servers = append(servers,
			newRunningNode(ctx, t, bus, fmt.Sprintf("ants-0%d", i), fmt.Sprintf("10.0.0.%d", i), node.RoleServer))
	}
	dying := seedMember(t, bus, "ants-04", "10.0.0.4", node.StateStableAgent)

	bus.markFailed(dying)
	waitForMemberGone(t, servers[0].manager, "ants-04")

	acted := 0
	for _, server := range servers {
		if server.controlPlane.called("DeleteNode") {
			acted++
		}
	}
	if acted != 1 {
		t.Errorf("%d servers ran the repair, want exactly 1 (the lowest-named one)", acted)
	}
	if !servers[0].controlPlane.calledOn("DeleteNode", "ants-04") {
		t.Errorf("the lowest-named server did not act (calls: %v)",
			servers[0].controlPlane.calledMethods())
	}
}

// TestRescaleEvictsDeadAgentWithoutChangingRoles checks that eviction is role-blind.
//
// A dead agent changes no target (the population and the desired count fall by the same step), so
// removing it triggers nothing.
func TestRescaleEvictsDeadAgentWithoutChangingRoles(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	coordinator := newRunningNode(ctx, t, bus, "ants-01", "10.0.0.1", node.RoleServer)
	dying := seedMember(t, bus, "ants-02", "10.0.0.2", node.StateStableAgent)

	bus.markFailed(dying)
	waitForMemberGone(t, coordinator.manager, "ants-02")

	// Long enough for a conversion the repair must not have decided on.
	time.Sleep(300 * time.Millisecond)

	if got := coordinator.manager.State(); got != string(node.StateStableServer) {
		t.Errorf("coordinator state = %q, want %q: evicting an agent changes no role",
			got, node.StateStableServer)
	}
	if coordinator.controlPlane.called("DrainNode") {
		t.Errorf("a conversion was prepared after an agent's eviction (calls: %v)",
			coordinator.controlPlane.calledMethods())
	}
}

// TestRescaleWaitsForEtcdMembershipChanges checks that a repair waits on the same cluster-wide
// advisory check as every other etcd membership change, instead of a serialization of its own.
func TestRescaleWaitsForEtcdMembershipChanges(t *testing.T) {
	for _, blocking := range []node.State{node.StateBootstrapInstallServer, node.StateRejoinCluster} {
		t.Run(string(blocking), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			bus := newFakeBus()
			coordinator := newRunningNode(ctx, t, bus, "ants-01", "10.0.0.1", node.RoleServer)
			seedMember(t, bus, "ants-02", "10.0.0.2", node.StateStableServer)
			dying := seedMember(t, bus, "ants-03", "10.0.0.3", node.StateStableServer)
			seedMember(t, bus, "ants-09", "10.0.0.9", blocking)

			bus.markFailed(dying)

			// Far more time than the grace period and the repair would need.
			time.Sleep(500 * time.Millisecond)

			if coordinator.controlPlane.called("DeleteNode") {
				t.Errorf("a repair started while %q was in progress (calls: %v)",
					blocking, coordinator.controlPlane.calledMethods())
			}
			if _, found := memberOf(coordinator.manager, "ants-03"); !found {
				t.Error("the dead machine was evicted despite the membership change in progress")
			}
		})
	}
}

// TestRescaleRefusesToPromoteDuringADemotion pins the ordering the whole detection rests on:
// isEtcdMembershipChanging is checked before the imbalance, never after.
//
// Checking the imbalance first would answer a demotion with a
// promotion, undoing it.
//
// A virgin machine arriving in that window is asserted here too: it must go on installing its
// agent, unbothered, since that touches no etcd member.
func TestRescaleRefusesToPromoteDuringADemotion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	coordinator := newRunningNode(ctx, t, bus, "ants-01", "10.0.0.1", node.RoleServer)
	seedMember(t, bus, "ants-02", "10.0.0.2", node.StateStableServer)
	seedMember(t, bus, "ants-03", "10.0.0.3", node.StateRescaleDemoting)
	agent := newRunningNode(ctx, t, bus, "ants-04", "10.0.0.4", node.RoleAgent)

	// A virgin machine arrives meanwhile, which raises the target further.
	joinerInstaller := newRecordingInstaller()
	joiner := newTestManagerWithInstaller(t, bus, "ants-05", "10.0.0.5", joinerInstaller)
	go func() { _ = joiner.Run(ctx) }()

	// The demotion blocks etcd membership changes, but an agent install never waits on them.
	waitForState(t, joiner, node.StateStableAgent)

	// Far more time than a promotion would need.
	time.Sleep(400 * time.Millisecond)

	if got := agent.manager.State(); got != string(node.StateStableAgent) {
		t.Errorf("agent state = %q, want %q: no promotion may start during a demotion",
			got, node.StateStableAgent)
	}
	if coordinator.controlPlane.called("DrainNode") {
		t.Errorf("the coordinator prepared a conversion during a demotion (calls: %v)",
			coordinator.controlPlane.calledMethods())
	}
	if joinerInstaller.called("InstallServerJoin") {
		t.Errorf("a joining machine took a server slot during a demotion (calls: %v)",
			joinerInstaller.calledMethods())
	}
}

// TestRescaleFailedConversionStopsOneMachineOnly checks the blast radius of a conversion that
// fails: the machine latches, because its K3s was torn down and nothing local is trustworthy any
// more, and the rest of the cluster keeps the topology it had.
func TestRescaleFailedConversionStopsOneMachineOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	coordinator := newRunningNode(ctx, t, bus, "ants-01", "10.0.0.1", node.RoleServer)
	seedMember(t, bus, "ants-02", "10.0.0.2", node.StateStableServer)
	dying := seedMember(t, bus, "ants-03", "10.0.0.3", node.StateStableServer)

	// The agent that will be promoted cannot convert.
	installer := newConvertFailingInstaller(node.RoleAgent)
	agent := newTestManagerWith(t, bus, "ants-04", "10.0.0.4", installer, newFastFakeControlPlane())
	writePersistedState(t, agent, node.PersistedState{
		NodeName: "ants-04", Role: node.RoleAgent, BootCount: 1, FirstBootCompletedAt: firstBootDate,
	})
	go func() { _ = agent.Run(ctx) }()
	waitForState(t, agent, node.StateStableAgent)

	bus.markFailed(dying)

	waitForState(t, agent, node.StateRescaleFailed)
	if got := coordinator.manager.State(); got != string(node.StateStableServer) {
		t.Errorf("coordinator state = %q, want %q: one machine's failure is not the cluster's",
			got, node.StateStableServer)
	}

	// The state file still describes the role the machine really had, so a reboot does not chase
	// a promotion that never happened.
	persisted := readPersistedState(t, agent.config.StateFilePath)
	if persisted.Role != node.RoleAgent {
		t.Errorf("persisted role = %q, want %q: nothing is persisted before readiness",
			persisted.Role, node.RoleAgent)
	}
}

// TestRescaleGrowsControlPlaneWithThePopulation checks that promotion is not only a failure
// reaction: a machine added to a four-node cluster raises the target from three to five, and both
// the newcomer and the incumbent agent end up filling the two new server slots.
//
// It is also what makes possible the "always join as agent": the newcomer installs an
// agent first and is converted afterward.
func TestRescaleGrowsControlPlaneWithThePopulation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	for i := 1; i <= 3; i++ {
		newRunningNode(ctx, t, bus, fmt.Sprintf("ants-0%d", i), fmt.Sprintf("10.0.0.%d", i), node.RoleServer)
	}
	agent := newRunningNode(ctx, t, bus, "ants-04", "10.0.0.4", node.RoleAgent)

	// A fifth machine is plugged in, and nobody touches it.
	newcomerInstaller := newRecordingInstaller()
	newcomer := newTestManagerWithInstaller(t, bus, "ants-05", "10.0.0.5", newcomerInstaller)
	go func() { _ = newcomer.Run(ctx) }()

	waitForState(t, newcomer, node.StateStableServer)
	waitForState(t, agent.manager, node.StateStableServer)

	if !agent.installer.called("Convert:server") {
		t.Errorf("the incumbent agent was not promoted (calls: %v)", agent.installer.calledMethods())
	}
	if calls := newcomerInstaller.calledMethods(); !slices.Contains(calls, "InstallAgent") ||
		!slices.Contains(calls, "Convert:server") {
		t.Errorf("the newcomer did not reach the control plane through an agent install then a "+
			"conversion (calls: %v)", calls)
	}
	if newcomerInstaller.called("InstallServerJoin") {
		t.Errorf("the newcomer installed a server directly (calls: %v)", newcomerInstaller.calledMethods())
	}
}

// TestRescaleConvergesWithConcurrentJoins is the case joining and rescaling could most plausibly
// undo each other in: two virgin machines arrive at a four-node cluster (target three) while its
// agent may be mid-promotion, taking the population to six and the target to five.
//
// They cannot disagree because the newcomers decide nothing: they install agents, and every change
// of the control plane's size goes through the one coordinator, one at a time behind the advisory check.
// Whichever order the conversions happen in, the cluster has to settle on five servers and one
// agent.
func TestRescaleConvergesWithConcurrentJoins(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	managers := make([]*Manager, 0, 6)
	for i := 1; i <= 3; i++ {
		running := newRunningNode(ctx, t, bus, fmt.Sprintf("ants-0%d", i), fmt.Sprintf("10.0.0.%d", i), node.RoleServer)
		managers = append(managers, running.manager)
	}
	managers = append(managers, newRunningNode(ctx, t, bus, "ants-04", "10.0.0.4", node.RoleAgent).manager)

	for i := 5; i <= 6; i++ {
		newcomer := newTestManager(t, bus, fmt.Sprintf("ants-0%d", i), fmt.Sprintf("10.0.0.%d", i))
		managers = append(managers, newcomer)
		go func() { _ = newcomer.Run(ctx) }()
	}

	waitForStableTopology(t, managers, node.DesiredServerCount(len(managers)))
}

// TestEarliest checks the deadline merge that arms the evaluation timer. A zero time means "no
// deadline", and reading it as an instant in the year 1 would wake the run loop immediately,
// forever.
func TestEarliest(t *testing.T) {
	soon := time.Now().Add(time.Minute)
	later := soon.Add(time.Hour)

	tests := []struct {
		name string
		a, b time.Time
		want time.Time
	}{
		{name: "both set, first is nearer", a: soon, b: later, want: soon},
		{name: "both set, second is nearer", a: later, b: soon, want: soon},
		{name: "first is unset", a: time.Time{}, b: soon, want: soon},
		{name: "second is unset", a: soon, b: time.Time{}, want: soon},
		{name: "none set", a: time.Time{}, b: time.Time{}, want: time.Time{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := earliest(tt.a, tt.b); !got.Equal(tt.want) {
				t.Errorf("earliest() = %s, want %s", got, tt.want)
			}
		})
	}
}

// waitForStableTopology polls until the cluster settles on the wanted number of servers, every
// machine being stable. It is the assertion for the cases where which machine holds which role is
// not determined, only how many hold each.
func waitForStableTopology(t *testing.T, managers []*Manager, wantServers int) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for {
		servers, agents := 0, 0
		for _, m := range managers {
			switch node.State(m.State()) {
			case node.StateStableServer:
				servers++
			case node.StateStableAgent:
				agents++
			}
		}
		if servers == wantServers && servers+agents == len(managers) {
			return
		}
		if !time.Now().Before(deadline) {
			states := make([]string, 0, len(managers))
			for _, m := range managers {
				states = append(states, m.config.NodeName+"="+m.State())
			}
			t.Fatalf("cluster did not settle on %d servers and %d agents (states: %v)",
				wantServers, len(managers)-wantServers, states)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// msgCounter counts the log records carrying one message, to tell a paced retry from a spin.
type msgCounter struct {
	msg   string
	count atomic.Int64
}

func (c *msgCounter) Enabled(context.Context, slog.Level) bool { return true }
func (c *msgCounter) WithAttrs([]slog.Attr) slog.Handler       { return c }
func (c *msgCounter) WithGroup(string) slog.Handler            { return c }

func (c *msgCounter) Handle(_ context.Context, r slog.Record) error {
	if r.Message == c.msg {
		c.count.Add(1)
	}
	return nil
}

// TestRescaleDoesNotSpinWithNothingToPromote pins the pacing of an imbalance nothing can repair:
// a machine stuck in rescale_failed still counts in the population, so the control plane reads as
// too small while no agent is promotable.
func TestRescaleDoesNotSpinWithNothingToPromote(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	counter := &msgCounter{msg: "control plane too small but no agent available to promote"}
	newRunningNodeWith(ctx, t, bus, "ants-01", "10.0.0.1", node.RoleServer,
		func(m *Manager) { m.logger = slog.New(counter) })
	seedMember(t, bus, "ants-02", "10.0.0.2", node.StateStableServer)
	seedMember(t, bus, "ants-03", "10.0.0.3", node.StateRescaleFailed)

	time.Sleep(10 * testSettleDelay)

	switch got := counter.count.Load(); {
	case got == 0:
		t.Fatal("the coordinator never noticed the imbalance")
	case got > 30:
		t.Errorf("evaluated %d times in ten periods: the check is spinning", got)
	}
}
