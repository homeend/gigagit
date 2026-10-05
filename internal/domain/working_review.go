package domain

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// A working-changes review (spec 2026-10-04 working reviews) identifies its
// files by (path, blob id). This file matches them against the worktree now.

// WorkingFileState is one reviewed file against the worktree now.
type WorkingFileState int

const (
	WorkingFileMatches WorkingFileState = iota // the reviewed bytes (or a reviewed deletion, still absent)
	WorkingFileChanged                         // edited since, or recreated after a reviewed deletion
	WorkingFileGone                            // the reviewed file is no longer there
)

// WorkingReviewMatch is a working review's files against the worktree:
// each file's state, and Current while at least one still matches (§7).
type WorkingReviewMatch struct {
	States  map[string]WorkingFileState
	Current bool
}

// WorkingReview is a stored working review, matched now.
type WorkingReview struct {
	Review
	WorkingReviewMatch
}

// WorkingReviewState matches files against worktree. A worktree that is gone
// leaves every file gone — the review is outdated (a reviewed deletion must
// not keep "matching" a directory that no longer exists).
func WorkingReviewState(worktree string, files []model.NoteFile) WorkingReviewMatch {
	m := WorkingReviewMatch{States: make(map[string]WorkingFileState, len(files))}
	fi, err := os.Stat(worktree)
	if worktree == "" || err != nil || !fi.IsDir() {
		for _, f := range files {
			m.States[f.Path] = WorkingFileGone
		}
		return m
	}
	for _, f := range files {
		st := matchWorkingFile(filepath.Join(worktree, filepath.FromSlash(f.Path)), f)
		m.States[f.Path] = st
		m.Current = m.Current || st == WorkingFileMatches
	}
	return m
}

func matchWorkingFile(abs string, f model.NoteFile) WorkingFileState {
	if f.Deleted {
		if _, err := os.Lstat(abs); errors.Is(err, fs.ErrNotExist) {
			return WorkingFileMatches
		}
		return WorkingFileChanged
	}
	id, err := workingBlobs.id(git.BlobFormatOf(f.Blob), abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return WorkingFileGone
	case err != nil || id != f.Blob:
		return WorkingFileChanged
	}
	return WorkingFileMatches
}

// blobCacheMax bounds the cache; past it the cache starts over.
const blobCacheMax = 20000

// blobFreshness: a file modified this recently is hashed every time — a
// same-size rewrite inside the filesystem's mtime granularity would otherwise
// be served from the cache (git's "racily clean" problem).
const blobFreshness = 2 * time.Second

// workingBlobs caches working-file blob ids process-wide by absolute path,
// trusted while the file's mtime and size are unchanged (spec §7): a status
// refresh re-hashes only files that changed on disk.
var workingBlobs = newBlobCache()

type blobEntry struct {
	format string
	mtime  time.Time
	size   int64
	id     string
}

type blobCache struct {
	mu     sync.Mutex
	m      map[string]blobEntry
	hashes int // how many files were actually hashed (tests)
}

func newBlobCache() *blobCache { return &blobCache{m: map[string]blobEntry{}} }

func (c *blobCache) id(format, abs string) (string, error) {
	fi, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	e, ok := c.m[abs]
	c.mu.Unlock()
	if ok && e.format == format && e.size == fi.Size() && e.mtime.Equal(fi.ModTime()) {
		return e.id, nil
	}
	b, err := git.HashWorktreeFile(format, abs)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.hashes++
	if time.Since(fi.ModTime()) >= blobFreshness {
		if len(c.m) >= blobCacheMax {
			c.m = map[string]blobEntry{}
		}
		c.m[abs] = blobEntry{format: format, mtime: fi.ModTime(), size: fi.Size(), id: b.ID}
	}
	c.mu.Unlock()
	return b.ID, nil
}

// WorkingReviews is this worktree's working reviews, newest first, each
// matched now — current and outdated alike (View all notes lists both). It
// reads ONLY this worktree's part: a live file read never parses commits.
func (s *Service) WorkingReviews(ctx context.Context) ([]WorkingReview, error) {
	ns, err := s.workingReviewNotes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]WorkingReview, 0, len(ns))
	for _, n := range ns {
		r := s.reviewOf(ctx, n, nil)
		out = append(out, WorkingReview{Review: r, WorkingReviewMatch: WorkingReviewState(r.Worktree, r.Files)})
	}
	return out, nil
}

// workingReviewNotes is this worktree's working-review roots, newest first.
func (s *Service) workingReviewNotes(ctx context.Context) ([]model.Note, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return nil, ErrNotesDisabled
	}
	top, _ := s.TopLevel(ctx)
	top = strings.TrimSpace(top)
	if top == "" {
		return nil, nil
	}
	all, err := st.Load(notes.WorktreePart(top))
	if err != nil {
		return nil, err
	}
	var out []model.Note
	for _, n := range all {
		if !n.IsReply() && n.IsWorkingReview() && sameWorktreePath(n.Address.Worktree, top) {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Created.After(out[b].Created) })
	return out, nil
}
