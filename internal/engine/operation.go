package engine

import (
	"context"

	"github.com/gigagit/gg/internal/git"
)

// Result is the outcome of an operation.
type Result struct {
	Summary string
	Changed bool
}

// OpDeps is everything an operation needs: the repo to act on, an optional
// event channel, and an optional Decider for mid-flight forks.
type OpDeps struct {
	Repo    *git.Repo
	Events  chan<- Event
	Decider Decider
}

// emit sends an event if a channel is configured; a nil channel is a no-op.
func (d OpDeps) emit(e Event) {
	if d.Events != nil {
		d.Events <- e
	}
}

// decide resolves a fork. With no Decider it returns ErrDecisionRequired so a
// non-blocking caller never hangs. It also emits a DecisionNeeded event.
func (d OpDeps) decide(ctx context.Context, req DecisionRequest) (DecisionResponse, error) {
	d.emit(DecisionNeeded{Request: req})
	if d.Decider == nil {
		return DecisionResponse{}, ErrDecisionRequired
	}
	return d.Decider.Decide(ctx, req)
}

// Operation is a long-running, cancellable git workflow driven via OpDeps.
type Operation interface {
	Run(ctx context.Context, deps OpDeps) (Result, error)
}
