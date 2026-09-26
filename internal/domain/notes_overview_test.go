package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// overviewRepo is a real repo with two commits (c1 adds a.go and b.go, c2
// changes a.go), then a staged and an unstaged edit.
func overviewRepo(t *testing.T) (dir, c1, c2 string) {
	t.Helper()
	dir = t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gittest.Run(t, dir, "init", "-b", "main")
	write("a.go", "one\ntwo\n")
	write("b.go", "bee\n")
	gittest.Run(t, dir, "add", ".")
	gittest.Run(t, dir, "commit", "-m", "first", "--date", "2026-01-01T00:00:00Z")
	c1 = headSHA(t, dir)
	write("a.go", "one\ntwo\nthree\n")
	gittest.Run(t, dir, "add", ".")
	gittest.Run(t, dir, "commit", "-m", "second", "--date", "2026-02-01T00:00:00Z")
	c2 = headSHA(t, dir)
	write("b.go", "bee staged\n")
	gittest.Run(t, dir, "add", "b.go")
	write("a.go", "one\ntwo\nthree\nfour\n")
	return dir, c1, c2
}

func addNote(t *testing.T, svc *Service, addr model.FileAddress, line int, text, summary string) model.Note {
	t.Helper()
	n, err := svc.NoteAdd(context.Background(), model.Note{
		Address: addr, Side: model.NoteSideNew, Range: [2]int{line, line},
		Summary: summary, ContextHash: model.NoteContextHash([]string{text}),
	})
	if err != nil {
		t.Fatalf("NoteAdd %s: %v", summary, err)
	}
	return n
}

// The overview groups every note this checkout can see by what it hangs off:
// working-tree states, then commits newest first (a missing commit last), each
// resolved against its own text — an orphaned or stale note stays listed.
func TestNotesOverviewGroupsEveryNote(t *testing.T) {
	t.Parallel()
	dir, c1, c2 := overviewRepo(t)
	svc := svcIn(t, dir)
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	ctx := context.Background()
	top, err := svc.TopLevel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	live := func(st model.FileState, path string) model.FileAddress {
		return model.FileAddress{State: st, Worktree: top, Path: path}
	}
	commit := func(sha, path string) model.FileAddress {
		return model.FileAddress{State: model.StateCommitted, Commit: sha, Path: path}
	}

	addNote(t, svc, live(model.StateUnstaged, "a.go"), 4, "four", "unstaged a")
	addNote(t, svc, live(model.StateStaged, "b.go"), 1, "bee staged", "staged b")
	stale := addNote(t, svc, commit(c1, "b.go"), 1, "bee", "old b")
	addNote(t, svc, commit(c1, "a.go"), 2, "two", "first a")
	root := addNote(t, svc, commit(c2, "a.go"), 3, "three", "second a")
	if _, err := svc.NoteReply(ctx, root.ID, model.Note{Summary: "agreed"}); err != nil {
		t.Fatalf("NoteReply: %v", err)
	}
	gone := "0123456789abcdef0123456789abcdef01234567"
	addNote(t, svc, commit(gone, "x.go"), 1, "x", "on a missing commit")
	// A sibling worktree's working-tree note is not this checkout's.
	addNote(t, svc, model.FileAddress{State: model.StateUnstaged, Worktree: "/elsewhere", Path: "a.go"}, 1, "one", "sibling")

	// Make the c1/b.go note stale: its anchored text is not what the file says.
	all, _ := svc.notesStore(ctx).Load()
	for _, n := range all {
		if n.ID == stale.ID {
			n.ContextHash = model.NoteContextHash([]string{"nothing like it"})
			if err := svc.notesStore(ctx).Put(n); err != nil {
				t.Fatal(err)
			}
		}
	}

	ov, err := svc.NotesOverview(ctx)
	if err != nil {
		t.Fatalf("NotesOverview: %v", err)
	}
	if len(ov.Unstaged) != 1 || ov.Unstaged[0].Addr.Path != "a.go" || ov.Unstaged[0].Notes[0].Note.Summary != "unstaged a" {
		t.Fatalf("Unstaged = %+v", ov.Unstaged)
	}
	if len(ov.Staged) != 1 || ov.Staged[0].Addr.Path != "b.go" {
		t.Fatalf("Staged = %+v", ov.Staged)
	}
	if len(ov.Untracked) != 0 {
		t.Fatalf("Untracked = %+v", ov.Untracked)
	}
	if len(ov.Commits) != 3 {
		t.Fatalf("want 3 commits (c2, c1, missing), got %+v", ov.Commits)
	}
	if ov.Commits[0].Hash != c2 || ov.Commits[0].Subject != "second" || ov.Commits[0].Missing {
		t.Fatalf("newest commit first: %+v", ov.Commits[0])
	}
	if got := ov.Commits[0].Files[0]; got.Addr.Path != "a.go" || got.Status != "M" || len(got.Notes[0].Replies) != 1 {
		t.Fatalf("c2 file = %+v", got)
	}
	if ov.Commits[1].Hash != c1 || len(ov.Commits[1].Files) != 2 {
		t.Fatalf("c1 = %+v", ov.Commits[1])
	}
	if f := ov.Commits[1].Files[0]; f.Addr.Path != "a.go" || f.Status != "A" || f.Notes[0].Status != model.NoteActive {
		t.Fatalf("c1 a.go = %+v", f)
	}
	if f := ov.Commits[1].Files[1]; f.Addr.Path != "b.go" || f.Notes[0].Status != model.NoteStale {
		t.Fatalf("c1 b.go must be listed stale: %+v", f)
	}
	if m := ov.Commits[2]; m.Hash != gone || !m.Missing || m.Files[0].Notes[0].Note.Summary != "on a missing commit" {
		t.Fatalf("missing commit last: %+v", m)
	}
	if ov.Count() != 6 {
		t.Fatalf("Count = %d, want 6 threads (the sibling's excluded)", ov.Count())
	}
}

func TestNotesOverviewEmpty(t *testing.T) {
	t.Parallel()
	dir, _, _ := overviewRepo(t)
	svc := svcIn(t, dir)
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	ov, err := svc.NotesOverview(context.Background())
	if err != nil || ov.Count() != 0 {
		t.Fatalf("empty store: %+v, %v", ov, err)
	}
}

// Files are dir-major: root files first, then each directory's files together,
// so a frontend drawing one heading per directory never draws one twice.
func TestSortNoteFilesIsDirMajor(t *testing.T) {
	t.Parallel()
	var fs []NoteFileNotes
	for _, p := range []string{"a/p.go", "z.go", "a/n/o.go", "a/m.go", "b.go"} {
		fs = append(fs, NoteFileNotes{Addr: model.FileAddress{Path: p}})
	}
	sortNoteFiles(fs)
	var got []string
	for _, f := range fs {
		got = append(got, f.Addr.Path)
	}
	want := []string{"b.go", "z.go", "a/m.go", "a/p.go", "a/n/o.go"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("order = %v, want %v", got, want)
	}
}
