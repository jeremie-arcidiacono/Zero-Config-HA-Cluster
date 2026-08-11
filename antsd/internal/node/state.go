// Package node defines the node lifecycle :
// the lifecycle State carried in the Serf "state" tag, the K3s Role of a node,
// and the state persisted on disk between reboots.
package node

// State is the main state machine of antsd.
// It is broadcast to the cluster through the Serf "state" tag on every transition.
//
// States prefix :
// - "fb_" belongs to the first-boot protocol.
// - "stable_" means the node is a functioning member of the K3s cluster.
// - "rescale_" belongs to the rescaling workflow, which adjusts the control plane after the population changed.
type State string

const (
	// StateStarting is the initial state while antsd boots its subsystems.
	StateStarting State = "starting"

	// StateDiscovering means the node is on its first boot, discovering
	// peers via mDNS, and waiting for either an existing cluster or a user
	// request to create a new one.
	StateDiscovering State = "fb_discovering"

	// StateBootstrapConfirm means the user asked to create a new
	// cluster and the local node is waiting for their confirmation.
	StateBootstrapConfirm State = "fb_bootstrap_confirm"

	// StateBootstrapWaiting means a new-cluster creation was confirmed
	// (locally or by another node); a grace timer runs so that every node
	// has time to transition to this state before roles are computed.
	StateBootstrapWaiting State = "fb_bootstrap_waiting"

	// StateBootstrapInstallInit means this node is N0 (the lowest node ID)
	// and is installing the first K3s server (cluster-init).
	StateBootstrapInstallInit State = "fb_bootstrap_install_init"

	// StateBootstrapInstallServer means this node is installing K3s as an
	// additional server, joining N0.
	StateBootstrapInstallServer State = "fb_bootstrap_install_servers"

	// StateBootstrapInstallAgent means this node is installing K3s as an
	// agent, joining N0.
	StateBootstrapInstallAgent State = "fb_bootstrap_install_agent"

	// StateBootstrapFailed is a terminal state: the bootstrap process
	// failed on this node and it no longer progresses.
	StateBootstrapFailed State = "fb_bootstrap_failed"

	// StateJoiningWaiting means the node discovered an existing cluster on its
	// first boot: it advertises itself as a candidate and lets the membership
	// settle before picking the server it joins through.
	StateJoiningWaiting State = "fb_joining_waiting"

	// StateJoiningAgent means the node is installing K3s as an agent of the
	// cluster it discovered. A newcomer always joins as an agent, whatever the
	// size of the control plane: growing it belongs to the rescaling workflow.
	StateJoiningAgent State = "fb_joining_agent"

	// StateJoiningFailed is a terminal state: the node could not join the
	// cluster it discovered and no longer progresses.
	StateJoiningFailed State = "fb_joining_failed"

	// StateRejoinCluster means the node found a persisted state (reboot,
	// crash or power cut): it waits for its already-installed K3s to report ready again.
	StateRejoinCluster State = "rejoin_cluster"

	// StateRejoinFailed is a terminal state: the node could not rejoin the
	// cluster it belonged to. It does not fall back to the
	// first-boot protocol, which would reinstall K3s over existing data.
	// Currently, the user should use the reset node feature if this state persists.
	StateRejoinFailed State = "rejoin_failed"

	// StateRescaleCoordinating means this node drives one rescaling round:
	// it evicts the machines that are gone and/or if the control plane no longer has the right size,
	// designates the node that converts.
	StateRescaleCoordinating State = "rescale_coordinating"

	// StateRescalePromoting means the node was designated to grow the control
	// plane: it converts its K3s agent into a server.
	StateRescalePromoting State = "rescale_promoting"

	// StateRescaleDemoting means the node was designated to shrink the control
	// plane: it converts its K3s server into an agent.
	StateRescaleDemoting State = "rescale_demoting"

	// StateRescaleFailed is a terminal state: a rescaling operation (promoting/demoting) failed on this node.
	// The cluster keeps running with the topology it had, this machine no longer progresses.
	StateRescaleFailed State = "rescale_failed"

	// StateStableServer means the node runs a K3s server.
	StateStableServer State = "stable_server"

	// StateStableAgent means the node runs a K3s agent.
	StateStableAgent State = "stable_agent"
)

// InCluster reports whether the state means the node already belongs to a K3s cluster:
// it runs in one, is coming back to one, is changing its role, or failed to.
func (s State) InCluster() bool {
	switch s {
	case StateStableServer, StateStableAgent,
		StateRejoinCluster, StateRejoinFailed,
		StateRescaleCoordinating, StateRescalePromoting, StateRescaleDemoting, StateRescaleFailed:
		return true
	default:
		return false
	}
}
