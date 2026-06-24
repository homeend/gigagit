package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/repogate"
)

// DeleteRemoteTag deletes a tag on a remote (git push <remote> --delete
// refs/tags/<tag>). The remote is resolved like PushTag (auto for one, else a
// Decider pick); then a confirm gates the push (destructive + outward-facing).
// The CLI pre-answers the confirm. RefWrite: mutates remote refs.
type DeleteRemoteTag struct {
	Tag    string // required
	Remote string // "" = resolve (auto when one remote, else Decider)
}

func (op DeleteRemoteTag) LockMode() repogate.Mode { return repogate.RefWrite }

func (op DeleteRemoteTag) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Tag == "" {
		return Result{}, fmt.Errorf("delete remote tag: Tag is required")
	}
	remote := op.Remote
	if remote == "" {
		names, err := deps.Repo.RemoteNames(ctx)
		if err != nil {
			return Result{}, fmt.Errorf("delete remote tag: %w", err)
		}
		switch len(names) {
		case 0:
			return Result{}, fmt.Errorf("delete remote tag: no remotes configured")
		case 1:
			remote = names[0]
		default:
			choice, derr := deps.decide(ctx, DecisionRequest{
				ID:      "delete-remote-tag-remote",
				Prompt:  "Delete " + op.Tag + " from which remote?",
				Options: append(append([]string{}, names...), "abort"),
			})
			if derr != nil {
				return Result{}, derr
			}
			if choice.Option == "abort" {
				return Result{Changed: false}, nil
			}
			remote = choice.Option
		}
	}

	confirm, err := deps.decide(ctx, DecisionRequest{
		ID:      "delete-remote-tag",
		Prompt:  "Delete tag " + op.Tag + " from " + remote + "? This pushes a deletion to " + remote + ".",
		Options: []string{"delete", "abort"},
	})
	if err != nil {
		return Result{}, err
	}
	if confirm.Option != "delete" {
		return Result{Summary: "cancelled", Changed: false}, nil
	}

	deps.emit(ctx, Progress{Step: "deleting remote tag", Detail: op.Tag + " ← " + remote})
	if err := deps.Repo.PushDeleteTag(ctx, remote, op.Tag); err != nil {
		return Result{}, fmt.Errorf("delete remote tag: %w", err)
	}
	res := Result{Summary: "deleted " + op.Tag + " from " + remote, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = DeleteRemoteTag{}
