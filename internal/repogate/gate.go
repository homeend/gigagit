// Package repogate serializes access to a git repository within one gg
// process. The unit of exclusion is a whole high-level operation — which may
// span many git invocations and block on user decisions — not a single git
// call, because operations like SmartPull leave the repo in deliberately
// wrong intermediate states between invocations. Gates are keyed by the
// repository's git common dir, so all linked worktrees of a repo share one
// gate. Cross-process coordination is out of scope; git's own index.lock
// remains the backstop there.
package repogate

import (
	"context"
	"sync"
	"time"

	"github.com/gigagit/gg/internal/observ"
)

// Mode is the kind of access a reservation grants.
type Mode int

const (
	// Read observes repo state (status, branches, log, …).
	Read Mode = iota
	// RefWrite moves refs only, never index/worktree/HEAD (e.g. a
	// background fast-forward). Ref updates are atomic, so reads overlap.
	RefWrite
	// TreeWrite may touch index, worktree, or HEAD. Exclusive.
	TreeWrite
)

func (m Mode) String() string {
	switch m {
	case Read:
		return "read"
	case RefWrite:
		return "ref-write"
	default:
		return "tree-write"
	}
}

// compatible reports whether a reservation of mode b may run alongside an
// active holder of mode a: reads overlap reads and ref-writes; everything
// else excludes.
func compatible(a, b Mode) bool {
	if a == TreeWrite || b == TreeWrite {
		return false
	}
	return !(a == RefWrite && b == RefWrite)
}

// holder is the in-gate identity of one granted reservation; Escalate swaps
// a Reservation's holder, so identity must live here, not on Reservation.
type holder struct {
	mode  Mode
	label string
}

// waiter is one queued Acquire.
type waiter struct {
	mode  Mode
	label string
	ready chan struct{} // closed on grant; h is set before the close
	h     *holder
}

// Gate serializes reservations for one repository.
type Gate struct {
	mu      sync.Mutex
	holders []*holder
	waiters []*waiter // FIFO
}

// Reservation is a granted hold on the gate.
type Reservation struct {
	g *Gate
	h *holder // nil once released
}

// Acquire blocks until the reservation is granted or ctx is cancelled.
// label names the holder in Queue() and wait spans (e.g. "op SmartPull").
func (g *Gate) Acquire(ctx context.Context, mode Mode, label string) (*Reservation, error) {
	start := time.Now()
	g.mu.Lock()
	// Immediate grant only when nobody is queued: a non-empty queue means
	// someone arrived first, and FIFO fairness (which is also the writer
	// preference — new reads queue behind a waiting writer) wins over
	// opportunistic overlap.
	if len(g.waiters) == 0 && g.holdersCompatibleWith(mode) {
		h := &holder{mode: mode, label: label}
		g.holders = append(g.holders, h)
		g.mu.Unlock()
		return &Reservation{g: g, h: h}, nil
	}
	w := &waiter{mode: mode, label: label, ready: make(chan struct{})}
	g.waiters = append(g.waiters, w)
	g.mu.Unlock()

	select {
	case <-w.ready:
		observ.EmitSpan(observ.Span{
			Name:     "gate wait",
			Args:     []string{mode.String(), label},
			Start:    start,
			Duration: time.Since(start),
		})
		return &Reservation{g: g, h: w.h}, nil
	case <-ctx.Done():
		g.mu.Lock()
		for i, q := range g.waiters {
			if q == w { // still queued: just leave
				g.waiters = append(g.waiters[:i], g.waiters[i+1:]...)
				g.grant() // removing a queue head may unblock the rest
				g.mu.Unlock()
				return nil, ctx.Err()
			}
		}
		g.mu.Unlock()
		// Lost the race: granted concurrently with cancellation. Take the
		// grant and immediately give it back.
		<-w.ready
		(&Reservation{g: g, h: w.h}).Release()
		return nil, ctx.Err()
	}
}

// holdersCompatibleWith reports whether mode can run beside every holder.
// Callers hold g.mu.
func (g *Gate) holdersCompatibleWith(mode Mode) bool {
	for _, h := range g.holders {
		if !compatible(h.mode, mode) {
			return false
		}
	}
	return true
}

// grant admits queue heads while they are compatible with the active
// holders — strict FIFO with batch grants, so a run of compatible reads at
// the head is admitted together. Callers hold g.mu.
func (g *Gate) grant() {
	for len(g.waiters) > 0 {
		w := g.waiters[0]
		if !g.holdersCompatibleWith(w.mode) {
			return
		}
		g.waiters = g.waiters[1:]
		w.h = &holder{mode: w.mode, label: w.label}
		g.holders = append(g.holders, w.h)
		close(w.ready)
	}
}

// Release ends the reservation. Releasing twice panics (programming error).
func (r *Reservation) Release() {
	g := r.g
	g.mu.Lock()
	defer g.mu.Unlock()
	if r.h == nil {
		panic("repogate: reservation released twice")
	}
	for i, h := range g.holders {
		if h == r.h {
			g.holders = append(g.holders[:i], g.holders[i+1:]...)
			break
		}
	}
	r.h = nil
	g.grant()
}

// Entry describes one holder (Waiting=false) or queued waiter.
type Entry struct {
	Label   string
	Mode    Mode
	Waiting bool
}

// Queue snapshots current holders then waiters, in FIFO order, for
// frontends to render ("queued: smart pull (2nd)").
func (g *Gate) Queue() []Entry {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]Entry, 0, len(g.holders)+len(g.waiters))
	for _, h := range g.holders {
		out = append(out, Entry{Label: h.label, Mode: h.mode})
	}
	for _, w := range g.waiters {
		out = append(out, Entry{Label: w.label, Mode: w.mode, Waiting: true})
	}
	return out
}

// Escalate trades the held reservation for an exclusive (TreeWrite) one: it
// RELEASES the current reservation and joins the queue — there is no atomic
// upgrade (the classic deadlock). Callers must therefore only escalate at a
// boundary where the operation holds no partial state of its own. A
// reservation that is already TreeWrite returns immediately. On error (ctx
// cancelled while queued) the reservation is gone — the caller must not
// Release it.
func (r *Reservation) Escalate(ctx context.Context) error {
	if r.h == nil {
		panic("repogate: escalate after release")
	}
	if r.h.mode == TreeWrite {
		return nil
	}
	g, label := r.g, r.h.label
	r.Release()
	nr, err := g.Acquire(ctx, TreeWrite, label)
	if err != nil {
		return err
	}
	r.h = nr.h
	return nil
}

// Released reports whether the reservation has ended (also true after a
// failed Escalate, which releases before re-acquiring).
func (r *Reservation) Released() bool {
	r.g.mu.Lock()
	defer r.g.mu.Unlock()
	return r.h == nil
}

var (
	regMu sync.Mutex
	gates = map[string]*Gate{}
)

// For returns the process-wide gate for key (a git common dir), creating it
// on first use.
func For(key string) *Gate {
	regMu.Lock()
	defer regMu.Unlock()
	g, ok := gates[key]
	if !ok {
		g = &Gate{}
		gates[key] = g
	}
	return g
}
