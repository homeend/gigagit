package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	"github.com/homeend/gigagit/internal/repogate"
)

// ExportFile writes Data to the absolute Path OUTSIDE the working tree (the
// export-a-patch primitive). It is the file-grained sibling of WriteFile: if
// Path already exists with different bytes it asks the Decider to overwrite or
// cancel; identical bytes are a silent no-op. Parent dirs are created. Writes via
// os directly (like ExportToDir), not deps.Repo.WriteWorktreeFile, because the
// destination is outside the repo. Read reservation: touches neither refs nor the
// working tree.
type ExportFile struct {
	Path string
	Data []byte
}

var _ Operation = ExportFile{}

func (op ExportFile) LockMode() repogate.Mode { return repogate.Read }

func (op ExportFile) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if existing, err := os.ReadFile(op.Path); err == nil {
		if bytes.Equal(existing, op.Data) {
			res := Result{Path: op.Path}.WithSummary("unchanged")
			deps.emit(ctx, Done{Result: res})
			return res, nil
		}
		choice, derr := deps.decide(ctx, PromptReq(
			"overwrite",
			"File exists: %s",
			[]string{writeOverwrite, writeCancel},
			op.Path,
		))
		if derr != nil {
			return Result{}, derr
		}
		if choice.Option != writeOverwrite {
			return Result{}, ErrWriteCancelled
		}
	}
	if err := os.MkdirAll(filepath.Dir(op.Path), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(op.Path, op.Data, 0o644); err != nil {
		return Result{}, err
	}
	res := Result{Changed: true, Path: op.Path}.WithSummary("wrote %s", op.Path)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
