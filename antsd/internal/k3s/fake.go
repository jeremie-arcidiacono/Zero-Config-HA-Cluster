package k3s

import (
	"context"
	"fmt"
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

func (f *FakeInstaller) Convert(ctx context.Context, to node.Role, serverIP string) error {
	if _, err := f.InstalledRole(ctx); err != nil {
		return fmt.Errorf("fake installer cannot convert this node to %q: %w", to, err)
	}
	return f.simulateInstall(ctx, "convert to "+string(to), serverIP, to)
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

// FakeControlPlane is the ControlPlane counterpart of FakeInstaller: every operation logs,
// waits a short delay, and succeeds.
type FakeControlPlane struct {
	logger *slog.Logger

	// Delay overrides fakeInstallDelay when set (used by tests to run faster).
	Delay time.Duration

	// deleted holds the node objects a simulated repair removed.
	deleted map[string]bool
}

// NewFakeControlPlane returns a ControlPlane that only simulates cluster operations.
func NewFakeControlPlane(logger *slog.Logger) *FakeControlPlane {
	return &FakeControlPlane{logger: logger, Delay: fakeInstallDelay, deleted: make(map[string]bool)}
}

func (f *FakeControlPlane) DrainNode(ctx context.Context, name string) error {
	return f.simulate(ctx, "drain node", name)
}

func (f *FakeControlPlane) DeleteNode(ctx context.Context, name string) error {
	if err := f.simulate(ctx, "delete node", name); err != nil {
		return err
	}
	f.deleted[name] = true
	return nil
}

func (f *FakeControlPlane) NodeExists(_ context.Context, name string) (bool, error) {
	return !f.deleted[name], nil
}

func (f *FakeControlPlane) simulate(ctx context.Context, operation, name string) error {
	f.logger.Info("fake k3s control_plane", "operation", operation, "node", name)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(f.Delay):
		return nil
	}
}
