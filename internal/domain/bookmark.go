package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"

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
	if b.IsCommit() {
		return nil, errors.New("bookmark: commit bookmark has no file bytes")
	}
	switch b.State {
	case model.StateCommitted:
		return query(ctx, s, "catfile:"+b.SHA, func(ctx context.Context) ([]byte, error) {
			return s.repo.CatFileBlob(ctx, b.SHA)
		})
	case model.StateShelf:
		return s.ShelfBlob(ctx, b.ShelfID)
	case model.StateStaged:
		return query(ctx, s, "showindir:"+b.Worktree+":"+b.Path, func(ctx context.Context) ([]byte, error) {
			return s.repo.ShowFileInDir(ctx, b.Worktree, "", b.Path)
		})
	case model.StateUnstaged, model.StateUntracked:
		return query(ctx, s, "bookmarkfile:"+b.Worktree+":"+b.Path, func(ctx context.Context) ([]byte, error) {
			return os.ReadFile(filepath.Join(b.Worktree, filepath.FromSlash(b.Path)))
		})
	default:
		return nil, errors.New("bookmark: unknown state")
	}
}
