package domain

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// ReviewFileDiff is one file of a review as the change the review covered:
// what the overview split's right column shows when D flips it (spec
// 2026-10-11-overview-split-diff §3).
type ReviewFileDiff struct {
	Diff Diff
	// Changed is false when the review's range leaves the file alone: no
	// diff was read and Diff is empty.
	Changed bool
	// Status is the file's letter in the review (A M D R …), OldPath a
	// rename's or copy's old path ("" otherwise).
	Status, OldPath string
	// Left and Right label the two sides: short hashes ("empty" for a root
	// commit's missing parent), or HEAD / working tree for a working review.
	Left, Right string
	// Notes are the review's remarks on both sides of Diff (an old-side one
	// on the line the review deleted); none for a binary or too-large diff.
	Notes []ResolvedNote
}

// ReviewFileDiff reads path's change in review reviewID: the left end of
// the review's STORED range (a single commit's parent) against its tip, or
// for a working review HEAD against the file on disk. The sides are
// reviewRevs', the status and old path ReviewFiles', so the diff is the one
// the review view lists for the file. ErrReviewNotFound when it is gone.
func (s *Service) ReviewFileDiff(ctx context.Context, reviewID, path string) (ReviewFileDiff, error) {
	r, err := s.Review(ctx, reviewID)
	if err != nil {
		return ReviewFileDiff{}, err
	}
	path = reviewPath(path)
	base, tip, _ := s.reviewRevs(ctx, r)
	working := r.Kind == ReviewOnWorktree
	out := ReviewFileDiff{Left: "HEAD", Right: "working tree"}
	if !working {
		out.Left, out.Right = "empty", reviewShort(tip)
		if h, ok, err := s.ResolveRev(ctx, base); err == nil && ok {
			base = strings.TrimSpace(h)
			out.Left = reviewShort(base)
		}
	}
	files, err := s.ReviewFiles(ctx, r)
	if err != nil {
		return ReviewFileDiff{}, err
	}
	var f *model.CommitFile
	for i := range files {
		if reviewPath(files[i].Path) == path {
			f = &files[i]
			break
		}
	}
	if f == nil {
		return out, nil
	}
	out.Changed, out.Status, out.OldPath = true, f.Status, f.OldPath
	oldPath := path
	if f.OldPath != "" {
		oldPath = reviewPath(f.OldPath)
	}
	var oldSrc, newSrc ByteSource
	if f.Status != "A" && out.Left != "empty" {
		oldSrc = absentOnError(func(ctx context.Context) ([]byte, error) { return s.ShowFile(ctx, base, oldPath) })
	}
	if f.Status != "D" {
		if working {
			newSrc = absentOnError(func(ctx context.Context) ([]byte, error) {
				return s.ResolveBytes(ctx, model.FileRef{Source: model.SourceUnstaged, Path: path})
			})
		} else {
			newSrc = absentOnError(func(ctx context.Context) ([]byte, error) { return s.ShowFile(ctx, tip, path) })
		}
	}
	key, rel := "", "" // the working tree moves under one key: never cached
	if !working {
		key = "review-diff:" + base + ".." + tip + ":" + oldPath + ">" + path
	}
	if oldPath != path {
		rel = oldPath
	}
	d, err := s.Differ().Diff(ctx, Request{Key: key, Path: path, OldPath: rel, Old: oldSrc, New: newSrc})
	if err != nil {
		return ReviewFileDiff{}, err
	}
	out.Diff = d
	if !d.Binary && !d.TooLarge {
		out.Notes, _ = s.ReviewNotesFor(ctx, reviewID, path, d)
	}
	return out, nil
}

// absentOnError reads a side that may not exist (an untracked file has no
// HEAD version, a listed file may be gone from disk) as absent instead of
// failing the diff; a cancelled read stays an error.
func absentOnError(read ByteSource) ByteSource {
	return func(ctx context.Context) ([]byte, error) {
		b, err := read(ctx)
		if err != nil && ctx.Err() == nil {
			return nil, nil
		}
		return b, err
	}
}

// reviewShort is a hash's seven-character form.
func reviewShort(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}
