// Package cluster contains the central orchestrator of antsd:
// the Manager owns the node lifecycle (Serf, K3s, admin HTTP server) and runs the cluster workflows (first-boot, etc).
package cluster

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/admin"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/config"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/k3s"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/serfnode"
)

// serfAPI is the subset of serfnode.Node used by the Manager.
// Allows tests to inject a fake Serf implementation.
type serfAPI interface {
	Start(ctx context.Context) (<-chan serfnode.Event, error)
	// Leave announces a permanent departure, reserved for the decommission workflow.
	Leave() error
	RemoveFailedNode(name string) error
	SetState(state node.State) error
	SendUserEvent(name string, payload []byte) error
	LocalIP() string
	Snapshot() admin.Snapshot
}

// commandType identifies an action submitted to the Manager run loop.
type commandType int

const (
	// User actions relayed by the admin HTTP server.
	cmdRequestBootstrap commandType = iota
	cmdConfirmBootstrap
	cmdCancelBootstrap

	// Internal notifications.
	cmdBootstrapWaitExpired
	cmdJoinWaitExpired
	cmdRescaleCheck
	cmdK3sOperationSucceeded
	cmdK3sOperationFailed
)

// command is a message processed by the Manager run loop.
type command struct {
	typ   commandType
	err   error        // set for cmdK3sOperationFailed
	reply chan<- error // set for user actions awaiting a result
}

// Manager is the central orchestrator of antsd. It owns the lifecycle of the
// node and reacts to Serf events, user actions, and internal notifications in a single run loop.
type Manager struct {
	config    *config.Config
	logger    *slog.Logger
	startedAt time.Time

	serf         serfAPI
	installer    k3s.Installer
	clusterAdmin k3s.ClusterAdmin

	ctx      context.Context
	commands chan command

	// state is owned exclusively by the run loop
	state node.State
	// stateView mirrors state for concurrent readers (admin HTTP server)
	stateView atomic.Value // todo : better to use atomic.Pointer instead ?

	// bootstrapWaitDelay is the fb_bootstrap_waiting grace period.
	bootstrapWaitDelay time.Duration
	// joinWaitDelay is the fb_joining_waiting grace period.
	joinWaitDelay time.Duration
	// evictionGrace is how long a machine must stay continuously Serf-failed before the
	// rescaling workflow evicts it.
	evictionGrace time.Duration
	// rescaleSettleDelay debounce the control-plane size decision.
	rescaleSettleDelay time.Duration

	// bootstrap tracks the first-boot bootstrap protocol progress.
	bootstrap bootstrapProgress
	// joining tracks the first-boot joining path progress.
	joining joiningProgress
	// rescale tracks the rescaling workflow progress.
	rescale rescaleProgress

	// persistedState is the state left by a previous boot, nil on a first boot. Owned by the run loop.
	persistedState *node.PersistedState
}

// New creates a new Manager with the given configuration.
func New(conf *config.Config, logger *slog.Logger, startedAt time.Time) *Manager {
	var installer k3s.Installer
	var clusterAdmin k3s.ClusterAdmin

	if conf.K3sInstaller == config.InstallerFake {
		fake := k3s.NewFakeInstaller(logger)
		fake.SetInstalledRole(node.Role(conf.K3sFakeInstalledRole))
		installer = fake
		clusterAdmin = k3s.NewFakeClusterAdmin(logger)
	} else {
		installer = k3s.NewExecInstaller(conf.K3sToken, logger)
		clusterAdmin = k3s.NewExecClusterAdmin(logger)
	}

	return newManager(conf, logger, startedAt, serfnode.New(conf, logger), installer, clusterAdmin)
}

// newManager creates a new Manager from explicit dependencies.
func newManager(
	conf *config.Config,
	logger *slog.Logger,
	startedAt time.Time,
	serf serfAPI,
	installer k3s.Installer,
	clusterAdmin k3s.ClusterAdmin,
) *Manager {
	m := &Manager{
		config:             conf,
		logger:             logger,
		startedAt:          startedAt,
		serf:               serf,
		installer:          installer,
		clusterAdmin:       clusterAdmin,
		commands:           make(chan command, 16),
		state:              node.StateStarting,
		bootstrapWaitDelay: bootstrapWaitDelay,
		joinWaitDelay:      joinWaitDelay,
		evictionGrace:      conf.EvictionGrace,
		rescaleSettleDelay: conf.RescaleSettleDelay,
	}
	m.stateView.Store(node.StateStarting)
	return m
}

