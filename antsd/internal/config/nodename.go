package config

// Derivation and validation of the node name.
//
// The name identifies the machine everywhere at once: Serf member, persisted state, and K3s node.

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
)

const (
	// nodeNamePrefix makes a generated name recognizable, the rest is the MAC address.
	nodeNamePrefix = "ants-"

	// nodeNameMaxLength is the RFC 1123 label limit, which a Kubernetes node name must respect.
	nodeNameMaxLength = 63

	// sysfsNetDir describes the network interfaces of the machine.
	sysfsNetDir = "/sys/class/net"
)

// nodeNamePattern is the RFC 1123 label form required by Kubernetes for a node name.
var nodeNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// defaultNodeName derives the name of this machine from the MAC address of its network card,
// keeping the last three bytes.
func defaultNodeName() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("list the network interfaces: %w", err)
	}

	// The lowest name wins, so the same machine always derives the same name.
	slices.SortFunc(interfaces, func(a, b net.Interface) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		default:
			return 0
		}
	})

	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 || len(iface.HardwareAddr) < 3 {
			continue
		}

		// Avoid virtual interfaces like the one created by K3s.
		if !isPhysicalInterface(iface.Name) {
			continue
		}

		mac := iface.HardwareAddr
		return fmt.Sprintf("%s%02x%02x%02x", nodeNamePrefix, mac[len(mac)-3], mac[len(mac)-2], mac[len(mac)-1]), nil
	}
	return "", fmt.Errorf("no physical network interface with a MAC address")
}

// isPhysicalInterface reports whether an interface is a real network card.
// Only those have a device entry in sysfs.
// On non-Linux systems, it always returns true (useful for development on Windows).
func isPhysicalInterface(name string) bool {
	if _, err := os.Stat(sysfsNetDir); err != nil {
		return true
	}
	_, err := os.Stat(filepath.Join(sysfsNetDir, name, "device"))
	return err == nil
}

// validateNodeName checks that a name is usable as a Kubernetes node name.
//
// An invalid name is refused. Otherwise, K3s would silently alter the name it receives.
// A desync between Serf and K3s would break the rescaling workflow, which relies on kubectl call.
func validateNodeName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("node-name must not be empty")
	case len(name) > nodeNameMaxLength:
		return fmt.Errorf("node-name must be at most %d characters, got %d", nodeNameMaxLength, len(name))
	case !nodeNamePattern.MatchString(name):
		return fmt.Errorf("node-name %q is not a valid kubernetes node name", name)
	}
	return nil
}
