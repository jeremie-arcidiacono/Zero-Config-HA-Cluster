package cluster

// Tests of the first-boot joining path.
//
// The fake bus, installers and helpers come from bootstrap_test.go.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/admin"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/serfnode"
)

// seedCluster puts count synthetic servers on the bus, the way a cluster
// already running advertises itself: alive members tagged stable_server.
// They are named ants-01.. so that they sort before the joiners below.
//
// The lowest-named one also answers the forget-me protocol, since a joiner installs nothing before
// a coordinator confirms its name is free. Only a Manager plays that part for real, and these
// servers are bare fakeSerf: without the stand-in, every joiner here would wait forever.
func seedCluster(t *testing.T, bus *fakeBus, count int) []*fakeSerf {
	t.Helper()
	servers := make([]*fakeSerf, 0, count)
	for i := 1; i <= count; i++ {
		server := bus.addNode(fmt.Sprintf("ants-%02d", i), fmt.Sprintf("10.0.0.%d", i))
		if err := server.SetState(node.StateStableServer); err != nil {
			t.Fatalf("SetState on the seeded server: %v", err)
		}
		servers = append(servers, server)
	}
	for i, server := range servers {
		// Only the lowest-named one answers, as the real coordinator election would have it.
		serveForgetMe(t, server, i == 0)
	}
	return servers
}

// serveForgetMe drains the events of a synthetic server, and optionally confirms every forget-me
// request the way the coordinator of a real cluster does once it erased the name from K3s.
//
// Draining matters even for the silent ones: nobody consumes the channel of a managerless node,
// and a full one blocks the whole bus, including the run loop that broadcasts into it.
func serveForgetMe(t *testing.T, server *fakeSerf, answer bool) {
	t.Helper()

	done := make(chan struct{})
	t.Cleanup(func() { close(done) })

	go func() {
		for {
			select {
			case <-done:
				return
			case e := <-server.events:
				// Both events carry the same payload, so it is echoed back as is.
				if answer && e.Type == serfnode.EventUser && e.Name == eventForgetMe {
					_ = server.SendUserEvent(eventForgotten, e.Payload)
				}
			}
		}
	}()
}

// TestJoinExistingClusterAsAgent is the nominal case: a machine plugged into a
// cluster whose control plane is complete becomes an agent, with no user action.
func TestJoinExistingClusterAsAgent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	seedCluster(t, bus, 3)

	installer := newRecordingInstaller()
	joiner := newTestManagerWithInstaller(t, bus, "ants-11", "10.0.1.1", installer)
	go func() { _ = joiner.Run(ctx) }()

	waitForState(t, joiner, node.StateStableAgent)

	if !installer.called("InstallAgent") {
		t.Errorf("joiner did not install as an agent (calls: %v)", installer.calledMethods())
	}
	if installer.called("WaitServerReady") {
		t.Errorf("an agent must not wait on WaitServerReady (calls: %v)", installer.calledMethods())
	}
	// The K3s join URL is built from this address, so it must be the bare IP
	// of the server, not the address:port Serf displays.
	if got := installer.lastJoinTarget(); got != "10.0.0.1" {
		t.Errorf("join target = %q, want the lowest-named server's bare IP %q", got, "10.0.0.1")
	}

	persisted := readPersistedState(t, joiner.config.StateFilePath)
	if persisted.Role != node.RoleAgent {
		t.Errorf("persisted role %q, want %q", persisted.Role, node.RoleAgent)
	}
	if persisted.BootCount != 1 {
		t.Errorf("persisted boot count %d, want 1: joining is still a first boot", persisted.BootCount)
	}
}