// Run starts the manager's main loop. It blocks until ctx is canceled.
func (m *Manager) Run(ctx context.Context) error {
	m.ctx = ctx

	events, err := m.serf.Start(ctx)
	if err != nil {
		return err
	}

	adminServer, err := admin.NewServer(m.config.HTTPPort, m.serf, m, m.logger, m.startedAt)
	if err != nil {
		return err
	}
	if err := adminServer.Start(ctx); err != nil {
		return err
	}

	m.chooseInitialState()

	for {
		select {
		case <-ctx.Done():
			m.bootstrap.stopTimer()
			m.joining.stopTimer()
			m.rescale.stopTimer()
			// no serf.Leave() here : it's reserved for the decommission workflow (not implemented yet)
			m.logger.Info("manager shutting down")
			return nil
		case e := <-events:
			m.handleSerfEvent(e)
		case c := <-m.commands:
			m.handleCommand(c)
		}
	}
}

// chooseInitialState decides, from the state left on disk, whether this node
// is booting for the first time or coming back to a cluster it already belongs to.
func (m *Manager) chooseInitialState() {
	persisted, err := node.Load(m.config.StateFilePath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		m.logger.Info("no persisted state, running the first-boot protocol",
			"path", m.config.StateFilePath)
		m.transition(node.StateDiscovering)
	case err != nil:
		m.logger.Error("unusable persisted state, refusing to rejoin or bootstrap",
			"path", m.config.StateFilePath, "error", err)
		m.transition(node.StateRejoinFailed)
	default:
		m.persistedState = &persisted
		m.startRejoin()
	}
}

// transition moves the node to a new global lifecycle state and change the Serf state tag.
// Must only be called from the run loop to avoid concurrent writes to m.state.
func (m *Manager) transition(to node.State) {
	from := m.state
	m.state = to
	m.stateView.Store(to)
	m.logger.Info("state transition", "from", from, "to", to)

	if err := m.serf.SetState(to); err != nil {
		m.logger.Error("failed to change state tag", "state", to, "error", err)
	}
}

// becomeStable enters the stable state of the node's role and persists state on disk.
// Every workflow ends here: first boot, rejoin, and later the joining path.
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

// State implements admin.Controller. Safe for concurrent use.
func (m *Manager) State() string {
	return string(m.stateView.Load().(node.State))
}

// RequestBootstrap implements admin.Controller (screen A button).
func (m *Manager) RequestBootstrap() error {
	return m.submitUserAction(cmdRequestBootstrap)
}

// ConfirmBootstrap implements admin.Controller (screen C confirm button).
func (m *Manager) ConfirmBootstrap() error {
	return m.submitUserAction(cmdConfirmBootstrap)
}

// CancelBootstrap implements admin.Controller (screen C back button).
func (m *Manager) CancelBootstrap() error {
	return m.submitUserAction(cmdCancelBootstrap)
}

// submitUserAction sends a user action to the run loop and waits for its
// result.
func (m *Manager) submitUserAction(typ commandType) error {
	reply := make(chan error, 1)
	if err := m.submit(command{typ: typ, reply: reply}); err != nil {
		return err
	}
	select {
	case err := <-reply:
		return err
	case <-m.ctx.Done():
		return fmt.Errorf("antsd is shutting down")
	}
}

// submit enqueues a command for the run loop, aborting if antsd shuts down.
func (m *Manager) submit(c command) error {
	select {
	case m.commands <- c:
		return nil
	case <-m.ctx.Done():
		return fmt.Errorf("antsd is shutting down")
	}
}

