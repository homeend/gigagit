package observ

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestTraceRecorderWritesJSONLinesAndForwards(t *testing.T) {
	var buf bytes.Buffer
	ring := NewRing(10)
	tr := NewTraceRecorder(ring, &buf)

	tr.Record(Span{Name: "git status", Duration: 5 * time.Millisecond})

	// Forwarded to the underlying ring.
	if len(ring.Snapshot()) != 1 {
		t.Fatalf("expected span forwarded to ring")
	}
	// One JSON line written.
	line := bytes.TrimSpace(buf.Bytes())
	var s Span
	if err := json.Unmarshal(line, &s); err != nil {
		t.Fatalf("trace line not valid JSON: %v (%q)", err, line)
	}
	if s.Name != "git status" {
		t.Fatalf("trace line name = %q, want 'git status'", s.Name)
	}
}

func TestTraceRecorderNilWriterStillForwards(t *testing.T) {
	ring := NewRing(10)
	tr := NewTraceRecorder(ring, nil)
	tr.Record(Span{Name: "x"})
	if len(ring.Snapshot()) != 1 {
		t.Fatal("nil-writer trace recorder must still forward to ring")
	}
}
