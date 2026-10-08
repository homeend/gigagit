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

	"github.com/homeend/gigagit/internal/observ"
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
	since time.Time // when granted (the emergency state dump shows hold times)
	// long marks a hold that lasts minutes, not milliseconds (a headless AI
	// task running an agent). Nobody is queued behind one: an incompatible
	// Acquire is refused with a BusyError instead — see Acquire.
	long bool
}

// BusyError is Acquire's refusal when the reservation would have to wait
// for a long-lived holder (see AcquireLong). It names the holder so a
// frontend can say what is running instead of showing a frozen "working…".
type BusyError struct {
	Holder string    // the holder's label, e.g. "op ConflictAgent"
	Mode   Mode      // the holder's mode
	Since  time.Time // when it was granted
}

func (e *BusyError) Error() string {
	return "repository busy: " + e.Holder + " is running"
}

// waiter is one queued Acquire.
type waiter struct {
	mode  Mode
	label string
	since time.Time     // when queued
	ready chan struct{} // closed on grant; h is set before the close
	h     *holder
	long  bool // the granted holder is long-lived (AcquireLong)
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
//
// One case never waits: when an ACTIVE long-lived holder (AcquireLong) is
// incompatible with mode, Acquire returns a *BusyError at once. Queuing
// there would park the caller for the holder's whole run with nothing to
// show for it, and — the queue being strict FIFO — every later read would
// stall behind the parked writer too. A short holder keeps the old
// behaviour: an incompatible Acquire queues and is granted on release.
func (g *Gate) Acquire(ctx context.Context, mode Mode, label string) (*Reservation, error) {
	return g.acquire(ctx, mode, label, false)
}

// AcquireLong is Acquire for a hold that will last minutes (a headless AI
// task). It is granted by the same rules; the difference is for others:
// an incompatible Acquire while it is held is refused, never queued.
func (g *Gate) AcquireLong(ctx context.Context, mode Mode, label string) (*Reservation, error) {
	return g.acquire(ctx, mode, label, true)
}

func (g *Gate) acquire(ctx context.Context, mode Mode, label string, long bool) (*Reservation, error) {
	start := time.Now()
	g.mu.Lock()
	if busy := g.longBlocker(mode); busy != nil {
		g.mu.Unlock()
		return nil, busy
	}
	// Immediate grant only when nobody is queued: a non-empty queue means
	// someone arrived first, and FIFO fairness (which is also the writer
	// preference — new reads queue behind a waiting writer) wins over
	// opportunistic overlap.
	if len(g.waiters) == 0 && g.holdersCompatibleWith(mode) {
		h := &holder{mode: mode, label: label, since: start, long: long}
		g.holders = append(g.holders, h)
		g.mu.Unlock()
		return &Reservation{g: g, h: h}, nil
	}
	w := &waiter{mode: mode, label: label, since: start, ready: make(chan struct{}), long: long}
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

// longBlocker returns the refusal for mode when an active long-lived holder
// excludes it, nil otherwise. Callers hold g.mu.
func (g *Gate) longBlocker(mode Mode) *BusyError {
	for _, h := range g.holders {
		if h.long && !compatible(h.mode, mode) {
			return &BusyError{Holder: h.label, Mode: h.mode, Since: h.since}
		}
	}
	return nil
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
		w.h = &holder{mode: w.mode, label: w.label, since: time.Now(), long: w.long}
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
	Since   time.Time // granted (holder) or queued (waiter)
	Long    bool      // a long-lived hold (AcquireLong); never set on a waiter
}

// Queue snapshots current holders then waiters, in FIFO order, for
// frontends to render ("queued: smart pull (2nd)").
func (g *Gate) Queue() []Entry {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]Entry, 0, len(g.holders)+len(g.waiters))
	for _, h := range g.holders {
		out = append(out, Entry{Label: h.label, Mode: h.mode, Since: h.since, Long: h.long})
	}
	for _, w := range g.waiters {
		out = append(out, Entry{Label: w.label, Mode: w.mode, Waiting: true, Since: w.since})
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
	// Released() may inspect r.h from another goroutine (concurrent ops,
	// queue UI), so the swap happens under the gate mutex like every other
	// r.h write.
	g.mu.Lock()
	r.h = nr.h
	g.mu.Unlock()
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

// All snapshots every gate in the process that has a holder or a waiter,
// keyed by git common dir — the emergency state dump's "who holds the repo".
func All() map[string][]Entry {
	regMu.Lock()
	gs := make(map[string]*Gate, len(gates))
	for k, g := range gates {
		gs[k] = g
	}
	regMu.Unlock()
	out := map[string][]Entry{}
	for k, g := range gs {
		if q := g.Queue(); len(q) > 0 {
			out[k] = q
		}
	}
	return out
}
