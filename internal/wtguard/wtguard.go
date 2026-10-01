// Package wtguard is the one question "may this worktree be taken?" asked
// of many independent data sources. A Guard answers for its own source
// (git state, a claim, a session registry, config…); consumers (the
// inventory, claims, the recycle op) take a []Guard and know none of the
// sources; the composition root decides which guards exist. DAG leaf:
// stdlib only.
package wtguard

import "context"

// Target is the worktree a guard judges, as the caller sees it.
type Target struct {
	Dir, Branch    string
	Main, Detached bool
	CallerSession  string // "" = a human / not an agent; the claim guard exempts its holder
}

// Blocker is one reason the worktree may not be taken.
type Blocker struct {
	Reason string
	Detail string
	Hard   bool // never overridable (a human or --force cannot proceed)
}

// Result is a guard's answer: an optional blocker plus the fact it read.
type Result struct {
	Blocker *Blocker
	Fact    any
}

// Guard answers for one data source.
type Guard interface {
	Reason() string  // the blocked_by string it produces
	FactKey() string // the key of its fact in Report.Facts; "" = none
	Cheap() bool     // stat-level; expensive guards run only when no cheap one blocked
	Check(ctx context.Context, t Target) (Result, error)
}

// Report is every guard's verdict on one target.
type Report struct {
	Blockers []Blocker
	Facts    map[string]any
}

// Run asks every cheap guard, then — only when none blocked — the
// expensive ones (a `git status` on a 20 GB checkout is seconds; a blocked
// worktree's dirt answers nothing). A skipped guard's fact is present and
// nil. A guard error is a hard check-failed blocker: never read "could not
// tell" as "free".
func Run(ctx context.Context, gs []Guard, t Target) Report {
	r := Report{Facts: map[string]any{}}
	ask := func(g Guard) {
		res, err := g.Check(ctx, t)
		if err != nil {
			r.Blockers = append(r.Blockers, Blocker{Reason: "check-failed", Detail: g.Reason() + ": " + err.Error(), Hard: true})
			return
		}
		if k := g.FactKey(); k != "" {
			r.Facts[k] = res.Fact
		}
		if res.Blocker != nil {
			r.Blockers = append(r.Blockers, *res.Blocker)
		}
	}
	for _, g := range gs {
		if g.Cheap() {
			ask(g)
		}
	}
	blocked := len(r.Blockers) > 0
	for _, g := range gs {
		if g.Cheap() {
			continue
		}
		if blocked {
			if k := g.FactKey(); k != "" {
				r.Facts[k] = nil
			}
			continue
		}
		ask(g)
	}
	return r
}

// Reasons lists the blockers' reasons in guard order.
func (r Report) Reasons() []string {
	out := make([]string, 0, len(r.Blockers))
	for _, b := range r.Blockers {
		out = append(out, b.Reason)
	}
	return out
}

// Hard is the blockers nothing may override.
func (r Report) Hard() []Blocker { return r.filter(true) }

// Overridable is the blockers a human or --force may proceed past.
func (r Report) Overridable() []Blocker { return r.filter(false) }

func (r Report) filter(hard bool) []Blocker {
	var out []Blocker
	for _, b := range r.Blockers {
		if b.Hard == hard {
			out = append(out, b)
		}
	}
	return out
}
