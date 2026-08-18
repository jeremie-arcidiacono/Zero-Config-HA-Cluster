package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/admin"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/config"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/k3s"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/serfnode"
)

// fakeBus simulates the Serf gossip layer between fakeSerf instances: user
// events and state-tag updates are delivered to every node (sender included),
// like real Serf broadcasts.
type fakeBus struct {
	mu    sync.Mutex
	nodes map[string]*fakeSerf
}

func newFakeBus() *fakeBus {
	return &fakeBus{nodes: make(map[string]*fakeSerf)}
}

func (b *fakeBus) addNode(name, ip string) *fakeSerf {
	b.mu.Lock()
	defer b.mu.Unlock()
	fs := &fakeSerf{
		bus:    b,
		name:   name,
		ip:     ip,
		state:  string(node.StateStarting),
		status: memberStatusAlive,
		// Generously buffered: the run loop is the only consumer and it broadcasts from inside its
		// own turn (through SetState), so a channel that filled up would deadlock the very loop
		// that drains it. Keep the margin when adding tests with more nodes.
		events: make(chan serfnode.Event, 256),
	}
	b.nodes[name] = fs
	return fs
}

func (b *fakeBus) broadcastUser(name string, payload []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, n := range b.nodes {
		n.events <- serfnode.Event{Type: serfnode.EventUser, Name: name, Payload: payload}
	}
}

func (b *fakeBus) updateState(sender *fakeSerf, state node.State) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sender.state = string(state)
	for _, n := range b.nodes {
		n.events <- serfnode.Event{
			Type:   serfnode.EventMemberUpdate,
			Name:   sender.name,
			NodeIP: sender.ip,
			Tags:   map[string]string{"state": sender.state},
		}
	}
}

// markFailed simulates a node dying: Serf keeps the member and its last known
// tags, and moves it to the "failed" status.
func (b *fakeBus) markFailed(sender *fakeSerf) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sender.status = memberStatusFailed
	for _, n := range b.nodes {
		n.events <- serfnode.Event{
			Type:   serfnode.EventMemberFailed,
			Name:   sender.name,
			NodeIP: sender.ip,
			Tags:   map[string]string{"state": sender.state},
		}
	}
}

// prune simulates Serf erasing a failed member from the memberlist: the member disappears from
// every snapshot, after a leave event immediately followed by a reap one.
//
// Serf holds its member lock across the status change and the erasure, so no reader ever sees the
// intermediate "left" status. The node is removed from the map here for the same reason: a test
// must not be able to observe a state production code cannot.
func (b *fakeBus) prune(name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	pruned, found := b.nodes[name]
	if !found {
		return nil // Already gone: pruning is idempotent, like the real thing.
	}
	if pruned.status != memberStatusFailed {
		return fmt.Errorf("refusing to prune %q: status is %q, want %q", name, pruned.status, memberStatusFailed)
	}
	delete(b.nodes, name)

	for _, n := range b.nodes {
		for _, typ := range []serfnode.EventType{serfnode.EventMemberLeave, serfnode.EventMemberReap} {
			n.events <- serfnode.Event{
				Type:   typ,
				Name:   pruned.name,
				NodeIP: pruned.ip,
				Tags:   map[string]string{"state": pruned.state},
			}
		}
	}
	return nil
}

// markAlive simulates a machine coming back before anyone gave up on it.
func (b *fakeBus) markAlive(sender *fakeSerf) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sender.status = memberStatusAlive
	for _, n := range b.nodes {
		n.events <- serfnode.Event{
			Type:   serfnode.EventMemberJoin,
			Name:   sender.name,
			NodeIP: sender.ip,
			Tags:   map[string]string{"state": sender.state},
		}
	}
}

func (b *fakeBus) snapshot(observer string) admin.Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	snapshot := admin.Snapshot{
		CollectedAt: time.Now(),
		NodeName:    observer,
		Available:   true,
	}
	for _, n := range b.nodes {
		snapshot.Members = append(snapshot.Members, admin.Member{
			Name:   n.name,
			IP:     n.ip,
			Status: n.status,
			Tags:   map[string]string{"state": n.state},
		})
	}
	return snapshot
}

// fakeSerf implements serfAPI on top of a fakeBus.
type fakeSerf struct {
	bus    *fakeBus
	name   string
	ip     string
	state  string // guarded by bus.mu
	status string // guarded by bus.mu
	events chan serfnode.Event

	// onLeave, when set, is called by Leave to police the decommission boundary
	// (see newTestManagerWith).
	onLeave func()
}

