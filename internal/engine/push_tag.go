package engine

import (
	"context"
	"fmt"
)

// PushTag pushes Name to a remote. Remote "" is resolved: one configured remote
// is used automatically; multiple remotes fork through the push-tag-remote
// Decider (remote names are a fixed option list). An "abort" choice cancels.
type PushTag struct {
	Name   string // tag (required)
	Remote string // "" = resolve (auto when one remote, else Decider)
}

func (op PushTag) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Name == "" {
		return Result{}, fmt.Errorf("push tag: Name is required")
	}
	remote := op.Remote
	if remote == "" {
		names, err := deps.Repo.RemoteNames(ctx)
		if err != nil {
			return Result{}, fmt.Errorf("push tag: %w", err)
		}
		switch len(names) {
		case 0:
			return Result{}, fmt.Errorf("push tag: no remotes configured")
		case 1:
			remote = names[0]
		default:
			choice, derr := deps.decide(ctx, PromptReq("push-tag-remote", "Push %s to which remote?", append(append([]string{}, names...), "abort"), op.Name))
			if derr != nil {
				return Result{}, derr
			}
			if choice.Option == "abort" {
				return Result{Changed: false}, nil
			}
			remote = choice.Option
		}
	}
	deps.emit(ctx, Progress{Step: "pushing tag", Detail: op.Name + " → " + remote})
	if err := deps.Repo.PushTag(ctx, remote, op.Name); err != nil {
		return Result{}, fmt.Errorf("push tag: %w", err)
	}
	res := Result{Changed: true}.WithSummary("pushed %s to %s", op.Name, remote)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = PushTag{}
