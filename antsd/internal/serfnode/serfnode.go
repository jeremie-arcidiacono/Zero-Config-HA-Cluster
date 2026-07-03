package serfnode

import (
	"context"
	"log/slog"
	"os"

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

type Event struct {
	Type   EventType
	NodeIP string
	// NodePort ??
	Name   string
	Tags   map[string]string
	// Status ??
}

type Node struct {
	config *config.Config
	logger *slog.Logger

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
	node.serf = serf

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
	if node.serf == nil {
		return nil
	}
	n, err := node.serf.Join(addrs, true)
	if err != nil {
		node.logger.Warn("serf join partially failed", "joined", n, "error", err)
	}
	return err
}

// Leave gracefully leaves the cluster.
func (node *Node) Leave() error {
	if node.serf == nil {
		return nil
	}
	return node.serf.Leave()
}
