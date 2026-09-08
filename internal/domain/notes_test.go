package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
	"github.com/homeend/gigagit/internal/textdiff"
)

func notesSvc(t *testing.T) (*Service, *gitexec.FakeRunner) {
	t.Helper()
	f := gitexec.NewFakeRunner()
	svc := New(&git.Repo{Runner: f})
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	return svc, f
}

// wtAddr is the working-tree address every test note hangs off.
func wtAddr(path string) model.FileAddress {
	return model.FileAddress{State: model.StateUnstaged, Worktree: "/wt", Path: path}
}

// sideDiff builds a Diff whose NEW side is the given lines (numbered from 1)
// and whose OLD side is the same lines — enough for resolution tests.
func sideDiff(lines ...string) Diff {
	rows := make([]textdiff.Row, len(lines))
	for i, l := range lines {
		rows[i] = textdiff.Row{Kind: textdiff.Same, Left: l, Right: l, LeftNo: i + 1, RightNo: i + 1}
	}
	return Diff{Result: textdiff.Result{Rows: rows}}
}

func TestNoteAddFillsIDTimesAndSource(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	got, err := svc.NoteAdd(context.Background(), model.Note{
		Address: wtAddr("a/b.go"), Side: model.NoteSideNew, Range: [2]int{2, 2},
		Summary: "off by one", ContextHash: model.NoteContextHash([]string{"b"}),
	})
	if err != nil {
		t.Fatalf("NoteAdd: %v", err)
	}
	if len(got.ID) != 8 || got.Created.IsZero() || got.Updated.IsZero() {
		t.Fatalf("NoteAdd must fill ID/Created/Updated: %+v", got)
	}
	if got.Source != model.NoteSourceUser {
		t.Fatalf("default Source = %q, want user", got.Source)
	}
}

