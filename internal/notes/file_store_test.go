package notes

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

// noteAt builds a root note created at t0+d seconds.
func noteAt(id string, sec int) model.Note {
	return model.Note{
		ID: id, Source: model.NoteSourceUser, Side: model.NoteSideNew,
		Range: [2]int{sec + 1, sec + 1}, Summary: "s" + id,
		Address: model.FileAddress{State: model.StateUnstaged, Path: "a/b.go"},
		Created: time.Unix(int64(1_700_000_000+sec), 0).UTC(),
	}
}

func TestPutLoadRoundTrip(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	n := noteAt("aaaaaaaa", 1)
	n.Rationale = "because"
	n.Tags = []string{"perf", "api"}
	n.Confidence = 0.5
	if err := fs.Put(n); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := fs.Load()
	if err != nil || len(got) != 1 {
		t.Fatalf("Load = %v (%d), err %v", got, len(got), err)
	}
	if got[0].Summary != "saaaaaaaa" || got[0].Rationale != "because" ||
		got[0].Tags[1] != "api" || got[0].Confidence != 0.5 ||
		got[0].Address.Path != "a/b.go" || got[0].Side != model.NoteSideNew ||
		got[0].Range != [2]int{2, 2} || !got[0].Created.Equal(n.Created) {
		t.Fatalf("round trip lost data: %+v", got[0])
	}
	// Put by an existing ID REPLACES.
	n.Summary = "edited"
	if err := fs.Put(n); err != nil {
		t.Fatal(err)
	}
	got, _ = fs.Load()
	if len(got) != 1 || got[0].Summary != "edited" {
		t.Fatalf("Put must replace by ID, got %+v", got)
	}
}

func TestRemoveRootTakesReplies(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	root := noteAt("rrrrrrrr", 1)
	reply := noteAt("pppppppp", 2)
	reply.ParentID = root.ID
	other := noteAt("oooooooo", 3)
	for _, n := range []model.Note{root, reply, other} {
		if err := fs.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := fs.Remove(root.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	got, _ := fs.Load()
	if len(got) != 1 || got[0].ID != other.ID {
		t.Fatalf("removing a root must take its replies, left %+v", got)
	}
	if err := fs.Remove("nosuch"); err != ErrNotFound {
		t.Fatalf("Remove(unknown) = %v, want ErrNotFound", err)
	}
}

func TestCapDropsOldestRootsWithTheirReplies(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	fs.SetPolicy(Policy{MaxEntries: 3})
	oldRoot := noteAt("old00000", 1)
	oldReply := noteAt("oldreply", 2)
	oldReply.ParentID = oldRoot.ID
	for _, n := range []model.Note{oldRoot, oldReply, noteAt("mid00000", 5)} {
		if err := fs.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	// The 4th record trips the cap: the OLDEST ROOT goes, and its reply with it.
	if err := fs.Put(noteAt("new00000", 9)); err != nil {
		t.Fatal(err)
	}
	got, _ := fs.Load()
	if len(got) != 2 {
		t.Fatalf("cap 3 with a 2-record oldest thread must leave 2, got %d: %+v", len(got), got)
	}
	for _, n := range got {
		if n.ID == oldRoot.ID || n.ID == oldReply.ID {
			t.Fatalf("the oldest thread must be dropped whole, got %+v", got)
		}
	}
}

func TestCapUncappedWhenNonPositive(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	fs.SetPolicy(Policy{MaxEntries: -1})
	for i := 0; i < 12; i++ {
		if err := fs.Put(noteAt(string(rune('a'+i))+"0000000", i)); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := fs.Load()
	if len(got) != 12 {
		t.Fatalf("MaxEntries <= 0 must be uncapped, got %d", len(got))
	}
}

func TestSweepKeepsWhatThePredicateKeeps(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	root := noteAt("rrrrrrrr", 1)
	reply := noteAt("pppppppp", 2)
	reply.ParentID = root.ID
	keep := noteAt("kkkkkkkk", 3)
	for _, n := range []model.Note{root, reply, keep} {
		if err := fs.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	dropped, err := fs.Sweep(func(n model.Note) bool { return n.ID != root.ID })
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	// The root is dropped by the predicate; its reply goes with it => 2.
	if dropped != 2 {
		t.Fatalf("dropped = %d, want 2 (root + orphaned reply)", dropped)
	}
	got, _ := fs.Load()
	if len(got) != 1 || got[0].ID != keep.ID {
		t.Fatalf("Sweep left %+v", got)
	}
}

func TestLoadNeverWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := NewFileStore(dir)
	if got, err := fs.Load(); err != nil || len(got) != 0 {
		t.Fatalf("Load on an empty dir = %v, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.toml")); !os.IsNotExist(err) {
		t.Fatal("Load must not create the file")
	}
}

func TestStaleLockIsTakenOver(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := NewFileStore(dir)
	lock := filepath.Join(dir, "notes.toml.lock")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * lockStale)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := fs.Put(noteAt("aaaaaaaa", 1)); err != nil {
		t.Fatalf("a stale lock must be broken, got %v", err)
	}
	if time.Since(start) > lockWait {
		t.Fatal("a stale lock must be broken without waiting out the full retry budget")
	}
	if got, _ := fs.Load(); len(got) != 1 {
		t.Fatalf("write under a broken stale lock lost the note: %+v", got)
	}
}

func TestConcurrentPutsSerialize(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			n := noteAt(string(rune('a'+i))+"0000000", i)
			if err := fs.Put(n); err != nil {
				t.Errorf("Put %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	got, _ := fs.Load()
	if len(got) != 8 {
		t.Fatalf("concurrent writers lost records: %d of 8", len(got))
	}
}

// TestPutWaitsForAHeldLock proves the CROSS-PROCESS lock actually serialises:
// a live (non-stale) notes.toml.lock held by "another process" makes Put
// block, and releasing it lets the same Put through. The in-process mutex
// cannot explain this — the lock file is taken behind the store's back.
func TestPutWaitsForAHeldLock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := NewFileStore(dir)
	lock := filepath.Join(dir, "notes.toml.lock")
	held, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	held.Close()

	done := make(chan error, 1)
	go func() { done <- fs.Put(noteAt("aaaaaaaa", 1)) }()

	select {
	case err := <-done:
		t.Fatalf("Put returned (%v) while the lock was held", err)
	case <-time.After(200 * time.Millisecond): // well under lockWait
	}

	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Put after the lock was released: %v", err)
	}
	if got, _ := fs.Load(); len(got) != 1 {
		t.Fatalf("the waiting writer lost its note: %+v", got)
	}
}

// TestHeldLockGivesUpAtTheDeadline pins the retry loop's exit: a lock held
// past lockWait fails the write instead of waiting or spinning forever.
func TestHeldLockGivesUpAtTheDeadline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := NewFileStore(dir)
	held, err := os.OpenFile(filepath.Join(dir, "notes.toml.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	held.Close()

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- fs.Put(noteAt("aaaaaaaa", 1)) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Put must fail while the lock is held")
		}
		if elapsed := time.Since(start); elapsed < lockWait {
			t.Fatalf("gave up after %v, before the %v budget", elapsed, lockWait)
		}
	case <-time.After(4 * lockWait):
		t.Fatal("Put never gave up: the retry loop does not honour its deadline")
	}
}

// TestUnremovableStaleLockStillGivesUp is the regression test for the spin: a
// STALE lock that cannot be removed used to `continue` past both the deadline
// check and the backoff, burning a core forever while holding fs.mu.
func TestUnremovableStaleLockStillGivesUp(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX directory permissions and a non-root user")
	}
	dir := t.TempDir()
	fs := NewFileStore(dir)
	lock := filepath.Join(dir, "notes.toml.lock")
	if err := os.WriteFile(lock, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * lockStale)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	// A read-only directory keeps the entry undeletable: os.Remove fails, the
	// lock stays stale, and the loop meets the same condition every pass.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if err := os.Remove(lock); err == nil {
		t.Skip("this filesystem allows deletion from a read-only directory")
	}

	done := make(chan error, 1)
	go func() { done <- fs.Put(noteAt("aaaaaaaa", 1)) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Put must fail: the stale lock could not be broken")
		}
	case <-time.After(4 * lockWait):
		t.Fatal("Put spun on an unremovable stale lock instead of giving up")
	}
}