func (f *fakeSerf) Start(context.Context) (<-chan serfnode.Event, error) {
	return f.events, nil
}
func (f *fakeSerf) LocalIP() string {
	return f.ip
}
func (f *fakeSerf) Snapshot() admin.Snapshot {
	return f.bus.snapshot(f.name)
}

func (f *fakeSerf) Leave() error {
	if f.onLeave != nil {
		f.onLeave()
	}
	return nil
}

func (f *fakeSerf) RemoveFailedNode(name string) error {
	return f.bus.prune(name)
}

func (f *fakeSerf) SetState(state node.State) error {
	f.bus.updateState(f, state)
	return nil
}

func (f *fakeSerf) SendUserEvent(name string, payload []byte) error {
	f.bus.broadcastUser(name, payload)
	return nil
}

// newTestManager builds a Manager wired to the fake bus with a fake, fast
// K3s installer and a short waiting period.
func newTestManager(t *testing.T, bus *fakeBus, name, ip string) *Manager {
	t.Helper()
	return newTestManagerWithInstaller(t, bus, name, ip, newFastFakeInstaller())
}

// newTestManagerWithInstaller is newTestManager with a caller-supplied
// installer, so a test can observe the installation calls.
func newTestManagerWithInstaller(t *testing.T, bus *fakeBus, name, ip string, installer k3s.Installer) *Manager {
	t.Helper()
	return newTestManagerWith(t, bus, name, ip, installer, newFastFakeControlPlane())
}