func TestNoteEditReplyRemoveThread(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	ctx := context.Background()
	root, err := svc.NoteAdd(ctx, model.Note{
		Address: wtAddr("a/b.go"), Side: model.NoteSideNew, Range: [2]int{1, 1},
		Summary: "first", ContextHash: model.NoteContextHash([]string{"a"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.NoteEdit(ctx, root.ID, "edited", "why"); err != nil {
		t.Fatalf("NoteEdit: %v", err)
	}
	rep, err := svc.NoteReply(ctx, root.ID, model.Note{Summary: "agreed", Source: model.NoteSourceAgent, Author: "bot"})
	if err != nil {
		t.Fatalf("NoteReply: %v", err)
	}
	if rep.ParentID != root.ID || rep.Side != root.Side || rep.Range != root.Range ||
		rep.ContextHash != root.ContextHash || rep.Address.Path != root.Address.Path {
		t.Fatalf("a reply must inherit the parent's anchor: %+v", rep)
	}
	res, err := svc.NotesFor(ctx, wtAddr("a/b.go"), sideDiff("a", "b"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Note.Summary != "edited" || res[0].Note.Rationale != "why" ||
		len(res[0].Replies) != 1 || res[0].Replies[0].Note.Summary != "agreed" {
		t.Fatalf("threading/edit lost: %+v", res)
	}
	if err := svc.NoteRemove(ctx, root.ID); err != nil {
		t.Fatal(err)
	}
	res, _ = svc.NotesFor(ctx, wtAddr("a/b.go"), sideDiff("a", "b"))
	if len(res) != 0 {
		t.Fatalf("removing a root must take its replies, left %+v", res)
	}
}

func TestResolveOneFourOutcomes(t *testing.T) {
	t.Parallel()
	lines := []string{"alpha", "beta", "gamma", "delta"}
	base := model.Note{Side: model.NoteSideNew, Range: [2]int{2, 2}}

	// 1. hash matches where it was → active, same range.
	n := base
	n.ContextHash = model.NoteContextHash([]string{"beta"})
	if st, rg := resolveOne(n, lines); st != model.NoteActive || rg != [2]int{2, 2} {
		t.Fatalf("unchanged = %v %v, want active {2 2}", st, rg)
	}

	// 2. the anchored line moved (two lines inserted above) → active, moved.
	moved := []string{"x", "y", "alpha", "beta", "gamma"}
	if st, rg := resolveOne(n, moved); st != model.NoteActive || rg != [2]int{4, 4} {
		t.Fatalf("moved = %v %v, want active {4 4}", st, rg)
	}
	// Re-indentation must not break the match (the hash trims).
	indented := []string{"alpha", "    beta", "gamma"}
	if st, rg := resolveOne(n, indented); st != model.NoteActive || rg != [2]int{2, 2} {
		t.Fatalf("re-indented = %v %v, want active {2 2}", st, rg)
	}

	// 3. the line's text is gone but the file is not → stale, clamped.
	if st, rg := resolveOne(n, []string{"alpha"}); st != model.NoteStale || rg != [2]int{1, 1} {
		t.Fatalf("gone-text = %v %v, want stale clamped {1 1}", st, rg)
	}

	// 4. the side itself is absent (file/commit gone) → orphaned.
	if st, _ := resolveOne(n, nil); st != model.NoteOrphaned {
		t.Fatalf("absent side = %v, want orphaned", st)
	}
}

// TestFindAnchorPrefersTheNearestMatch pins the outward search: with the same
// text above AND below the stored line, the nearer copy wins, and on a tie the
// one before it does.
func TestFindAnchorPrefersTheNearestMatch(t *testing.T) {
	t.Parallel()
	hash := model.NoteContextHash([]string{"target"})
	// target at 1 and 5; stored start 4 → the one below (5) is nearer.
	lines := []string{"target", "a", "b", "c", "target"}
	if got := findAnchor(lines, hash, 1, 4); got != 5 {
		t.Fatalf("nearest below = %d, want 5", got)
	}
	// stored start 2 → the one above (1) is nearer.
	if got := findAnchor(lines, hash, 1, 2); got != 1 {
		t.Fatalf("nearest above = %d, want 1", got)
	}
	// equidistant (3 is the midpoint of 1 and 5) → the earlier one wins.
	if got := findAnchor(lines, hash, 1, 3); got != 1 {
		t.Fatalf("tie = %d, want the earlier match 1", got)
	}
	// no match anywhere terminates and reports 0.
	if got := findAnchor(lines, model.NoteContextHash([]string{"nope"}), 1, 3); got != 0 {
		t.Fatalf("missing = %d, want 0", got)
	}
	// a stored start far past the end still terminates.
	if got := findAnchor(lines, hash, 1, 99); got != 5 {
		t.Fatalf("out-of-range start = %d, want 5", got)
	}
	// a window longer than the side cannot match.
	if got := findAnchor(lines, hash, 99, 1); got != 0 {
		t.Fatalf("span > side = %d, want 0", got)
	}
}

func TestNotesForHidesOrphansAndSortsByLine(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	ctx := context.Background()
	add := func(path string, line int, text string) {
		if _, err := svc.NoteAdd(ctx, model.Note{
			Address: wtAddr(path), Side: model.NoteSideNew, Range: [2]int{line, line},
			Summary: text, ContextHash: model.NoteContextHash([]string{text}),
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("a/b.go", 3, "gamma")
	add("a/b.go", 1, "alpha")
	add("other.go", 1, "alpha")

	res, err := svc.NotesFor(ctx, wtAddr("a/b.go"), sideDiff("alpha", "beta", "gamma"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].Range[0] != 1 || res[1].Range[0] != 3 {
		t.Fatalf("notes must be filtered by address and sorted by line: %+v", res)
	}
	// An address whose side is empty (deleted file) hides its notes entirely.
	res, _ = svc.NotesFor(ctx, wtAddr("a/b.go"), Diff{})
	if len(res) != 0 {
		t.Fatalf("orphaned notes must be hidden, got %+v", res)
	}
}

func TestNotesForIsStateInsensitiveWithinTheWorkingTree(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	ctx := context.Background()
	if _, err := svc.NoteAdd(ctx, model.Note{
		Address: wtAddr("a/b.go"), Side: model.NoteSideNew, Range: [2]int{1, 1},
		Summary: "x", ContextHash: model.NoteContextHash([]string{"alpha"}),
	}); err != nil {
		t.Fatal(err)
	}
	staged := model.FileAddress{State: model.StateStaged, Worktree: "/wt", Path: "a/b.go"}
	res, err := svc.NotesFor(ctx, staged, sideDiff("alpha"))
	if err != nil || len(res) != 1 {
		t.Fatalf("a working-tree note must show on the staged diff too: %+v %v", res, err)
	}
	commit := model.FileAddress{State: model.StateCommitted, Commit: "deadbee", Path: "a/b.go"}
	res, _ = svc.NotesFor(ctx, commit, sideDiff("alpha"))
	if len(res) != 0 {
		t.Fatalf("a working-tree note must NOT show on a commit diff: %+v", res)
	}
}

func TestNoteCountsCachedAndInvalidated(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	ctx := context.Background()
	root, err := svc.NoteAdd(ctx, model.Note{
		Address: wtAddr("a/b.go"), Side: model.NoteSideNew, Range: [2]int{1, 1}, Summary: "x",
		ContextHash: "h",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NoteAdd(ctx, model.Note{
		Address: model.FileAddress{State: model.StateCommitted, Commit: "c0ffee", Path: "z.go"},
		Side:    model.NoteSideNew, Range: [2]int{2, 2}, Summary: "y", ContextHash: "h",
	}); err != nil {
		t.Fatal(err)
	}
	c, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c.ByPath["a/b.go"] != 1 || c.ByCommit["c0ffee"] != 1 || c.ByCommitPath["c0ffee:z.go"] != 1 {
		t.Fatalf("counts = %+v", c)
	}
	// A reply must NOT bump the count (a badge counts THREADS).
	if _, err := svc.NoteReply(ctx, root.ID, model.Note{Summary: "r"}); err != nil {
		t.Fatal(err)
	}
	if c, _ = svc.NoteCounts(ctx); c.ByPath["a/b.go"] != 1 {
		t.Fatalf("replies must not count: %+v", c)
	}
	// Removal invalidates the cache.
	if err := svc.NoteRemove(ctx, root.ID); err != nil {
		t.Fatal(err)
	}
	if c, _ = svc.NoteCounts(ctx); c.ByPath["a/b.go"] != 0 {
		t.Fatalf("count cache must be invalidated by a mutation: %+v", c)
	}
}

func TestNotesDisabledWithoutAStore(t *testing.T) {
	t.Parallel()
	svc := New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	svc.disableNotesForTest() // never write NotesStatePath here: package var, parallel test
	if _, err := svc.NoteCounts(context.Background()); err != ErrNotesDisabled {
		t.Fatalf("NoteCounts without a store = %v, want ErrNotesDisabled", err)
	}
}
