package engine

import "context"

// Commit stages (optionally) and commits with a message.
type Commit struct {
	Message string
	All     bool
}

func (op Commit) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(Progress{Step: "committing", Detail: op.Message})
	if err := deps.Repo.Commit(ctx, op.Message, op.All); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "committed", Changed: true}
	deps.emit(Done{Result: res})
	return res, nil
}

// Push pushes branch to remote, optionally setting upstream.
type Push struct {
	Remote      string
	Branch      string
	SetUpstream bool
}

func (op Push) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(Progress{Step: "pushing", Detail: op.Remote + " " + op.Branch})
	if err := deps.Repo.Push(ctx, op.Remote, op.Branch, op.SetUpstream); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "pushed", Changed: true}
	deps.emit(Done{Result: res})
	return res, nil
}

// Stash saves the working tree to a new stash.
type Stash struct {
	Message string
}

func (op Stash) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(Progress{Step: "stashing", Detail: op.Message})
	if err := deps.Repo.StashPush(ctx, op.Message); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "stashed", Changed: true}
	deps.emit(Done{Result: res})
	return res, nil
}

// Compile-time checks that these satisfy Operation.
var (
	_ Operation = Commit{}
	_ Operation = Push{}
	_ Operation = Stash{}
)
