package domain

import (
	"slices"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/agentstate"
)

// stateTiming is one snapshot of the timing rules (session_states.go).
type stateTiming struct{ grace, stall, spinStall, idleSettle, titleSettle time.Duration }

func currentTiming() stateTiming {
	statesMu.Lock()
	defer statesMu.Unlock()
	return stateTiming{grace: stateGrace, stall: stallAfter, spinStall: spinStallAfter, idleSettle: idleSettle, titleSettle: titleSettle}
}

// progressMark: the session's tail without its spinner (agentstate.Progress)
// and since when it has read so.
type progressMark struct {
	key   string
	since time.Time
}

// sessionTracker folds one session's readings into its activity over time:
// the trust its idle hint needs, the idle hold, the grace after a start, the
// question that opened inside it, both stall rules. One per running session,
// owned by the watcher (under its mu).
type sessionTracker struct {
	dedicated   bool            // the profile knows this agent's screens
	act         SessionActivity // the last published activity
	pendingIdle time.Time       // the first idle read while working (zero: none)
	pendingQ    bool            // a question seen inside the grace, not yet announced
	progress    progressMark    // the spinner-only stall clock
	animated    bool            // a non-screen source has said working: its idle hint is trusted
}

// Step folds one reading in. It returns the new activity, the notices to
// post (unnumbered), whether anything a subscriber shows changed, and how
// soon a pending idle wants another look (0: none). An Unknown reading
// changes nothing (output lands mid-redraw often enough that acting on it
// would flap). A working session that reads idle shows idle only once that
// has held — titleSettle when a trusted idle hint agrees, else idleSettle —
// and then from when it began.
func (t *sessionTracker) Step(rd agentstate.Reading, info agentsession.Info, lastOut, now time.Time, tm stateTiming) (SessionActivity, []ActivityNotice, bool, time.Duration) {
	var notes []ActivityNotice
	changed := false
	var recheck time.Duration
	if rd.Spinning {
		t.animated = true
	}
	prev := t.act
	next := prev
	st := rd.State
	switch st {
	case agentstate.Question:
		next.Options = rd.Options
	case agentstate.Unknown:
	default:
		next.Options = nil
	}
	note := func(kind string, quiet time.Duration, spinning bool) {
		notes = append(notes, ActivityNotice{ID: info.ID, Kind: kind, Label: info.Label, Dir: info.Dir, Quiet: quiet, Spinning: spinning})
		changed = true
	}
	trusted := now.Sub(info.Started) >= tm.grace
	since := now
	hold := tm.idleSettle
	if rd.IdleHint && t.animated {
		hold = tm.titleSettle
	}
	switch {
	case st == agentstate.Waiting && prev.State == agentstate.Working:
		if t.pendingIdle.IsZero() {
			t.pendingIdle = now
		}
		if held := now.Sub(t.pendingIdle); held < hold {
			st = agentstate.Unknown // not yet: the session stays working
			recheck = hold - held
		} else {
			since, t.pendingIdle = t.pendingIdle, time.Time{}
		}
	case st != agentstate.Unknown:
		t.pendingIdle = time.Time{} // working again, or a question
	}
	if st != agentstate.Unknown && st != prev.State {
		next.State, next.Since, next.ReadyAt = st, since, since
		if st == agentstate.Waiting {
			next.ReadyAt = since.Add(hold)
		}
		changed = true
		switch {
		case st == agentstate.Question && trusted:
			note("question", 0, false)
		case st == agentstate.Question:
			t.pendingQ = true
		case st == agentstate.Waiting && prev.State == agentstate.Working && since.Sub(info.Started) >= tm.grace:
			note("idle", 0, false)
		}
	} else if t.pendingQ && trusted {
		// The question that opened inside the grace is still up: the user
		// must hear about it (a trust dialog at start).
		t.pendingQ = false
		if next.State == agentstate.Question {
			note("question", 0, false)
		}
	}
	if next.State != agentstate.Question {
		t.pendingQ = false
	}
	next.StepFor = rd.StepFor
	stalled := !lastOut.IsZero() && now.Sub(lastOut) > tm.stall &&
		(next.State == agentstate.Working || (next.State == agentstate.Unknown && t.dedicated))
	quiet, spinning := now.Sub(lastOut), false
	if rd.Progress != t.progress.key || t.progress.since.IsZero() {
		t.progress = progressMark{key: rd.Progress, since: now}
	}
	if !stalled && next.State == agentstate.Working && now.Sub(t.progress.since) >= tm.spinStall {
		stalled, quiet, spinning = true, now.Sub(t.progress.since), true
	}
	if stalled != prev.Stalled {
		changed = true
		if stalled {
			note("stalled", quiet, spinning)
		}
	}
	next.Stalled = stalled
	if !slices.Equal(prev.Options, next.Options) {
		changed = true
	}
	t.act = next
	return next, notes, changed, recheck
}
