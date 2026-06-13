package engine

import (
	"context"
)

// Result is the outcome of an operation.
type Result struct {
	Summary string
	Changed bool
	Path    string // when an operation creates/targets a path (e.g. CreateWorktree), its absolute path
}

// OpDeps is everything an operation needs: the repo to act on, an optional
// event channel, an optional Decider for mid-flight forks, and an optional
// hook to escalate the operation's gate reservation.
type OpDeps struct {
	Repo    GitOps
	Events  chan<- Event
	Decider Decider
	// Escalate trades the operation's gate reservation for an exclusive
	// (TreeWrite) one. Nil (direct engine use, tests) is a no-op. Call it
	// only at a boundary where the operation holds no partial state.
	Escalate func(ctx context.Context) error
}

// escalate is the nil-safe form of Escalate (style of emit/decide).
func (d OpDeps) escalate(ctx context.Context) error {
	if d.Escalate == nil {
		return nil
	}
	return d.Escalate(ctx)
}

// emit sends an event if a channel is configured. A nil channel is a no-op; a
// cancelled context aborts the send so an unconsumed channel can never block
// the operation goroutine.
func (d OpDeps) emit(ctx context.Context, e Event) {
	if d.Events == nil {
		return
	}
	select {
	case d.Events <- e:
	case <-ctx.Done():
	}
}

// decide resolves a fork. With no Decider it returns ErrDecisionRequired so a
// non-blocking caller never hangs. It also emits a DecisionNeeded event.
func (d OpDeps) decide(ctx context.Context, req DecisionRequest) (DecisionResponse, error) {
	d.emit(ctx, DecisionNeeded{Request: req})
	if d.Decider == nil {
		return DecisionResponse{}, ErrDecisionRequired
	}
	return d.Decider.Decide(ctx, req)
}

// Operation is a long-running, cancellable git workflow driven via OpDeps.
type Operation interface {
	Run(ctx context.Context, deps OpDeps) (Result, error)
}
