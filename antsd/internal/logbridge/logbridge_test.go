package logbridge

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLine(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantLevel slog.Level
		wantMsg   string
	}{
		{
			name:      "memberlist debug",
			line:      "[DEBUG] memberlist: Initiating push/pull sync with: ants01 10.10.9.24:7946\n",
			wantLevel: slog.LevelDebug,
			wantMsg:   "memberlist: Initiating push/pull sync with: ants01 10.10.9.24:7946",
		},
		{
			name:      "serf info",
			line:      "[INFO] serf: EventMemberJoin: ants02 10.10.9.25\n",
			wantLevel: slog.LevelInfo,
			wantMsg:   "serf: EventMemberJoin: ants02 10.10.9.25",
		},
		{
			name:      "serf warn",
			line:      "[WARN] serf: Shutdown without a Leave\n",
			wantLevel: slog.LevelWarn,
			wantMsg:   "serf: Shutdown without a Leave",
		},
		{
			// the HashiCorp libraries abbreviate the error level as ERR
			name:      "mdns err",
			line:      "[ERR] mdns: Failed to read packet: timeout\n",
			wantLevel: slog.LevelError,
			wantMsg:   "mdns: Failed to read packet: timeout",
		},
		{
			name:      "no prefix falls back to default level",
			line:      "no responses for query with questions: _antsd-cluster._tcp.local.\n",
			wantLevel: defaultLevel,
			wantMsg:   "no responses for query with questions: _antsd-cluster._tcp.local.",
		},
		{
			name:      "unknown prefix is kept verbatim",
			line:      "[SOMETHING] unexpected\n",
			wantLevel: defaultLevel,
			wantMsg:   "[SOMETHING] unexpected",
		},
		{
			name:      "empty line",
			line:      "\n",
			wantLevel: defaultLevel,
			wantMsg:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, msg := parseLine(tt.line)
			if level != tt.wantLevel {
				t.Errorf("level = %v, want %v", level, tt.wantLevel)
			}
			if msg != tt.wantMsg {
				t.Errorf("msg = %q, want %q", msg, tt.wantMsg)
			}
		})
	}
}

func TestTag(t *testing.T) {
	tests := []struct {
		name      string
		component string
		msg       string
		want      string
	}{
		{
			name:      "library name is dropped when it duplicates the component",
			component: "SERF",
			msg:       "serf: EventMemberJoin: ants02 10.10.9.25",
			want:      "[SERF] EventMemberJoin: ants02 10.10.9.25",
		},
		{
			name:      "message without a library name is only tagged",
			component: "SERF",
			msg:       "timeout waiting for leave broadcast",
			want:      "[SERF] timeout waiting for leave broadcast",
		},
		{
			// only an exact (case-insensitive) match is dropped, so an unexpected
			// message keeps everything it carries
			name:      "unrelated leading word is kept",
			component: "MDNS",
			msg:       "dns: Failed to unpack packet",
			want:      "[MDNS] dns: Failed to unpack packet",
		},
		{
			name:      "colon without a space is not a library name",
			component: "MEMBERLIST",
			msg:       "Stream connection from=10.10.9.24:49828",
			want:      "[MEMBERLIST] Stream connection from=10.10.9.24:49828",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &writer{component: tt.component}
			if got := w.tag(tt.msg); got != tt.want {
				t.Errorf("tag() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestWriterFiltersBelowLevel checks that library logs now obey the configured
// log level: debug chatter is dropped at info level, and tagged when emitted.
func TestWriterFiltersBelowLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	w := newWriter(logger, "MEMBERLIST", false)

	if _, err := w.Write([]byte("[DEBUG] memberlist: Initiating push/pull sync\n")); err != nil {
		t.Fatalf("write returned an error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("debug line should have been filtered out, got %q", buf.String())
	}

	if _, err := w.Write([]byte("[WARN] memberlist: Refuting a suspect message\n")); err != nil {
		t.Fatalf("write returned an error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "level=WARN") {
		t.Errorf("expected a WARN record, got %q", out)
	}
	if !strings.Contains(out, `msg="[MEMBERLIST] Refuting a suspect message"`) {
		t.Errorf("expected the component tag at the start of the message, got %q", out)
	}
	if strings.Contains(out, "[WARN]") {
		t.Errorf("severity prefix should have been stripped from the message, got %q", out)
	}
}

// TestQuietWriterDropsBelowWarn checks that the quiet bridge keeps hot-path
// boilerplate out of the log while preserving real problems. The handler is set
// to debug on purpose: a demoted line would show up there, a dropped one never
// does.
func TestQuietWriterDropsBelowWarn(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	w := newWriter(logger, "MDNS", true)

	// the constructor is what the call sites use, so it must select the quiet writer
	NewQuietStdLogger(logger, "MDNS").Printf("[INFO] mdns: Failed to bind to udp6 port")
	if buf.Len() != 0 {
		t.Errorf("NewQuietStdLogger should build a quiet writer, got %q", buf.String())
	}

	dropped := []string{
		"[INFO] mdns: Failed to listen to both unicast and multicast on IPv6\n",
		// mDNS logs its noisiest line without any severity prefix
		"no responses for query with questions: _antsd-cluster._tcp.local.\n",
	}
	for _, line := range dropped {
		n, err := w.Write([]byte(line))
		if err != nil {
			t.Fatalf("write returned an error: %v", err)
		}
		// a dropped line must still report the whole buffer as consumed
		if n != len(line) {
			t.Errorf("n = %d, want %d", n, len(line))
		}
		if buf.Len() != 0 {
			t.Errorf("%q should have been dropped, got %q", line, buf.String())
		}
	}

	if _, err := w.Write([]byte("[ERR] mdns: Failed to read packet: timeout\n")); err != nil {
		t.Fatalf("write returned an error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "level=ERROR") {
		t.Errorf("errors must keep their severity, got %q", out)
	}
	if !strings.Contains(out, `msg="[MDNS] Failed to read packet: timeout"`) {
		t.Errorf("expected the component tag at the start of the message, got %q", out)
	}
}

// TestWriterReportsFullLength guards the io.Writer contract: log.Logger treats a
// short write as an error.
func TestWriterReportsFullLength(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	line := []byte("[INFO] serf: something happened\n")

	n, err := newWriter(logger, "SERF", false).Write(line)
	if err != nil {
		t.Fatalf("write returned an error: %v", err)
	}
	if n != len(line) {
		t.Errorf("n = %d, want %d", n, len(line))
	}
}
