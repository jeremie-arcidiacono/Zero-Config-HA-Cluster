// Package logbridge adapts the old standard library loggers used by HashiCorp
// dependencies (Serf, memberlist, mDNS) to antsd slog logger.
//
// Those libraries only accept a *log.Logger and encode severity as a textual
// prefix in the message itself, e.g.:
//
//	[DEBUG] memberlist: Initiating push/pull sync with: ants01 10.10.x.x:7946
//
// The bridge parses that prefix, re-emits the line as a proper slog record at
// the matching level and tags it with the component it came from, e.g.
// "[MEMBERLIST] Initiating push/pull sync with: ants01". Library logs
// therefore land in the same stream and format as antsd's own logs.
package logbridge

import (
	"context"
	"io"
	"log"
	"log/slog"
	"strings"
)

// defaultLevel is used for lines without a recognized severity prefix.
const defaultLevel = slog.LevelInfo

// levels maps the severity prefixes used by the HashiCorp libraries to slog levels.
var levels = map[string]slog.Level{
	"TRACE": slog.LevelDebug,
	"DEBUG": slog.LevelDebug,
	"INFO":  slog.LevelInfo,
	"WARN":  slog.LevelWarn,
	"ERR":   slog.LevelError,
	"ERROR": slog.LevelError,
}

// writer turns each line written to it into a single slog record.
type writer struct {
	logger    *slog.Logger
	component string
	// quiet demotes info lines to debug level, see NewQuietStdLogger.
	quiet bool
}

// newWriter returns an io.Writer that forwards every line it receives to logger,
// using the severity prefix from the line and adding the component name.
func newWriter(logger *slog.Logger, component string, quiet bool) io.Writer {
	return &writer{logger: logger, component: component, quiet: quiet}
}

// NewStdLogger returns a *log.Logger (suitable for the HashiCorp libraries).
func NewStdLogger(logger *slog.Logger, component string) *log.Logger {
	// Flags are turned off since slog already adds timestamps
	return log.New(newWriter(logger, component, false), "", 0)
}

// NewQuietStdLogger is like NewStdLogger but reports info lines at
// debug level (warnings and errors keep their severity).
//
// It is meant for libraries that spam tons of useless logs at info level.
func NewQuietStdLogger(logger *slog.Logger, component string) *log.Logger {
	return log.New(newWriter(logger, component, true), "", 0)
}

// Write implements io.Writer.
func (w *writer) Write(p []byte) (int, error) {
	level, msg := parseLine(string(p))
	if w.quiet && level < slog.LevelWarn {
		level = slog.LevelDebug
	}
	w.logger.Log(context.Background(), level, w.tag(msg))
	return len(p), nil
}

// tag marks the message with its component: "[SERF] EventMemberJoin: ants01".
//
// The libraries name themselves at the start of most of their messages
// ("serf: EventMemberJoin: ..."). That prefix is dropped when it duplicates the
// component name, to avoid the redundant "[SERF] serf: ...".
func (w *writer) tag(msg string) string {
	if name, rest, found := strings.Cut(msg, ": "); found && strings.EqualFold(name, w.component) {
		msg = rest
	}
	return "[" + w.component + "] " + msg
}

// parseLine splits a "[LEVEL] message" line into its slog level and message.
// Lines without a known severity prefix are reported at defaultLevel, unchanged.
func parseLine(line string) (slog.Level, string) {
	line = strings.TrimSpace(line)

	if strings.HasPrefix(line, "[") {
		if end := strings.IndexByte(line, ']'); end > 0 {
			if level, ok := levels[line[1:end]]; ok {
				return level, strings.TrimSpace(line[end+1:])
			}
		}
	}
	return defaultLevel, line
}
