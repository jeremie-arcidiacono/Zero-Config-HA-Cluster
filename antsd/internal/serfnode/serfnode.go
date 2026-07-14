package serfnode

import (
	"antsd/internal/discovery"
	"antsd/internal/monitor"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"antsd/internal/config"

	serflib "github.com/hashicorp/serf/serf"
)

type EventType int

const (
	EventMemberJoin EventType = iota
	EventMemberLeave
	EventMemberFailed
	EventMemberUpdate
	EventMemberReap
	EventUser
	EventQuery
)

func (t EventType) String() string {
	switch t {
	case EventMemberJoin:
		return "member-join"
	case EventMemberLeave:
		return "member-leave"
	case EventMemberFailed:
		return "member-failed"
	case EventMemberUpdate:
		return "member-update"
	case EventMemberReap:
		return "member-reap"
	case EventUser:
		return "user"
	case EventQuery:
		return "query"
	default:
		panic(fmt.Sprintf("unknown event type: %d", t))
	}
}

type Event struct {
	Type   EventType
	NodeIP string
	Name   string
	Tags   map[string]string
	// NodePort ??
	// Status ??
}

type Node struct {
	config *config.Config
	logger *slog.Logger

	mu         sync.RWMutex
	name       string
	serf       *serflib.Serf
	rawEventCh chan serflib.Event
	eventCh    chan Event
}

// New creates a Serf node but does not start it yet.
func New(config *config.Config, logger *slog.Logger) *Node {
	return &Node{
		config:     config,
		logger:     logger,
		rawEventCh: make(chan serflib.Event, 64),
		eventCh:    make(chan Event, 64),
	}
}

// Start initializes and starts the embedded Serf agent.
// It spawns the internal event loop and returns the event channel to the caller.
func (node *Node) Start(ctx context.Context) (<-chan Event, error) {
	conf := serflib.DefaultConfig()

	conf.NodeName, _ = os.Hostname() // todo use better naming (derived from MAC address ?)
	conf.MemberlistConfig.BindAddr = node.config.SerfBindAddr
	conf.MemberlistConfig.BindPort = node.config.SerfBindPort
	conf.EventCh = node.rawEventCh

	conf.Tags = map[string]string{
		"state": "starting",
	}

	serf, err := serflib.Create(conf)
	if err != nil {
		return nil, err
	}

	node.mu.Lock()
	node.serf = serf
	node.name = conf.NodeName
	node.mu.Unlock()

	// Start the mDNS discovery process
	discoverer := discovery.New(discovery.Config{
		ClusterName:   "antsd-cluster", // TODO: expose this in config ?
		NodeName:      serf.Memberlist().LocalNode().Name,
		BindIP:        serf.Memberlist().LocalNode().Addr,
		BindPort:      node.config.SerfBindPort,
		QueryInterval: 5 * time.Second,
	}, node.logger, func(addr string) {
		if err := node.Join([]string{addr}); err != nil {
			node.logger.Error("failed to join discovered peer", "addr", addr, "error", err)
		}
	})

	if err := discoverer.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start mdns discovery: %w", err)
	}

	go node.loop(ctx)

	return node.eventCh, nil
}

// loop processes events from the raw channel and dispatches them until the provided context is canceled.
func (node *Node) loop(ctx context.Context) {
	node.logger.Info("serf internal event loop started")

	for {
		select {
		case <-ctx.Done():
			//node.logger.Info("serf event loop stopping due to context cancel")
			return
		case e := <-node.rawEventCh:
			node.handleRawEvent(e)
		}
	}
}

func (node *Node) handleRawEvent(e serflib.Event) {
	switch ev := e.(type) {
	case serflib.MemberEvent:
		for _, m := range ev.Members {
			converted := Event{
				Type:   mapMemberEventType(ev.EventType()),
				NodeIP: m.Addr.String(),
				Name:   m.Name,
				Tags:   m.Tags,
			}
			select {
			case node.eventCh <- converted:
			default:
				node.logger.Warn("dropping Serf event, channel full", "event", converted)
			}
		}
	default:
		// ignore other event types for now
	}
}

// mapMemberEventType maps a Serf-lib event type to our own internal event type.
func mapMemberEventType(t serflib.EventType) EventType {
	switch t {
	case serflib.EventMemberJoin:
		return EventMemberJoin
	case serflib.EventMemberLeave:
		return EventMemberLeave
	case serflib.EventMemberFailed:
		return EventMemberFailed
	case serflib.EventMemberUpdate:
		return EventMemberUpdate
	case serflib.EventMemberReap:
		return EventMemberReap
	case serflib.EventQuery:
		return EventQuery
	default:
		return EventUser
	}
}

// Join attempts to join the cluster via the given peer addresses.
func (node *Node) Join(addrs []string) error {
	node.mu.RLock()
	serf := node.serf
	node.mu.RUnlock()

	if serf == nil {
		return nil
	}
	n, err := serf.Join(addrs, true)
	if err != nil {
		node.logger.Warn("serf join partially failed", "joined", n, "error", err)
	}
	return err
}

// Leave gracefully leaves the cluster.
func (node *Node) Leave() error {
	node.mu.RLock()
	serf := node.serf
	node.mu.RUnlock()

	if serf == nil {
		return nil
	}
	return serf.Leave()
}

// UpdateTags updates the Serf node tags
func (node *Node) UpdateTags(tags map[string]string) error {
	node.mu.RLock()
	serf := node.serf
	node.mu.RUnlock()

	if serf == nil {
		return nil
	}

	// Always set the special "state" tag
	// TODO : remove the hard-coded value and use the not-implemented yet state machine instead
	tags["state"] = "starting"

	return serf.SetTags(tags)
}

// Snapshot returns the current local Serf observation for monitoring and observability purposes.
func (node *Node) Snapshot() monitor.Snapshot {
	node.mu.RLock()
	serf := node.serf
	nodeName := node.name
	node.mu.RUnlock()

	snapshot := monitor.Snapshot{
		CollectedAt: time.Now(),
		NodeName:    nodeName,
	}

	if serf == nil {
		return snapshot
	}

	members := serf.Members()
	snapshot.Available = true
	snapshot.Members = make([]monitor.Member, 0, len(members))

	for _, member := range members {
		tags := make(map[string]string, len(member.Tags))
		for key, value := range member.Tags {
			tags[key] = value
		}

		snapshot.Members = append(snapshot.Members, monitor.Member{
			Name:    member.Name,
			Address: net.JoinHostPort(member.Addr.String(), strconv.Itoa(int(member.Port))),
			Status:  member.Status.String(),
			Tags:    tags,
		})
	}

	sort.Slice(snapshot.Members, func(i, j int) bool {
		return snapshot.Members[i].Name < snapshot.Members[j].Name
	})

	return snapshot
}
