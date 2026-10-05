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
	// Unreadable: a read FAILED (permissions, an unmounted drive) — that
	// proves nothing about the files, so the sweep keeps the review.
	Unreadable bool
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
		m.Unreadable = err != nil && !errors.Is(err, fs.ErrNotExist)
		for _, f := range files {
			m.States[f.Path] = WorkingFileGone
		}
		return m
	}
	for _, f := range files {
		st, unreadable := matchWorkingFile(filepath.Join(worktree, filepath.FromSlash(f.Path)), f)
		m.States[f.Path] = st
		m.Current = m.Current || st == WorkingFileMatches
		m.Unreadable = m.Unreadable || unreadable
	}
	return m
}

// matchWorkingFile is one file's state; unreadable when the read failed for
// a reason other than the file being absent (the state is then Changed:
// nothing is drawn on it, but nothing may be deleted because of it).
func matchWorkingFile(abs string, f model.NoteFile) (WorkingFileState, bool) {
	if f.Deleted {
		_, err := os.Lstat(abs)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return WorkingFileMatches, false
		case err != nil:
			return WorkingFileChanged, true
		}
		return WorkingFileChanged, false
	}
	id, err := workingBlobs.id(git.BlobFormatOf(f.Blob), abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return WorkingFileGone, false
	case err != nil:
		return WorkingFileChanged, true
	case id != f.Blob:
		return WorkingFileChanged, false
	}
	return WorkingFileMatches, false
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
	ns, th, err := s.workingReviewNotes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]WorkingReview, 0, len(ns))
	for _, n := range ns {
		r := th.attach(s.reviewOf(ctx, n, nil))
		out = append(out, WorkingReview{Review: r, WorkingReviewMatch: WorkingReviewState(r.Worktree, r.Files)})
	}
	return out, nil
}

// workingReviewNotes is this worktree's working-review roots, newest first,
// and their remark threads.
func (s *Service) workingReviewNotes(ctx context.Context) ([]model.Note, remarkThreadSet, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return nil, remarkThreadSet{}, ErrNotesDisabled
	}
	top, _ := s.TopLevel(ctx)
	top = strings.TrimSpace(top)
	if top == "" {
		return nil, remarkThreadSet{}, nil
	}
	part := notes.WorktreePart(top)
	all, err := st.Load(part)
	if err != nil {
		return nil, remarkThreadSet{}, err
	}
	var out []model.Note
	for _, n := range all {
		if !n.IsReply() && n.IsWorkingReview() && sameWorktreePath(n.Address.Worktree, top) {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Created.After(out[b].Created) })
	return out, loadReviewThreads(st, all, []notes.Part{part}), nil
}

// workingReviewNotesOn are the notes the current working reviews of worktree
// place on path, new side only — the old side a review read is HEAD, not the
// index a Files-panel diff shows (spec §7). lines reads the file's new side,
// lazily: an address no review matches costs no file read.
func (s *Service) workingReviewNotesOn(ctx context.Context, worktree, path string, lines func() []string) []ResolvedNote {
	revs, err := s.WorkingReviews(ctx)
	if err != nil {
		return nil
	}
	var out []ResolvedNote
	var newLines []string
	read := false
	for _, wr := range revs {
		if wr.Doc == nil || !sameWorktreePath(wr.Worktree, worktree) || wr.States[reviewPath(path)] != WorkingFileMatches {
			continue
		}
		if !read {
			newLines, read = lines(), true
		}
		addr := model.FileAddress{State: model.StateUnstaged, Worktree: wr.Worktree, Path: path}
		out = append(out, reviewDocNotes(wr.Review, path, addr, nil, newLines, true)...)
	}
	return out
}

// workingDiffAddr: addr is a working-tree diff of one file (unstaged or
// untracked) — the new side a working review read. A staged diff's new side
// is the index, where the review's line numbers do not hold.
func workingDiffAddr(addr model.FileAddress) bool {
	return worktreeScopedNote(addr) && addr.Path != "" &&
		(addr.State == model.StateUnstaged || addr.State == model.StateUntracked)
}