// TestJoinLateNodeAfterBootstrap covers a machine that reaches the LAN after a
// cluster was built: one whose first boot failed and that was factory-reset, or
// simply one plugged in later.
func TestJoinLateNodeAfterBootstrap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()

	// A first cluster of two nodes, bootstrapped by its user.
	first := newTestManager(t, bus, "ants-01", "10.0.0.1")
	second := newTestManager(t, bus, "ants-02", "10.0.0.2")
	for _, m := range []*Manager{first, second} {
		go func() { _ = m.Run(ctx) }()
		waitForState(t, m, node.StateDiscovering)
	}
	if err := first.RequestBootstrap(); err != nil {
		t.Fatalf("RequestBootstrap: %v", err)
	}
	if err := first.ConfirmBootstrap(); err != nil {
		t.Fatalf("ConfirmBootstrap: %v", err)
	}
	waitForState(t, first, node.StateStableServer)
	waitForState(t, second, node.StateStableAgent)

	// The late node boots now, and nobody touches it.
	installer := newRecordingInstaller()
	late := newTestManagerWithInstaller(t, bus, "ants-03", "10.0.0.3", installer)
	go func() { _ = late.Run(ctx) }()

	waitForState(t, late, node.StateStableAgent)

	if !installer.called("InstallAgent") {
		t.Errorf("late node did not join the existing cluster (calls: %v)", installer.calledMethods())
	}
	if installer.called("InstallServerInit") {
		t.Errorf("late node initialized a second cluster (calls: %v)", installer.calledMethods())
	}
	if got := installer.lastJoinTarget(); got != "10.0.0.1" {
		t.Errorf("join target = %q, want the server's bare IP %q", got, "10.0.0.1")
	}
}

// TestJoinAgentsInParallel checks that newcomers install together: an agent never
// touches the etcd membership, so serializing them would only slow a large batch
// of machines down for nothing.
func TestJoinAgentsInParallel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	seedCluster(t, bus, 1)

	tracker := &installTracker{}
	names := []string{"ants-11", "ants-12", "ants-13"}
	joiners := make([]*Manager, 0, len(names))
	for i, name := range names {
		installer := newTrackingInstaller(tracker)
		// Long enough that installs started together are still running together.
		installer.Delay = 150 * time.Millisecond
		m := newTestManagerWithInstaller(t, bus, name, fmt.Sprintf("10.0.1.%d", i+1), installer)
		joiners = append(joiners, m)
		go func() { _ = m.Run(ctx) }()
	}

	for _, m := range joiners {
		waitForState(t, m, node.StateStableAgent)
	}

	if got := tracker.agents.maxConcurrent(); got < 2 {
		t.Errorf("agent joins never overlapped (max %d at once), they must not be serialized", got)
	}
}

// TestJoinerNeverTakesAServerSlot pins the rule the whole path now rests on: a newcomer installs
// an agent whatever the control plane looks like, and growing it is left to the rescaling
// coordinator.
//
// The joiner is the least-informed node in the cluster, so it does not get to size the control
// plane.
func TestJoinerNeverTakesAServerSlot(t *testing.T) {
	tests := []struct {
		name        string
		servers     int
		failLastOne bool
	}{
		{name: "a cluster short of servers", servers: 2},
		{name: "a single-server cluster", servers: 1},
		{name: "a complete control plane", servers: 3},
		{name: "a server is down", servers: 3, failLastOne: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			bus := newFakeBus()
			servers := seedCluster(t, bus, tt.servers)
			if tt.failLastOne {
				bus.markFailed(servers[len(servers)-1])
			}

			installer := newRecordingInstaller()
			joiner := newTestManagerWithInstaller(t, bus, "ants-11", "10.0.1.1", installer)
			go func() { _ = joiner.Run(ctx) }()

			waitForState(t, joiner, node.StateStableAgent)

			if installer.called("InstallServerJoin") {
				t.Errorf("joiner installed a server (calls: %v)", installer.calledMethods())
			}
			if !installer.called("InstallAgent") {
				t.Errorf("joiner did not install an agent (calls: %v)", installer.calledMethods())
			}
		})
	}
}

