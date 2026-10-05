package notes

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/filelock"
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

// newTestFile is one part file named like the pre-split store, so these
// single-file tests keep their file names (notes.toml, notes.toml.lock).
func newTestFile(dir string) *partFile { return &partFile{path: filepath.Join(dir, "notes.toml")} }

func TestRemovingTheLastNoteDeletesTheFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := newTestFile(dir)
	if err := fs.Put(noteAt("aaaaaaaa", 1)); err != nil {
		t.Fatal(err)
	}
	if err := fs.Remove("aaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.toml")); !os.IsNotExist(err) {
		t.Fatalf("an emptied part must be removed, stat err = %v", err)
	}
	if ns, err := fs.Load(); err != nil || len(ns) != 0 {
		t.Fatalf("Load after removal = %v, %v", ns, err)
	}
}

func TestPutLoadRoundTrip(t *testing.T) {
	t.Parallel()
	fs := newTestFile(t.TempDir())
	n := noteAt("aaaaaaaa", 1)
	n.Rationale = "because"
	n.Tags = []string{"perf", "api"}
	n.Confidence = 0.5
	n.Preview = "main...feat"
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
		got[0].Range != [2]int{2, 2} || !got[0].Created.Equal(n.Created) ||
		got[0].Preview != "main...feat" {
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
	fs := newTestFile(t.TempDir())
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
	fs := newTestFile(t.TempDir())
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
	fs := newTestFile(t.TempDir())
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
	fs := newTestFile(t.TempDir())
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
	fs := newTestFile(dir)
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
	fs := newTestFile(dir)
	lock := filepath.Join(dir, "notes.toml.lock")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * filelock.Stale)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := fs.Put(noteAt("aaaaaaaa", 1)); err != nil {
		t.Fatalf("a stale lock must be broken, got %v", err)
	}
	if time.Since(start) > filelock.Wait {
		t.Fatal("a stale lock must be broken without waiting out the full retry budget")
	}
	if got, _ := fs.Load(); len(got) != 1 {
		t.Fatalf("write under a broken stale lock lost the note: %+v", got)
	}
}

func TestConcurrentPutsSerialize(t *testing.T) {
	t.Parallel()
	fs := newTestFile(t.TempDir())
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
	fs := newTestFile(dir)
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
	case <-time.After(200 * time.Millisecond): // well under filelock.Wait
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
// past filelock.Wait fails the write instead of waiting or spinning forever.
func TestHeldLockGivesUpAtTheDeadline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := newTestFile(dir)
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
		if elapsed := time.Since(start); elapsed < filelock.Wait {
			t.Fatalf("gave up after %v, before the %v budget", elapsed, filelock.Wait)
		}
	case <-time.After(4 * filelock.Wait):
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
	fs := newTestFile(dir)
	lock := filepath.Join(dir, "notes.toml.lock")
	if err := os.WriteFile(lock, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * filelock.Stale)
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
	case <-time.After(4 * filelock.Wait):
		t.Fatal("Put spun on an unremovable stale lock instead of giving up")
	}
}