// newTestManagerWith is the full form, for the tests that observe the cluster-wide operations too.
func newTestManagerWith(
	t *testing.T,
	bus *fakeBus,
	name, ip string,
	installer k3s.Installer,
	controlPlane k3s.ControlPlane,
) *Manager {
	t.Helper()

	conf := &config.Config{
		NodeName:           name,
		HTTPPort:           0, // random free port, the admin server is not exercised here
		StateFilePath:      filepath.Join(t.TempDir(), name+".json"),
		K3sInstaller:       config.InstallerFake,
		RescaleEnabled:     true,
		EvictionGrace:      time.Hour,
		RescaleSettleDelay: time.Minute,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	serf := bus.addNode(name, ip)
	// "left" is the decommission signal alone, and nothing
	// implements decommission yet. Any Leave() call, on shutdown or during an eviction, is a
	// regression, so it must break a test instead of being noticed on the bench.
	serf.onLeave = func() {
		t.Errorf("node %s called serf.Leave(): the left status belongs to decommission alone", name)
	}

	m := newManager(conf, logger, time.Now(), serf, installer, controlPlane)
	m.bootstrapWaitDelay = 50 * time.Millisecond
	m.joinWaitDelay = 50 * time.Millisecond
	m.forgetRetryInterval = 50 * time.Millisecond
	return m
}

// newFastFakeInstaller returns the fake installer with a delay short enough for tests.
func newFastFakeInstaller() *k3s.FakeInstaller {
	installer := k3s.NewFakeInstaller(slog.New(slog.NewTextHandler(io.Discard, nil)))
	installer.Delay = 5 * time.Millisecond
	return installer
}

// newFastFakeControlPlane returns the fake cluster admin with a delay short enough for tests.
func newFastFakeControlPlane() *k3s.FakeControlPlane {
	controlPlane := k3s.NewFakeControlPlane(slog.New(slog.NewTextHandler(io.Discard, nil)))
	controlPlane.Delay = 5 * time.Millisecond
	return controlPlane
}

// recordingControlPlane wraps the fake cluster admin and records, in order, the cluster-wide
// operations the workflow ran from this node.
type recordingControlPlane struct {
	*k3s.FakeControlPlane

	mu    sync.Mutex
	calls []string
	// deleteFailures is the number of DeleteNode calls that must fail before the double behaves
	// again, simulating an API server that refuses a repair this node will have to retry.
	deleteFailures int
	// noQuorum makes every lookup fail, the way etcd answers nothing on the minority side of a
	// partition. Set it to put this node on the losing side.
	noQuorum bool
}

func newRecordingControlPlane() *recordingControlPlane {
	return &recordingControlPlane{FakeControlPlane: newFastFakeControlPlane()}
}

func (r *recordingControlPlane) record(call string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

// calledMethods returns a copy of the recorded calls, each as "Method(node)".
func (r *recordingControlPlane) calledMethods() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// called reports whether a method ran, whatever the node it targeted.
func (r *recordingControlPlane) called(method string) bool {
	return r.callCount(method) > 0
}

// callCount returns how many times a method ran, whatever the node it targeted.
func (r *recordingControlPlane) callCount(method string) int {
	count := 0
	for _, call := range r.calledMethods() {
		if strings.HasPrefix(call, method+"(") {
			count++
		}
	}
	return count
}

// failNextDeletes makes the next count DeleteNode calls fail.
func (r *recordingControlPlane) failNextDeletes(count int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleteFailures = count
}

// loseQuorum cuts this node off from the cluster state, as a network partition does.
func (r *recordingControlPlane) loseQuorum() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.noQuorum = true
}

// calledOn reports whether a method ran against a given node.
func (r *recordingControlPlane) calledOn(method, name string) bool {
	for _, call := range r.calledMethods() {
		if call == method+"("+name+")" {
			return true
		}
	}
	return false
}

func (r *recordingControlPlane) DrainNode(ctx context.Context, name string) error {
	r.record("DrainNode(" + name + ")")
	return r.FakeControlPlane.DrainNode(ctx, name)
}

// NodeExists is left out of the recorded calls: it is a read, and the assertions are about writes.
func (r *recordingControlPlane) NodeExists(ctx context.Context, name string) (bool, error) {
	r.mu.Lock()
	refuse := r.noQuorum
	r.mu.Unlock()

	if refuse {
		return false, errors.New("etcdserver: request timed out")
	}
	return r.FakeControlPlane.NodeExists(ctx, name)
}

func (r *recordingControlPlane) DeleteNode(ctx context.Context, name string) error {
	r.record("DeleteNode(" + name + ")")

	r.mu.Lock()
	refuse := r.deleteFailures > 0
	if refuse {
		r.deleteFailures--
	}
	r.mu.Unlock()

	if refuse {
		return errors.New("the api server refused the node deletion")
	}
	return r.FakeControlPlane.DeleteNode(ctx, name)
}

// recordingInstaller wraps the fake installer and records, in order, the
// installer methods the workflow called on this node.
type recordingInstaller struct {
	*k3s.FakeInstaller

	mu    sync.Mutex
	calls []string
	// joinTarget is the address the node was told to join, as passed to the
	// last join installation.
	joinTarget string

	// beforeInstall, when set, runs at the start of every installation, so a test can assert what
	// the cluster looked like at that instant rather than after the fact.
	beforeInstall func()
}

func newRecordingInstaller() *recordingInstaller {
	return &recordingInstaller{FakeInstaller: newFastFakeInstaller()}
}

func (r *recordingInstaller) record(method string) {
	r.mu.Lock()
	r.calls = append(r.calls, method)
	hook := r.beforeInstall
	r.mu.Unlock()

	// Called with the lock released, so a hook may read this installer too.
	if hook != nil && strings.HasPrefix(method, "Install") {
		hook()
	}
}

// calledMethods returns a copy of the recorded calls.
func (r *recordingInstaller) calledMethods() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func (r *recordingInstaller) called(method string) bool {
	for _, c := range r.calledMethods() {
		if c == method {
			return true
		}
	}
	return false
}

func (r *recordingInstaller) InstallServerInit(ctx context.Context) error {
	r.record("InstallServerInit")
	return r.FakeInstaller.InstallServerInit(ctx)
}

func (r *recordingInstaller) InstallServerJoin(ctx context.Context, serverIP string) error {
	r.record("InstallServerJoin")
	r.recordJoinTarget(serverIP)
	return r.FakeInstaller.InstallServerJoin(ctx, serverIP)
}

func (r *recordingInstaller) InstallAgent(ctx context.Context, serverIP string) error {
	r.record("InstallAgent")
	r.recordJoinTarget(serverIP)
	return r.FakeInstaller.InstallAgent(ctx, serverIP)
}

func (r *recordingInstaller) Convert(ctx context.Context, to node.Role, serverIP string) error {
	r.record("Convert:" + string(to))
	r.recordJoinTarget(serverIP)
	return r.FakeInstaller.Convert(ctx, to, serverIP)
}

func (r *recordingInstaller) recordJoinTarget(serverIP string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.joinTarget = serverIP
}

func (r *recordingInstaller) lastJoinTarget() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.joinTarget
}

