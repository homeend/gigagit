package gitexec

import (
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
	inflightMu  sync.Mutex
	inflight    = map[int64]*Proc{}
	inflightSeq int64

	// slotWaiters counts callers queued for a gitSem slot: eight hung gits
	// stall every later read here, before any process starts.
	slotWaiters atomic.Int64
)

func trackProc(name string, argv []string, start time.Time) int64 {
	inflightMu.Lock()
	defer inflightMu.Unlock()
	inflightSeq++
	inflight[inflightSeq] = &Proc{Name: name, Argv: slices.Clone(argv), Start: start}
	return inflightSeq
}

func setProcPID(id int64, pid int) {
	inflightMu.Lock()
	defer inflightMu.Unlock()
	if p := inflight[id]; p != nil {
		p.PID = pid
	}
}

func untrackProc(id int64) {
	inflightMu.Lock()
	defer inflightMu.Unlock()
	delete(inflight, id)
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
