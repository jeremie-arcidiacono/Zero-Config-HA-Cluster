package cluster

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/admin"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
)

// firstBootDate is an arbitrary past first-boot date, used to check that
// rejoining does not overwrite it.
var firstBootDate = time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

// writePersistedState pre-loads the manager's state file, simulating a node
// that already completed its first boot. Must be called before Run.
func writePersistedState(t *testing.T, m *Manager, state node.PersistedState) {
	t.Helper()
	if err := state.Save(m.config.StateFilePath); err != nil {
		t.Fatalf("write persisted state: %v", err)
	}
}

// writeRawState puts arbitrary content in the manager's state file.
func writeRawState(t *testing.T, m *Manager, content string) {
	t.Helper()
	if err := os.WriteFile(m.config.StateFilePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write raw state file: %v", err)
	}
}

// newRejoiningNode builds a manager that finds the given role on disk and in
// its (simulated) K3s installation: the nominal reboot situation.
func newRejoiningNode(t *testing.T, bus *fakeBus, name, ip string, role node.Role, bootCount int) (*Manager, *recordingInstaller) {
	t.Helper()

	installer := newRecordingInstaller()
	installer.SetInstalledRole(role)

	m := newTestManagerWithInstaller(t, bus, name, ip, installer)
	writePersistedState(t, m, node.PersistedState{
		NodeName:             name,
		Role:                 role,
		BootCount:            bootCount,
		FirstBootCompletedAt: firstBootDate,
	})
	return m, installer
}

// assertNoInstall fails if the node touched its K3s installation, by installing one or by
// converting the one it had. This is the property the whole rejoin path exists to guarantee:
// reinstalling K3s on a node that already has one wipes a populated etcd.
func assertNoInstall(t *testing.T, installer *recordingInstaller) {
	t.Helper()
	for _, method := range installer.calledMethods() {
		if strings.HasPrefix(method, "Install") || strings.HasPrefix(method, "Convert") {
			t.Fatalf("node installed K3s while it had to refuse (calls: %v)", installer.calledMethods())
		}
	}
}

// TestRejoinServerKeepsItsRoleAndHistory checks the nominal reboot of a
// server: no installation, the server readiness probe, and a persisted state
// whose boot counter advanced without losing the first-boot date.
func TestRejoinServerKeepsItsRoleAndHistory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	m, installer := newRejoiningNode(t, bus, "ants-01", "10.0.0.1", node.RoleServer, 3)
	go func() { _ = m.Run(ctx) }()

	waitForState(t, m, node.StateStableServer)

	assertNoInstall(t, installer)
	if !installer.called("WaitServerReady") {
		t.Errorf("did not wait on the server readiness probe (calls: %v)", installer.calledMethods())
	}
	if installer.called("WaitAgentReady") {
		t.Errorf("a server must not wait on WaitAgentReady (calls: %v)", installer.calledMethods())
	}

	persisted := readPersistedState(t, m.config.StateFilePath)
	if persisted.BootCount != 4 {
		t.Errorf("boot count = %d, want 4 (3 + this boot)", persisted.BootCount)
	}
	if !persisted.FirstBootCompletedAt.Equal(firstBootDate) {
		t.Errorf("first boot date rewritten: got %s, want %s",
			persisted.FirstBootCompletedAt, firstBootDate)
	}
	if persisted.Role != node.RoleServer {
		t.Errorf("persisted role = %q, want %q", persisted.Role, node.RoleServer)
	}
}

// TestRejoinAgentUsesAgentProbe checks that a rebooted agent waits on the
// probe of its own role.
func TestRejoinAgentUsesAgentProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	m, installer := newRejoiningNode(t, bus, "ants-04", "10.0.0.4", node.RoleAgent, 1)
	go func() { _ = m.Run(ctx) }()

	waitForState(t, m, node.StateStableAgent)

	assertNoInstall(t, installer)
	if !installer.called("WaitAgentReady") {
		t.Errorf("did not wait on the agent readiness probe (calls: %v)", installer.calledMethods())
	}
	if installer.called("WaitServerReady") {
		t.Errorf("an agent must not wait on WaitServerReady (calls: %v)", installer.calledMethods())
	}
}

