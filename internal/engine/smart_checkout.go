package engine

import (
	"context"
	"fmt"
)

// CheckoutIntent selects whether SmartCheckout leaves the current branch (c) or
// switches to the materialized branch (s).
type CheckoutIntent int

const (
	CheckoutStay   CheckoutIntent = iota // c
	CheckoutSwitch                       // s
)

// CheckoutDivergedError is the typed refusal returned when an existing local
// branch cannot fast-forward to the remote ref. Frontends detect it with
// errors.As to offer recovery (check the remote out under a different local
// name); the rendered message is byte-identical to the old fmt.Errorf.
type CheckoutDivergedError struct{ Local, RemoteRef string }

func (e CheckoutDivergedError) Error() string {
	return fmt.Sprintf("%s has diverged from %s; cannot fast-forward", e.Local, e.RemoteRef)
}

// SmartCheckout materializes a remote-tracking branch as a local tracking
// branch, fast-forward-safe, optionally switching to it. RemoteRef is the short
// remote ref ("origin/foo"); Local is the target local name ("foo").
type SmartCheckout struct {
	RemoteRef string
	Local     string
	Intent    CheckoutIntent
}

func (op SmartCheckout) Run(ctx context.Context, deps OpDeps) (Result, error) {
	exists, err := deps.Repo.LocalBranchExists(ctx, op.Local)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		deps.emit(ctx, Progress{Step: "creating tracking branch", Detail: op.Local})
		if err := deps.Repo.CreateTrackingBranch(ctx, op.Local, op.RemoteRef); err != nil {
			return Result{}, err
		}
	} else {
		// Reuse the existing local branch only when it can fast-forward to the
		// remote ref. All refusals run before any mutation (no partial state).
		cur, err := deps.Repo.CurrentBranch(ctx)
		if err != nil {
			return Result{}, err
		}
		if cur == op.Local {
			return Result{}, fmt.Errorf("%s is the current branch; use pull to update it", op.Local)
		}
		if wt, err := deps.Repo.WorktreeForBranch(ctx, op.Local); err != nil {
			return Result{}, err
		} else if wt != nil {
			return Result{}, fmt.Errorf("%s is checked out in another worktree: %s", op.Local, wt.Path)
		}
		ff, err := deps.Repo.IsAncestor(ctx, op.Local, op.RemoteRef)
		if err != nil {
			return Result{}, err
		}
		if !ff {
			return Result{}, CheckoutDivergedError{Local: op.Local, RemoteRef: op.RemoteRef}
		}
		deps.emit(ctx, Progress{Step: "fast-forwarding", Detail: op.Local})
		if err := deps.Repo.FastForwardToRef(ctx, op.Local, "refs/remotes/"+op.RemoteRef); err != nil {
			return Result{}, err
		}
	}

	if op.Intent == CheckoutSwitch {
		// Reuse SmartSwitch for autostash + the stash-pop-conflict decision. Run
		// inline (shared deps) — never via domain.Execute, which would take a
		// nested repogate reservation under the one we already hold.
		return SmartSwitch{Branch: op.Local}.Run(ctx, deps)
	}
	return Result{Summary: "checked out " + op.RemoteRef + " as " + op.Local, Changed: true}, nil
}

var _ Operation = SmartCheckout{}
