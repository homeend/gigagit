package agentstate

import (
	"slices"
	"time"
)

// Observation is what gg observed of one session at one moment: its screen
// and what it announced outside it (the window title, the OSC 9;4 report).
type Observation struct {
	Text     string   // the screen (dialog options read the raw text)
	Lines    []string // Tail(Text, 15)
	Title    string   // last OSC 0/2 title
	Progress int      // last OSC 9;4 state, −1 none
}

// Verdict is what one detector concludes. Unknown: no verdict — the next
// part decides, and with none the session keeps the state it had.
type Verdict struct {
	State State
	// IdleHint: a source other than the screen says idle. It never decides;
	// the tracker may hold a trusted one shorter.
	IdleHint bool
	// Spinning: a source other than the screen says working — the trust the
	// idle hint needs.
	Spinning bool
}

// Detector reads one kind of evidence. Every source of agent state —
// the screen, the title, the progress report — implements it, and an
// agent's Profile composes them (agents.go).
type Detector interface{ Read(Observation) Verdict }

// Reading is a profile's conclusion: the verdict plus what any screen
// shows whatever the agent — the dialog's choices, the spinner's timer,
// the tail without what moves while nothing happens (the stall key).
type Reading struct {
	Verdict
	Options  []Option      // when State is Question
	StepFor  time.Duration // StepDuration(Lines)
	StallKey string        // StallKey(Lines)
	// Dedicated is the profile's: an unreadable screen of a dedicated agent
	// counts towards a stall.
	Dedicated bool
}

// Profile is how one agent is read.
type Profile struct {
	Detector Detector
	// Dedicated: the rules know this agent's screens (not the generic set);
	// an unreadable screen of a dedicated agent counts towards a stall.
	Dedicated bool
}

// Read applies the profile's detector and adds the agent-agnostic extras.
func (p Profile) Read(o Observation) Reading {
	var v Verdict
	if p.Detector != nil {
		v = p.Detector.Read(o)
	}
	rd := Reading{Verdict: v, StepFor: StepDuration(o.Lines), StallKey: StallKey(o.Lines), Dedicated: p.Dedicated}
	if v.State == Question {
		rd.Options = DialogOptions(o.Text, o.Lines)
	}
	return rd
}

// First consults its parts in order: the first one with a verdict decides,
// carrying the hints of the parts before it; later parts are not asked.
// (herdr's order: a title question or spinner outranks every screen rule;
// an idle title only hints.) It keeps its own copy of the list.
func First(parts ...Detector) Detector { return first(slices.Clone(parts)) }

type first []Detector

func (f first) Read(o Observation) Verdict {
	var hints Verdict
	for _, d := range f {
		v := d.Read(o)
		hints.IdleHint = hints.IdleHint || v.IdleHint
		hints.Spinning = hints.Spinning || v.Spinning
		if v.State != Unknown {
			v.IdleHint, v.Spinning = hints.IdleHint, hints.Spinning
			return v
		}
	}
	return hints
}
