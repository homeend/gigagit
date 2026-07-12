package shelf

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func newStore(t *testing.T) *FileStore {
	t.Helper()
	return NewFileStore(t.TempDir())
}

// fsRoot exposes a FileStore's root for reopen-round-trip tests.
func fsRoot(fs *FileStore) string { return fs.root }

func addr(path string) model.FileAddress {
	return model.FileAddress{State: model.StateUnstaged, Path: path}
}

func TestPutGetRoundTrip(t *testing.T) {
	s := newStore(t)
	data := []byte("hello shelf\n")
	e, err := s.Put("", addr("a/b.go"), data)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if e.Bucket != "default" {
		t.Errorf("bucket = %q, want default", e.Bucket)
	}
	if e.Origin.Path != "a/b.go" || e.Size != int64(len(data)) {
		t.Errorf("entry meta wrong: %+v", e)
	}
	got, err := s.Get(e.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("Get = %q, want %q", got, data)
	}
}

func TestPutDedupsIdenticalBytes(t *testing.T) {
	s := newStore(t)
	data := []byte("same")
	e1, _ := s.Put("", addr("x.go"), data)
	e2, _ := s.Put("", addr("y.go"), data)
	if e1.SHA != e2.SHA {
		t.Fatalf("identical bytes got different SHAs: %s vs %s", e1.SHA, e2.SHA)
	}
	if e1.ID == e2.ID {
		t.Fatalf("different paths should get different IDs")
	}
}

