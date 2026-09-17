package linkhist

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func entryAt(link string, sec int) Entry {
	return Entry{
		Link:    link,
		Desc:    "desc-" + link,
		Created: time.Unix(int64(1_700_000_000+sec), 0).UTC().Format(time.RFC3339),
	}
}

// 1. List on a missing file is empty with a nil error.
func TestListOnMissingFileIsEmpty(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	got, err := fs.List()
	if err != nil || len(got) != 0 {
		t.Fatalf("List on a missing file = %v, %v; want empty, nil", got, err)
	}
}

// 2. Record then List returns the entry.
func TestRecordThenListReturnsEntry(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	e := entryAt("gg://repo/abc123", 1)
	if err := fs.Record(e); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, err := fs.List()
	if err != nil || len(got) != 1 || got[0] != e {
		t.Fatalf("List = %+v, %v; want [%+v]", got, err, e)
	}
}

// 3. Dedup-to-top: record A, B, A -> [A, B], and A's Desc is the second one's.
func TestRecordDedupToTop(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	a1 := entryAt("gg://repo/a", 1)
	b := entryAt("gg://repo/b", 2)
	a2 := entryAt("gg://repo/a", 3)
	a2.Desc = "second"
	for _, e := range []Entry{a1, b, a2} {
		if err := fs.Record(e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := fs.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Link != "gg://repo/a" || got[1].Link != "gg://repo/b" {
		t.Fatalf("ring = %+v, want [a, b]", got)
	}
	if got[0].Desc != "second" {
		t.Fatalf("re-recorded entry's Desc = %q, want %q (the SECOND record's)", got[0].Desc, "second")
	}
}

// 4. Cap: record 25 distinct links -> len == 20 and the oldest five are gone.
func TestRecordCapsAtMax(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	for i := 0; i < 25; i++ {
		if err := fs.Record(entryAt(fmt.Sprintf("gg://repo/%02d", i), i)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := fs.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != Max {
		t.Fatalf("len = %d, want %d (Max)", len(got), Max)
	}
	if got[0].Link != "gg://repo/24" {
		t.Fatalf("newest-first: got[0] = %+v, want link 24", got[0])
	}
	for _, dropped := range []string{"gg://repo/00", "gg://repo/01", "gg://repo/02", "gg://repo/03", "gg://repo/04"} {
		for _, e := range got {
			if e.Link == dropped {
				t.Fatalf("the oldest five must be gone, found %q in %+v", dropped, got)
			}
		}
	}
}

// 5. Blank link is a silent no-op.
func TestRecordBlankLinkIsNoop(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	if err := fs.Record(Entry{Link: ""}); err != nil {
		t.Fatal(err)
	}
	if err := fs.Record(Entry{Link: "   "}); err != nil {
		t.Fatal(err)
	}
	got, err := fs.List()
	if err != nil || len(got) != 0 {
		t.Fatalf("blank Link must not record, got %+v, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(fs.root, "links.toml")); !os.IsNotExist(err) {
		t.Fatal("a blank-Link Record must not even create the file")
	}
}

// 6. Corrupt file -> List returns an ERROR, never empty.
func TestListOnCorruptFileErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "links.toml"), []byte("links = [not-toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := NewFileStore(dir)
	if _, err := fs.List(); err == nil {
		t.Fatal("List on a corrupt file must surface the error, not read as empty")
	}
}

// Gap 2: Record on a corrupt file must fail (propagate the parse error from
// the read-merge INSIDE the lock) rather than treat it as empty and rewrite
// the store from scratch. Mirrors notes.TestUnreadableFileSurfacesAndDoesNotClobber.
func TestRecordOnCorruptFileErrorsAndDoesNotClobber(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := NewFileStore(dir)
	if err := fs.Record(entryAt("gg://repo/a", 1)); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte("links = [not-toml\n")
	if err := os.WriteFile(filepath.Join(dir, "links.toml"), corrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.List(); err == nil {
		t.Fatal("List on a corrupt file must surface the error")
	}
	if err := fs.Record(entryAt("gg://repo/b", 2)); err == nil {
		t.Fatal("Record on a corrupt file must fail rather than rewrite from an empty base")
	}
	got, err := os.ReadFile(filepath.Join(dir, "links.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(corrupt) {
		t.Fatalf("the corrupt file was clobbered:\n%s", got)
	}
}

// 7. Concurrent writers: two goroutines on ONE FileStore each recording 20
// distinct links; afterwards exactly 20 remain and every one is well-formed.
// Run under -race.
//
// This proves the process-local sync.Mutex: both goroutines share fs, so
// they are serialised in-process regardless of the file lock. It does NOT
// exercise the cross-process lock — see TestRecordWaitsForAHeldLock for that.
func TestConcurrentRecordsOnOneStoreAllLand(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	var wg sync.WaitGroup
	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				link := fmt.Sprintf("gg://repo/g%d-%02d", g, i)
				if err := fs.Record(entryAt(link, g*100+i)); err != nil {
					t.Errorf("Record(%s): %v", link, err)
				}
			}
		}(g)
	}
	wg.Wait()
	got, err := fs.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != Max {
		t.Fatalf("len = %d, want %d — a writer lost an entry", len(got), Max)
	}
	for _, e := range got {
		if e.Link == "" || e.Desc == "" || e.Created == "" {
			t.Fatalf("malformed entry survived: %+v", e)
		}
	}
}

// Gap 1: TestConcurrentRecordsOnOneStoreAllLand proves the in-process mutex
// only — both goroutines share one FileStore, so the O_EXCL file lock is
// never contended and removing it would leave that test green.
//
// TestRecordWaitsForAHeldLock is the only test whose failure proves the
// CROSS-PROCESS lock is load-bearing: it creates links.toml.lock itself (no
// FileStore in this process holds the process mutex for it), then calls
// Record from a goroutine and asserts it blocks until the externally-held
// lock is released. Mirrors internal/notes's TestPutWaitsForAHeldLock.
func TestRecordWaitsForAHeldLock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := NewFileStore(dir)
	lock := filepath.Join(dir, "links.toml.lock")
	held, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	held.Close()

	done := make(chan error, 1)
	go func() { done <- fs.Record(entryAt("gg://repo/a", 1)) }()

	select {
	case err := <-done:
		t.Fatalf("Record returned (%v) while the lock was held", err)
	case <-time.After(200 * time.Millisecond): // well under filelock.Wait
	}

	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Record after the lock was released: %v", err)
	}
	if got, _ := fs.List(); len(got) != 1 {
		t.Fatalf("the waiting writer lost its entry: %+v", got)
	}
}
