package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/shelf"
)

// svcIn builds a Service rooted at dir over a real git.
func svcIn(t *testing.T, dir string) *Service {
	t.Helper()
	return New(&git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))})
}

// noteSideRepo is a real repo whose file has a different body in HEAD, in the
// index and in the working tree, so every old/new row of the address table
// reads a DISTINGUISHABLE side.
func noteSideRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gittest.Run(t, dir, "init", "-b", "main")
	write("a.go", "head\n")
	gittest.Run(t, dir, "add", "a.go")
	gittest.Run(t, dir, "commit", "-m", "root") // a ROOT commit: no first parent
	write("a.go", "index\n")
	gittest.Run(t, dir, "add", "a.go")
	write("a.go", "worktree\n")
	write("new.go", "untracked\n")
	return dir
}

func headSHA(t *testing.T, dir string) string {
	t.Helper()
	svc := svcIn(t, dir)
	c, err := svc.CommitMeta(context.Background(), "HEAD")
	if err != nil {
		t.Fatalf("CommitMeta: %v", err)
	}
	return c.Hash
}

// The old-side base table of §4.4, against real git: every Address.State reads
// the right pair of sides. A ROOT commit's old side is EMPTY, not an error —
// otherwise NoteAdd cannot fill a fingerprint and the note is stale forever.
func TestNoteSideLinesReadsEveryAddressState(t *testing.T) {
	t.Parallel()
	dir := noteSideRepo(t)
	svc := svcIn(t, dir)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	ctx := context.Background()
	sha := headSHA(t, dir)

	shelved, err := svc.ShelfAdd(ctx, model.FileAddress{
		State: model.StateUnstaged, Worktree: dir, Path: "a.go",
	}, "")
	if err != nil {
		t.Fatalf("ShelfAdd: %v", err)
	}

	live := func(state model.FileState, path string) model.FileAddress {
		return model.FileAddress{State: state, Worktree: dir, Path: path}
	}
	cases := []struct {
		name string
		addr model.FileAddress
		side model.NoteSide
		want []string // nil = the side must be ABSENT
	}{
		{"unstaged/new = the working file", live(model.StateUnstaged, "a.go"), model.NoteSideNew, []string{"worktree"}},
		{"unstaged/old = the index blob", live(model.StateUnstaged, "a.go"), model.NoteSideOld, []string{"index"}},
		{"staged/new = the index blob", live(model.StateStaged, "a.go"), model.NoteSideNew, []string{"index"}},
		{"staged/old = the HEAD blob", live(model.StateStaged, "a.go"), model.NoteSideOld, []string{"head"}},
		{"untracked/new = the working file", live(model.StateUntracked, "new.go"), model.NoteSideNew, []string{"untracked"}},
		{"untracked/old is absent", live(model.StateUntracked, "new.go"), model.NoteSideOld, nil},
		{"committed/new = the commit blob",
			model.FileAddress{State: model.StateCommitted, Commit: sha, Path: "a.go"}, model.NoteSideNew, []string{"head"}},
		{"committed/old of a ROOT commit is empty, not an error",
			model.FileAddress{State: model.StateCommitted, Commit: sha, Path: "a.go"}, model.NoteSideOld, []string{}},
		{"shelf/new = the stored bytes",
			model.FileAddress{State: model.StateShelf, ShelfID: shelved.ID, Path: "a.go"}, model.NoteSideNew, []string{"worktree"}},
		{"shelf/old is absent",
			model.FileAddress{State: model.StateShelf, ShelfID: shelved.ID, Path: "a.go"}, model.NoteSideOld, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.noteSideLines(ctx, tc.addr, tc.side)
			if err != nil {
				t.Fatalf("noteSideLines: %v", err)
			}
			if tc.want == nil {
				if got != nil {
					t.Fatalf("side must be absent (nil), got %q", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("side must be present, got nil")
			}
			if len(got) != len(tc.want) {
				t.Fatalf("lines = %q, want %q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("lines = %q, want %q", got, tc.want)
				}
			}
		})
	}
}

// A note on the old side of a root commit must be STORABLE with a real
// fingerprint: the empty old side hashes, rather than leaving ContextHash ""
// (which never re-anchors, so the note would be stale forever).
func TestNoteAddOnARootCommitFillsAFingerprint(t *testing.T) {
	t.Parallel()
	dir := noteSideRepo(t)
	svc := svcIn(t, dir)
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	ctx := context.Background()

	got, err := svc.NoteAdd(ctx, model.Note{
		Address: model.FileAddress{State: model.StateCommitted, Commit: headSHA(t, dir), Path: "a.go"},
		Side:    model.NoteSideOld, Range: [2]int{1, 1}, Summary: "on the empty base",
	})
	if err != nil {
		t.Fatalf("NoteAdd: %v", err)
	}
	if got.ContextHash == "" {
		t.Fatal("a root commit's old side must still produce a fingerprint")
	}
}

// NoteAdd must refuse an anchor past the end of its side rather than silently
// clamping it (anchorLines used to do exactly that): "the new side has 1
// line" and Range{2,2} is a bad batch item, not a note on line 1.
func TestNoteAddRejectsAnchorPastTheEndOfItsSide(t *testing.T) {
	t.Parallel()
	dir := noteSideRepo(t)
	svc := svcIn(t, dir)
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	ctx := context.Background()

	addr := model.FileAddress{State: model.StateUnstaged, Worktree: dir, Path: "a.go"} // 1 line on the new (working) side
	_, err := svc.NoteAdd(ctx, model.Note{
		Address: addr, Side: model.NoteSideNew, Range: [2]int{2, 2}, Summary: "past end",
	})
	if err == nil {
		t.Fatal("an anchor past the end of the side must be refused")
	}
	if !strings.Contains(err.Error(), "line 2 is past the end of the new side of a.go (1 lines)") {
		t.Fatalf("err = %q, want it to name the line, side, path and length", err)
	}
	if left, _ := svc.notesStore(ctx).Load(); len(left) != 0 {
		t.Fatalf("a refused add must store nothing, got %+v", left)
	}

	// A structurally bad range (start > end) is refused too, with the
	// invalid-range wording.
	_, err = svc.NoteAdd(ctx, model.Note{
		Address: addr, Side: model.NoteSideNew, Range: [2]int{2, 1}, Summary: "backwards",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid range") {
		t.Fatalf("err = %v, want an invalid-range error", err)
	}
}

// twoWorktrees returns a repo and a linked worktree of it, each with its OWN
// body for the same path.
func twoWorktrees(t *testing.T) (main, linked string) {
	t.Helper()
	main = t.TempDir()
	gittest.Run(t, main, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(main, "a.go"), []byte("main body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, main, "add", "a.go")
	gittest.Run(t, main, "commit", "-m", "init")

	linked = filepath.Join(t.TempDir(), "linked")
	gittest.Run(t, main, "worktree", "add", "-b", "side", linked)
	if err := os.WriteFile(filepath.Join(linked, "a.go"), []byte("linked body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return main, linked
}

// The store is keyed by the git COMMON dir, so every worktree of a repo shares
// it — but worktree content is per-checkout. A note taken in the linked
// worktree must therefore be invisible to, and unswept by, a Service rooted in
// the main checkout; and it must be dropped once its worktree is gone.
func TestWorktreeNotesAreScopedToTheirCheckout(t *testing.T) {
	t.Parallel()
	main, linked := twoWorktrees(t)
	store := notes.NewFileStore(t.TempDir()) // one store, as production shares one
	ctx := context.Background()

	svcLinked := svcIn(t, linked)
	svcLinked.SetNotesStore(store)
	svcMain := svcIn(t, main)
	svcMain.SetNotesStore(store)

	// A note in the LINKED worktree, anchored on its own body. The address
	// carries no Worktree: NoteAdd must pin it from the Service's checkout.
	n, err := svcLinked.NoteAdd(ctx, model.Note{
		Address: model.FileAddress{State: model.StateUnstaged, Path: "a.go"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "linked note",
	})
	if err != nil {
		t.Fatalf("NoteAdd: %v", err)
	}
	// Compare against git's own notion of the checkout, not the Go-built path:
	// git resolves symlinks (on macOS t.TempDir() is /var/... and git prints
	// /private/var/...), so a literal comparison would be a Linux-only pass.
	wantWT, err := svcLinked.TopLevel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n.Address.Worktree != filepath.Clean(wantWT) {
		t.Fatalf("NoteAdd must pin the Service's checkout: Worktree = %q, want %q", n.Address.Worktree, wantWT)
	}
	if n.ContextHash != model.NoteContextHash([]string{"linked body"}) {
		t.Fatal("the fingerprint must come from the note's OWN worktree")
	}

	// The main checkout must neither list it nor badge its path.
	res, err := svcMain.NotesFor(ctx, model.FileAddress{
		State: model.StateUnstaged, Worktree: main, Path: "a.go",
	}, sideDiff("main body"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 0 {
		t.Fatalf("a sibling worktree's note must not surface here: %+v", res)
	}
	c, err := svcMain.NoteCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c.ByPath["a.go"] != 0 {
		t.Fatalf("ByPath must count only THIS checkout: %+v", c.ByPath)
	}
	if c, err = svcLinked.NoteCounts(ctx); err != nil || c.ByPath["a.go"] != 1 {
		t.Fatalf("the owning checkout must count it: %+v %v", c, err)
	}

	// A sweep run from the MAIN checkout must resolve the note against the
	// LINKED worktree's content and therefore keep it.
	svcMain.SetNotesPolicy(-1, 2000) // isolate: expiry off, resolution only
	dropped, err := svcMain.sweepNotes(ctx)
	if err != nil {
		t.Fatalf("sweepNotes: %v", err)
	}
	if dropped != 0 {
		t.Fatalf("a sweep from another worktree must not drop %s", n.ID)
	}

	// Once the worktree is gone the note is unreadable — orphaned — and the
	// next sweep drops it.
	if err := os.RemoveAll(linked); err != nil {
		t.Fatal(err)
	}
	if dropped, err = svcMain.sweepNotes(ctx); err != nil || dropped != 1 {
		t.Fatalf("a note whose worktree is gone must be swept: dropped %d err %v", dropped, err)
	}
}

// TestNoteAddRefusesWhenTheSideCannotBeFingerprinted pins Minor 1. A note
// stored with an EMPTY ContextHash can never re-anchor (findAnchor returns 0
// for an empty hash), so it is born permanently stale and the next sweep
// deletes it — silently, long after the caller was told the write succeeded.
// An unreadable side, and a side that is not there at all, are refused instead.
func TestNoteAddRefusesWhenTheSideCannotBeFingerprinted(t *testing.T) {
	t.Parallel()
	dir := noteSideRepo(t)
	svc := svcIn(t, dir)
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	ctx := context.Background()

	// The path is in no index and on no disk: git says "does not exist".
	if _, err := svc.NoteAdd(ctx, model.Note{
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: dir, Path: "nowhere.go"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "unreadable side",
	}); err == nil {
		t.Fatal("a side that cannot be read must be an error, not a doomed note")
	}
	// An untracked file has NO old side at all.
	if _, err := svc.NoteAdd(ctx, model.Note{
		Address: model.FileAddress{State: model.StateUntracked, Worktree: dir, Path: "a.go"},
		Side:    model.NoteSideOld, Range: [2]int{1, 1}, Summary: "absent side",
	}); err == nil {
		t.Fatal("an absent side must be an error, not a doomed note")
	}
	// Nothing was written either way.
	if left, _ := svc.notesStore(ctx).Load(); len(left) != 0 {
		t.Fatalf("a refused add must store nothing, got %+v", left)
	}
	// The readable case still fills a fingerprint and stores.
	got, err := svc.NoteAdd(ctx, model.Note{
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: dir, Path: "a.go"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "fine",
	})
	if err != nil || got.ContextHash == "" {
		t.Fatalf("a readable side must still store with a fingerprint: %v / %q", err, got.ContextHash)
	}
}
