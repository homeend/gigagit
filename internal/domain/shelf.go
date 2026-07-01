package domain

import (
	"context"
	"errors"
	"fmt"

	"github.com/homeend/gigagit/internal/model"
)

// ErrShelfDisabled means no state directory was resolvable.
var ErrShelfDisabled = errors.New("shelf: no state directory available")

// ShelfAdd resolves addr's bytes (Read reservation) and stores a frozen copy
// tagged with its structured origin.
func (s *Service) ShelfAdd(ctx context.Context, addr model.FileAddress, bucket string) (model.ShelfEntry, error) {
	st := s.shelfStore(ctx)
	if st == nil {
		return model.ShelfEntry{}, ErrShelfDisabled
	}
	data, err := s.ResolveBytes(ctx, addr.FileRef())
	if err != nil {
		return model.ShelfEntry{}, err
	}
	return st.Put(bucket, addr, data)
}

// ShelfList returns one page of a bucket's entries, newest first.
func (s *Service) ShelfList(ctx context.Context, bucket string, skip, limit int) ([]model.ShelfEntry, error) {
	st := s.shelfStore(ctx)
	if st == nil {
		return nil, ErrShelfDisabled
	}
	return st.List(bucket, skip, limit)
}

// ShelfBuckets returns the visible buckets.
func (s *Service) ShelfBuckets(ctx context.Context) ([]model.ShelfBucket, error) {
	st := s.shelfStore(ctx)
	if st == nil {
		return nil, ErrShelfDisabled
	}
	return st.Buckets()
}

// ShelfBlob returns an entry's stored bytes (a local read; no reservation).
func (s *Service) ShelfBlob(ctx context.Context, entryID string) ([]byte, error) {
	st := s.shelfStore(ctx)
	if st == nil {
		return nil, ErrShelfDisabled
	}
	return st.Get(entryID)
}

// ShelfAddCommit freezes commit sha's changed files into a durable, path-less
// ShelfKindCommit entry: it archives just the paths the commit touched (content
// AT sha) so the entry restores even after the commit leaves git. Content only —
// no message/author/parents.
func (s *Service) ShelfAddCommit(ctx context.Context, sha string) (model.ShelfEntry, error) {
	st := s.shelfStore(ctx)
	if st == nil {
		return model.ShelfEntry{}, ErrShelfDisabled
	}
	paths, err := s.commitChangedPaths(ctx, sha)
	if err != nil {
		return model.ShelfEntry{}, err
	}
	if len(paths) == 0 {
		return model.ShelfEntry{}, fmt.Errorf("shelf: commit %s changes no files", sha)
	}
	tar, err := s.archiveFiles(ctx, sha, paths)
	if err != nil {
		return model.ShelfEntry{}, err
	}
	addr := model.FileAddress{State: model.StateCommitted, Commit: sha, Path: ""}
	return st.PutCommit("", addr, tar)
}

// ShelfRemove deletes an entry (and reclaims its blob if unreferenced).
func (s *Service) ShelfRemove(ctx context.Context, entryID string) error {
	st := s.shelfStore(ctx)
	if st == nil {
		return ErrShelfDisabled
	}
	return st.Remove(entryID)
}
