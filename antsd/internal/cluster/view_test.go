package cluster

// These predicates are the shared vocabulary of every workflow: the joining path and the
// rescaling both decide from them, which is exactly why they must never disagree.
// They are cheap to test directly and expensive to debug through a choreography.

import (
	"fmt"
	"testing"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/admin"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"
)

// viewOf builds an observation from "name: state" pairs, every member alive.
func viewOf(members ...[2]string) clusterView {
	return viewWithStatus(memberStatusAlive, members...)
}

// viewWithStatus is viewOf with a caller-chosen Serf status for every member.
func viewWithStatus(status string, members ...[2]string) clusterView {
	snapshot := admin.Snapshot{Available: true}
	for _, member := range members {
		snapshot.Members = append(snapshot.Members, admin.Member{
			Name:   member[0],
			IP:     "10.0.0.1",
			Status: status,
			Tags:   map[string]string{"state": member[1]},
		})
	}
	return clusterView{snapshot: snapshot}
}

func member(name string, state node.State) [2]string {
	return [2]string{name, string(state)}
}

// TestJoinTargetIgnoresRescalingNodes checks that a node busy changing its own role is never
// offered as a place to install against.
//
// findK3sJoinTarget matches stable_server exactly, and it must stay that way: a promoting node
// has no API server yet, and a demoting one is tearing its own down.
func TestJoinTargetIgnoresRescalingNodes(t *testing.T) {
	for _, state := range []node.State{
		node.StateRescaleCoordinating,
		node.StateRescalePromoting,
		node.StateRescaleDemoting,
		node.StateRescaleFailed,
	} {
		t.Run(string(state), func(t *testing.T) {
			view := viewOf(member("ants-01", state), member("ants-02", node.StateStableServer))

			target, found := view.findK3sJoinTarget()
			if !found {
				t.Fatal("no join target found, want the stable server")
			}
			if target.Name != "ants-02" {
				t.Errorf("join target = %q, want ants-02: a node in %q is not joinable", target.Name, state)
			}
		})
	}
}

// TestStatesHoldingAServerSlot pins which states make a member count as a K3s server, next to one
// that already is: a node installing a server holds its slot before it reaches stable_server, or
// the cluster would send a second machine after the same one.
func TestStatesHoldingAServerSlot(t *testing.T) {
	cases := map[node.State]int{
		node.StateBootstrapInstallInit:   2,
		node.StateBootstrapInstallServer: 2,
		node.StateRescaleCoordinating:    2,
		node.StateRescalePromoting:       2,

		node.StateBootstrapInstallAgent: 1,
		node.StateJoiningAgent:          1,
		node.StateStableAgent:           1,
		node.StateRescaleDemoting:       1,
		node.StateRescaleFailed:         1,

		node.StateRejoinCluster: 1,
	}

	for state, want := range cases {
		t.Run(string(state), func(t *testing.T) {
			view := viewOf(member("ants-01", node.StateStableServer), member("ants-02", state))
			if got := view.k3sServerCount(); got != want {
				t.Errorf("k3sServerCount() = %d, want %d with a node in %q", got, want, state)
			}
		})
	}
}

// TestStatesBlockingEtcdMembershipChanges checks which states the advisory serialization check
// reads as busy, which is what serializes rescaling against the joining and bootstrap paths.
//
// The false rows matter as much as the true ones, and the joining path is entirely among them: a
// newcomer only ever installs an agent, which touches no etcd member. Blocking on it would
// serialize a whole batch of newcomers for nothing, and worse, a newcomer that cannot finish would
// block the repair of the cluster.
func TestStatesBlockingEtcdMembershipChanges(t *testing.T) {
	cases := map[node.State]bool{
		node.StateBootstrapInstallInit:   true,
		node.StateBootstrapInstallServer: true,
		node.StateRejoinCluster:          true,
		node.StateRescaleCoordinating:    true,
		node.StateRescalePromoting:       true,
		node.StateRescaleDemoting:        true,

		node.StateBootstrapInstallAgent: false,
		node.StateJoiningAgent:          false,
		node.StateJoiningWaiting:        false,
		node.StateStableServer:          false,
		node.StateStableAgent:           false,
		// The point of bounding rejoin_cluster: giving up stops blocking, so the cluster can
		// evict and rescale again around a machine that cannot come back.
		node.StateRejoinFailed: false,
	}

	for state, want := range cases {
		t.Run(string(state), func(t *testing.T) {
			view := viewOf(member("ants-01", node.StateStableServer), member("ants-02", state))
			if got := view.isEtcdMembershipChanging(); got != want {
				t.Errorf("isEtcdMembershipChanging() = %t with a node in %q, want %t", got, state, want)
			}
		})
	}
}

