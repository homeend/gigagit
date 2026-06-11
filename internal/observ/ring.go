// Package observ provides always-on lightweight observability: a bounded ring
// buffer of recent spans, opt-in tracing, and debug-dump serialization.
package observ

import (
	"sync"
	"time"
)

// Span is one recorded unit of work: a git subprocess or an operation step.
type Span struct {
	ID       int64         `json:"id"`
	ParentID int64         `json:"parent_id,omitempty"`
	Name     string        `json:"name"`
	Args     []string      `json:"args,omitempty"`
	ExitCode int           `json:"exit_code"`
	Err      string        `json:"err,omitempty"`
	Start    time.Time     `json:"start"`
	Duration time.Duration `json:"duration_ns"`
}

// Recorder receives completed spans.
type Recorder interface{ Record(s Span) }

// Ring is a bounded, concurrency-safe Recorder retaining the last N spans.
type Ring struct {
	mu     sync.Mutex
	buf    []Span
	cap    int
	nextID int64
}

// NewRing returns a Ring retaining up to capacity spans.
func NewRing(capacity int) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{cap: capacity}
}

// Record stores a span, assigning it a monotonic ID and evicting the oldest
// span when capacity is exceeded.
func (r *Ring) Record(s Span) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	s.ID = r.nextID
	r.buf = append(r.buf, s)
	if len(r.buf) > r.cap {
		r.buf = r.buf[len(r.buf)-r.cap:]
	}
}

// Snapshot returns a copy of the retained spans, oldest first.
func (r *Ring) Snapshot() []Span {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Span, len(r.buf))
	copy(out, r.buf)
	return out
}

// PeekNextID exposes the next ID the ring will assign; used by callers that need
// a parent span ID before recording children. It does not advance the counter.
func (r *Ring) PeekNextID() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nextID + 1
}
