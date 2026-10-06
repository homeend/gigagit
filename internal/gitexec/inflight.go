package gitexec

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// Proc is one git subprocess still running — what the emergency state dump
// shows when "⏳ reloading…" never ends: a git that never came back is the
// usual cause, and a span is only recorded once it exits.
type Proc struct {
	Name  string
	Argv  []string // raw; redact before writing it anywhere
	PID   int      // 0 until the process started
	Start time.Time
}

var (
	inflightMu     sync.Mutex
	inflight       = map[int64]*Proc{}
	inflightCancel = map[int64]context.CancelFunc{}
	inflightSeq    int64

	// slotWaiters counts callers queued for a gitSem slot: eight hung gits
	// stall every later read here, before any process starts.
	slotWaiters atomic.Int64
)

// trackProc registers a git about to start and derives the context it runs
// under, so CancelInFlight can end it (the runner's own SIGTERM-then-WaitDelay
// cancel). The caller untrackProc(id)s it once it exits and calls cancel only
// on return — after its own ctx.Err() check, which must not see this cancel.
func trackProc(ctx context.Context, name string, argv []string, start time.Time) (context.Context, int64, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	inflightMu.Lock()
	defer inflightMu.Unlock()
	inflightSeq++
	inflight[inflightSeq] = &Proc{Name: name, Argv: slices.Clone(argv), Start: start}
	inflightCancel[inflightSeq] = cancel
	return ctx, inflightSeq, cancel
}

func setProcPID(id int64, pid int) {
	inflightMu.Lock()
	defer inflightMu.Unlock()
	if p := inflight[id]; p != nil {
		p.PID = pid
	}
}

// untrackProc takes a git off the list; safe to call twice.
func untrackProc(id int64) {
	inflightMu.Lock()
	defer inflightMu.Unlock()
	delete(inflight, id)
	delete(inflightCancel, id)
}

// CancelInFlight ends every git subprocess running right now — the emergency
// unlock's last resort: an abandoned read that hangs still holds its
// singleflight slot, its git slot and its repo reservation, so the next
// reload would only join it. git gets SIGTERM (it releases its lockfiles)
// and the caller sees a cancelled error. Returns how many it signalled.
func CancelInFlight() int {
	inflightMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(inflightCancel))
	for _, c := range inflightCancel {
		cancels = append(cancels, c)
	}
	inflightMu.Unlock()
	for _, c := range cancels {
		c()
	}
	return len(cancels)
}

// InFlight snapshots the git subprocesses running right now, oldest first.
func InFlight() []Proc {
	inflightMu.Lock()
	out := make([]Proc, 0, len(inflight))
	for _, p := range inflight {
		out = append(out, *p)
	}
	inflightMu.Unlock()
	slices.SortFunc(out, func(a, b Proc) int { return a.Start.Compare(b.Start) })
	return out
}

// Slots reports the process-global git ceiling: slots held, its capacity, and
// callers queued for one.
func Slots() (held, capacity, waiting int) {
	return len(gitSem), cap(gitSem), int(slotWaiters.Load())
}
