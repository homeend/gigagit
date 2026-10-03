package domain

// agent_wait (stage 3b): a parent's long-poll on its workers. Level-
// triggered — the predicate is re-evaluated on every wake over the state
// watcher, the session lifecycle and the report store — and consumed once
// per (caller, worker): wait returns what is new; agent_list says what is.

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// AgentWaitResult is agent_wait's answer: one event, or a timeout carrying
// the named worker's current activity.
type AgentWaitResult struct {
	ID       string           `json:"id,omitempty"`
	Event    string           `json:"event,omitempty"` // idle | question | exit | report
	TimedOut bool             `json:"timed_out,omitempty"`
	State    string           `json:"state,omitempty"` // running | exited (the worker, when one is named)
	Activity string           `json:"activity,omitempty"`
	Since    time.Time        `json:"since,omitzero"`
	Stalled  bool             `json:"stalled,omitempty"`
	Options  []ActivityOption `json:"options,omitempty"` // question only
	ExitCode *int             `json:"exit_code,omitempty"`
	Report   *AgentReport     `json:"report,omitempty"`
}

// WaitDefaultTimeout is agent_wait's timeout when none is given — under
// common MCP clients' own tool timeouts, so agents loop; WaitMaxTimeout the
// cap; waitTick the settle clock (events wake the loop themselves).
const (
	WaitDefaultTimeout = 45 * time.Second
	WaitMaxTimeout     = 600 * time.Second
	waitTick           = 500 * time.Millisecond
)

// waitEvents in delivery order.
var waitEvents = []string{"report", "exit", "question", "idle"}

func parseWaitUntil(s string) (map[string]bool, error) {
	want := map[string]bool{}
	switch s {
	case "", "any":
		for _, e := range waitEvents {
			want[e] = true
		}
	case "idle", "question", "exit", "report":
		want[s] = true
	default:
		return nil, fmt.Errorf("until must be idle, question, exit, report or any (not %q)", s)
	}
	return want, nil
}

func clampWaitTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return WaitDefaultTimeout
	}
	return d
}

// AgentWait blocks until target (or, with target "", any direct child of
// caller) has a new event among until — or exits, which ends every wait —
// or timeout passes. A ctx end is an error (the client hung up), a timeout
// a normal answer.
func AgentWait(ctx context.Context, caller, target, until string, timeout time.Duration) (AgentWaitResult, error) {
	want, err := parseWaitUntil(until)
	if err != nil {
		return AgentWaitResult{}, err
	}
	timeout = clampWaitTimeout(timeout)
	if timeout > WaitMaxTimeout {
		return AgentWaitResult{}, fmt.Errorf("timeout_s is 1 … %d", int(WaitMaxTimeout.Seconds()))
	}
	candidates := func() []string { return childrenOf(caller) }
	if target != "" {
		if err := reach(caller, target); err != nil {
			return AgentWaitResult{}, err
		}
		candidates = func() []string { return []string{target} }
	} else if len(candidates()) == 0 {
		return AgentWaitResult{}, errors.New("you have no workers: nothing to wait for")
	}
	w := SessionStates()
	sch, scancel := w.Subscribe()
	defer scancel()
	mch, mcancel := Sessions().Subscribe()
	defer mcancel()
	rch, rcancel := registry().bc.Subscribe()
	defer rcancel()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	tick := time.NewTicker(waitTick)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return AgentWaitResult{}, err // a client that is gone consumes nothing
		}
		for _, id := range candidates() {
			res, ok, err := nextWaitEvent(w, caller, id, want, time.Now())
			if err != nil {
				if target != "" {
					return AgentWaitResult{}, err
				}
				continue // a child removed mid-wait
			}
			if ok {
				return res, nil
			}
		}
		select {
		case <-ctx.Done():
			return AgentWaitResult{}, ctx.Err()
		case <-timer.C:
			res := AgentWaitResult{TimedOut: true}
			if target != "" {
				res.ID = target
				if s, err := sessionOf(target); err == nil {
					res.State = sessionStateName(s.Info().State)
					if a, ok := w.Get(s.Info().ID); ok {
						res.Activity, res.Since, res.Stalled = a.Name(), a.Since, a.Stalled
					}
				}
			}
			return res, nil
		case <-sch:
		case <-mch:
		case <-rch:
		case <-tick.C:
		}
	}
}

// childrenOf: caller's direct children in start order.
func childrenOf(caller string) []string {
	listed := Sessions().List()
	r := registry()
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, in := range listed {
		full := FullSessionID(in.ID)
		if rec, ok := r.records[full]; ok && rec.Parent == caller {
			out = append(out, full)
		}
	}
	return out
}

// nextWaitEvent is the first undelivered event of id for caller, in
// delivery order; ok false when there is none. The mark is advanced under
// the registry lock so two waiters never return the same event.
func nextWaitEvent(w *StateWatcher, caller, id string, want map[string]bool, now time.Time) (AgentWaitResult, bool, error) {
	s, err := sessionOf(id)
	if err != nil {
		return AgentWaitResult{}, false, err
	}
	info := s.Info()
	res := AgentWaitResult{ID: id, State: sessionStateName(info.State)}
	a, classified := w.Get(info.ID)
	if classified {
		res.Activity, res.Since, res.Stalled = a.Name(), a.Since, a.Stalled
	}
	lastIn := s.LastInput()
	if lastIn.IsZero() {
		lastIn = info.Started
	}
	statesMu.Lock()
	settle := idleSettle
	statesMu.Unlock()

	r := registry()
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.markOf(caller, id)
	if want["report"] {
		for _, rep := range r.reports[id] {
			if rep.Seq > m.reportSeq {
				m.reportSeq = rep.Seq
				r.setMark(caller, id, m)
				res.Event, res.Report = "report", &rep
				return res, true, nil
			}
		}
	}
	// An exit ends every wait, whatever was asked: a dead worker can do
	// nothing else, and a parent waiting for its report would loop forever.
	if info.State == SessionExited && !m.exit {
		m.exit = true
		r.setMark(caller, id, m)
		code := info.ExitCode
		res.Event, res.ExitCode = "exit", &code
		return res, true, nil
	}
	if !classified || !a.Since.After(lastIn) {
		return AgentWaitResult{}, false, nil
	}
	if want["question"] && a.State == ActivityQuestion && !m.questionSince.Equal(a.Since) {
		m.questionSince = a.Since
		r.setMark(caller, id, m)
		res.Event, res.Options = "question", a.Options
		return res, true, nil
	}
	if want["idle"] && a.State == ActivityIdle && now.Sub(a.Since) >= settle && !m.idleSince.Equal(a.Since) {
		m.idleSince = a.Since
		r.setMark(caller, id, m)
		res.Event = "idle"
		return res, true, nil
	}
	return AgentWaitResult{}, false, nil
}
