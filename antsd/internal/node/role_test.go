package node

import "testing"

// TestDesiredServerCount pins the odd target: the largest odd count that fits, capped at MaxServers.
// The two lines that carry the design are total=2 (one server, not two: a two-member etcd
// tolerates no failure and doubles the surface) and total=8 (7, etcd's recommended ceiling).
func TestDesiredServerCount(t *testing.T) {
	tests := []struct {
		total int
		want  int
	}{
		{total: 0, want: 0},
		{total: 1, want: 1},
		{total: 2, want: 1},
		{total: 3, want: 3},
		{total: 4, want: 3},
		{total: 5, want: 5},
		{total: 6, want: 5},
		{total: 7, want: 7},
		{total: 8, want: 7},
		{total: 100, want: 7},
	}

	for _, tt := range tests {
		if got := DesiredServerCount(tt.total); got != tt.want {
			t.Errorf("DesiredServerCount(%d) = %d, want %d", tt.total, got, tt.want)
		}
	}
}

func TestRank(t *testing.T) {
	// Deliberately unsorted input: Rank must not rely on caller-side ordering.
	names := []string{"ants-03", "ants-01", "ants-06", "ants-02", "ants-05", "ants-04"}

	tests := []struct {
		self string
		want int
	}{
		{self: "ants-01", want: 0},
		{self: "ants-02", want: 1},
		{self: "ants-04", want: 3},
		{self: "ants-06", want: 5},
	}

	for _, tt := range tests {
		got, err := Rank(names, tt.self)
		if err != nil {
			t.Fatalf("Rank(%q) returned error: %v", tt.self, err)
		}
		if got != tt.want {
			t.Errorf("Rank(%q) = %d, want %d", tt.self, got, tt.want)
		}
	}
}

func TestRankUnknownNode(t *testing.T) {
	if _, err := Rank([]string{"ants-01", "ants-02"}, "ghost"); err == nil {
		t.Error("Rank with unknown self should return an error")
	}
}

func TestRankDoesNotMutateInput(t *testing.T) {
	names := []string{"b", "a", "c"}
	if _, err := Rank(names, "a"); err != nil {
		t.Fatalf("Rank returned error: %v", err)
	}
	if names[0] != "b" || names[1] != "a" || names[2] != "c" {
		t.Errorf("Rank mutated its input slice: %v", names)
	}
}

func TestRoleForRank(t *testing.T) {
	tests := []struct {
		name  string
		rank  int
		total int
		want  Role
	}{
		{name: "single node is server", rank: 0, total: 1, want: RoleServer},
		{name: "second of two is agent", rank: 1, total: 2, want: RoleAgent},
		{name: "third of three is server", rank: 2, total: 3, want: RoleServer},
		{name: "fourth of four is agent", rank: 3, total: 4, want: RoleAgent},
		{name: "fifth of six is server", rank: 4, total: 6, want: RoleServer},
		{name: "last of six is agent", rank: 5, total: 6, want: RoleAgent},
		{name: "last of eight is agent", rank: 7, total: 8, want: RoleAgent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RoleForRank(tt.rank, tt.total); got != tt.want {
				t.Errorf("RoleForRank(%d, %d) = %q, want %q", tt.rank, tt.total, got, tt.want)
			}
		})
	}
}

func TestRoleOther(t *testing.T) {
	if got := RoleServer.Other(); got != RoleAgent {
		t.Errorf("RoleServer.Other() = %q, want %q", got, RoleAgent)
	}
	if got := RoleAgent.Other(); got != RoleServer {
		t.Errorf("RoleAgent.Other() = %q, want %q", got, RoleServer)
	}
}

func TestRoleStableState(t *testing.T) {
	if got := RoleServer.StableState(); got != StateStableServer {
		t.Errorf("RoleServer.StableState() = %q, want %q", got, StateStableServer)
	}
	if got := RoleAgent.StableState(); got != StateStableAgent {
		t.Errorf("RoleAgent.StableState() = %q, want %q", got, StateStableAgent)
	}
}