// TestDeadMemberStopsBlockingEtcdMembershipChanges checks that the state tag is only read on alive
// members.
//
// Serf keeps the last tags of a failed member, so a state that means "busy right
// now" survives the machine that carried it. A node powered off mid-installation would otherwise
// block every etcd membership change until Serf forgot it.
func TestDeadMemberStopsBlockingEtcdMembershipChanges(t *testing.T) {
	busy := member("ants-02", node.StateBootstrapInstallServer)

	if !viewOf(busy).isEtcdMembershipChanging() {
		t.Fatal("an alive member installing a server must block etcd membership changes")
	}
	if viewWithStatus(memberStatusFailed, busy).isEtcdMembershipChanging() {
		t.Error("a failed member still blocked: it answers nothing and will never stop on its own")
	}
}

// TestClusterMemberIsNotAJoinTarget separates the two predicates that read alike and answer
// different questions: "is there a server I can install against ?" and "does anyone here already
// belong to a cluster ?".
//
// Confusing them would let a user create a second cluster next to one whose servers are all
// restarting, on top of the etcd data those machines still hold.
func TestClusterMemberIsNotAJoinTarget(t *testing.T) {
	view := viewOf(member("ants-01", node.StateRejoinCluster))

	if _, found := view.findK3sJoinTarget(); found {
		t.Error("a restarting server is not joinable: nothing serves the K3s API yet")
	}
	existing, found := view.findExistingK3sClusterMember()
	if !found {
		t.Fatal("a restarting server still belongs to a cluster, and forbids creating another one")
	}
	if existing.Name != "ants-01" {
		t.Errorf("existing cluster member = %q, want ants-01", existing.Name)
	}

	// A failed member counts too: it may come back with its data.
	dead := viewWithStatus(memberStatusFailed, member("ants-01", node.StateStableServer))
	if _, found := dead.findExistingK3sClusterMember(); !found {
		t.Error("a failed server still belongs to a cluster: it may come back with its etcd data")
	}
	if _, found := dead.findK3sJoinTarget(); found {
		t.Error("a failed server is not joinable")
	}
}

// TestCoordinatorIsNeverTheDemotionTarget guards the one off-by-one that would have a node drain
// itself: the coordinator is the lowest-named server, the demotion target the highest-named one.
//
// Two servers is the tightest case, and the only one a demotion can reach: the target size never
// drops below one, so a demotion needs at least two servers to start from.
func TestCoordinatorIsNeverTheDemotionTarget(t *testing.T) {
	for count := 2; count <= node.MaxServers; count++ {
		members := make([][2]string, 0, count)
		for i := 1; i <= count; i++ {
			members = append(members, member(nodeName(i), node.StateStableServer))
		}
		view := viewOf(members...)

		target, found := view.findDemotionTarget()
		if !found {
			t.Fatalf("%d servers: no demotion target found", count)
		}
		if !view.isRescaleCoordinator(nodeName(1)) {
			t.Fatalf("%d servers: the lowest-named server is not the coordinator", count)
		}
		if target.Name == nodeName(1) {
			t.Errorf("%d servers: the coordinator was picked as the demotion target", count)
		}
	}
}

