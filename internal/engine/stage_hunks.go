package engine

import "context"

// StageHunks sets the index entry for Path to exactly Content (assembled by the
// TUI staging picker via internal/hunkpick), leaving the working tree
// untouched. Default exclusive (TreeWrite) reservation.
type StageHunks struct {
	Path    string
	Content []byte
}

func (op StageHunks) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "staging", Detail: op.Path})
	if err := deps.Repo.StageBlob(ctx, op.Path, op.Content); err != nil {
		return Result{}, err
	}
	res := Result{Changed: true}.WithSummary("staged hunks in %s", op.Path)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = StageHunks{}
