package engine

import "context"

// ResolveConflictHunks writes a hunk-resolved file to the working tree and
// stages it, clearing the unmerged index entry. Content is assembled by the
// frontend (the TUI picker) via internal/hunkpick. Runs with the default
// exclusive (TreeWrite) reservation.
type ResolveConflictHunks struct {
	Path    string
	Content []byte
}

func (op ResolveConflictHunks) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "resolving", Detail: op.Path})
	if err := deps.Repo.WriteWorktreeFile(ctx, op.Path, op.Content); err != nil {
		return Result{}, err
	}
	if err := deps.Repo.StagePaths(ctx, []string{op.Path}); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "resolved " + op.Path + " (hunks)", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = ResolveConflictHunks{}
