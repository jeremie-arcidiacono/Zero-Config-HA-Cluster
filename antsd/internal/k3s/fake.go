package k3s

import (
	"context"
	"log/slog"
	"time"
)

// fakeInstallDelay simulates the duration of a real K3s installation.
const fakeInstallDelay = 3 * time.Second

// FakeInstaller is a no-op Installer used for development and tests.
// Every operation logs, waits a short delay, and succeeds.
type FakeInstaller struct {
	logger *slog.Logger

	// Delay overrides fakeInstallDelay when set (used by tests to run faster).
	Delay time.Duration
}

// NewFakeInstaller returns an Installer that only simulates installations.
func NewFakeInstaller(logger *slog.Logger) *FakeInstaller {
	return &FakeInstaller{logger: logger, Delay: fakeInstallDelay}
}

func (f *FakeInstaller) InstallServerInit(ctx context.Context) error {
	return f.simulate(ctx, "install server (cluster-init)", "")
}

func (f *FakeInstaller) InstallServerJoin(ctx context.Context, serverIP string) error {
	return f.simulate(ctx, "install server (join)", serverIP)
}

func (f *FakeInstaller) InstallAgent(ctx context.Context, serverIP string) error {
	return f.simulate(ctx, "install agent (join)", serverIP)
}

func (f *FakeInstaller) WaitServerReady(ctx context.Context) error {
	return f.simulate(ctx, "wait server ready", "")
}

func (f *FakeInstaller) WaitAgentReady(ctx context.Context) error {
	return f.simulate(ctx, "wait agent ready", "")
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