func TestPutRefusesOversize(t *testing.T) {
	s := newStore(t)
	big := make([]byte, MaxShelfBytes+1)
	if _, err := s.Put("", addr("big.bin"), big); err != ErrTooLarge {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestListPagingAndExhaustion(t *testing.T) {
	s := newStore(t)
	for _, p := range []string{"a", "b", "c"} {
		if _, err := s.Put("", addr(p), []byte(p)); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.List("", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page))
	}
	page2, _ := s.List("", 2, 2)
	if len(page2) != 1 {
		t.Fatalf("page2 len = %d, want 1 (exhausted)", len(page2))
	}
}

func TestRemoveReclaimsUnreferencedBlobKeepsShared(t *testing.T) {
	s := newStore(t)
	shared := []byte("shared")
	e1, _ := s.Put("", addr("p1"), shared)
	e2, _ := s.Put("", addr("p2"), shared) // same blob
	if err := s.Remove(e1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(e2.ID); err != nil {
		t.Fatalf("shared blob removed too early: %v", err)
	}
	if err := s.Remove(e2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(e2.ID); err == nil {
		t.Fatalf("blob should be gone after last reference removed")
	}
}

func TestBucketsListNamed(t *testing.T) {
	s := newStore(t)
	s.Put("feature", addr("f.go"), []byte("f"))
	bs, err := s.Buckets()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range bs {
		names = append(names, b.Name)
	}
	if !strings.Contains(strings.Join(names, ","), "feature") {
		t.Fatalf("buckets %v missing 'feature'", names)
	}
}

func TestPutCommitStoresArchiveKind(t *testing.T) {
	fs := NewFileStore(t.TempDir())
	tar := []byte("PK-not-really-a-tar-but-bytes")
	addr := model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5f6", Path: ""}
	e, err := fs.PutCommit("", addr, tar, nil, "")
	if err != nil {
		t.Fatalf("PutCommit: %v", err)
	}
	if e.Kind != model.ShelfKindCommit {
		t.Fatalf("Kind = %v, want ShelfKindCommit", e.Kind)
	}
	if e.ID != "commit-a1b2c3d-"+e.SHA[:8] {
		t.Fatalf("ID = %q, want commit-a1b2c3d-<sha8>", e.ID)
	}
	got, err := fs.Get(e.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(tar) {
		t.Fatalf("Get returned %q, want the stored tar", got)
	}
	// A plain file Put must report ShelfKindFile.
	fe, err := fs.Put("", model.FileAddress{State: model.StateUnstaged, Path: "x.txt"}, []byte("hi"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if fe.Kind != model.ShelfKindFile {
		t.Fatalf("file Put Kind = %v, want ShelfKindFile", fe.Kind)
	}
}

func TestPutCommitPersistsLabel(t *testing.T) {
	fs := NewFileStore(t.TempDir())
	addr := model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5f6", Path: ""}
	e, err := fs.PutCommit("", addr, []byte("tarbytes"), nil, "my fix")
	if err != nil {
		t.Fatalf("PutCommit: %v", err)
	}
	if e.Label != "my fix" {
		t.Fatalf("Label = %q, want %q", e.Label, "my fix")
	}
	// Survives the TOML index round-trip (reopen the store, list).
	fs2 := NewFileStore(fsRoot(fs))
	page, err := fs2.List("", 0, 10)
	if err != nil || len(page) != 1 {
		t.Fatalf("List: %v (n=%d)", err, len(page))
	}
	if page[0].Label != "my fix" {
		t.Fatalf("reloaded Label = %q, want %q", page[0].Label, "my fix")
	}
}

func TestPutCommitStoresPatchBlob(t *testing.T) {
	fs := NewFileStore(t.TempDir())
	addr := model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5f6", Path: ""}
	patch := []byte("From a1b2c3d4e5f6 Mon Sep 17 00:00:00 2001\nSubject: x\n")
	e, err := fs.PutCommit("", addr, []byte("tarbytes"), patch, "fix")
	if err != nil {
		t.Fatalf("PutCommit: %v", err)
	}
	if e.PatchSHA == "" || e.PatchSize != int64(len(patch)) {
		t.Fatalf("patch fields = %q / %d, want set / %d", e.PatchSHA, e.PatchSize, len(patch))
	}
	got, err := fs.GetPatch(e.ID)
	if err != nil || string(got) != string(patch) {
		t.Fatalf("GetPatch = %q, %v", got, err)
	}
	// The tar payload is untouched by the patch blob.
	tar, err := fs.Get(e.ID)
	if err != nil || string(tar) != "tarbytes" {
		t.Fatalf("Get = %q, %v", tar, err)
	}
	// Patch fields survive the TOML index round-trip.
	fs2 := NewFileStore(fsRoot(fs))
	e2, err := fs2.Find(e.ID)
	if err != nil || e2.PatchSHA != e.PatchSHA || e2.PatchSize != e.PatchSize {
		t.Fatalf("reloaded patch fields = %q / %d, %v", e2.PatchSHA, e2.PatchSize, err)
	}
}

func TestPutCommitWithoutPatch(t *testing.T) {
	fs := NewFileStore(t.TempDir())
	addr := model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5f6", Path: ""}
	e, err := fs.PutCommit("", addr, []byte("tarbytes"), nil, "")
	if err != nil {
		t.Fatalf("PutCommit: %v", err)
	}
	if e.PatchSHA != "" || e.PatchSize != 0 {
		t.Fatalf("no-patch entry carries patch fields: %q / %d", e.PatchSHA, e.PatchSize)
	}
	if _, err := fs.GetPatch(e.ID); !errors.Is(err, ErrNoPatch) {
		t.Fatalf("GetPatch = %v, want ErrNoPatch", err)
	}
	if _, err := fs.GetPatch("no-such-entry"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPatch(unknown) = %v, want ErrNotFound", err)
	}
}

func TestRemoveReclaimsPatchBlob(t *testing.T) {
	fs := NewFileStore(t.TempDir())
	addr := model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5f6", Path: ""}
	e, err := fs.PutCommit("", addr, []byte("tarbytes"), []byte("patchbytes"), "")
	if err != nil {
		t.Fatalf("PutCommit: %v", err)
	}
	tarBlob, patchBlob := fs.blobPath(e.SHA), fs.blobPath(e.PatchSHA)
	if _, err := os.Stat(patchBlob); err != nil {
		t.Fatalf("patch blob missing after PutCommit: %v", err)
	}
	if err := fs.Remove(e.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(tarBlob); !os.IsNotExist(err) {
		t.Fatalf("tar blob not reclaimed: %v", err)
	}
	if _, err := os.Stat(patchBlob); !os.IsNotExist(err) {
		t.Fatalf("patch blob not reclaimed: %v", err)
	}
}

func TestRemoveKeepsSharedPatchBlob(t *testing.T) {
	fs := NewFileStore(t.TempDir())
	a := model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5f6"}
	b := model.FileAddress{State: model.StateCommitted, Commit: "f6e5d4c3b2a1"}
	e1, err := fs.PutCommit("", a, []byte("tar-one"), []byte("same-patch"), "")
	if err != nil {
		t.Fatalf("PutCommit e1: %v", err)
	}
	e2, err := fs.PutCommit("", b, []byte("tar-two"), []byte("same-patch"), "")
	if err != nil {
		t.Fatalf("PutCommit e2: %v", err)
	}
	if e1.PatchSHA != e2.PatchSHA {
		t.Fatal("content-addressing must dedup identical patch blobs")
	}
	if err := fs.Remove(e1.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := fs.GetPatch(e2.ID); err != nil {
		t.Fatalf("survivor's patch must remain readable: %v", err)
	}
}
