package node

import "testing"

// TestStateInCluster lists every lifecycle state, because a state missing from
// InCluster is what lets a user create a second cluster next to an existing one.
func TestStateInCluster(t *testing.T) {
	tests := []struct {
		state State
		want  bool
	}{
		{state: StateStableServer, want: true},
		{state: StateStableAgent, want: true},
		{state: StateRejoinCluster, want: true},
		{state: StateRejoinFailed, want: true},

		// A node changing its role belongs to the cluster throughout: it holds
		// its K3s data even while its installation is being replaced.
		{state: StateRescaleCoordinating, want: true},
		{state: StateRescalePromoting, want: true},
		{state: StateRescaleDemoting, want: true},
		{state: StateRescaleFailed, want: true},

		{state: StateStarting, want: false},
		{state: StateDiscovering, want: false},
		{state: StateBootstrapConfirm, want: false},
		{state: StateBootstrapWaiting, want: false},
		{state: StateBootstrapInstallInit, want: false},
		{state: StateBootstrapInstallServer, want: false},
		{state: StateBootstrapInstallAgent, want: false},
		{state: StateBootstrapFailed, want: false},

		// The first boot is not over until the node reaches its stable state,
		// so a machine still installing K3s is not yet a member.
		{state: StateJoiningWaiting, want: false},
		{state: StateJoiningCleanup, want: false},
		{state: StateJoiningAgent, want: false},
		{state: StateJoiningFailed, want: false},

		// A node whose state tag is unknown (older or newer antsd) is not
		// assumed to be a cluster member.
		{state: State("something-else"), want: false},
	}

	for _, tt := range tests {
		if got := tt.state.InCluster(); got != tt.want {
			t.Errorf("State(%q).InCluster() = %t, want %t", tt.state, got, tt.want)
		}
	}
}

// TestStateIsFirstBoot lists every lifecycle state too, because this predicate is what lets the
// cluster erase a K3s node under someone's name: a state wrongly reported as a first boot exposes
// a real member to the forget-me protocol, and one wrongly left out hangs the machine that asks,
// since it waits for a confirmation nobody may send.
func TestStateIsFirstBoot(t *testing.T) {
	tests := []struct {
		state State
		want  bool
	}{
		{state: StateDiscovering, want: true},
		{state: StateBootstrapConfirm, want: true},
		{state: StateBootstrapWaiting, want: true},
		{state: StateBootstrapInstallInit, want: true},
		{state: StateBootstrapInstallServer, want: true},
		{state: StateBootstrapInstallAgent, want: true},
		{state: StateBootstrapFailed, want: true},
		{state: StateJoiningWaiting, want: true},

		// The machine asks from this very state, so leaving it out would make every request
		// refused and every joiner wait forever.
		{state: StateJoiningCleanup, want: true},

		{state: StateJoiningAgent, want: true},
		{state: StateJoiningFailed, want: true},

		// Starting is not a first boot: the node has not read its state file yet, so nothing
		// says it never completed one.
		{state: StateStarting, want: false},

		{state: StateStableServer, want: false},
		{state: StateStableAgent, want: false},
		{state: StateRejoinCluster, want: false},
		{state: StateRejoinFailed, want: false},
		{state: StateRescaleCoordinating, want: false},
		{state: StateRescalePromoting, want: false},
		{state: StateRescaleDemoting, want: false},
		{state: StateRescaleFailed, want: false},

		{state: State("something-else"), want: false},
	}

	for _, tt := range tests {
		if got := tt.state.IsFirstBoot(); got != tt.want {
			t.Errorf("State(%q).IsFirstBoot() = %t, want %t", tt.state, got, tt.want)
		}
	}
}
