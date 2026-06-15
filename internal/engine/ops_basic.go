package engine

import "context"

// Commit stages (optionally) and commits with a message. Amend rewrites the
// last commit instead of creating a new one.
type Commit struct {
	Message string
	All     bool
	Amend   bool
}

func (op Commit) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "committing", Detail: op.Message})
	if err := deps.Repo.Commit(ctx, op.Message, op.All, op.Amend); err != nil {
		return Result{}, err
	}
	summary := "committed"
	if op.Amend {
		summary = "amended"
	}
	res := Result{Summary: summary, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// Push pushes branch to remote, optionally setting upstream.
type Push struct {
	Remote      string
	Branch      string
	SetUpstream bool
}

func (op Push) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "pushing", Detail: op.Remote + " " + op.Branch})
	if err := deps.Repo.Push(ctx, op.Remote, op.Branch, op.SetUpstream); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "pushed", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// Stash saves the working-tree changes for Paths (all when empty) to a new stash.
type Stash struct {
	Message          string
	Paths            []string
	IncludeUntracked bool
}

func (op Stash) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "stashing", Detail: op.Message})
	if err := deps.Repo.StashPush(ctx, op.Message, op.Paths, op.IncludeUntracked); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "stashed: " + op.Message, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// StashApply restores a stash, keeping it in the list.
type StashApply struct{ Ref string }

func (op StashApply) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "applying stash", Detail: op.Ref})
	if err := deps.Repo.StashApply(ctx, op.Ref); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "applied " + op.Ref, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// StashPop restores a stash and drops it.
type StashPop struct{ Ref string }

func (op StashPop) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "popping stash", Detail: op.Ref})
	if err := deps.Repo.StashPop(ctx, op.Ref); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "popped " + op.Ref, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// StashDrop deletes a stash without applying it.
type StashDrop struct{ Ref string }

func (op StashDrop) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "dropping stash", Detail: op.Ref})
	if err := deps.Repo.StashDrop(ctx, op.Ref); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "dropped " + op.Ref, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// Compile-time checks that these satisfy Operation.
var (
	_ Operation = Commit{}
	_ Operation = Push{}
	_ Operation = Stash{}
	_ Operation = StashApply{}
	_ Operation = StashPop{}
	_ Operation = StashDrop{}
)
