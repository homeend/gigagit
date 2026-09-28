package domain

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/shelf"
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
func (s *Service) ShelfAddCommit(ctx context.Context, sha, label string) (model.ShelfEntry, error) {
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
	// Best-effort patch snapshot: lets the entry be re-applied as a commit
	// (git am) even after the commit object is gc'd. A merge commit (refused
	// by CommitPatch), an oversized patch, or a format-patch failure just
	// skips the snapshot — shelving must never fail over it.
	patch, _, perr := s.CommitPatch(ctx, sha)
	if perr != nil || len(patch) > shelf.MaxCommitArchiveBytes {
		patch = nil
	}
	addr := model.FileAddress{State: model.StateCommitted, Commit: sha, Path: ""}
	return st.PutCommit("", addr, tar, patch, label)
}

// ShelfAddFiles freezes several working-tree / index files into ONE durable
// ShelfKindFiles entry — a tar with one member per address, so the set stays
// together (browse, restore one, compare) instead of scattering into N file
// entries. Atomic: one unreadable member (a deleted path has no bytes) fails
// the whole call naming the path — a set the user reads as "related" is
// complete or absent. The entry's origin is the first address without its
// path (the callers hand over one panel's files, so state and worktree are
// uniform). label names the set; "" is allowed.
func (s *Service) ShelfAddFiles(ctx context.Context, addrs []model.FileAddress, label string) (model.ShelfEntry, error) {
	st := s.shelfStore(ctx)
	if st == nil {
		return model.ShelfEntry{}, ErrShelfDisabled
	}
	if len(addrs) == 0 {
		return model.ShelfEntry{}, errors.New("shelf: no files to shelve")
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	seen := map[string]bool{}
	for _, a := range addrs {
		name := pathpkg.Clean(filepath.ToSlash(a.Path))
		if name == "" || name == "." || seen[name] {
			return model.ShelfEntry{}, fmt.Errorf("shelf: %q is not a shelvable path", a.Path)
		}
		seen[name] = true
		data, err := s.ResolveBytes(ctx, a.FileRef())
		if err != nil {
			return model.ShelfEntry{}, fmt.Errorf("shelf: %s: %w", a.Path, err)
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg, ModTime: time.Now()}); err != nil {
			return model.ShelfEntry{}, err
		}
		if _, err := tw.Write(data); err != nil {
			return model.ShelfEntry{}, err
		}
	}
	if err := tw.Close(); err != nil {
		return model.ShelfEntry{}, err
	}
	origin := addrs[0]
	origin.Path = ""
	return st.PutFiles("", origin, buf.Bytes(), label)
}

// ShelfCommitFiles lists the files frozen in an archive entry's tar (a
// shelved commit or a shelved file set) — a header scan only, no data copy.
// Rows carry an empty Status: A-vs-M relative to the commit's original parent
// is not recorded in the tar. Backs the files-view shelf mode.
func (s *Service) ShelfCommitFiles(ctx context.Context, entryID string) ([]model.CommitFile, error) {
	st := s.shelfStore(ctx)
	if st == nil {
		return nil, ErrShelfDisabled
	}
	e, err := st.Find(entryID)
	if err != nil {
		return nil, err
	}
	if !e.IsArchive() {
		return nil, fmt.Errorf("shelf: entry %s holds one file, not a set of files", entryID)
	}
	blob, err := st.Get(entryID)
	if err != nil {
		return nil, err
	}
	names, err := tarMemberNames(blob)
	if err != nil {
		return nil, err
	}
	out := make([]model.CommitFile, 0, len(names))
	for _, n := range names {
		out = append(out, model.CommitFile{Path: n})
	}
	return out, nil
}

// shelfResolve is ResolveBytes' shelf branch: an archive entry (commit or
// file set) with a path resolves to that member's bytes from the tar; one
// without a path stays the whole tar (backs export); a file entry stays the
// whole blob — the discriminator is the ENTRY KIND, never the content (a
// shelved .tar *file* must stay a blob).
func (s *Service) shelfResolve(ctx context.Context, entryID, path string) ([]byte, error) {
	st := s.shelfStore(ctx)
	if st == nil {
		return nil, ErrShelfDisabled
	}
	e, err := st.Find(entryID)
	if err != nil {
		return nil, err
	}
	blob, err := st.Get(entryID)
	if err != nil {
		return nil, err
	}
	if !e.IsArchive() || path == "" {
		return blob, nil
	}
	return tarMember(blob, path)
}

// ErrShelfEntryNotFound is ShelfFind's "no such id" — the store's own value,
// re-exported because frontends never import internal/shelf.
var ErrShelfEntryNotFound = shelf.ErrNotFound

// ShelfFind returns an entry's metadata by id (a local index read; no
// reservation). The entry-kind discriminator for callers that must branch on
// file-vs-commit without fetching the blob (the MCP read/export tools).
func (s *Service) ShelfFind(ctx context.Context, entryID string) (model.ShelfEntry, error) {
	st := s.shelfStore(ctx)
	if st == nil {
		return model.ShelfEntry{}, ErrShelfDisabled
	}
	return st.Find(entryID)
}

// ShelfRemove deletes an entry (and reclaims its blob if unreferenced).
func (s *Service) ShelfRemove(ctx context.Context, entryID string) error {
	st := s.shelfStore(ctx)
	if st == nil {
		return ErrShelfDisabled
	}
	return st.Remove(entryID)
}

// ShelfPatchFile materializes entryID's stored format-patch mailbox to a temp
// file (engine.ApplyPatch takes a disk path) and returns that path. The caller
// owns deletion once the op that consumed it finishes. shelf.ErrNoPatch when
// the entry has no snapshot.
func (s *Service) ShelfPatchFile(ctx context.Context, entryID string) (string, error) {
	st := s.shelfStore(ctx)
	if st == nil {
		return "", ErrShelfDisabled
	}
	data, err := st.GetPatch(entryID)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "gg-shelf-*.patch")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
