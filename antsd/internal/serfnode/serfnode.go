package serfnode

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/admin"
	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/discovery"

	"github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/config"
	nodepkg "github.com/jeremie-arcidiacono/Zero-Config-HA-Cluster/antsd/internal/node"

	serflib "github.com/hashicorp/serf/serf"
)

// clusterName scopes the mDNS discovery so unrelated clusters on the
// same LAN do not merge. // TODO : expose it in config ?
const clusterName = "antsd-cluster"

// stateTagKey is the Serf tag carrying the node lifecycle state.
const stateTagKey = "state"

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

// Event is antsd's own representation of a Serf event.
//
// For member events, Name is the member's node name and Tags its tags.
// For user events, Name is the event name and Payload its (possibly empty)
// payload; NodeIP and Tags are unset (Serf user events carry no sender).
// TODO : better to split nameNode and nameEvent ? or even make 2 structs for member and user events?
type Event struct {
	Type    EventType
	NodeIP  string
	Name    string
	Tags    map[string]string
	Payload []byte
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

	conf.NodeName = node.config.NodeName
	conf.MemberlistConfig.BindAddr = node.config.SerfBindAddr
	conf.MemberlistConfig.BindPort = node.config.SerfBindPort
	conf.EventCh = node.rawEventCh

	conf.Tags = map[string]string{
		stateTagKey: string(nodepkg.StateStarting),
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
		ClusterName:   clusterName,
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
			node.dispatch(Event{
				Type:   mapMemberEventType(ev.EventType()),
				NodeIP: m.Addr.String(),
				Name:   m.Name,
				Tags:   m.Tags,
			})
		}
	case serflib.UserEvent:
		node.dispatch(Event{
			Type:    EventUser,
			Name:    ev.Name,
			Payload: ev.Payload,
		})
	default:
		// ignore other event types (queries, ...) for now
	}
}

// dispatch forwards a converted event to the consumer channel,
// dropping it instead of blocking if the consumer lags behind.
func (node *Node) dispatch(e Event) {
	select {
	case node.eventCh <- e:
	default:
		node.logger.Warn("dropping Serf event, channel full", "event", e)
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

// SetState broadcasts the node lifecycle state to the cluster by updating
// the Serf "state" tag.
func (node *Node) SetState(state nodepkg.State) error {
	node.mu.RLock()
	serf := node.serf
	node.mu.RUnlock()

	if serf == nil {
		return fmt.Errorf("serf not started")
	}

	// Preserve any other tags: SetTags replaces the whole tag set.
	current := serf.LocalMember().Tags
	tags := make(map[string]string, len(current)+1)
	for key, value := range current {
		tags[key] = value
	}
	tags[stateTagKey] = string(state)
	return serf.SetTags(tags)
}

// SendUserEvent broadcasts a Serf user event to the whole cluster,
// including this node itself.
func (node *Node) SendUserEvent(name string, payload []byte) error {
	node.mu.RLock()
	serf := node.serf
	node.mu.RUnlock()

	if serf == nil {
		return fmt.Errorf("serf not started")
	}
	// coalesce=false: bootstrap protocol events should never be merged.
	return serf.UserEvent(name, payload, false)
}

// LocalIP returns the IP address this node advertises to the cluster.
func (node *Node) LocalIP() string {
	node.mu.RLock()
	serf := node.serf
	node.mu.RUnlock()

	if serf == nil {
		return ""
	}
	return serf.LocalMember().Addr.String()
}

// Snapshot returns the current local Serf observation for monitoring and observability purposes.
// todo : change comment, as now used for more thing that just monitoring ?
func (node *Node) Snapshot() admin.Snapshot {
	node.mu.RLock()
	serf := node.serf
	nodeName := node.name
	node.mu.RUnlock()

	snapshot := admin.Snapshot{
		CollectedAt: time.Now(),
		NodeName:    nodeName,
	}

	if serf == nil {
		return snapshot
	}

	members := serf.Members()
	snapshot.Available = true
	snapshot.Members = make([]admin.Member, 0, len(members))

	for _, member := range members {
		tags := make(map[string]string, len(member.Tags))
		for key, value := range member.Tags {
			tags[key] = value
		}

		snapshot.Members = append(snapshot.Members, admin.Member{
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
