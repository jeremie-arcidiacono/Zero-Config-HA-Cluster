package node

import (
	"fmt"
	"slices"
)

// MaxServers is the highest number of K3s servers in a cluster.
// Seven is etcd's recommended ceiling.
const MaxServers = 7

// DesiredServerCount returns how many of the total nodes must run as K3s servers:
// the largest odd count, capped at MaxServers.
func DesiredServerCount(total int) int {
	if total <= 0 {
		return 0
	}

	count := min(total, MaxServers)
	if count%2 == 0 {
		count--
	}
	return count
}

// Role is the K3s role a node runs.
// Assigned during the first boot, and changed afterward by the rescaling workflow.
type Role string

const (
	RoleServer Role = "server"
	RoleAgent  Role = "agent"
)

// StableState returns the stable lifecycle state corresponding to the role.
func (r Role) StableState() State {
	if r == RoleServer {
		return StateStableServer
	}
	return StateStableAgent
}

// Other returns the opposite role (agent <=> server)
func (r Role) Other() Role {
	if r == RoleServer {
		return RoleAgent
	}
	return RoleServer
}

// Rank returns the position of self among names, ordered lexicographically.
// Every node computes the same ranking from the same member list, which is what makes the role election leaderless.
// rank 0 is N0, the node that initializes the cluster.
func Rank(names []string, self string) (int, error) {
	sorted := slices.Clone(names)
	slices.Sort(sorted)

	rank := slices.Index(sorted, self)
	if rank < 0 {
		return 0, fmt.Errorf("node %q not found in member list %v", self, sorted)
	}
	return rank, nil
}

// RoleForRank returns the K3s role of the node with the given rank in a cluster of total nodes.
func RoleForRank(rank, total int) Role {
	if rank < DesiredServerCount(total) {
		return RoleServer
	}
	return RoleAgent
}