func (r *recordingInstaller) WaitServerReady(ctx context.Context) error {
	r.record("WaitServerReady")
	return r.FakeInstaller.WaitServerReady(ctx)
}

func (r *recordingInstaller) WaitAgentReady(ctx context.Context) error {
	r.record("WaitAgentReady")
	return r.FakeInstaller.WaitAgentReady(ctx)
}

// failingInstaller makes the install steps fail, like the K3s install script
// does when "systemctl start" reports that K3s did not come up on first try.
//
// leavesUnit tells whether the simulated script still wrote its systemd unit
// before failing, which is what separates a transient start failure from an
// installation that never happened.
type failingInstaller struct {
	*k3s.FakeInstaller
	leavesUnit bool
}

func newFailingInstaller(leavesUnit bool) *failingInstaller {
	return &failingInstaller{FakeInstaller: newFastFakeInstaller(), leavesUnit: leavesUnit}
}

func (f *failingInstaller) fail(role node.Role) error {
	if f.leavesUnit {
		f.SetInstalledRole(role)
	}
	return errors.New("exit status 1 (output: Job for k3s.service failed)")
}

func (f *failingInstaller) InstallServerInit(context.Context) error {
	return f.fail(node.RoleServer)
}

func (f *failingInstaller) InstallServerJoin(context.Context, string) error {
	return f.fail(node.RoleServer)
}

func (f *failingInstaller) InstallAgent(context.Context, string) error {
	return f.fail(node.RoleAgent)
}

// installTracker watches the installations running across the whole cluster,
// so a test can assert whether they overlap. Server joins must never do, agent
// joins are expected to.
type installTracker struct {
	servers concurrencyCounter
	agents  concurrencyCounter
}

// concurrencyCounter counts how many operations of one kind are in flight and
// remembers the highest number seen at once.
type concurrencyCounter struct {
	mu      sync.Mutex
	current int
	max     int
}

func (j *concurrencyCounter) enter() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.current++
	if j.current > j.max {
		j.max = j.current
	}
}

func (j *concurrencyCounter) leave() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.current--
}

func (j *concurrencyCounter) maxConcurrent() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.max
}

// trackingInstaller reports its installations to a tracker shared by every node.
type trackingInstaller struct {
	*k3s.FakeInstaller
	tracker *installTracker
}

func newTrackingInstaller(tracker *installTracker) *trackingInstaller {
	return &trackingInstaller{FakeInstaller: newFastFakeInstaller(), tracker: tracker}
}

func (t *trackingInstaller) InstallServerJoin(ctx context.Context, serverIP string) error {
	t.tracker.servers.enter()
	defer t.tracker.servers.leave()
	return t.FakeInstaller.InstallServerJoin(ctx, serverIP)
}

func (t *trackingInstaller) InstallAgent(ctx context.Context, serverIP string) error {
	t.tracker.agents.enter()
	defer t.tracker.agents.leave()
	return t.FakeInstaller.InstallAgent(ctx, serverIP)
}

// waitForState polls until the manager reaches the wanted state.
func waitForState(t *testing.T, m *Manager, want node.State) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if m.State() == string(want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("node %s: state %q not reached (still %q)", m.config.NodeName, want, m.State())
}

func readPersistedState(t *testing.T, path string) node.PersistedState {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted state: %v", err)
	}
	var state node.PersistedState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal persisted state: %v", err)
	}
	return state
}

