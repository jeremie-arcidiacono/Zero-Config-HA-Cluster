package node

import (
	"fmt"
	"slices"
)

// MaxServers is the target number of K3s servers in a cluster.
// Rescaling process isn't implemented yet.
const MaxServers = 3

// DesiredServerCount returns how many of the total nodes must run as K3s servers.
// With fewer than MaxServers nodes, every node is a server.
func DesiredServerCount(total int) int {
	return min(total, MaxServers)
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
