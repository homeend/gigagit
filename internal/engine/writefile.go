package engine

import (
	"bytes"
	"context"
	"errors"
)

// WriteFile writes Data into the working tree at Path as an unstaged change.
// If Path already exists with different bytes, it asks the Decider whether to
// Overwrite or Cancel. Identical existing bytes are a no-op. TreeWrite default
// (it touches the working tree). Generic on purpose: it backs shelf restore and
// (later) bookmark paste — the "copy a file anywhere as unstaged" primitive.
type WriteFile struct {
	Path string
	Data []byte
}

var _ Operation = WriteFile{}

const (
	writeOverwrite = "overwrite"
	writeCancel    = "cancel"
)

// ErrWriteCancelled is returned when the user declines an overwrite.
var ErrWriteCancelled = errors.New("write cancelled")

func (op WriteFile) Run(ctx context.Context, deps OpDeps) (Result, error) {
	existing, err := deps.Repo.ReadWorktreeFile(ctx, op.Path)
	if err == nil {
		if bytes.Equal(existing, op.Data) {
			// Identical content already there — nothing to do.
			res := Result{Summary: "unchanged"}
			deps.emit(ctx, Done{Result: res})
			return res, nil
		}
		// Exists and differs — ask before clobbering.
		choice, derr := deps.decide(ctx, DecisionRequest{
			ID:      "overwrite",
			Prompt:  "File exists: " + op.Path,
			Options: []string{writeOverwrite, writeCancel},
		})
		if derr != nil {
			return Result{}, derr
		}
		if choice.Option != writeOverwrite {
			return Result{}, ErrWriteCancelled
		}
	}
	// Absent/unreadable, or Overwrite chosen → write.
	if err := deps.Repo.WriteWorktreeFile(ctx, op.Path, op.Data); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "wrote " + op.Path, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
