package domain

import (
	"context"
	"errors"
	"os"

	"github.com/homeend/gigagit/internal/bookmark"
	"github.com/homeend/gigagit/internal/model"
)

// ErrBookmarksDisabled means no state directory was resolvable.
var ErrBookmarksDisabled = errors.New("bookmark: no state directory available")

// BookmarkAdd stores a bookmark, filling SHA for permanent states: a committed
// file gets its blob sha via BlobSHA; a shelf bookmark carries the entry's SHA
// already. The store derives the address ID.
func (s *Service) BookmarkAdd(ctx context.Context, b model.Bookmark) (model.Bookmark, error) {
	st := s.bookmarkStore(ctx)
	if st == nil {
		return model.Bookmark{}, ErrBookmarksDisabled
	}
	if b.State == model.StateCommitted && b.SHA == "" && b.Path != "" {
		sha, err := query(ctx, s, "blobsha:"+b.Commit+":"+b.Path, func(ctx context.Context) (string, error) {
			return s.repo.BlobSHA(ctx, b.Commit, b.Path)
		})
		if err != nil {
			return model.Bookmark{}, err
		}
		b.SHA = sha
	}
	return st.Add(b)
}

// BookmarkList returns one page of bookmarks, newest first.
func (s *Service) BookmarkList(ctx context.Context, skip, limit int) ([]model.Bookmark, error) {
	st := s.bookmarkStore(ctx)
	if st == nil {
		return nil, ErrBookmarksDisabled
	}
	return st.List(skip, limit)
}

// ErrBookmarkNotFound is BookmarkGet's "no such id" — the store's own value,
// re-exported because frontends never import internal/bookmark.
var ErrBookmarkNotFound = bookmark.ErrNotFound

// BookmarkGet returns one bookmark by id.
func (s *Service) BookmarkGet(ctx context.Context, id string) (model.Bookmark, error) {
	st := s.bookmarkStore(ctx)
	if st == nil {
		return model.Bookmark{}, ErrBookmarksDisabled
	}
	return st.Get(id)
}

// BookmarkRemove deletes a bookmark by id.
func (s *Service) BookmarkRemove(ctx context.Context, id string) error {
	st := s.bookmarkStore(ctx)
	if st == nil {
		return ErrBookmarksDisabled
	}
	return st.Remove(id)
}

// BookmarkBytes resolves a bookmark to bytes, routing on state: permanent →
// the blob (cat-file / shelf store); live → the named worktree's index or
// working file. Repo-touching paths (git reads AND the live working-file read)
// run under a Read reservation like every other domain read, so a paste or
// compare never races a TreeWrite op mid-rewrite; the shelf store is not git
// state and stays ungated.
func (s *Service) BookmarkBytes(ctx context.Context, b model.Bookmark) ([]byte, error) {
	return s.bookmarkBytes(ctx, b, false)
}

// bookmarkBytes is BookmarkBytes with the failure seam selectable. quiet is
// for BookmarkProbe, whose whole job is to ask about entries that may be dead:
// a miss there is the ANSWER, not a failure, and logged through query it would
// add an errors.log row on every click of a dead bookmark (the CommitLookup
// convention).
func (s *Service) bookmarkBytes(ctx context.Context, b model.Bookmark, quiet bool) ([]byte, error) {
	read := query[[]byte]
	if quiet {
		read = queryQuiet[[]byte]
	}
	if b.IsCommit() {
		return nil, errors.New("bookmark: commit bookmark has no file bytes")
	}
	switch b.State {
	case model.StateCommitted:
		return read(ctx, s, "catfile:"+b.SHA, func(ctx context.Context) ([]byte, error) {
			return s.repo.CatFileBlob(ctx, b.SHA)
		})
	case model.StateShelf:
		return s.ShelfBlob(ctx, b.ShelfID)
	case model.StateStaged:
		return read(ctx, s, "showindir:"+b.Worktree+":"+b.Path, func(ctx context.Context) ([]byte, error) {
			return s.repo.ShowFileInDir(ctx, b.Worktree, "", b.Path)
		})
	case model.StateUnstaged, model.StateUntracked:
		full, err := worktreeJoin(b.Worktree, b.Path)
		if err != nil {
			return nil, err
		}
		return read(ctx, s, "bookmarkfile:"+b.Worktree+":"+b.Path, func(ctx context.Context) ([]byte, error) {
			return os.ReadFile(full)
		})
	default:
		return nil, errors.New("bookmark: unknown state")
	}
}

// EntryGoneError reports a stored entry whose target can no longer be read: a
// bookmark is a pointer, so once the commit or blob it names is rebased away
// and gc'd (or the live file is deleted) there is nothing left to open. What
// names the target for the notice every frontend shows; Cause keeps git's own
// words for a detail line.
type EntryGoneError struct {
	What  string
	Cause error
}

func (e *EntryGoneError) Error() string { return e.What + " is no longer available" }

func (e *EntryGoneError) Unwrap() error { return e.Cause }

// BookmarkProbe reports whether what b points at can still be opened: nil, or
// an *EntryGoneError. It is the ONE availability check the frontends ask
// before opening a bookmark, so the TUI and the web answer a dead one with the
// same sentence instead of a raw git error and silence respectively. A
// cancelled context is returned as itself — never as "gone".
func (s *Service) BookmarkProbe(ctx context.Context, b model.Bookmark) error {
	short := b.Commit
	if len(short) > 7 {
		short = short[:7]
	}
	if b.IsCommit() {
		_, found, err := s.CommitLookup(ctx, b.Commit)
		if err != nil {
			return err
		}
		if !found {
			return &EntryGoneError{What: "commit " + short, Cause: &CommitGoneError{SHA: b.Commit}}
		}
		return nil
	}
	if _, err := s.bookmarkBytes(ctx, b, true); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		what := b.Path
		if b.State == model.StateCommitted && short != "" {
			what += " @ " + short
		}
		return &EntryGoneError{What: what, Cause: err}
	}
	return nil
}