// handleCommand dispatches a command inside the run loop.
func (m *Manager) handleCommand(c command) {
	switch c.typ {
	case cmdRequestBootstrap:
		c.reply <- m.onRequestBootstrap()
	case cmdConfirmBootstrap:
		c.reply <- m.onConfirmBootstrap()
	case cmdCancelBootstrap:
		c.reply <- m.onCancelBootstrap()
	case cmdBootstrapWaitExpired:
		m.onBootstrapWaitExpired()
	case cmdJoinWaitExpired:
		m.onJoinWaitExpired()
	case cmdRescaleCheck:
		m.onRescaleCheck()
	case cmdK3sOperationSucceeded:
		m.onK3sOperationSucceeded()
	case cmdK3sOperationFailed:
		m.onK3sOperationFailed(c.err)
	}
}

// startK3sOperation runs a K3s operation (install, readiness wait, ...) in a
// goroutine and reports the outcome back to the run loop, which stays responsive meanwhile.
func (m *Manager) startK3sOperation(op func(ctx context.Context) error) {
	ctx := m.ctx
	go func() {
		if err := op(ctx); err != nil {
			_ = m.submit(command{typ: cmdK3sOperationFailed, err: err})
			return
		}
		_ = m.submit(command{typ: cmdK3sOperationSucceeded})
	}()
}

// onK3sOperationSucceeded hands the result to the workflow that started the operation,
// identified by the current state.
func (m *Manager) onK3sOperationSucceeded() {
	switch m.state {
	case node.StateBootstrapInstallInit, node.StateBootstrapInstallServer, node.StateBootstrapInstallAgent:
		m.onBootstrapInstallSucceeded()
	case node.StateJoiningServer, node.StateJoiningAgent:
		m.onJoiningInstallSucceeded()
	case node.StateRejoinCluster:
		m.onRejoinReady()
	case node.StateRescaleCoordinating:
		m.onRescaleCoordinationDone()
	case node.StateRescalePromoting, node.StateRescaleDemoting:
		m.onRescaleConverted()
	default:
		m.logger.Warn("k3s operation succeeded in unexpected state", "state", m.state)
	}
}

// onK3sOperationFailed puts the node in the terminal state of the workflow that started the operation.
func (m *Manager) onK3sOperationFailed(err error) {
	switch m.state {
	case node.StateBootstrapInstallInit, node.StateBootstrapInstallServer, node.StateBootstrapInstallAgent:
		m.failBootstrap(fmt.Errorf("k3s installation failed: %w", err))
	case node.StateJoiningServer, node.StateJoiningAgent:
		m.failJoining(fmt.Errorf("k3s installation failed: %w", err))
	case node.StateRejoinCluster:
		m.failRejoin(err)
	case node.StateRescaleCoordinating:
		m.abandonCoordination(err)
	case node.StateRescalePromoting, node.StateRescaleDemoting:
		m.failRescale(fmt.Errorf("k3s conversion failed: %w", err))
	default:
		m.logger.Warn("k3s operation failed in unexpected state", "state", m.state, "error", err)
	}
}

// handleSerfEvent dispatches a Serf event inside the run loop.
func (m *Manager) handleSerfEvent(e serfnode.Event) {
	switch e.Type {
	case serfnode.EventUser:
		m.handleUserEvent(e)
	default:
		m.logger.Debug("serf event received", "type", e.Type, "name", e.Name)
		// Membership and tag changes are the signal for a server or agent.
		// The joining path comes first: it may move the node off the bootstrap protocol.
		view := m.observeCluster()
		m.maybeStartJoiningPath(view)
		m.maybeJoinCluster(view)
		m.maybeInstallServer(view)
		m.maybeInstallAgent(view)
		m.maybeRescale(view)
	}
}

// handleUserEvent dispatches a received Serf user event to the workflow that owns it.
func (m *Manager) handleUserEvent(e serfnode.Event) {
	switch e.Name {
	case eventBootstrapRequested:
		m.onBootstrapRequested()
	case eventBootstrapStart:
		m.onBootstrapStart()
	case eventRescaleConvert:
		m.onRescaleConvert(e.Payload)
	default:
		m.logger.Debug("ignoring unknown serf user event", "name", e.Name)
	}
}
