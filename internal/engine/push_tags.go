package engine

import (
	"context"
	"fmt"
)

// PushTags pushes several tags to a remote in one git invocation. Remote must
// be set by the caller (the TUI passes "origin"); if empty, it resolves like
// PushTag (auto when one remote, else a Decider fork). Empty Names is a no-op.
type PushTags struct {
	Remote string
	Names  []string
}

func (op PushTags) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if len(op.Names) == 0 {
		return Result{Changed: false}.WithSummary("no tags to push"), nil
	}
	remote := op.Remote
	if remote == "" {
		names, err := deps.Repo.RemoteNames(ctx)
		if err != nil {
			return Result{}, fmt.Errorf("push tags: %w", err)
		}
		switch len(names) {
		case 0:
			return Result{}, fmt.Errorf("push tags: no remotes configured")
		case 1:
			remote = names[0]
		default:
			choice, derr := deps.decide(ctx, PromptReq("push-tags-remote", "Push tags to which remote?", append(append([]string{}, names...), "abort")))
			if derr != nil {
				return Result{}, derr
			}
			if choice.Option == "abort" {
				return Result{Changed: false}, nil
			}
			remote = choice.Option
		}
	}
	deps.emit(ctx, Progress{Step: "pushing tags", Detail: remote})
	if err := deps.Repo.PushTags(ctx, remote, op.Names); err != nil {
		return Result{}, fmt.Errorf("push tags: %w", err)
	}
	res := Result{Changed: true}.WithSummary("pushed tags")
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = PushTags{}