// TestBootstrapFourNodes runs the full first-boot choreography with four
// nodes on the fake bus: the three lowest names must become K3s servers, the
// fourth an agent, and every node must persist its role.
func TestBootstrapFourNodes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	names := []string{"ants-01", "ants-02", "ants-03", "ants-04"}
	managers := make([]*Manager, 0, len(names))
	for i, name := range names {
		m := newTestManager(t, bus, name, fmt.Sprintf("10.0.0.%d", i+1))
		managers = append(managers, m)
		go func() { _ = m.Run(ctx) }()
	}

	for _, m := range managers {
		waitForState(t, m, node.StateDiscovering)
	}

	// The user triggers the creation from the first node's screen.
	if err := managers[0].RequestBootstrap(); err != nil {
		t.Fatalf("RequestBootstrap: %v", err)
	}
	if err := managers[0].ConfirmBootstrap(); err != nil {
		t.Fatalf("ConfirmBootstrap: %v", err)
	}

	waitForState(t, managers[0], node.StateStableServer)
	waitForState(t, managers[1], node.StateStableServer)
	waitForState(t, managers[2], node.StateStableServer)
	waitForState(t, managers[3], node.StateStableAgent)

	for i, m := range managers {
		persisted := readPersistedState(t, m.config.StateFilePath)
		wantRole := node.RoleServer
		if i == 3 {
			wantRole = node.RoleAgent
		}
		if persisted.Role != wantRole {
			t.Errorf("node %s: persisted role %q, want %q", m.config.NodeName, persisted.Role, wantRole)
		}
		if persisted.NodeName != m.config.NodeName {
			t.Errorf("node %s: persisted name %q", m.config.NodeName, persisted.NodeName)
		}
		if persisted.BootCount != 1 {
			t.Errorf("node %s: persisted boot count %d, want 1", m.config.NodeName, persisted.BootCount)
		}
	}
}

// TestBootstrapReadinessProbeMatchesRole checks that each node waits on the
// readiness probe of its own role.
func TestBootstrapReadinessProbeMatchesRole(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	names := []string{"ants-01", "ants-02", "ants-03", "ants-04"}
	managers := make([]*Manager, 0, len(names))
	installers := make([]*recordingInstaller, 0, len(names))

	for i, name := range names {
		installer := newRecordingInstaller()
		m := newTestManagerWithInstaller(t, bus, name, fmt.Sprintf("10.0.0.%d", i+1), installer)
		managers = append(managers, m)
		installers = append(installers, installer)
		go func() { _ = m.Run(ctx) }()
	}

	for _, m := range managers {
		waitForState(t, m, node.StateDiscovering)
	}
	if err := managers[0].RequestBootstrap(); err != nil {
		t.Fatalf("RequestBootstrap: %v", err)
	}
	if err := managers[0].ConfirmBootstrap(); err != nil {
		t.Fatalf("ConfirmBootstrap: %v", err)
	}

	waitForState(t, managers[0], node.StateStableServer)
	waitForState(t, managers[1], node.StateStableServer)
	waitForState(t, managers[2], node.StateStableServer)
	waitForState(t, managers[3], node.StateStableAgent)

	// N0 is the only node that initializes the cluster, the other servers join it.
	if !installers[0].called("InstallServerInit") {
		t.Errorf("node %s: did not initialize the cluster (calls: %v)", names[0], installers[0].calledMethods())
	}

	// The three servers wait on the server probe, never on the agent one.
	for i := range 3 {
		if !installers[i].called("WaitServerReady") {
			t.Errorf("node %s: did not wait on WaitServerReady (calls: %v)",
				names[i], installers[i].calledMethods())
		}
		if installers[i].called("WaitAgentReady") {
			t.Errorf("node %s: a server must not wait on WaitAgentReady (calls: %v)",
				names[i], installers[i].calledMethods())
		}
	}

	// The agent waits on the agent probe, never on the server one.
	agent := installers[3]
	if !agent.called("InstallAgent") {
		t.Errorf("node %s: did not install as an agent (calls: %v)", names[3], agent.calledMethods())
	}
	if !agent.called("WaitAgentReady") {
		t.Errorf("node %s: did not wait on WaitAgentReady (calls: %v)", names[3], agent.calledMethods())
	}
	if agent.called("WaitServerReady") {
		t.Errorf("node %s: an agent must not wait on WaitServerReady (calls: %v)",
			names[3], agent.calledMethods())
	}
}

// TestBootstrapSingleNode checks the degenerate cluster of one machine: the
// node must initialize K3s alone and become a stable server.
func TestBootstrapSingleNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	m := newTestManager(t, bus, "ants-01", "10.0.0.1")
	go func() { _ = m.Run(ctx) }()

	waitForState(t, m, node.StateDiscovering)
	if err := m.RequestBootstrap(); err != nil {
		t.Fatalf("RequestBootstrap: %v", err)
	}
	if err := m.ConfirmBootstrap(); err != nil {
		t.Fatalf("ConfirmBootstrap: %v", err)
	}
	waitForState(t, m, node.StateStableServer)
}

