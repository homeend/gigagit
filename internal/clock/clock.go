// Package clock is gg's one freezable "now" for times that are STORED
// (a note's, shelf entry's, bookmark's creation) or DRAWN (ages, date
// lines, elapsed time on screen). Timing code — debounces, double-click
// windows, timeouts, measured durations — keeps time.Now: freezing it
// would stall them. Tests (the e2e golden screens) freeze it so a screen
// renders the same text on every run. Stdlib-only DAG leaf.
package clock

import (
	"sync/atomic"
	"time"
)

var frozen atomic.Pointer[time.Time]

// Now is the frozen instant when Freeze is in effect, else time.Now().
func Now() time.Time {
	if t := frozen.Load(); t != nil {
		return *t
	}
	return time.Now()
}

// Since is Now().Sub(t).
func Since(t time.Time) time.Duration { return Now().Sub(t) }

// Freeze pins Now to t until the returned restore runs. Process-global:
// callers freeze once (a TestMain) or run serially.
func Freeze(t time.Time) (restore func()) {
	prev := frozen.Swap(&t)
	return func() { frozen.Store(prev) }
}
