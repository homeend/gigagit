package observ

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRingRecordMirrorsToSink(t *testing.T) {
	var buf bytes.Buffer
	SetSpanSink(&buf)
	t.Cleanup(func() { SetSpanSink(nil) })

	r := NewRing(4)
	r.Record(Span{Name: "git status", Args: []string{"status", "--porcelain"}, Start: time.Now(), Duration: time.Millisecond})

	line := strings.TrimSpace(buf.String())
	var got Span
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("sink line is not valid JSON: %v\n%s", err, line)
	}
	if got.ID != 1 {
		t.Errorf("ID = %d, want the ring-assigned 1", got.ID)
	}
	if got.Name != "git status" {
		t.Errorf("Name = %q, want git status", got.Name)
	}
	// The ring itself still retains the span (mirroring, not rerouting).
	if snap := r.Snapshot(); len(snap) != 1 {
		t.Fatalf("ring snapshot = %d spans, want 1", len(snap))
	}
}

func TestEmitSpanWritesToSink(t *testing.T) {
	var buf bytes.Buffer
	SetSpanSink(&buf)
	t.Cleanup(func() { SetSpanSink(nil) })

	EmitSpan(Span{Name: "op SmartPull", Duration: 2 * time.Second})
	var got Span
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Name != "op SmartPull" || got.Duration != 2*time.Second {
		t.Errorf("got %+v", got)
	}
}

func TestSinkRedactsArgs(t *testing.T) {
	var buf bytes.Buffer
	SetSpanSink(&buf)
	t.Cleanup(func() { SetSpanSink(nil) })

	EmitSpan(Span{Name: "gg start", Args: []string{"pull", "https://user:secret@host/repo"}})
	if strings.Contains(buf.String(), "secret") {
		t.Fatalf("sink line leaked a credential: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "<redacted>") {
		t.Fatalf("expected redaction marker in: %s", buf.String())
	}
}

func TestEmitSpanWithoutSinkIsNoOp(t *testing.T) {
	SetSpanSink(nil)
	EmitSpan(Span{Name: "op X"}) // must not panic, must not block
}

func TestSinkConcurrentWriters(t *testing.T) {
	var buf bytes.Buffer
	SetSpanSink(&buf)
	t.Cleanup(func() { SetSpanSink(nil) })

	r := NewRing(8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Record(Span{Name: "git x"})
			EmitSpan(Span{Name: "op y"})
		}()
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 16 {
		t.Fatalf("lines = %d, want 16 (8 ring + 8 emit)", len(lines))
	}
	for i, ln := range lines {
		if !json.Valid([]byte(ln)) {
			t.Fatalf("line %d is not valid JSON (interleaved write?): %q", i, ln)
		}
	}
}