// TestJoinerRefusesBootstrapProtocol checks that a node on the joining path
// cannot be dragged back into creating a cluster, by its own user or by a
// bootstrap request gossiped from another machine.
func TestJoinerRefusesBootstrapProtocol(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	servers := seedCluster(t, bus, 1)

	installer := newRecordingInstaller()
	joiner := newTestManagerWithInstaller(t, bus, "ants-11", "10.0.1.1", installer)
	// Never expires: the node stays where the divert put it.
	joiner.joinWaitDelay = time.Hour
	go func() { _ = joiner.Run(ctx) }()

	waitForState(t, joiner, node.StateJoiningWaiting)

	if err := joiner.RequestBootstrap(); !errors.Is(err, admin.ErrConflict) {
		t.Errorf("RequestBootstrap while joining: got %v, want ErrConflict", err)
	}
	if err := servers[0].SendUserEvent(eventBootstrapRequested, nil); err != nil {
		t.Fatalf("SendUserEvent: %v", err)
	}

	// The events are handled asynchronously: leave far more time than the
	// transition would need before concluding that the node ignored them.
	time.Sleep(250 * time.Millisecond)

	if got := joiner.State(); got != string(node.StateJoiningWaiting) {
		t.Errorf("state = %q, want %q: bootstrap events must not move a joining node",
			got, node.StateJoiningWaiting)
	}
}

// TestJoinDivertsBootstrappingNode covers a cohort of virgin machines
// bootstrapping their own cluster while another one is already running: they
// must abandon their roles and join the existing cluster instead.
//
// Without the divert, only the node holding rank 0 refuses (it is the one that
// would run cluster-init): every other node reaches the install step, sees the
// foreign server in the Serf tags and joins it with a role computed over its own
// cohort. Five virgin machines beside a healthy cluster would leave it with
// five servers.
func TestJoinDivertsBootstrappingNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()

	// A cohort of two machines, alone on the LAN, starts building a cluster.
	initInstaller := newRecordingInstaller()
	initInstaller.Delay = time.Second // N0 stays mid-install for the whole test
	first := newTestManagerWithInstaller(t, bus, "ants-01", "10.0.0.1", initInstaller)

	installer := newRecordingInstaller()
	second := newTestManagerWithInstaller(t, bus, "ants-02", "10.0.0.2", installer)
	// Never expires: the divert is observable without the join that follows it.
	second.joinWaitDelay = time.Hour

	for _, m := range []*Manager{first, second} {
		go func() { _ = m.Run(ctx) }()
		waitForState(t, m, node.StateDiscovering)
	}
	if err := first.RequestBootstrap(); err != nil {
		t.Fatalf("RequestBootstrap: %v", err)
	}
	if err := first.ConfirmBootstrap(); err != nil {
		t.Fatalf("ConfirmBootstrap: %v", err)
	}

	// Roles are computed once N0 starts cluster-init: the second node now holds
	// a role of its own cohort, and waits for N0 to come up.
	waitForState(t, first, node.StateBootstrapInstallInit)
	waitForState(t, second, node.StateBootstrapWaiting)

	// Only now does a machine of an existing cluster become visible.
	foreign := bus.addNode("ants-99", "10.0.0.99")
	if err := foreign.SetState(node.StateStableServer); err != nil {
		t.Fatalf("SetState on the foreign server: %v", err)
	}

	waitForState(t, second, node.StateJoiningWaiting)

	if calls := installer.calledMethods(); len(calls) != 0 {
		t.Errorf("diverted node installed K3s with its cohort role (calls: %v)", calls)
	}
}

// TestJoinRefusesNonVirginNode checks that the joining path honours the same
// guard as the bootstrap one: a node with no state file but a K3s installation
// on disk was restarted instead of being factory-reset, and joining would
// reinstall over its data.
func TestJoinRefusesNonVirginNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	seedCluster(t, bus, 1)

	installer := newRecordingInstaller()
	installer.SetInstalledRole(node.RoleAgent)
	joiner := newTestManagerWithInstaller(t, bus, "ants-11", "10.0.1.1", installer)
	go func() { _ = joiner.Run(ctx) }()

	waitForState(t, joiner, node.StateJoiningFailed)
	assertNoInstall(t, installer)
}
