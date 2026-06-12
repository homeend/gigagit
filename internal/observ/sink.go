package observ

import (
	"bytes"
	"encoding/json"
	"io"
	"sync"
)

// The process-wide span sink. nil (the default) disables mirroring entirely —
// the cli.RepoStatePath seam pattern: wired only by cmd/gg, so tests and
// library consumers see no side effects unless they opt in.
var (
	sinkMu sync.Mutex
	sink   io.Writer
)

// SetSpanSink routes a copy of every recorded span (ring-recorded and
// EmitSpan-emitted alike) to w as redacted JSON lines. nil disables. Call once
// at startup, before recorders run on other goroutines.
func SetSpanSink(w io.Writer) {
	sinkMu.Lock()
	defer sinkMu.Unlock()
	sink = w
}

// EmitSpan writes a synthetic span (process start, an engine operation) to the
// sink. It does not enter any ring. No-op when no sink is set.
func EmitSpan(s Span) { sinkWrite(s) }

// sinkWrite appends s to the sink as one JSON line. The single package mutex
// serializes all writers — every ring and every EmitSpan caller shares it.
func sinkWrite(s Span) {
	sinkMu.Lock()
	defer sinkMu.Unlock()
	if sink == nil {
		return
	}
	writeSpanLine(sink, s)
}

// writeSpanLine is the shared redact+marshal+write step used by the sink and
// TraceRecorder. Callers hold their own locks.
func writeSpanLine(w io.Writer, s Span) {
	s.Args = Redact(s.Args)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return
	}
	_, _ = w.Write(buf.Bytes())
}
