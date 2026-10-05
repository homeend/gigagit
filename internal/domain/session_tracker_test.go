package domain

import (
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/agentstate"
)

var trkTiming = stateTiming{grace: 30 * time.Second, stall: 2 * time.Minute, spinStall: 11 * time.Minute, idleSettle: 2 * time.Second, titleSettle: 700 * time.Millisecond}

var trkInfo = agentsession.Info{ID: "s1", Label: "Claude", Dir: "/wt/a", Started: actT0}

func rdg(st agentstate.State) agentstate.Reading {
	return agentstate.Reading{Verdict: agentstate.Verdict{State: st}, StallKey: string(st)}
}

func noteKinds(ns []ActivityNotice) string {
	s := ""
	for _, n := range ns {
		s += n.Kind + ","
	}
	return s
}

func TestTrackerTitledIdleHoldsShortOnlyWhenTrusted(t *testing.T) {
	t.Parallel()
	late := actT0.Add(time.Minute)
	for _, c := range []struct {
		name     string
		spunOnce bool
		hint     bool
		want     time.Duration
	}{
		{"trusted hint", true, true, 700 * time.Millisecond},
		{"untrusted hint", false, true, 2 * time.Second},
		{"no hint", true, false, 2 * time.Second},
	} {
		var tr sessionTracker
		w := rdg(agentstate.Working)
		w.Spinning = c.spunOnce
		tr.Step(w, trkInfo, late, late, trkTiming)
		idle := rdg(agentstate.Waiting)
		idle.IdleHint = c.hint
		if _, _, _, again := tr.Step(idle, trkInfo, late, late.Add(time.Second), trkTiming); again != c.want {
			t.Errorf("%s: recheck %v, want %v", c.name, again, c.want)
		}
		act, notes, _, _ := tr.Step(idle, trkInfo, late, late.Add(time.Second+c.want), trkTiming)
		if act.State != ActivityIdle || !act.Since.Equal(late.Add(time.Second)) || !act.ReadyAt.Equal(late.Add(time.Second+c.want)) || noteKinds(notes) != "idle," {
			t.Errorf("%s: %+v notes %q", c.name, act, noteKinds(notes))
		}
	}
}

// An Unknown read while an idle is held keeps the re-check: the rest of the
// hold, at least stateCoalesce once it has passed. Without one the idle waits
// for the 2 s tick (an idle agent prints nothing that would wake the loop).
func TestTrackerUnknownReadKeepsThePendingIdleRecheck(t *testing.T) {
	t.Parallel()
	late := actT0.Add(time.Minute)
	var tr sessionTracker
	tr.Step(rdg(agentstate.Working), trkInfo, late, late, trkTiming)
	idleAt := late.Add(time.Second)
	tr.Step(rdg(agentstate.Waiting), trkInfo, late, idleAt, trkTiming)
	if _, _, _, again := tr.Step(rdg(agentstate.Unknown), trkInfo, late, idleAt.Add(500*time.Millisecond), trkTiming); again != 1500*time.Millisecond {
		t.Errorf("inside the hold: recheck %v, want 1.5s", again)
	}
	if _, _, _, again := tr.Step(rdg(agentstate.Unknown), trkInfo, late, idleAt.Add(3*time.Second), trkTiming); again != stateCoalesce {
		t.Errorf("past the hold: recheck %v, want %v", again, stateCoalesce)
	}
	act, notes, _, _ := tr.Step(rdg(agentstate.Waiting), trkInfo, late, idleAt.Add(3300*time.Millisecond), trkTiming)
	if act.State != ActivityIdle || !act.Since.Equal(idleAt) || noteKinds(notes) != "idle," {
		t.Errorf("after the Unknown reads: %+v notes %q", act, noteKinds(notes))
	}
	// No pending idle: an Unknown read asks for nothing.
	var calm sessionTracker
	calm.Step(rdg(agentstate.Working), trkInfo, late, late, trkTiming)
	if _, _, _, again := calm.Step(rdg(agentstate.Unknown), trkInfo, late, idleAt, trkTiming); again != 0 {
		t.Errorf("no pending idle: recheck %v, want 0", again)
	}
}