// TestPrunedMemberLeavesEveryCount checks the accounting the whole workflow rests on: an evicted
// machine is erased from the memberlist, so it stops counting in the population and in the server
// slots. A failed one, which may still come back, keeps its place.
func TestPrunedMemberLeavesEveryCount(t *testing.T) {
	failed := viewWithStatus(memberStatusFailed, member("ants-03", node.StateStableServer))
	alive := viewOf(member("ants-01", node.StateStableServer), member("ants-02", node.StateStableServer))

	degraded := clusterView{snapshot: admin.Snapshot{
		Members: append(append([]admin.Member{}, alive.snapshot.Members...), failed.snapshot.Members...),
	}}

	if got := degraded.population(); got != 3 {
		t.Errorf("population with a failed member = %d, want 3: it may still come back", got)
	}
	if got := degraded.k3sServerCount(); got != 3 {
		t.Errorf("server count with a failed server = %d, want 3: its slot is held", got)
	}

	// After the eviction the member is gone from the snapshot entirely, never "left".
	if got := alive.population(); got != 2 {
		t.Errorf("population after the eviction = %d, want 2", got)
	}
	if got := alive.k3sServerCount(); got != 2 {
		t.Errorf("server count after the eviction = %d, want 2", got)
	}
	if got := alive.desiredServerCount(); got != 1 {
		t.Errorf("desired server count for 2 machines = %d, want 1", got)
	}
	if !alive.hasTooManyK3sServers() {
		t.Error("two servers for two machines must call for a demotion")
	}
}

// TestDurablyFailedMembers checks the local failure clock.
func TestDurablyFailedMembers(t *testing.T) {
	now := time.Now()
	const grace = time.Hour

	view := clusterView{snapshot: admin.Snapshot{Members: []admin.Member{
		{Name: "ants-01", Status: memberStatusAlive, Tags: map[string]string{"state": string(node.StateStableServer)}},
		{Name: "ants-02", Status: memberStatusFailed, Tags: map[string]string{"state": string(node.StateStableServer)}},
		{Name: "ants-03", Status: memberStatusFailed, Tags: map[string]string{"state": string(node.StateStableAgent)}},
	}}}

	failedSince := map[string]time.Time{
		"ants-02": now.Add(-2 * grace), // long gone
		"ants-03": now.Add(-grace / 2), // still within its grace period
	}

	evictable, next := view.durablyFailedMembersNames(failedSince, grace, now)
	if len(evictable) != 1 || evictable[0] != "ants-02" {
		t.Errorf("evictable = %v, want [ants-02]", evictable)
	}
	if want := failedSince["ants-03"].Add(grace); !next.Equal(want) {
		t.Errorf("next deadline = %s, want %s (when ants-03 becomes evictable)", next, want)
	}

	// A member seen failing for the first time starts its grace period now, it is never
	// evicted on the spot.
	evictable, next = view.durablyFailedMembersNames(map[string]time.Time{}, grace, now)
	if len(evictable) != 0 {
		t.Errorf("evictable = %v, want none: both failures are being seen for the first time", evictable)
	}
	if want := now.Add(grace); !next.Equal(want) {
		t.Errorf("next deadline = %s, want %s", next, want)
	}
}

// TestIsVirginMember covers the predicate that decides whether the cluster may erase the K3s node
// carrying a given name. Answering yes for a machine that belongs to the cluster would delete a
// working member, so the two ways of not being virgin are checked separately: a state that is not
// a first boot, and a member Serf no longer sees alive.
func TestIsVirginMember(t *testing.T) {
	view := viewOf(
		member("ants-01", node.StateStableServer),
		member("ants-02", node.StateJoiningCleanup),
		member("ants-03", node.StateDiscovering),
		member("ants-04", node.StateRejoinCluster),
		member("ants-05", node.StateJoiningFailed),
	)

	cases := map[string]bool{
		"ants-02": true,  // the machine asking, from the state it asks in
		"ants-03": true,  // still discovering, nothing installed
		"ants-05": true,  // its first boot failed, so it never completed one
		"ants-01": false, // a working server
		"ants-04": false, // coming back to a cluster it belongs to
		"ants-99": false, // not a member at all
	}

	for name, want := range cases {
		if got := view.isVirginMember(name); got != want {
			t.Errorf("isVirginMember(%q) = %t, want %t", name, got, want)
		}
	}

	// A machine Serf no longer sees cannot vouch for anything: it may be a member that is simply
	// unreachable, still holding its K3s data.
	failed := viewWithStatus(memberStatusFailed, member("ants-06", node.StateDiscovering))
	if failed.isVirginMember("ants-06") {
		t.Error("isVirginMember accepted a member that is not alive")
	}
}

// nodeName builds the conventional name of the nth machine.
func nodeName(n int) string {
	return fmt.Sprintf("ants-%02d", n)
}
