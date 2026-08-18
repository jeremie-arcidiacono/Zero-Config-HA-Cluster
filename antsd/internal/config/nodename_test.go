package config

import (
	"strings"
	"testing"
)

// TestValidateNodeNameAccepts covers the names antsd generates itself and the ones an operator may
// reasonably pass to -node-name.
func TestValidateNodeNameAccepts(t *testing.T) {
	names := []string{
		"ants-a2af15", // what defaultNodeName produces
		"ants-01",
		"node1",
		"n",
		strings.Repeat("a", nodeNameMaxLength),
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if err := validateNodeName(name); err != nil {
				t.Errorf("validateNodeName(%q) = %v, want no error", name, err)
			}
		})
	}
}

// TestValidateNodeNameRejects pins the names Kubernetes would refuse or silently rewrite. An
// uppercase letter is the dangerous one: K3s lowercases the name, so the node would answer to a
// name the Serf membership never knows, and every kubectl call made with a name read from Serf
// would miss it.
func TestValidateNodeNameRejects(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"uppercase":        "Ants-01",
		"underscore":       "ants_01",
		"dot":              "ants.01",
		"space":            "ants 01",
		"leading dash":     "-ants01",
		"trailing dash":    "ants01-",
		"too long":         strings.Repeat("a", nodeNameMaxLength+1),
		"non-ascii letter": "ants-é",
	}

	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateNodeName(value); err == nil {
				t.Errorf("validateNodeName(%q) accepted a %s", value, name)
			}
		})
	}
}

// TestDefaultNodeNameIsUsable checks the derivation against the machine running the tests: whatever
// network card it picks, the name it builds must be one Kubernetes accepts, since it is handed to
// K3s as K3S_NODE_NAME.
func TestDefaultNodeNameIsUsable(t *testing.T) {
	name, err := defaultNodeName()
	if err != nil {
		t.Skipf("no usable network interface on this machine: %v", err)
	}

	if err := validateNodeName(name); err != nil {
		t.Errorf("defaultNodeName() = %q, which is not a valid node name: %v", name, err)
	}
	if !strings.HasPrefix(name, nodeNamePrefix) {
		t.Errorf("defaultNodeName() = %q, want the %q prefix", name, nodeNamePrefix)
	}
}

// TestDefaultNodeNameIsStable guards the property the whole lifecycle depends on: the same machine
// must derive the same name at every boot, including once K3s added its own interfaces.
func TestDefaultNodeNameIsStable(t *testing.T) {
	first, err := defaultNodeName()
	if err != nil {
		t.Skipf("no usable network interface on this machine: %v", err)
	}

	for range 5 {
		again, err := defaultNodeName()
		if err != nil {
			t.Fatalf("defaultNodeName() failed on a later call: %v", err)
		}
		if again != first {
			t.Fatalf("defaultNodeName() returned %q then %q", first, again)
		}
	}
}
