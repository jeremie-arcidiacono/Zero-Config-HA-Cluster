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

// joinTargetIP returns the address of the K3s server this node should join,
// or an empty string while no server is up yet.
// The address is read from Serf membership.
// When multiple servers are available: the lowest name wins.
// todo : better to use a random server instead of the lowest name ? (to avoid overloading IO on the same server ?)
func (v clusterView) joinTargetIP() string {
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
	return target.IP
}

// findExistingClusterMember returns a Serf member that already belongs to a K3s cluster, if there is one.
//
// It looks at failed members too: a failed node may come back with its K3s data.
// Only a decommissioned node ("left") stops counting.
func (v clusterView) findExistingClusterMember() (admin.Member, bool) {
	for _, member := range v.snapshot.Members {
		if member.Status != memberStatusAlive && member.Status != memberStatusFailed {
			continue
		}
		if node.State(member.Tags["state"]).InCluster() {
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
