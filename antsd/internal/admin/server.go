// Package admin provides an HTTP server for system monitoring, observability, and control.
//
// It exposes three endpoints:
//   - GET / renders an HTML dashboard
//   - GET /health returns a simple health check
//   - GET /status returns system and process information as JSON
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"time"
)

const shutdownTimeout = 5 * time.Second

// stateTag is the special tag that carries a node's lifecycle state
const stateTag = "state"

// Member represents a Serf member at a specific point in time.
type Member struct {
	Name    string            `json:"name"`
	Address string            `json:"address"`
	Status  string            `json:"status"`
	Tags    map[string]string `json:"tags"`
}

// Tag is a single key/value tag entry, used for rendering.
type Tag struct {
	Key   string
	Value string
}

// OtherTags returns the member's tags excluding the special "state" tag,
// sorted by key for stable rendering.
func (m Member) OtherTags() []Tag {
	tags := make([]Tag, 0, len(m.Tags))
	for key, value := range m.Tags {
		if key == stateTag {
			continue
		}
		tags = append(tags, Tag{Key: key, Value: value})
	}

	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Key < tags[j].Key
	})

	return tags
}

// Snapshot captures the local observation of the cluster at a specific instant.
type Snapshot struct {
	CollectedAt time.Time `json:"collected_at"`
	NodeName    string    `json:"node_name"`
	Available   bool      `json:"available"`
	Members     []Member  `json:"members"`
}

// Status is the payload served by the /status endpoint and rendered on the dashboard.
// It contains the cluster Snapshot and local process information.
type Status struct {
	Snapshot
	StartedAt     time.Time `json:"started_at"`
	UptimeSeconds int64     `json:"uptime_seconds"`
}

// Uptime returns the antsd process uptime as of the snapshot's collection time.
func (s Status) Uptime() time.Duration {
	return s.CollectedAt.Sub(s.StartedAt).Round(time.Second)
}

// Source provides a read-only view of the local cluster state.
type Source interface {
	// Snapshot returns the current cluster state as observed by this node.
	Snapshot() Snapshot
}

// Server exposes the administration HTTP interface.
type Server struct {
	logger    *slog.Logger
	source    Source
	server    *http.Server
	template  *template.Template
	startedAt time.Time
}

// NewServer creates a new server that listens on the specified port.
// The source parameter provides cluster snapshot data for the monitoring endpoints.
// Returns an error if template parsing fails.
func NewServer(port int, source Source, logger *slog.Logger, startedAt time.Time) (*Server, error) {
	page, err := parseTemplates()
	if err != nil {
		return nil, err
	}

	server := &Server{
		logger:    logger,
		source:    source,
		template:  page,
		startedAt: startedAt,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", server.handleDashboard)
	mux.HandleFunc("GET /health", server.handleHealth)
	mux.HandleFunc("GET /status", server.handleStatus)

	server.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return server, nil
}

// Start launches the HTTP server and manages its lifecycle with the provided context.
// It runs concurrently and gracefully shuts down when ctx is canceled (with a 5-second timeout).
func (s *Server) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.server.Addr, err)
	}

	s.logger.Info("admin server started", "address", listener.Addr())

	go func() {
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("admin server stopped unexpectedly", "error", err)
		}
	}()

	// Graceful shutdown on context cancellation
	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := s.server.Shutdown(shutdownCtx); err != nil {
			s.logger.Warn("admin server shutdown failed", "error", err)
		}
	}()

	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	// todo : we could also include metadata : version, commit hash, build date, etc.
	s.writeJSON(w, http.StatusOK, s.status())
}

func (s *Server) handleDashboard(w http.ResponseWriter, _ *http.Request) {
	if err := s.template.ExecuteTemplate(w, "dashboard", s.status()); err != nil {
		s.logger.Error("render dashboard", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// status builds the current Status of the system.
func (s *Server) status() Status {
	snapshot := s.source.Snapshot()

	return Status{
		Snapshot:      snapshot,
		StartedAt:     s.startedAt,
		UptimeSeconds: int64(snapshot.CollectedAt.Sub(s.startedAt).Seconds()),
	}
}

// writeJSON writes the given value as a JSON response with the specified HTTP status code.
func (s *Server) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(value); err != nil {
		s.logger.Warn("failed to encode JSON response", "error", err, "status", status)
	}
}
