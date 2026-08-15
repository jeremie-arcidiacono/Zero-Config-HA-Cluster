package cluster

// Reading of the Serf membership by the cluster workflows.
//
// Those are lifecycle policy (which server to join, what counts as a node already in a
// cluster), not Serf related: that's why some of those methods don't go in serfnode package

import (
	"slices"
	"time"

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

// aliveMemberNames returns the names of all alive Serf members, this node included.
func (v clusterView) aliveMemberNames() []string {
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
func (v clusterView) findK3sJoinTarget() (admin.Member, bool) {
	return v.lowestNamedAliveMemberIn(node.StateStableServer)
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

// isVirginMember reports whether a member is alive and still running the first-boot protocol.
func (v clusterView) isVirginMember(name string) bool {
	for _, member := range v.snapshot.Members {
		if member.Name == name {
			return member.Status == memberStatusAlive && node.State(member.Tags["state"]).IsFirstBoot()
		}
	}
	return false
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

// population returns the number of machines on the network (alive + failed because they may still come back).
// This is at the membership level, not the K3s level.
func (v clusterView) population() int {
	total := 0
	for _, member := range v.snapshot.Members {
		if isMemberAliveOrFailed(member) {
			total++
		}
	}
	return total
}

// desiredServerCount returns the number of K3s servers this population needs.
// Every decision that adds or removes a server reads this same value.
func (v clusterView) desiredServerCount() int {
	return node.DesiredServerCount(v.population())
}

// needsAnotherK3sServer reports whether the cluster is still missing a K3s server.
//
// A failed member keeps its place: it may come back with its data, and etcd refuses to
// grow while one of its members is unreachable anyway.
func (v clusterView) needsAnotherK3sServer() bool {
	return v.k3sServerCount() < v.desiredServerCount()
}

// hasTooManyK3sServers reports whether the cluster runs more K3s servers than its population needs.
// It's the demotion trigger.
func (v clusterView) hasTooManyK3sServers() bool {
	return v.k3sServerCount() > v.desiredServerCount()
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
// Only alive members count (a machine that died mid-installation doesn't block).
func (v clusterView) isEtcdMembershipChanging() bool {
	for _, member := range v.snapshot.Members {
		if member.Status == memberStatusAlive && isNodeChangingEtcdMembership(node.State(member.Tags["state"])) {
			return true
		}
	}
	return false
}

// isRescaleCoordinator reports whether self holds the turn to repair the cluster.
// The coordinator is the lowest-named alive stable_server.
func (v clusterView) isRescaleCoordinator(self string) bool {
	elected, found := v.lowestNamedAliveMemberIn(node.StateStableServer)
	return found && elected.Name == self
}

// findPromotionTarget returns the agent that must become a server, if there is one to promote.
// The lowest name wins.
func (v clusterView) findPromotionTarget() (admin.Member, bool) {
	return v.lowestNamedAliveMemberIn(node.StateStableAgent)
}

// findDemotionTarget returns the server that must become an agent, if there is one to demote.
// The highest name wins, because isRescaleCoordinator take the lowest one.
func (v clusterView) findDemotionTarget() (admin.Member, bool) {
	return v.highestNamedAliveMemberIn(node.StateStableServer)
}

// highestNamedAliveMemberIn returns the highest-named alive member in the given state.
func (v clusterView) highestNamedAliveMemberIn(state node.State) (admin.Member, bool) {
	target := admin.Member{}
	for _, member := range v.snapshot.Members {
		if member.Status != memberStatusAlive || member.Tags["state"] != string(state) {
			continue
		}
		if target.Name == "" || member.Name > target.Name {
			target = member
		}
	}
	return target, target.Name != ""
}

// lowestNamedAliveMemberIn returns the lowest-named alive member in the given state.
func (v clusterView) lowestNamedAliveMemberIn(state node.State) (admin.Member, bool) {
	target := admin.Member{}
	for _, member := range v.snapshot.Members {
		if member.Status != memberStatusAlive || member.Tags["state"] != string(state) {
			continue
		}
		if target.Name == "" || member.Name < target.Name {
			target = member
		}
	}
	return target, target.Name != ""
}

// failedMembersNames returns the names of the members Serf currently reports as failed.
func (v clusterView) failedMembersNames() []string {
	names := make([]string, 0, len(v.snapshot.Members))
	for _, member := range v.snapshot.Members {
		if member.Status == memberStatusFailed {
			names = append(names, member.Name)
		}
	}
	return names
}

// durablyFailedMembersNames returns the members that have been continuously failed for at least grace,
// and the instant at which the next one becomes evictable (zero when none is pending).
//
// Serf exposes no failure timestamp, so the first observation of each failure is kept by the
// caller in failedSince. A member missing from that map is treated as failing right now.
func (v clusterView) durablyFailedMembersNames(
	failedSince map[string]time.Time,
	grace time.Duration,
	now time.Time,
) (evictable []string, nextDeadline time.Time) {
	for _, name := range v.failedMembersNames() {
		since, seen := failedSince[name]
		if !seen {
			since = now
		}

		deadline := since.Add(grace)
		if !deadline.After(now) {
			evictable = append(evictable, name)
			continue
		}
		if nextDeadline.IsZero() || deadline.Before(nextDeadline) {
			nextDeadline = deadline
		}
	}

	slices.Sort(evictable)
	return evictable, nextDeadline
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
		node.StateRescaleCoordinating,
		node.StateRescalePromoting:
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
		node.StateRejoinCluster,
		node.StateRescaleCoordinating,
		node.StateRescalePromoting,
		node.StateRescaleDemoting:
		return true
	default:
		return false
	}
}
