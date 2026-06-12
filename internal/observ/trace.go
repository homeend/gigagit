package observ

import (
	"io"
	"sync"
)

// TraceRecorder forwards spans to an inner Recorder and, when w is non-nil,
// also appends each span as a JSON line to w. Use it to enable verbose tracing
// while keeping the always-on ring buffer populated.
type TraceRecorder struct {
	inner Recorder
	mu    sync.Mutex
	w     io.Writer
}

// NewTraceRecorder wraps inner; if w is nil, only forwarding occurs.
func NewTraceRecorder(inner Recorder, w io.Writer) *TraceRecorder {
	return &TraceRecorder{inner: inner, w: w}
}

func (t *TraceRecorder) Record(s Span) {
	if t.inner != nil {
		t.inner.Record(s)
	}
	if t.w == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	writeSpanLine(t.w, s)
}

// compile-time check: TraceRecorder satisfies Recorder.
var _ Recorder = (*TraceRecorder)(nil)