// TestUserActionGuards checks that user actions are refused outside the
// states where the protocol allows them.
func TestUserActionGuards(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	m := newTestManager(t, bus, "ants-01", "10.0.0.1")
	go func() { _ = m.Run(ctx) }()

	waitForState(t, m, node.StateDiscovering)

	if err := m.ConfirmBootstrap(); !errors.Is(err, admin.ErrConflict) {
		t.Errorf("ConfirmBootstrap while discovering: got %v, want ErrConflict", err)
	}
	if err := m.CancelBootstrap(); !errors.Is(err, admin.ErrConflict) {
		t.Errorf("CancelBootstrap while discovering: got %v, want ErrConflict", err)
	}

	if err := m.RequestBootstrap(); err != nil {
		t.Fatalf("RequestBootstrap: %v", err)
	}
	waitForState(t, m, node.StateBootstrapConfirm)

	if err := m.RequestBootstrap(); !errors.Is(err, admin.ErrConflict) {
		t.Errorf("second RequestBootstrap: got %v, want ErrConflict", err)
	}

	if err := m.CancelBootstrap(); err != nil {
		t.Fatalf("CancelBootstrap: %v", err)
	}
	waitForState(t, m, node.StateDiscovering)
}

// TestBootstrapSurvivesTransientInstallFailure covers the K3s install script
// reporting a failure while having written its systemd unit.
//
// This guards something seen on the physical nodes: the script ends with
// "systemctl start", which fails when K3s does not come up on its first attempt.
// K3s restarts on its own (Restart=always) and joins seconds later, but antsd
// used to return that error immediately and latch fb_bootstrap_failed, reporting
// a failure on a cluster that kubectl showed as fully Ready.
func TestBootstrapSurvivesTransientInstallFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	m := newTestManagerWithInstaller(t, bus, "ants-01", "10.0.0.1", newFailingInstaller(true))
	go func() { _ = m.Run(ctx) }()

	waitForState(t, m, node.StateDiscovering)
	if err := m.RequestBootstrap(); err != nil {
		t.Fatalf("RequestBootstrap: %v", err)
	}
	if err := m.ConfirmBootstrap(); err != nil {
		t.Fatalf("ConfirmBootstrap: %v", err)
	}

	waitForState(t, m, node.StateStableServer)
}

// TestBootstrapFailsWhenInstallLeavesNoUnit is the counterpart: an install that
// wrote no systemd unit never happened, and must still fail the bootstrap.
// Tolerating a transient start failure must not swallow a real one.
func TestBootstrapFailsWhenInstallLeavesNoUnit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	m := newTestManagerWithInstaller(t, bus, "ants-01", "10.0.0.1", newFailingInstaller(false))
	go func() { _ = m.Run(ctx) }()

	waitForState(t, m, node.StateDiscovering)
	if err := m.RequestBootstrap(); err != nil {
		t.Fatalf("RequestBootstrap: %v", err)
	}
	if err := m.ConfirmBootstrap(); err != nil {
		t.Fatalf("ConfirmBootstrap: %v", err)
	}

	waitForState(t, m, node.StateBootstrapFailed)
}

// TestBootstrapJoinsServersOneAtATime checks that the joining servers never
// install concurrently.
//
// etcd only admits a single learner member at a time. When every rank >= 1
// server started as soon as N0 became stable, the losers were rejected.
func TestBootstrapJoinsServersOneAtATime(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	tracker := &installTracker{}
	names := []string{"ants-01", "ants-02", "ants-03", "ants-04"}
	managers := make([]*Manager, 0, len(names))

	for i, name := range names {
		installer := newTrackingInstaller(tracker)
		// Long enough that two overlapping joins could not be missed.
		installer.Delay = 80 * time.Millisecond
		m := newTestManagerWithInstaller(t, bus, name, fmt.Sprintf("10.0.0.%d", i+1), installer)
		managers = append(managers, m)
		go func() { _ = m.Run(ctx) }()
	}

	for _, m := range managers {
		waitForState(t, m, node.StateDiscovering)
	}
	if err := managers[0].RequestBootstrap(); err != nil {
		t.Fatalf("RequestBootstrap: %v", err)
	}
	if err := managers[0].ConfirmBootstrap(); err != nil {
		t.Fatalf("ConfirmBootstrap: %v", err)
	}

	waitForState(t, managers[0], node.StateStableServer)
	waitForState(t, managers[1], node.StateStableServer)
	waitForState(t, managers[2], node.StateStableServer)
	waitForState(t, managers[3], node.StateStableAgent)

	if got := tracker.servers.maxConcurrent(); got != 1 {
		t.Errorf("%d server joins ran concurrently, want at most 1", got)
	}
}

