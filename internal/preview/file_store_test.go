package preview

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/filelock"
	"github.com/homeend/gigagit/internal/model"
)

func rec(src, tgt string) model.MergePreview {
	return model.MergePreview{Source: src, Target: tgt, Created: time.Unix(1_700_000_000, 0).UTC()}
}

func TestIDIsDirectionSensitiveAndStable(t *testing.T) {
	t.Parallel()
	a, b := ID("feat/x", "main"), ID("main", "feat/x")
	if a == b {
		t.Fatal("swapping source and target must change the id")
	}
	if a != ID("feat/x", "main") || len(a) != 8 {
		t.Fatalf("id must be stable and 8 hex chars, got %q", a)
	}
}

func TestAddGetListRoundTrip(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	got, err := fs.Add(rec("feat/x", "main"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != ID("feat/x", "main") || got.Label != "feat/x → main" {
		t.Fatalf("Add must fill the id and the default label, got %+v", got)
	}
	back, err := fs.Get(got.ID)
	if err != nil || back != got {
		t.Fatalf("Get = %+v, %v; want %+v", back, err, got)
	}
	fs.Add(rec("feat/y", "main"))
	list, err := fs.List()
	if err != nil || len(list) != 2 || list[0].Source != "feat/x" {
		t.Fatalf("List = %+v, %v; want 2 in insertion order", list, err)
	}
}

func TestAddDuplicateIsErrExists(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	first, _ := fs.Add(rec("feat/x", "main"))
	again, err := fs.Add(rec("feat/x", "main"))
	if !errors.Is(err, ErrExists) || again.ID != first.ID {
		t.Fatalf("second Add = %+v, %v; want ErrExists with the existing record", again, err)
	}
	// The reverse direction is a different record.
	if _, err := fs.Add(rec("main", "feat/x")); err != nil {
		t.Fatalf("reverse pair must be addable: %v", err)
	}
}

func TestRenameAndRemove(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	r, _ := fs.Add(rec("feat/x", "main"))
	if err := fs.Rename(r.ID, "login fix"); err != nil {
		t.Fatal(err)
	}
	if back, _ := fs.Get(r.ID); back.Label != "login fix" {
		t.Fatalf("label = %q", back.Label)
	}
	if err := fs.Rename("nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename unknown = %v", err)
	}
	if err := fs.Remove(r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Get(r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Remove = %v", err)
	}
	if err := fs.Remove(r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Remove = %v", err)
	}
}

func TestMissingFileReadsEmptyAndCorruptFileErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := NewFileStore(dir)
	if list, err := fs.List(); err != nil || len(list) != 0 {
		t.Fatalf("empty store: %v %v", list, err)
	}
	os.WriteFile(filepath.Join(dir, "previews.toml"), []byte("not = [toml"), 0o644)
	if _, err := fs.List(); err == nil {
		t.Fatal("a corrupt file must be reported, not read as empty")
	}
}

func TestConcurrentWritersAllLand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			fs := NewFileStore(dir) // separate FileStore per goroutine = separate process mutex
			if _, err := fs.Add(rec("feat/"+string(rune('a'+i)), "main")); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	list, _ := NewFileStore(dir).List()
	if len(list) != 8 {
		t.Fatalf("got %d records, want 8 — a writer lost the file lock race", len(list))
	}
}

func TestUnremovableStaleLockStillGivesUp(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX directory permissions and a non-root user")
	}
	dir := t.TempDir()
	fs := NewFileStore(dir)
	lock := filepath.Join(dir, "previews.toml.lock")
	if err := os.WriteFile(lock, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * filelock.Stale)
	os.Chtimes(lock, old, old)
	os.Chmod(dir, 0o555) // the stale lock cannot be removed
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	done := make(chan error, 1)
	go func() { _, err := fs.Add(rec("a", "b")); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Add must fail when the lock cannot be taken")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Add spun forever on an unremovable stale lock")
	}
}
