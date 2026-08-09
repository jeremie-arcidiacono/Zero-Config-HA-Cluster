// Package discovery internal/discovery/mdns.go
package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/mdns"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/logbridge"
)

// PeerFoundFunc is called whenever a new peer is discovered on the network.
// addr is in "host:port" format.
type PeerFoundFunc func(addr string)

// Config controls the mDNS discovery behavior.
type Config struct {
	ClusterName   string // used as the mDNS service domain, isolates unrelated clusters
	NodeName      string
	BindIP        net.IP        // the Serf address to advertise (usually the local IP)
	BindPort      int           // the Serf port to advertise
	QueryInterval time.Duration // how often to poll the network for peers
}

// Discoverer advertises this node via mDNS and periodically looks up peers.
type Discoverer struct {
	config Config
	logger *slog.Logger

	server *mdns.Server
	onFind PeerFoundFunc

	seen map[string]struct{}
}

// New creates a Discoverer. It does not start advertising or looking up yet.
func New(config Config, logger *slog.Logger, onFind PeerFoundFunc) *Discoverer {
	return &Discoverer{
		config: config,
		logger: logger,
		onFind: onFind,
		seen:   make(map[string]struct{}),
	}
}

// Start begins advertising this node and polling for peers until ctx is canceled.
func (d *Discoverer) Start(ctx context.Context) error {
	d.logger.Debug(
		"starting mDNS discovery",
		"node_name", d.config.NodeName,
		"bind_ip", d.config.BindIP,
		"bind_port", d.config.BindPort,
		"cluster_name", d.config.ClusterName,
	)

	service, err := mdns.NewMDNSService(
		d.config.NodeName,
		serviceName(d.config.ClusterName),
		"",
		"",
		d.config.BindPort,
		[]net.IP{d.config.BindIP},
		[]string{"antsd"},
	)
	if err != nil {
		return fmt.Errorf("failed to create mDNS service: %w", err)
	}

	server, err := mdns.NewServer(&mdns.Config{
		Zone:   service,
		Logger: logbridge.NewQuietStdLogger(d.logger, "MDNS"),
	})
	if err != nil {
		return fmt.Errorf("failed to start mDNS server: %w", err)
	}
	d.server = server

	go d.pollLoop(ctx)

	return nil
}

// pollLoop performs periodic mDNS lookups, based on the configured QueryInterval.
func (d *Discoverer) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(d.config.QueryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("mdns discovery stopping")
			if d.server != nil {
				_ = d.server.Shutdown()
			}
			return
		case <-ticker.C:
			// Lookups must stay sequential: handleEntry owns d.seen without a lock.
			d.lookupOnce()
		}
	}
}

// entriesBufferSize bounds how many peers a single lookup can report.
const entriesBufferSize = 64

// lookupOnce performs a single mDNS lookup and calls handleEntry for each discovered peer.
//
// Entries are drained only once mdns.Query has returned, never while it runs: avoid a data race.
func (d *Discoverer) lookupOnce() {
	entriesCh := make(chan *mdns.ServiceEntry, entriesBufferSize)

	params := mdns.DefaultParams(serviceName(d.config.ClusterName))
	params.Entries = entriesCh
	params.Logger = logbridge.NewQuietStdLogger(d.logger, "MDNS")

	if err := mdns.Query(params); err != nil {
		d.logger.Warn("mdns query failed", "error", err)
	}
	close(entriesCh)

	for entry := range entriesCh {
		d.handleEntry(entry)
	}
}

// handleEntry is called for each discovered mDNS entry. It checks if the peer is already known, and if not, it calls the onFind callback.
func (d *Discoverer) handleEntry(entry *mdns.ServiceEntry) {
	// The mDNS client parses every record reaching the multicast group, not only the answers to
	// our own query, so unrelated services show up here.
	if !strings.HasSuffix(entry.Name, instanceSuffix(d.config.ClusterName)) {
		//d.logger.Debug("ignoring mdns entry from another service", "name", entry.Name)
		return
	}

	if len(entry.AddrV4) == 0 {
		//d.logger.Debug("ignoring mdns entry without an IPv4 address", "name", entry.Name)
		return
	}

	addr := net.JoinHostPort(entry.AddrV4.String(), strconv.Itoa(entry.Port))

	if _, ok := d.seen[addr]; ok {
		return // already known, avoid redundant Join calls
	}
	d.seen[addr] = struct{}{}

	d.logger.Info("discovered peer via mdns", "addr", addr)
	d.onFind(addr)
}

// serviceName returns the service name to register and to lookup.
func serviceName(clusterName string) string {
	return fmt.Sprintf("_antsd-%s._tcp", clusterName)
}

// mdnsDomain is the domain the client queries in (mdns.DefaultParams default).
const mdnsDomain = "local"

// instanceSuffix returns the FQDN suffix shared by every instance name of our service,
// e.g. "._antsd-<cluster>._tcp.local." for "<node>._antsd-<cluster>._tcp.local.".
func instanceSuffix(clusterName string) string {
	return fmt.Sprintf(".%s.%s.", serviceName(clusterName), mdnsDomain)
}
