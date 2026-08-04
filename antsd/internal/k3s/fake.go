package k3s

import (
	"context"
	"log/slog"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
)

// fakeInstallDelay simulates the duration of a real K3s installation.
const fakeInstallDelay = 3 * time.Second

// FakeInstaller is a no-op Installer used for development and tests.
// Every operation logs, waits a short delay, and succeeds.
type FakeInstaller struct {
	logger *slog.Logger

	// Delay overrides fakeInstallDelay when set (used by tests to run faster).
	Delay time.Duration

	// installed is the role a simulated installation left behind, reported by InstalledRole.
	// It lives in memory only (forgotten after a restart).
	//mu        sync.Mutex
	installed node.Role
}

// NewFakeInstaller returns an Installer that only simulates installations.
func NewFakeInstaller(logger *slog.Logger) *FakeInstaller {
	return &FakeInstaller{logger: logger, Delay: fakeInstallDelay}
}

// SetInstalledRole pretends K3s is already installed with the given role.
// An empty role means "not installed".
func (f *FakeInstaller) SetInstalledRole(role node.Role) {
	//f.mu.Lock()
	//defer f.mu.Unlock()
	f.installed = role
}

func (f *FakeInstaller) InstallServerInit(ctx context.Context) error {
	return f.simulateInstall(ctx, "install server (cluster-init)", "", node.RoleServer)
}

func (f *FakeInstaller) InstallServerJoin(ctx context.Context, serverIP string) error {
	return f.simulateInstall(ctx, "install server (join)", serverIP, node.RoleServer)
}

func (f *FakeInstaller) InstallAgent(ctx context.Context, serverIP string) error {
	return f.simulateInstall(ctx, "install agent (join)", serverIP, node.RoleAgent)
}

func (f *FakeInstaller) WaitServerReady(ctx context.Context) error {
	return f.simulate(ctx, "wait server ready", "")
}

func (f *FakeInstaller) WaitAgentReady(ctx context.Context) error {
	return f.simulate(ctx, "wait agent ready", "")
}

func (f *FakeInstaller) InstalledRole(context.Context) (node.Role, error) {
	//f.mu.Lock()
	//defer f.mu.Unlock()

	if f.installed == "" {
		return "", ErrNotInstalled
	}
	return f.installed, nil
}

// simulateInstall simulates an installation and records the role,
// so a later InstalledRole answers like a real node would.
func (f *FakeInstaller) simulateInstall(ctx context.Context, operation, serverIP string, role node.Role) error {
	if err := f.simulate(ctx, operation, serverIP); err != nil {
		return err
	}
	f.SetInstalledRole(role)
	return nil
}

func (f *FakeInstaller) simulate(ctx context.Context, operation, serverIP string) error {
	args := []any{"operation", operation}
	if serverIP != "" {
		args = append(args, "server_url", joinURL(serverIP))
	}
	f.logger.Info("fake k3s installer", args...)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(f.Delay):
		return nil
	}
}
