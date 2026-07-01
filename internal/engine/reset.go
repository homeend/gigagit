package engine

import (
	"context"
	"fmt"
)

// Reset moves the current branch to Commit. The mode is chosen mid-flight via
// the "reset-mode" decision (soft/mixed/hard/cancel); in the TUI that modal is
// the deliberate confirmation. Because the Commits panel is the multi-branch
// feed, the target may not be on the current branch: when it is NOT an ancestor
// of HEAD, a second "reset-confirm" decision warns before moving the branch to
// an unrelated commit. Reset never conflicts, so there is no conflict path.
type Reset struct {
	Commit string
	// Mode, when non-empty ("soft"/"mixed"/"hard"), presets the reset mode and
	// SKIPS both the reset-mode picker and the non-ancestor confirm: the caller
	// has already decided and owns the confirmation (e.g. the TUI's confirmOp
	// before "reset current branch to the remote tip"). Empty Mode keeps the
	// interactive soft/mixed/hard/cancel decision flow.
	Mode string
}

var _ Operation = Reset{}

func (op Reset) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Commit == "" {
		return Result{}, fmt.Errorf("reset: Commit is required")
	}

	mode := op.Mode
	if mode == "" {
		modeChoice, err := deps.decide(ctx, DecisionRequest{
			ID:      "reset-mode",
			Prompt:  "Reset the current branch to " + op.Commit,
			Options: []string{"soft", "mixed", "hard", "cancel"},
		})
		if err != nil {
			return Result{}, err
		}
		mode = modeChoice.Option
		switch mode {
		case "soft", "mixed", "hard":
			// proceed
		case "cancel", "":
			return Result{Summary: "reset cancelled", Changed: false}, nil
		default:
			return Result{}, fmt.Errorf("reset: unknown mode %q", mode)
		}

		// Non-ancestor guard (interactive path, all modes): the target may be on
		// another branch. A preset Mode skips this — its caller already confirmed.
		anc, err := deps.Repo.IsAncestor(ctx, op.Commit, "HEAD")
		if err != nil {
			return Result{}, err
		}
		if !anc {
			confirm, derr := deps.decide(ctx, DecisionRequest{
				ID:      "reset-confirm",
				Prompt:  "Commit " + op.Commit + " is not on the current branch; reset will move the branch onto it",
				Options: []string{"proceed", "cancel"},
			})
			if derr != nil {
				return Result{}, derr
			}
			if confirm.Option != "proceed" {
				return Result{Summary: "reset cancelled", Changed: false}, nil
			}
		}
	} else {
		switch mode {
		case "soft", "mixed", "hard":
			// proceed with the caller-supplied mode
		default:
			return Result{}, fmt.Errorf("reset: unknown mode %q", mode)
		}
	}

	deps.emit(ctx, Progress{Step: "resetting", Detail: mode + " → " + op.Commit})
	if err := deps.Repo.Reset(ctx, mode, op.Commit); err != nil {
		return Result{}, fmt.Errorf("reset --%s %s: %w", mode, op.Commit, err)
	}
	res := Result{Summary: "reset (" + mode + ") to " + op.Commit, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
