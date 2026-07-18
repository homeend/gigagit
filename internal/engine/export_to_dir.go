package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repogate"
)

// ExportToDir writes Files under Dir at absolute paths OUTSIDE the working
// tree (the copy-to-temp-dir primitive). Each file's RelPath is joined onto
// Dir with parent dirs created, and written via the os package directly —
// this is the first op that writes outside the worktree, so it does not use
// deps.Repo.WriteWorktreeFile. If Dir already exists it asks the Decider to
// overwrite or cancel. Read reservation: it touches neither git refs nor the
// working tree.
type ExportToDir struct {
	Dir   string
	Files []model.ExportFile
}

var _ Operation = ExportToDir{}

// ErrExportCancelled is returned when the user declines to overwrite an
// existing target directory.
var ErrExportCancelled = errors.New("export cancelled")

func (op ExportToDir) LockMode() repogate.Mode { return repogate.Read }

func (op ExportToDir) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if _, err := os.Stat(op.Dir); err == nil {
		choice, derr := deps.decide(ctx, PromptReq(
			"overwrite",
			"Directory exists: %s",
			[]string{writeOverwrite, writeCancel},
			op.Dir,
		))
		if derr != nil {
			return Result{}, derr
		}
		if choice.Option != writeOverwrite {
			return Result{}, ErrExportCancelled
		}
	}
	if err := os.MkdirAll(op.Dir, 0o755); err != nil {
		return Result{}, err
	}
	for i, f := range op.Files {
		full := filepath.Join(op.Dir, filepath.Clean(f.RelPath))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return Result{}, err
		}
		if err := os.WriteFile(full, f.Data, 0o644); err != nil {
			return Result{}, err
		}
		deps.emit(ctx, Progressf("export", "wrote %s (%d/%d)", f.RelPath, i+1, len(op.Files)))
	}
	res := Result{Changed: true, Path: op.Dir}.WithSummary("exported %d file(s) to %s", len(op.Files), op.Dir)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