func TestUnreadableFileSurfacesAndDoesNotClobber(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := newTestFile(dir)
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
	fs := newTestFile(dir)
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
	fs := newTestFile(root)
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

// TestSweepCountIncludesTheCapsOwnDrops: Sweep's dropped count is the store's
// real shrinkage, not the predicate's rejection count. mutate applies the entry
// cap AFTER the predicate, and a count taken from the predicate alone
// under-reports the moment the cap bites.
func TestSweepCountIncludesTheCapsOwnDrops(t *testing.T) {
	t.Parallel()
	fs := newTestFile(t.TempDir())
	for i := 0; i < 5; i++ {
		if err := fs.Put(noteAt(string(rune('a'+i)), i)); err != nil {
			t.Fatal(err)
		}
	}
	fs.SetPolicy(Policy{MaxEntries: 2})
	// The predicate rejects one; the cap then trims the surviving four to two.
	dropped, err := fs.Sweep(func(n model.Note) bool { return n.ID != "e" })
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 3 {
		t.Fatalf("dropped = %d, want 3 (1 by the predicate + 2 by the cap)", dropped)
	}
	left, _ := fs.Load()
	if len(left) != 2 {
		t.Fatalf("store holds %d notes, want 2", len(left))
	}
}

// TestLockReleaseOnlyRemovesItsOwn: the release is not an unconditional
// os.Remove. After a stale takeover a SECOND writer can hold a freshly created
// lock under the same name, and removing that would strand it without a lock.
func TestLockReleaseOnlyRemovesItsOwn(t *testing.T) {
	t.Parallel()
	mine := newTestFile(t.TempDir())
	unlock, err := mine.lock()
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if _, err := os.Stat(mine.lockPath()); !os.IsNotExist(err) {
		t.Fatalf("a holder must release its OWN lock, stat err = %v", err)
	}

	taken := newTestFile(t.TempDir())
	unlock, err = taken.lock()
	if err != nil {
		t.Fatal(err)
	}
	// Someone else declared our lock stale and re-took it under the same name.
	if err := os.WriteFile(taken.lockPath(), []byte("another-gg"), 0o644); err != nil {
		t.Fatal(err)
	}
	unlock()
	b, err := os.ReadFile(taken.lockPath())
	if err != nil || string(b) != "another-gg" {
		t.Fatalf("release must leave a lock it no longer owns alone: %q err %v", b, err)
	}
}

func TestReadCorruptWrapsErrCorrupt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.toml"), []byte("notes = [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := newTestFile(dir).Load()
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

func TestCapNeverDropsCommitLevelNotes(t *testing.T) {
	t.Parallel()
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	review := model.Note{ID: "r1", Address: model.FileAddress{State: model.StateCommitted, Commit: strings.Repeat("a", 40)},
		Tags: []string{model.ReviewTag}, Created: old}
	line := func(id string, h int) model.Note {
		return model.Note{ID: id, Address: model.FileAddress{State: model.StateCommitted, Commit: strings.Repeat("b", 40), Path: "f.go"},
			Created: old.Add(time.Duration(h) * time.Hour)}
	}
	got := capOldestFirst([]model.Note{review, line("l1", 1), line("l2", 2), line("l3", 3)}, 2)
	var ids []string
	for _, n := range got {
		ids = append(ids, n.ID)
	}
	if !slices.Contains(ids, "r1") {
		t.Fatalf("cap dropped the review note: kept %v", ids)
	}
	if len(ids) != 3 || slices.Contains(ids, "l1") {
		t.Fatalf("kept %v, want r1 + the two newest line notes", ids)
	}
}

func TestQuarantineMovesTheFileAside(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.toml"), []byte("garbage [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := newTestFile(dir)
	moved, err := fs.Quarantine()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(moved), "notes.toml.corrupt-") {
		t.Fatalf("moved to %q", moved)
	}
	if b, _ := os.ReadFile(moved); string(b) != "garbage [[[" {
		t.Fatalf("moved file content = %q", b)
	}
	if ns, err := fs.Load(); err != nil || len(ns) != 0 {
		t.Fatalf("after quarantine Load = %v, %v; want empty, nil", ns, err)
	}
}

func TestCapExemptsShelfLevelNotes(t *testing.T) {
	t.Parallel()
	fs := newTestFile(t.TempDir())
	fs.SetPolicy(Policy{MaxEntries: 1})
	shelfNote := noteAt("shelf000", 1)
	shelfNote.Address = model.FileAddress{State: model.StateShelf, ShelfID: "e1"}
	for _, n := range []model.Note{shelfNote, noteAt("mid00000", 5), noteAt("new00000", 9)} {
		if err := fs.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := fs.Load()
	if len(got) != 2 {
		t.Fatalf("cap 1 must keep the exempt shelf note + the newest file note, got %d: %+v", len(got), got)
	}
	ids := got[0].ID + "," + got[1].ID
	if !strings.Contains(ids, "shelf000") || !strings.Contains(ids, "new00000") {
		t.Fatalf("want shelf000 and new00000 kept, got %s", ids)
	}
}

// A working review is entry-level: the cap never counts or drops it.
func TestCapExemptsWorkingReviews(t *testing.T) {
	t.Parallel()
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	review := model.Note{ID: "rv", Tags: []string{model.ReviewTag}, Created: old,
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: "/r"}}
	line := func(id string, h int) model.Note {
		return model.Note{ID: id, Address: model.FileAddress{State: model.StateUnstaged, Worktree: "/r", Path: "f.go"},
			Created: old.Add(time.Duration(h) * time.Hour)}
	}
	got := capOldestFirst([]model.Note{review, line("l1", 1), line("l2", 2), line("l3", 3)}, 2)
	var ids []string
	for _, n := range got {
		ids = append(ids, n.ID)
	}
	if !slices.Contains(ids, "rv") || len(ids) != 3 || slices.Contains(ids, "l1") {
		t.Fatalf("kept %v, want the review plus the two newest line notes", ids)
	}
}

// Files round-trips through the TOML part file.
func TestNoteFilesRoundTrip(t *testing.T) {
	t.Parallel()
	st := NewFileStore(t.TempDir())
	now := time.Now().UTC().Truncate(time.Second)
	n := model.Note{ID: "rv1", Source: model.NoteSourceAgent, Tags: []string{model.ReviewTag},
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: "/repo"},
		Files:   []model.NoteFile{{Path: "a b.go", Blob: strings.Repeat("c", 40)}, {Path: "gone.go", Deleted: true}},
		Created: now, Updated: now}
	if err := st.Put(n); err != nil {
		t.Fatal(err)
	}
	got, err := st.Load(WorktreePart("/repo"))
	if err != nil || len(got) != 1 {
		t.Fatalf("Load = %v, %v", got, err)
	}
	if !reflect.DeepEqual(got[0].Files, n.Files) {
		t.Fatalf("Files = %+v, want %+v", got[0].Files, n.Files)
	}
}
