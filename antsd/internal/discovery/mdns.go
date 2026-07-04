// Package discovery internal/discovery/mdns.go
package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/hashicorp/mdns"
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

// todo : mute "[INFO] mdns: Closing client {.....}" log messages from mdns"

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

	server, err := mdns.NewServer(&mdns.Config{Zone: service})
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
			d.lookupOnce() //todo : use a goroutine ?
		}
	}
}

// lookupOnce performs a single mDNS lookup and calls handleEntry for each discovered peer.
func (d *Discoverer) lookupOnce() {
	entriesCh := make(chan *mdns.ServiceEntry, 8)
	doneCh := make(chan struct{})

	go func() {
		defer close(doneCh)
		for entry := range entriesCh {
			d.handleEntry(entry)
		}
	}()

	params := mdns.DefaultParams(serviceName(d.config.ClusterName))
	params.Entries = entriesCh
	//params.Timeout = 2 * time.Second

	if err := mdns.Query(params); err != nil {
		d.logger.Warn("mdns query failed", "error", err)
	}
	close(entriesCh)
	<-doneCh
}

// handleEntry is called for each discovered mDNS entry. It checks if the peer is already known, and if not, it calls the onFind callback.
func (d *Discoverer) handleEntry(entry *mdns.ServiceEntry) {
	addr := net.JoinHostPort(entry.AddrV4.String(), fmt.Sprintf("%d", entry.Port))

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