// TestRejoinedNodeIgnoresBootstrapProtocol checks that a node that already completed its first boot does not
// participate in the bootstrap protocol, even if it sees the events on the LAN or a peer coming up.
func TestRejoinedNodeIgnoresBootstrapProtocol(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	m, installer := newRejoiningNode(t, bus, "ants-01", "10.0.0.1", node.RoleServer, 1)
	go func() { _ = m.Run(ctx) }()

	waitForState(t, m, node.StateStableServer)

	// The user action is refused outright on a node that is not on its first boot.
	if err := m.RequestBootstrap(); !errors.Is(err, admin.ErrConflict) {
		t.Errorf("RequestBootstrap on a stable node: got %v, want ErrConflict", err)
	}

	// A bootstrap started elsewhere on the LAN must not drag this node in
	// either, whichever step of the protocol it broadcasts.
	bus.broadcastUser(eventBootstrapRequested, nil)
	bus.broadcastUser(eventBootstrapStart, nil)

	// Nor must the membership signal that now drives the second half of the
	// bootstrap: a peer coming up as a K3s server is exactly what releases a
	// node waiting to install, and this one must stay put.
	peer := bus.addNode("ants-00", "10.0.0.9")
	_ = peer.SetState(node.StateStableServer)

	// The events are handled asynchronously: leave far more time than an
	// install would need (the fake installer answers in 5 ms) before
	// concluding that the node ignored them.
	time.Sleep(250 * time.Millisecond)

	if got := m.State(); got != string(node.StateStableServer) {
		t.Errorf("state = %q, want %q: bootstrap events must not move a stable node",
			got, node.StateStableServer)
	}
	assertNoInstall(t, installer)
}

// TestRejoinRefusesUnusableState checks that a node whose state file cannot be
// trusted stops instead of falling back to the first-boot protocol, which
// would reinstall K3s over the data the node may still hold.
func TestRejoinRefusesUnusableState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	installer := newRecordingInstaller()
	installer.SetInstalledRole(node.RoleServer)
	m := newTestManagerWithInstaller(t, bus, "ants-01", "10.0.0.1", installer)
	writeRawState(t, m, "{ this is not the state you are looking for")

	go func() { _ = m.Run(ctx) }()

	waitForState(t, m, node.StateRejoinFailed)
	assertNoInstall(t, installer)
}

// TestRejoinRefusesRenamedNode checks that a node whose name changed since its first boot stops
// instead of rejoining. Its name is its identity in Serf, in K3s and in etcd at once, so rejoining
// under a new one would leave the old name behind in the cluster and register a second node object.
func TestRejoinRefusesRenamedNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	installer := newRecordingInstaller()
	installer.SetInstalledRole(node.RoleServer)

	m := newTestManagerWithInstaller(t, bus, "ants-01", "10.0.0.1", installer)
	writePersistedState(t, m, node.PersistedState{
		NodeName:             "ants-99", // the name of a previous life
		Role:                 node.RoleServer,
		BootCount:            1,
		FirstBootCompletedAt: firstBootDate,
	})

	go func() { _ = m.Run(ctx) }()

	waitForState(t, m, node.StateRejoinFailed)
	assertNoInstall(t, installer)
	if installer.called("WaitServerReady") {
		t.Error("a renamed node waited for K3s readiness instead of stopping")
	}
}

// TestRejoinGivesUpAfterTimeout checks that a node whose K3s never reports ready again stops waiting
// instead of blocking every etcd membership change forever.
//
// A machine that was evicted and powered back on is the case that reaches it.
func TestRejoinGivesUpAfterTimeout(t *testing.T) {
	for _, role := range []node.Role{node.RoleServer, node.RoleAgent} {
		t.Run(string(role), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			bus := newFakeBus()
			m, installer := newRejoiningNode(t, bus, "ants-01", "10.0.0.1", role, 1)

			// K3s never comes back: the probe outlives the deadline by far.
			m.rejoinTimeout = 20 * time.Millisecond
			installer.Delay = 5 * time.Second

			go func() { _ = m.Run(ctx) }()

			waitForState(t, m, node.StateRejoinFailed)

			// The node must have given up *on the probe*, not earlier: bailing out at the role check
			// is the existing TestRejoinRefusesRoleMismatch path, and it would pass this test blindly.
			probe := "WaitServerReady"
			if role == node.RoleAgent {
				probe = "WaitAgentReady"
			}
			if !installer.called(probe) {
				t.Errorf("did not reach %s, so the deadline is not what stopped it (calls: %v)",
					probe, installer.calledMethods())
			}
			assertNoInstall(t, installer)
		})
	}
}

// TestRejoinRefusesRoleMismatch covers the two ways the state file and the
// local K3s installation can disagree. antsd cannot tell which one is right,
// so it must not act on either.
func TestRejoinRefusesRoleMismatch(t *testing.T) {
	cases := map[string]struct {
		persisted node.Role
		installed node.Role // empty: K3s not installed at all
	}{
		"server on disk, agent installed": {persisted: node.RoleServer, installed: node.RoleAgent},
		"agent on disk, server installed": {persisted: node.RoleAgent, installed: node.RoleServer},
		"no k3s installed":                {persisted: node.RoleServer, installed: ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			bus := newFakeBus()
			installer := newRecordingInstaller()
			installer.SetInstalledRole(tc.installed)

			m := newTestManagerWithInstaller(t, bus, "ants-01", "10.0.0.1", installer)
			writePersistedState(t, m, node.PersistedState{
				NodeName:             "ants-01",
				Role:                 tc.persisted,
				BootCount:            1,
				FirstBootCompletedAt: firstBootDate,
			})

			go func() { _ = m.Run(ctx) }()

			waitForState(t, m, node.StateRejoinFailed)
			assertNoInstall(t, installer)
		})
	}
}
