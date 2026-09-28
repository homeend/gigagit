package engine

import (
	"context"
	"errors"

	"github.com/homeend/gigagit/internal/model"
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
	// RepoAt returns a GitOps view acting on ANOTHER worktree of this
	// repository (git -C <dir>), for ops that recycle or inspect a worktree
	// gg is not running in. Nil (direct engine use, fakes) makes repoAt
	// return ErrNoRepoAt — an op that needs it fails cleanly instead of
	// acting on the wrong tree. domain.Execute wires it to *git.Repo.InDir.
	RepoAt func(dir string) GitOps
	// ShelveStaged freezes the INDEX of the worktree at dir (every path that
	// differs from HEAD) into one shelf file set labelled "WIP on <branch>",
	// and annotates it with a note on the entry when deletions or renames
	// cannot be carried by the set. The shelf is domain-owned, hence a seam;
	// it runs under the op's reservation, so it must not re-enter the gate.
	// Nil makes shelveStaged return ErrNoShelve. domain.Execute wires it.
	ShelveStaged func(ctx context.Context, dir, branch string) (model.ShelfEntry, error)
	// Versions governs pre-operation branch-version snapshots (see
	// snapshotBranchTip). Zero value = disabled.
	Versions VersionsPolicy
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

// ErrNoRepoAt is returned by repoAt when OpDeps carries no RepoAt seam.
var ErrNoRepoAt = errors.New("this repository handle cannot act on another worktree")

// ErrNoShelve is returned by shelveStaged when OpDeps carries no ShelveStaged seam.
var ErrNoShelve = errors.New("this repository handle cannot shelve")

// shelveStaged is the nil-safe form of ShelveStaged.
func (d OpDeps) shelveStaged(ctx context.Context, dir, branch string) (model.ShelfEntry, error) {
	if d.ShelveStaged == nil {
		return model.ShelfEntry{}, ErrNoShelve
	}
	return d.ShelveStaged(ctx, dir, branch)
}

// repoAt is the nil-safe form of RepoAt (style of hookRunner).
func (d OpDeps) repoAt(dir string) (GitOps, error) {
	if d.RepoAt == nil {
		return nil, ErrNoRepoAt
	}
	return d.RepoAt(dir), nil
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
