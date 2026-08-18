package discovery

import (
	"io"
	"log/slog"
	"net"
	"testing"

	"github.com/hashicorp/mdns"
)

const testClusterName = "antsd-cluster"

// newTestDiscoverer returns a Discoverer recording the addresses it reports, without
// touching the network: handleEntry is driven directly with hand-built entries.
func newTestDiscoverer() (*Discoverer, *[]string) {
	var found []string
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	d := New(Config{ClusterName: testClusterName}, logger, func(addr string) {
		found = append(found, addr)
	})

	return d, &found
}

// TestHandleEntryKeepsOnlyUsableClusterPeers covers what the mDNS client hands us. It parses
// every record reaching the multicast group, not only the answers to our query, so the entries
// below (other than the first) were all observed on the bench.
func TestHandleEntryKeepsOnlyUsableClusterPeers(t *testing.T) {
	tests := []struct {
		name  string
		entry *mdns.ServiceEntry
		want  string // empty means the entry must be ignored
	}{
		{
			name: "a peer of our own service",
			entry: &mdns.ServiceEntry{
				Name:   "ants02._antsd-antsd-cluster._tcp.local.",
				AddrV4: net.IPv4(10, 10, 9, 28),
				Port:   7946,
			},
			want: "10.10.9.28:7946",
		},
		{
			name: "another service on the LAN (avahi advertises _workstation._tcp on port 9)",
			entry: &mdns.ServiceEntry{
				Name:   "ants02._workstation._tcp.local.",
				AddrV4: net.IPv4(10, 10, 9, 28),
				Port:   9,
			},
		},
		{
			name: "another service, advertised on the k3s pod network",
			entry: &mdns.ServiceEntry{
				Name:   "ants02._workstation._tcp.local.",
				AddrV4: net.IPv4(10, 42, 1, 0),
				Port:   9,
			},
		},
		{
			// The client publishes an entry as soon as it holds any address, so an IPv6-only
			// one reaches us with AddrV4 unset. Serf is joined over IPv4.
			name: "an entry carrying no IPv4 address",
			entry: &mdns.ServiceEntry{
				Name:   "ants02._workstation._tcp.local.",
				AddrV6: net.ParseIP("fe80::ba27:ebff:fe4a:1"),
				Port:   9,
			},
		},
		{
			name: "an unrelated antsd cluster sharing the LAN",
			entry: &mdns.ServiceEntry{
				Name:   "other01._antsd-other-cluster._tcp.local.",
				AddrV4: net.IPv4(10, 10, 9, 99),
				Port:   7946,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, found := newTestDiscoverer()

			d.handleEntry(tt.entry)

			if tt.want == "" {
				if len(*found) != 0 {
					t.Fatalf("entry should have been ignored, got %v", *found)
				}
				return
			}
			if len(*found) != 1 || (*found)[0] != tt.want {
				t.Fatalf("got %v, want [%s]", *found, tt.want)
			}
		})
	}
}

// TestHandleEntryReportsAPeerOnce guards the deduplication: the same peer answers every
// lookup, and Join must not be re-issued for it on each round.
func TestHandleEntryReportsAPeerOnce(t *testing.T) {
	d, found := newTestDiscoverer()

	entry := &mdns.ServiceEntry{
		Name:   "ants02._antsd-antsd-cluster._tcp.local.",
		AddrV4: net.IPv4(10, 10, 9, 28),
		Port:   7946,
	}

	d.handleEntry(entry)
	d.handleEntry(entry)

	if len(*found) != 1 {
		t.Fatalf("peer reported %d times, want 1: %v", len(*found), *found)
	}
}

// TestIgnoredEntryIsNotMemoized pins the order of the two checks against the seen map: an
// entry rejected once must be re-examined later, since it is the mDNS records that decide,
// not an address we already turned down.
func TestIgnoredEntryIsNotMemoized(t *testing.T) {
	d, found := newTestDiscoverer()

	name := "ants02._antsd-antsd-cluster._tcp.local."
	d.handleEntry(&mdns.ServiceEntry{Name: name, Port: 7946}) // no address yet
	d.handleEntry(&mdns.ServiceEntry{Name: name, AddrV4: net.IPv4(10, 10, 9, 28), Port: 7946})

	if len(*found) != 1 || (*found)[0] != "10.10.9.28:7946" {
		t.Fatalf("got %v, want [10.10.9.28:7946]", *found)
	}
}

// TestInstanceSuffixMatchesServiceName ties the filter to the name we advertise: the mDNS
// server builds instance names as "<instance>.<service>.<domain>.".
func TestInstanceSuffixMatchesServiceName(t *testing.T) {
	const want = "._antsd-antsd-cluster._tcp.local."

	if got := instanceSuffix(testClusterName); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
