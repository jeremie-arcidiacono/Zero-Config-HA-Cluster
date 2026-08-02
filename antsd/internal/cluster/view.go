package cluster

// Reading of the Serf membership by the cluster workflows.
//
// Those are lifecycle policy (which server to join, what counts as a node already in a
// cluster), not Serf related: that's why some of those methods don't go in serfnode package

import (
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/admin"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
)

// Serf member statuses.
const (
	// memberStatusAlive is the status of a live node.
	memberStatusAlive = "alive"

	// memberStatusFailed is the status of a node that disappeared without leaving the cluster, it may come back.
	memberStatusFailed = "failed"
)

// clusterView is one observation of the cluster, taken at a single instant.
//
// The run loop builds one per turn and passes it to every decision step it makes (rank, server count, ...) so they all
// answer with the same cluster state.
type clusterView struct {
	snapshot admin.Snapshot
}

// observeCluster reads the current Serf membership.
func (m *Manager) observeCluster() clusterView {
	return clusterView{snapshot: m.serf.Snapshot()}
}

// aliveNames returns the names of all alive Serf members, this node included.
func (v clusterView) aliveNames() []string {
	names := make([]string, 0, len(v.snapshot.Members))
	for _, member := range v.snapshot.Members {
		if member.Status == memberStatusAlive {
			names = append(names, member.Name)
		}
	}
	return names
}

// findK3sJoinTarget returns the K3s server this node should join, if one is up.
// The address is read from Serf membership.
// When multiple servers are available: the lowest name wins.
// todo : better to use a random server instead of the lowest name ? (to avoid overloading IO on the same server ?)
func (v clusterView) findK3sJoinTarget() (admin.Member, bool) {
	target := admin.Member{}
	for _, member := range v.snapshot.Members {
		if member.Status != memberStatusAlive ||
			member.Tags["state"] != string(node.StateStableServer) {
			continue
		}
		if target.Name == "" || member.Name < target.Name {
			target = member
		}
	}
	return target, target.Name != ""
}

// findExistingK3sClusterMember returns a Serf member that already belongs to a K3s cluster, if there is one.
//
// It looks at failed members too: a failed node may come back with its K3s data.
// Only a decommissioned node ("left") stops counting.
func (v clusterView) findExistingK3sClusterMember() (admin.Member, bool) {
	for _, member := range v.snapshot.Members {
		if isMemberAliveOrFailed(member) && node.State(member.Tags["state"]).InCluster() {
			return member, true
		}
	}
	return admin.Member{}, false
}

// stableServerCount returns how many alive members currently expose the stable_server state.
func (v clusterView) stableServerCount() int {
	count := 0
	for _, member := range v.snapshot.Members {
		if member.Status == memberStatusAlive &&
			member.Tags["state"] == string(node.StateStableServer) {
			count++
		}
	}
	return count
}

// needsAnotherK3sServer reports whether the cluster is still missing a K3s server.
//
// A failed member keeps its place: replacing a dead server is the rescaling workflow's job.
func (v clusterView) needsAnotherK3sServer() bool {
	totalMember := 0
	for _, member := range v.snapshot.Members {
		if isMemberAliveOrFailed(member) {
			totalMember++
		}
	}
	return v.k3sServerCount() < node.DesiredServerCount(totalMember)
}

// k3sServerCount returns the number of members that are a K3s server or are installing one.
func (v clusterView) k3sServerCount() int {
	count := 0
	for _, member := range v.snapshot.Members {
		if isMemberAliveOrFailed(member) && isNodeAK3sServer(node.State(member.Tags["state"])) {
			count++
		}
	}
	return count
}

// isEtcdMembershipChanging reports whether a member is currently altering the etcd membership.
// Only alive members count (a machine that died mid-installation doesn't block later join).
func (v clusterView) isEtcdMembershipChanging() bool {
	for _, member := range v.snapshot.Members {
		if member.Status == memberStatusAlive && isNodeChangingEtcdMembership(node.State(member.Tags["state"])) {
			return true
		}
	}
	return false
}

// isFirstWaitingJoiner reports whether self holds the turn among the nodes waiting to join.
// The lowest name win.
func (v clusterView) isFirstWaitingJoiner(self string) bool {
	for _, member := range v.snapshot.Members {
		if member.Status != memberStatusAlive ||
			member.Tags["state"] != string(node.StateJoiningWaiting) {
			continue
		}
		if member.Name < self {
			return false
		}
	}
	return true
}

// isMemberAliveOrFailed reports whether a member status still belongs to the cluster's population.
// A failed node may come back, so only a decommissioned one ("left") stops counting.
func isMemberAliveOrFailed(member admin.Member) bool {
	return member.Status == memberStatusAlive || member.Status == memberStatusFailed
}

// isNodeAK3sServer reports whether a state means the member runs a K3s server or is installing one.
// Such a member holds one of the cluster's server slots.
func isNodeAK3sServer(state node.State) bool {
	switch state {
	// todo : add rejoin state ? split rejoin_cluster into two role ?
	case node.StateStableServer,
		node.StateBootstrapInstallInit,
		node.StateBootstrapInstallServer,
		node.StateJoiningServer:
		return true
	default:
		return false
	}
}

// isNodeChangingEtcdMembership reports whether a state means the member is currently
// modifying the etcd membership, which admits a single change at a time.
//
// rejoin_cluster is included although a restarting node adds no member: it may be
// a server reconnecting to the quorum, and adding one meanwhile is risky.
func isNodeChangingEtcdMembership(state node.State) bool {
	switch state {
	case node.StateBootstrapInstallInit,
		node.StateBootstrapInstallServer,
		node.StateJoiningServer,
		node.StateRejoinCluster:
		return true
	default:
		return false
	}
}
