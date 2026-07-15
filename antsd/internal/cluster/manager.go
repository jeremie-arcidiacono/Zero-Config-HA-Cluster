// Package cluster internal/cluster/manager.go
package cluster

import (
	"antsd/internal/admin"
	"antsd/internal/config"
	"antsd/internal/serfnode"
	"context"
	"log/slog"
)

// Manager is the central orchestrator of antsd. It owns the lifecycle of the node: Serf membership, K3s control, ...
type Manager struct {
	config *config.Config
	logger *slog.Logger

	serf *serfnode.Node
}

// New creates a new Manager with the given configuration.
func New(config *config.Config, logger *slog.Logger) *Manager {
	return &Manager{
		config: config,
		logger: logger,
		serf:   serfnode.New(config, logger),
	}
}

// Run starts the manager's main loop. It blocks until ctx is canceled.
func (m *Manager) Run(ctx context.Context) error {
	events, err := m.serf.Start(ctx)
	if err != nil {
		return err
	}

	// TODO: check persisted state ?

	adminServer, err := admin.NewServer(m.config.HTTPPort, m.serf, m.logger)
	if err != nil {
		return err
	}

	if err := adminServer.Start(ctx); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			_ = m.serf.Leave()
			m.logger.Info("manager shutting down")
			return nil
		case e := <-events:
			m.logger.Info("serf event received", "type", e.Type, "name", e.Name)
		}
	}
}