// TestBootstrapRefusesNonVirginNode covers the machine an operator restarted
// instead of factory resetting after a failed first boot: no state file, but a
// K3s installation, and possibly a populated etcd, still on disk.
//
// Replaying the first boot there reinstalls over live data, so the node must
// refuse.
func TestBootstrapRefusesNonVirginNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	installer := newRecordingInstaller()
	installer.SetInstalledRole(node.RoleServer)

	m := newTestManagerWithInstaller(t, bus, "ants-01", "10.0.0.1", installer)
	go func() { _ = m.Run(ctx) }()

	waitForState(t, m, node.StateDiscovering)
	if err := m.RequestBootstrap(); err != nil {
		t.Fatalf("RequestBootstrap: %v", err)
	}
	if err := m.ConfirmBootstrap(); err != nil {
		t.Fatalf("ConfirmBootstrap: %v", err)
	}

	waitForState(t, m, node.StateBootstrapFailed)
	assertNoInstall(t, installer)
}

// TestBootstrapRefusedBesideExistingCluster checks that the user cannot even
// start the creation of a cluster while another one is on the LAN: the two
// would live side by side, each ignoring the other.
//
// Neither case here is joinable (no server is serving), so the node stays in
// fb_discovering and keeps showing its button: the refusal must come from the
// button itself.
// The failed-member case : a dead N0 leaves
// the alive member list, so the next name recomputes rank 0 and would
// initialize a second cluster next to the etcd data N0 still holds.
func TestBootstrapRefusedBesideExistingCluster(t *testing.T) {
	cases := map[string]struct {
		peerState node.State
		peerDead  bool
	}{
		"a node is coming back":     {peerState: node.StateRejoinCluster},
		"the only server just died": {peerState: node.StateStableServer, peerDead: true},
		// Either operator-caused or a cold boot that overruns the RejoinTimeout deadline
		"a node gave up coming back": {peerState: node.StateRejoinFailed},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			bus := newFakeBus()
			// The peer sorts after this node, so this node is the rank 0 that runs cluster-init.
			peer := bus.addNode("ants-99", "10.0.0.99")
			if err := peer.SetState(tc.peerState); err != nil {
				t.Fatalf("SetState on the peer: %v", err)
			}
			if tc.peerDead {
				bus.markFailed(peer)
			}

			installer := newRecordingInstaller()
			m := newTestManagerWithInstaller(t, bus, "ants-01", "10.0.0.1", installer)
			go func() { _ = m.Run(ctx) }()

			waitForState(t, m, node.StateDiscovering)
			if err := m.RequestBootstrap(); !errors.Is(err, admin.ErrConflict) {
				t.Fatalf("RequestBootstrap beside an existing cluster: got %v, want ErrConflict", err)
			}
			assertNoInstall(t, installer)
		})
	}
}

// TestBootstrapRefusesToInitWhenClusterAppearsLate covers the same conflict
// appearing after the user pressed the button: the cluster-init guard is the
// last one standing, and it is the only one that still protects the etcd data.
func TestBootstrapRefusesToInitWhenClusterAppearsLate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := newFakeBus()
	installer := newRecordingInstaller()
	m := newTestManagerWithInstaller(t, bus, "ants-01", "10.0.0.1", installer)
	// Long enough to reveal the cluster while this node waits for the start signal.
	m.bootstrapWaitDelay = 300 * time.Millisecond
	go func() { _ = m.Run(ctx) }()

	waitForState(t, m, node.StateDiscovering)
	if err := m.RequestBootstrap(); err != nil {
		t.Fatalf("RequestBootstrap: %v", err)
	}
	if err := m.ConfirmBootstrap(); err != nil {
		t.Fatalf("ConfirmBootstrap: %v", err)
	}

	// A machine of an existing cluster shows up, still restarting its K3s so
	// there is nothing to join yet.
	peer := bus.addNode("ants-99", "10.0.0.99")
	if err := peer.SetState(node.StateRejoinCluster); err != nil {
		t.Fatalf("SetState on the peer: %v", err)
	}

	waitForState(t, m, node.StateBootstrapFailed)
	assertNoInstall(t, installer)
}
