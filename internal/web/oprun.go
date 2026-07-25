package web

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/engine"
)

// The op transport's server half: one live operation at a time streams its
// engine events into a replayable buffer fanned out to SSE subscribers, and
// parks deps.decide forks on a channel the decide endpoint feeds.

var (
	errOpBusy     = errors.New("an operation is already running")
	errBadOption  = errors.New("option not in the pending decision's list")
	errNotWaiting = errors.New("operation is not waiting on a decision")
	errOpDone     = errors.New("operation already finished")
)

const defaultDecideTimeout = 5 * time.Minute

// wireEvent is one SSE message, already shaped for the client.
type wireEvent map[string]any

type opRun struct {
	id     string
	cancel context.CancelFunc

	mu      sync.Mutex
	history []wireEvent
	subs    map[chan wireEvent]struct{}
	pending *engine.DecisionRequest // non-nil while parked on deps.decide
	answer  chan string
	done    bool
}

// startOp begins op in a background goroutine. Exactly one op may be live;
// the previous (finished) run's record is kept for late SSE reads until the
// next start replaces it.
func (s *Server) startOp(op engine.Operation) (*opRun, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.cur != nil {
		s.cur.mu.Lock()
		live := !s.cur.done
		s.cur.mu.Unlock()
		if live {
			return nil, errOpBusy
		}
	}
	s.opSeq++
	// The op must survive the HTTP request that started it: its context is
	// severed from the request and cancelled only when the run finishes.
	ctx, cancel := context.WithCancel(context.Background())
	run := &opRun{
		id:     fmt.Sprintf("op%d", s.opSeq),
		cancel: cancel,
		subs:   make(map[chan wireEvent]struct{}),
		answer: make(chan string, 1),
	}
	s.cur = run
	go s.runOpStream(ctx, run, op)
	return run, nil
}

func (s *Server) opByID(id string) *opRun {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.cur != nil && s.cur.id == id {
		return s.cur
	}
	return nil
}

// resetFeed drops the cached CommitFeed so the next /api/commits rebuilds —
// required after any op that changed state (e.g. a switch moved HEAD; the
// old feed would keep serving the previous branch's commits).
func (s *Server) resetFeed() {
	s.mu.Lock()
	s.feed = nil
	s.mu.Unlock()
}

func (s *Server) runOpStream(ctx context.Context, run *opRun, op engine.Operation) {
	svc := s.service() // pinned: the op runs against the repo it started on
	events := make(chan engine.Event, 32)
	pumpDone := make(chan struct{})
	go func() {
		for ev := range events {
			if we := toWire(ev); we != nil {
				run.publish(we)
			}
		}
		close(pumpDone)
	}()
	timeout := s.decideTimeout
	if timeout <= 0 {
		timeout = defaultDecideTimeout
	}
	res, err := svc.Execute(ctx, op, events, webDecider{run: run, timeout: timeout})
	close(events)
	<-pumpDone
	if res.Changed {
		s.resetFeed()
	}
	done := wireEvent{"type": "done", "ok": err == nil, "changed": res.Changed, "summary": res.Summary}
	if err != nil {
		done["error"] = err.Error()
	}
	run.finish(done)
	run.cancel()
}

// toWire maps an engine event to its SSE shape. engine.Done and Timing are
// dropped: done is synthesized from Execute's return (forwarding both would
// double-fire), Timing is observability.
func toWire(ev engine.Event) wireEvent {
	switch e := ev.(type) {
	case engine.Progress:
		return wireEvent{"type": "progress", "step": e.Step, "detail": e.Detail}
	case engine.GitLine:
		return wireEvent{"type": "gitline", "raw": e.Raw}
	case engine.DecisionNeeded:
		return wireEvent{"type": "decision", "id": e.Request.ID, "prompt": e.Request.Prompt, "options": e.Request.Options}
	}
	return nil
}

// publish appends to the replay buffer and fans out to live subscribers.
// A subscriber whose buffer is full drops the event (probe-tier; the
// replay-on-attach path is the correctness backstop).
func (r *opRun) publish(we wireEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.history = append(r.history, we)
	for ch := range r.subs {
		select {
		case ch <- we:
		default:
		}
	}
}

// finish publishes the terminal event, marks the run done, and closes all
// subscriber channels.
func (r *opRun) finish(done wireEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.history = append(r.history, done)
	for ch := range r.subs {
		select {
		case ch <- done:
		default:
		}
		close(ch)
	}
	r.subs = make(map[chan wireEvent]struct{})
	r.done = true
}

// subscribe returns a copy of the history so far plus a live channel (nil
// when the run already finished — the history then ends with done).
func (r *opRun) subscribe() ([]wireEvent, chan wireEvent, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	history := slices.Clone(r.history)
	if r.done {
		return history, nil, func() {}
	}
	ch := make(chan wireEvent, 64)
	r.subs[ch] = struct{}{}
	cancel := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if _, ok := r.subs[ch]; ok {
			delete(r.subs, ch)
			close(ch)
		}
	}
	return history, ch, cancel
}

// decide feeds a parked webDecider. Option must be one of the pending
// request's options — decisions are option-lists only, project-wide.
func (r *opRun) decide(option string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return errOpDone
	}
	if r.pending == nil {
		return errNotWaiting
	}
	if !slices.Contains(r.pending.Options, option) {
		return errBadOption
	}
	select {
	case r.answer <- option:
		r.pending = nil // consumed: a second decide must 409, and no stale answer can outlive its fork
		return nil
	default:
		return errNotWaiting // answer already queued
	}
}

func (r *opRun) setPending(req *engine.DecisionRequest) {
	r.mu.Lock()
	r.pending = req
	r.mu.Unlock()
}

// webDecider parks the op until the decide endpoint answers, the op context
// dies, or the timeout fires — an abandoned browser modal must never wedge
// the repo gate. The DecisionNeeded event was already emitted by
// deps.decide before this is called.
type webDecider struct {
	run     *opRun
	timeout time.Duration
}

func (d webDecider) Decide(ctx context.Context, req engine.DecisionRequest) (engine.DecisionResponse, error) {
	d.run.setPending(&req)
	defer d.run.setPending(nil)
	select {
	case opt := <-d.run.answer:
		return engine.DecisionResponse{Option: opt}, nil
	case <-ctx.Done():
		return engine.DecisionResponse{}, ctx.Err()
	case <-time.After(d.timeout):
		return engine.DecisionResponse{}, fmt.Errorf("decision %q timed out (no answer from the browser)", req.ID)
	}
}
