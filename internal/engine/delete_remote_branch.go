package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/repogate"
)

// DeleteRemoteBranch deletes a branch on a remote (git push <remote> --delete
// <branch>). Destructive and outward-facing, so it confirms via the Decider;
// the CLI pre-answers (the command is the confirmation). RefWrite: it mutates
// remote-tracking refs, like Prune.
type DeleteRemoteBranch struct {
	Remote string // required, e.g. "origin"
	Branch string // required, de-prefixed, e.g. "feat/x"
}

func (op DeleteRemoteBranch) LockMode() repogate.Mode { return repogate.RefWrite }

func (op DeleteRemoteBranch) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Remote == "" || op.Branch == "" {
		return Result{}, fmt.Errorf("delete remote branch: Remote and Branch are required")
	}
	ref := op.Remote + "/" + op.Branch

	confirm, err := deps.decide(ctx, PromptReq("delete-remote-branch", "Delete remote branch %s? This pushes a deletion to %s.", []string{"delete", "abort"}, ref, op.Remote))
	if err != nil {
		return Result{}, err
	}
	if confirm.Option != "delete" {
		return Result{Changed: false}.WithSummary("cancelled"), nil
	}

	deps.emit(ctx, Progress{Step: "deleting remote branch", Detail: ref})
	if err := deps.Repo.PushDelete(ctx, op.Remote, op.Branch); err != nil {
		return Result{}, fmt.Errorf("delete remote branch: %w", err)
	}

	res := Result{Changed: true}.WithSummary("deleted %s", ref)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = DeleteRemoteBranch{}
