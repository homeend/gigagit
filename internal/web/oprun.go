package web

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/domain"
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

// runFunc is the body of one run lane. Everything the transport provides —
// the severed context, the pinned service, the event channel and the parking
// decider — arrives as arguments, so a lane is free to be something other
// than a bare Execute: the review lane calls domain.ReviewReport, which owns
// persistence as well as the op. The extra map is merged into the terminal
// done event, which is how a review's report reaches the browser.
type runFunc func(ctx context.Context, svc *domain.Service, events chan<- engine.Event, dec engine.Decider) (engine.Result, map[string]any, error)

type opRun struct {
	id string
	// kind distinguishes lanes for the endpoints that must not apply to all
	// of them — only a review may be cancelled mid-flight.
	kind   string
	cancel context.CancelFunc

	mu        sync.Mutex
	history   []wireEvent
	subs      map[chan wireEvent]struct{}
	pending   *engine.DecisionRequest // non-nil while parked on deps.decide
	answer    chan string
	done      bool
	cancelled bool // set by requestCancel, so the done event says so
}

// startOp begins op in a background goroutine. Exactly one op may be live;
// the previous (finished) run's record is kept for late SSE reads until the
// next start replaces it.
func (s *Server) startOp(op engine.Operation) (*opRun, error) {
	return s.startRun("op", func(ctx context.Context, svc *domain.Service, events chan<- engine.Event, dec engine.Decider) (engine.Result, map[string]any, error) {
		res, err := svc.Execute(ctx, op, events, dec)
		return res, nil, err
	})
}

// startRun is the shared lane starter behind startOp and the review lane.
func (s *Server) startRun(kind string, fn runFunc) (*opRun, error) {
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
		kind:   kind,
		cancel: cancel,
		subs:   make(map[chan wireEvent]struct{}),
		answer: make(chan string, 1),
	}
	s.cur = run
	go s.runOpStream(ctx, run, fn)
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

// resetFeed drops the cached CommitFeed so the next /api/commits rebuilds it
// from scratch. Required when something the BUILD depends on changed — the
// commit-sort mode (settings), the solo scope, or the repo itself (re-root).
// A state-changing op does NOT need it: the next reload reconciles page 0 into
// the accumulation and re-derives the list from the new refs, so dropping the
// feed there would only discard the pages the browser scrolled in.
func (s *Server) resetFeed() {
	s.mu.Lock()
	s.feed = nil
	s.mu.Unlock()
}

func (s *Server) runOpStream(ctx context.Context, run *opRun, fn runFunc) {
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
	res, extra, err := fn(ctx, svc, events, webDecider{run: run, timeout: timeout, srv: s})
	close(events)
	<-pumpDone
	// No resetFeed here: the client reloads /api/commits after a changing op,
	// and that reload reconciles — a commit prepends, a moved HEAD trims or
	// re-walks — so the feed follows the new state without losing depth.
	done := wireEvent{"type": "done", "ok": err == nil, "changed": res.Changed, "summary": res.Summary}
	if err != nil {
		// A context-killed subprocess reports its signal, not context.Canceled
		// (the TUI's review lane learned the same), so the deliberate-cancel
		// case is recognised by the flag requestCancel set — never by the
		// error's type or text.
		if run.wasCancelled() {
			done["error"] = "cancelled"
			done["cancelled"] = true
		} else {
			done["error"] = err.Error()
		}
	}
	for k, v := range extra {
		done[k] = v
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
	r.publishLocked(we)
}

// publishLocked is publish for callers already holding r.mu — decide must
// append its resolved marker atomically with consuming the pending fork.
func (r *opRun) publishLocked(we wireEvent) {
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
		// The resolved marker makes replay idempotent (a reconnecting
		// client re-hides the modal it just re-showed) and closes a second
		// tab's modal live.
		r.publishLocked(wireEvent{"type": "resolved"})
		return nil
	default:
		return errNotWaiting // answer already queued
	}
}

// requestCancel cancels a live run's context. Only the review lane exposes
// this (see handleOpCancel): an agent that hangs would otherwise hold the
// single lane — and the whole UI — until the process gave up on its own,
// whereas cancelling a git operation part-way is a different question with a
// different answer.
func (r *opRun) requestCancel() error {
	r.mu.Lock()
	if r.done {
		r.mu.Unlock()
		return errOpDone
	}
	r.cancelled = true
	r.mu.Unlock()
	r.cancel()
	return nil
}

func (r *opRun) wasCancelled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cancelled
}

func (r *opRun) setPending(req *engine.DecisionRequest) {
	r.mu.Lock()
	r.pending = req
	r.mu.Unlock()
}

// AutoAnswer decides a fork WITHOUT asking the browser. It returns the option
// to take and true to claim the decision, or false to let it park in the modal
// as usual. It exists so a feature can honour a remembered "don't ask me this
// again" from its own file: a suppressed prompt has to be answered by
// somebody, and only the server-side decider can answer one with no client in
// the loop. The option MUST be one of req.Options — an answer outside the list
// would break the project-wide option-list contract.
type AutoAnswer func(s *Server, req engine.DecisionRequest) (string, bool)

var autoAnswers []AutoAnswer

// RegisterAutoAnswer adds a policy consulted before every fork parks.
func RegisterAutoAnswer(fn AutoAnswer) {
	if fn == nil {
		panic("web: RegisterAutoAnswer with a nil func")
	}
	autoAnswers = append(autoAnswers, fn)
}

// autoAnswer returns the first registered policy's claim on req.
func autoAnswer(s *Server, req engine.DecisionRequest) (string, bool) {
	for _, fn := range autoAnswers {
		if opt, ok := fn(s, req); ok && slices.Contains(req.Options, opt) {
			return opt, true
		}
	}
	return "", false
}

// webDecider parks the op until the decide endpoint answers, the op context
// dies, or the timeout fires — an abandoned browser modal must never wedge
// the repo gate. The DecisionNeeded event was already emitted by
// deps.decide before this is called.
type webDecider struct {
	run     *opRun
	timeout time.Duration
	srv     *Server // for the auto-answer policies
}

func (d webDecider) Decide(ctx context.Context, req engine.DecisionRequest) (engine.DecisionResponse, error) {
	d.run.setPending(&req)
	defer d.run.setPending(nil)
	// A remembered answer is fed through the SAME path a browser answer takes,
	// after the fork is parked rather than instead of parking it. That ordering
	// is deliberate: the DecisionNeeded event has already gone out, so the
	// browser may be answering this very moment, and whichever answer arrives
	// first wins while the other becomes a no-op. decide also publishes the
	// "resolved" marker, which closes the modal the client just opened.
	if opt, ok := autoAnswer(d.srv, req); ok {
		_ = d.run.decide(opt)
	}
	select {
	case opt := <-d.run.answer:
		return engine.DecisionResponse{Option: opt}, nil
	case <-ctx.Done():
		return engine.DecisionResponse{}, ctx.Err()
	case <-time.After(d.timeout):
		return engine.DecisionResponse{}, fmt.Errorf("decision %q timed out (no answer from the browser)", req.ID)
	}
}
