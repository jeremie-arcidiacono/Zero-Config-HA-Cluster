// Package cluster contains the central orchestrator of antsd:
// the Manager owns the node lifecycle (Serf, K3s, admin HTTP server) and runs the cluster workflows (first-boot, etc).
package cluster

import (
	"antsd/internal/admin"
	"antsd/internal/config"
	"antsd/internal/k3s"
	"antsd/internal/node"
	"antsd/internal/serfnode"
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
)

// serfAPI is the subset of serfnode.Node used by the Manager.
// Allows tests to inject a fake Serf implementation.
type serfAPI interface {
	Start(ctx context.Context) (<-chan serfnode.Event, error)
	Leave() error
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
	cmdWaitTimerExpired
	cmdInstallSucceeded
	cmdInstallFailed
)

// command is a message processed by the Manager run loop.
type command struct {
	typ   commandType
	err   error        // set for cmdInstallFailed
	reply chan<- error // set for user actions awaiting a result
}

// Manager is the central orchestrator of antsd. It owns the lifecycle of the
// node and reacts to Serf events, user actions, and internal notifications in a single run loop.
type Manager struct {
	config    *config.Config
	logger    *slog.Logger
	startedAt time.Time

	serf      serfAPI
	installer k3s.Installer

	ctx      context.Context
	commands chan command

	// state is owned exclusively by the run loop
	state node.State
	// stateView mirrors state for concurrent readers (admin HTTP server)
	stateView atomic.Value // todo : better to use atomic.Pointer instead ?

	// bootstrapWaitDelay is the fb_bootstrap_waiting grace period.
	bootstrapWaitDelay time.Duration

	// bootstrap tracks the first-boot protocol progress.
	bootstrap bootstrapProgress
}

// New creates a new Manager with the given configuration.
func New(conf *config.Config, logger *slog.Logger, startedAt time.Time) *Manager {
	var installer k3s.Installer
	if conf.K3sInstaller == config.InstallerFake {
		installer = k3s.NewFakeInstaller(logger)
	} else {
		installer = k3s.NewExecInstaller(conf.K3sToken, logger)
	}

	return newManager(conf, logger, startedAt, serfnode.New(conf, logger), installer)
}

// newManager create a new Manager from explicit dependencies.
func newManager(conf *config.Config, logger *slog.Logger, startedAt time.Time, serf serfAPI, installer k3s.Installer) *Manager {
	m := &Manager{
		config:             conf,
		logger:             logger,
		startedAt:          startedAt,
		serf:               serf,
		installer:          installer,
		commands:           make(chan command, 16),
		state:              node.StateStarting,
		bootstrapWaitDelay: bootstrapWaitDelay,
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

	// TODO: implement rejoin-cluster path with persisted state read
	m.transition(node.StateDiscovering)

	for {
		select {
		case <-ctx.Done():
			m.bootstrap.stopTimer()
			_ = m.serf.Leave()
			m.logger.Info("manager shutting down")
			return nil
		case e := <-events:
			m.handleSerfEvent(e)
		case c := <-m.commands:
			m.handleCommand(c)
		}
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
	case cmdWaitTimerExpired:
		m.onWaitTimerExpired()
	case cmdInstallSucceeded:
		m.onInstallSucceeded()
	case cmdInstallFailed:
		m.onInstallFailed(c.err)
	}
}

// handleSerfEvent dispatches a Serf event inside the run loop.
func (m *Manager) handleSerfEvent(e serfnode.Event) {
	switch e.Type {
	case serfnode.EventUser:
		m.handleUserEvent(e)
	default:
		m.logger.Debug("serf event received", "type", e.Type, "name", e.Name)
		// Membership or tag changes can complete the quorum an agent-role node is waiting for.
		m.maybeInstallAgent()
	}
}
