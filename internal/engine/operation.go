package engine

import (
	"context"
)

// Result is the outcome of an operation.
type Result struct {
	Summary string
	// SummaryParts is the localizable channel for Summary: English format
	// strings (doubling as i18n catalog keys) plus args. Built ONLY via
	// WithSummary/AppendSummary so the channels stay in lockstep. Empty =
	// frontends render the English Summary verbatim.
	SummaryParts []Msg
	Changed      bool
	Path         string // when an operation creates/targets a path (e.g. CreateWorktree), its absolute path
	// Captured is the captured stdout, set only by capture ops like GenerateMessage.
	Captured string
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
	// HookRunner runs a post-create worktree hook. Nil ⇒ ShellHookRunner{}
	// (production default); engine tests inject a fake.
	HookRunner HookRunner
	// CaptureRunner runs a headless capture command. Nil ⇒ ShellCaptureRunner{}
	// (production default); engine tests inject a fake.
	CaptureRunner CaptureRunner
}

// hookRunner is the nil-safe HookRunner (style of emit/escalate).
func (d OpDeps) hookRunner() HookRunner {
	if d.HookRunner == nil {
		return ShellHookRunner{}
	}
	return d.HookRunner
}

// captureRunner is the nil-safe CaptureRunner (style of hookRunner).
func (d OpDeps) captureRunner() CaptureRunner {
	if d.CaptureRunner == nil {
		return ShellCaptureRunner{}
	}
	return d.CaptureRunner
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