// A held idle reads as Unknown for the state, but Options follow the read:
// a Waiting read clears a question's options even while it is held.
func TestTrackerOptionsFollowTheReadNotTheHold(t *testing.T) {
	t.Parallel()
	late := actT0.Add(time.Minute)
	var tr sessionTracker
	q := rdg(agentstate.Question)
	q.Options = []agentstate.Option{{Key: "1", Label: "Yes"}}
	tr.Step(rdg(agentstate.Working), trkInfo, late, late, trkTiming)
	act, _, _, _ := tr.Step(q, trkInfo, late, late.Add(time.Second), trkTiming)
	if len(act.Options) != 1 {
		t.Fatalf("question options: %+v", act)
	}
	act, _, _, _ = tr.Step(rdg(agentstate.Unknown), trkInfo, late, late.Add(2*time.Second), trkTiming)
	if len(act.Options) != 1 || act.State != ActivityQuestion {
		t.Fatalf("unknown must keep state and options: %+v", act)
	}
	tr.Step(rdg(agentstate.Working), trkInfo, late, late.Add(3*time.Second), trkTiming)
	act, _, _, _ = tr.Step(rdg(agentstate.Waiting), trkInfo, late, late.Add(4*time.Second), trkTiming) // held
	if act.State != ActivityWorking || act.Options != nil {
		t.Fatalf("held idle: %+v", act)
	}
}

func TestTrackerQuestionInsideTheGraceIsAnnouncedAfterIt(t *testing.T) {
	t.Parallel()
	var tr sessionTracker
	_, notes, _, _ := tr.Step(rdg(agentstate.Question), trkInfo, actT0, actT0.Add(time.Second), trkTiming)
	if len(notes) != 0 {
		t.Fatalf("noticed inside the grace: %q", noteKinds(notes))
	}
	_, notes, _, _ = tr.Step(rdg(agentstate.Question), trkInfo, actT0, actT0.Add(31*time.Second), trkTiming)
	if noteKinds(notes) != "question," {
		t.Fatalf("after the grace: %q", noteKinds(notes))
	}
}

func TestTrackerStalls(t *testing.T) {
	t.Parallel()
	late := actT0.Add(time.Minute)
	// no output while working
	var tr sessionTracker
	tr.Step(rdg(agentstate.Working), trkInfo, late, late, trkTiming)
	act, notes, _, _ := tr.Step(rdg(agentstate.Working), trkInfo, late, late.Add(3*time.Minute), trkTiming)
	if !act.Stalled || noteKinds(notes) != "stalled," || notes[0].Spinning {
		t.Fatalf("silent stall: %+v %+v", act, notes)
	}
	// only the spinner moves for spinStall (output keeps coming)
	var sp sessionTracker
	sp.Step(rdg(agentstate.Working), trkInfo, late, late, trkTiming)
	at := late.Add(11 * time.Minute)
	act, notes, _, _ = sp.Step(rdg(agentstate.Working), trkInfo, at, at, trkTiming)
	if !act.Stalled || len(notes) != 1 || !notes[0].Spinning {
		t.Fatalf("spinner stall: %+v %+v", act, notes)
	}
	// unknown counts only for dedicated rules — the reading says which
	var gen sessionTracker
	if act, _, _, _ := gen.Step(rdg(agentstate.Unknown), trkInfo, late, late.Add(3*time.Minute), trkTiming); act.Stalled {
		t.Fatal("generic unknown stalled")
	}
	var ded sessionTracker
	dr := rdg(agentstate.Unknown)
	dr.Dedicated = true
	if act, _, _, _ := ded.Step(dr, trkInfo, late, late.Add(3*time.Minute), trkTiming); !act.Stalled {
		t.Fatal("dedicated unknown did not stall")
	}
}

func TestTrackerReadyAtOfAQuestionIsItsSince(t *testing.T) {
	t.Parallel()
	var tr sessionTracker
	at := actT0.Add(time.Minute)
	act, _, changed, _ := tr.Step(rdg(agentstate.Question), trkInfo, at, at, trkTiming)
	if !changed || !act.ReadyAt.Equal(at) || !act.Since.Equal(at) {
		t.Fatalf("%+v changed=%v", act, changed)
	}
}