func TestUnreadableFileSurfacesAndDoesNotClobber(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := NewFileStore(dir)
	if err := fs.Put(noteAt("aaaaaaaa", 1)); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte("[[notes]]\nid = \"unterminated\n")
	if err := os.WriteFile(filepath.Join(dir, "notes.toml"), corrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Load(); err == nil {
		t.Fatal("Load on a corrupt file must surface the error, not read as empty")
	}
	if err := fs.Put(noteAt("bbbbbbbb", 2)); err == nil {
		t.Fatal("Put on a corrupt file must fail rather than rewrite from an empty base")
	}
	got, err := os.ReadFile(filepath.Join(dir, "notes.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(corrupt) {
		t.Fatalf("the unreadable file was clobbered:\n%s", got)
	}
}

func TestSweepWithNothingToDropDoesNotRewrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := NewFileStore(dir)
	for _, n := range []model.Note{noteAt("aaaaaaaa", 1), noteAt("bbbbbbbb", 2)} {
		if err := fs.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(dir, "notes.toml")
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(file, old, old); err != nil {
		t.Fatal(err)
	}
	dropped, err := fs.Sweep(func(model.Note) bool { return true })
	if err != nil || dropped != 0 {
		t.Fatalf("Sweep = %d, %v", dropped, err)
	}
	fi, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.ModTime().Equal(old) {
		t.Fatalf("a no-op Sweep rewrote the file (mtime %v, want %v)", fi.ModTime(), old)
	}
	got, _ := os.ReadFile(file)
	if string(got) != string(want) {
		t.Fatalf("a no-op Sweep changed the content:\n%s", got)
	}
}

func TestSweepOnAMissingStoreCreatesNothing(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "state", "notes")
	fs := NewFileStore(root)
	dropped, err := fs.Sweep(func(model.Note) bool { return false })
	if err != nil || dropped != 0 {
		t.Fatalf("Sweep on a missing store = %d, %v", dropped, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("Sweep must not create the state directory")
	}
}

func TestNewIDAvoidsCollisions(t *testing.T) {
	t.Parallel()
	taken := []model.Note{}
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := NewID(taken)
		if len(id) != 8 {
			t.Fatalf("id %q is not 8 hex chars", id)
		}
		if seen[id] {
			t.Fatalf("NewID returned a taken id %q", id)
		}
		seen[id] = true
		taken = append(taken, model.Note{ID: id})
	}
}
