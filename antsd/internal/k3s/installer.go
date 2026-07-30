// Package k3s installs and configures the local K3s instance.
package k3s

import (
	"context"
	"net"
	"strconv"
)

// apiPort is the K3s Kubernetes API port, used to build the join URL.
const apiPort = 6443

// Installer abstracts the local K3s installation so the cluster workflows can be tested without real K3s.
type Installer interface {
	// InstallServerInit installs K3s as the first server of a new cluster (cluster-init).
	// Should be run by N0 only.
	InstallServerInit(ctx context.Context) error

	// InstallServerJoin installs K3s as an additional server joining the
	// existing cluster through the server at serverIP.
	InstallServerJoin(ctx context.Context, serverIP string) error

	// InstallAgent installs K3s as an agent joining the cluster through the server at serverIP.
	InstallAgent(ctx context.Context, serverIP string) error

	// WaitServerReady blocks until the local K3s server reports ready, the
	// context is canceled, or an internal timeout expires.
	WaitServerReady(ctx context.Context) error

	// WaitAgentReady blocks until the cluster reports this agent node as
	// Ready, the context is canceled, or an internal timeout expires.
	//
	// Readiness is role-specific because an agent hosts no Kubernetes API
	// server: the server probe cannot be reused here.
	WaitAgentReady(ctx context.Context) error
}

// joinURL builds the https URL used by joining nodes to reach the K3s API of the server.
func joinURL(ip string) string {
	return "https://" + net.JoinHostPort(ip, strconv.Itoa(apiPort))
}
