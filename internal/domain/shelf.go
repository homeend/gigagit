package domain

import (
	"context"
	"errors"

	"github.com/gigagit/gg/internal/model"
)

// ErrShelfDisabled means no state directory was resolvable.
var ErrShelfDisabled = errors.New("shelf: no state directory available")

// ShelfAdd resolves ref's bytes (Read reservation) and stores a frozen copy.
func (s *Service) ShelfAdd(ctx context.Context, ref model.FileRef, bucket string) (model.ShelfEntry, error) {
	st := s.shelfStore(ctx)
	if st == nil {
		return model.ShelfEntry{}, ErrShelfDisabled
	}
	data, err := s.ResolveBytes(ctx, ref)
	if err != nil {
		return model.ShelfEntry{}, err
	}
	return st.Put(bucket, ref, data)
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

// ShelfRemove deletes an entry (and reclaims its blob if unreferenced).
func (s *Service) ShelfRemove(ctx context.Context, entryID string) error {
	st := s.shelfStore(ctx)
	if st == nil {
		return ErrShelfDisabled
	}
	return st.Remove(entryID)
}
