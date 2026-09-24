package engine

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/git"
)

// StageHunks sets the index entry for Path to exactly Content (assembled by the
// TUI staging picker via internal/hunkpick), leaving the working tree
// untouched — or, with Files, every listed entry at once (the web stages a
// selection that spans files in one request). Default exclusive (TreeWrite)
// reservation.
type StageHunks struct {
	Path    string
	Content []byte
	Files   []StagedFile
}

// StagedFile is one file's new index content in a StageHunks batch.
type StagedFile struct {
	Path    string
	Content []byte
}

// IndexOnly: staging never moves a ref, so it never records a branch version
// and Execute skips the versions probe (a git process per op).
func (StageHunks) IndexOnly() {}

func (op StageHunks) Run(ctx context.Context, deps OpDeps) (Result, error) {
	var blobs []git.Blob
	if op.Path != "" {
		blobs = append(blobs, git.Blob{Path: op.Path, Content: op.Content})
	}
	for _, f := range op.Files {
		blobs = append(blobs, git.Blob{Path: f.Path, Content: f.Content})
	}
	paths := make([]string, len(blobs))
	for i, b := range blobs {
		paths[i] = b.Path
	}
	which := strings.Join(paths, ", ")
	deps.emit(ctx, Progress{Step: "staging", Detail: which})
	if err := deps.Repo.StageBlobs(ctx, blobs); err != nil {
		return Result{}, err
	}
	res := Result{Changed: true}.WithSummary("staged hunks in %s", which)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = StageHunks{}
