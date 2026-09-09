package domain

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
	"github.com/homeend/gigagit/internal/textdiff"
)

func notesSvc(t *testing.T) (*Service, *gitexec.FakeRunner) {
	t.Helper()
	f := gitexec.NewFakeRunner()
	// Worktree-state notes are scoped to their worktree, so every path that
	// stores or matches one resolves this Service's checkout.
	f.SetResponse("git rev-parse (toplevel)", gitexec.Result{Stdout: "/wt\n"})
	svc := New(&git.Repo{Runner: f})
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	return svc, f
}

// hookStore wraps a real store and fires onLoad on every Load — the seam that
// makes "a mutation landed while NoteCounts was computing" deterministic.
type hookStore struct {
	notes.Store
	onLoad func()
}

func (h *hookStore) Load() ([]model.Note, error) {
	if h.onLoad != nil {
		h.onLoad()
	}
	return h.Store.Load()
}

// countingStore records how often the write-time policy was pushed onto it.
type countingStore struct {
	notes.Store
	policy int
}

func (c *countingStore) SetPolicy(p notes.Policy) {
	c.policy++
	c.Store.SetPolicy(p)
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

// A reply to a REPLY must flatten onto the root: the store's orphan-reply
// prune builds its root set from non-replies, so a nested reply would be
// silently dropped inside Put while NoteReply reported success.
func TestNoteReplyToAReplyFlattensToTheRoot(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	ctx := context.Background()
	root, err := svc.NoteAdd(ctx, model.Note{
		Address: wtAddr("a/b.go"), Side: model.NoteSideNew, Range: [2]int{1, 1},
		Summary: "root", ContextHash: model.NoteContextHash([]string{"a"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.NoteReply(ctx, root.ID, model.Note{Summary: "first reply"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.NoteReply(ctx, first.ID, model.Note{Summary: "reply to the reply"})
	if err != nil {
		t.Fatalf("NoteReply to a reply: %v", err)
	}
	if second.ParentID != root.ID {
		t.Fatalf("a nested reply must re-point at the root: ParentID = %q, want %q", second.ParentID, root.ID)
	}
	res, err := svc.NotesFor(ctx, wtAddr("a/b.go"), sideDiff("a", "b"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || len(res[0].Replies) != 2 {
		t.Fatalf("both replies must survive under the root: %+v", res)
	}
}

// The write-time policy must be pushed when the store is resolved (and from
// SetNotesPolicy), never once per read: FileStore.SetPolicy takes the same
// mutex the writer holds across its lock spin, so a per-read push would make
// every read block behind a contended write.
func TestNotesPolicyIsPushedOnceNotPerRead(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-parse (toplevel)", gitexec.Result{Stdout: "/wt\n"})
	svc := New(&git.Repo{Runner: f})
	cs := &countingStore{Store: notes.NewFileStore(t.TempDir())}
	svc.SetNotesStore(cs)
	ctx := context.Background()
	if cs.policy != 1 {
		t.Fatalf("injecting a store must push the effective policy once, got %d", cs.policy)
	}

	for i := 0; i < 3; i++ {
		if _, err := svc.NotesFor(ctx, wtAddr("a/b.go"), sideDiff("a")); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.NoteCounts(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if cs.policy != 1 {
		t.Fatalf("reads must not push the policy: %d pushes after 6 reads", cs.policy)
	}
	svc.SetNotesPolicy(30, 10)
	if cs.policy != 2 {
		t.Fatalf("SetNotesPolicy must push: %d", cs.policy)
	}
}

// NoteCounts computes outside the lock, so a mutation that lands between the
// read and the store must DISCARD the now-stale result — a nil-check cannot
// see that, a generation counter can.
func TestNoteCountsDiscardsAResultRacedByAMutation(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-parse (toplevel)", gitexec.Result{Stdout: "/wt\n"})
	svc := New(&git.Repo{Runner: f})
	inner := notes.NewFileStore(t.TempDir())
	armed := false
	hooked := &hookStore{Store: inner, onLoad: func() {
		if armed {
			armed = false
			svc.invalidateNoteCounts() // a mutation lands mid-compute
		}
	}}
	svc.SetNotesStore(hooked)
	ctx := context.Background()
	root, err := svc.NoteAdd(ctx, model.Note{
		Address: wtAddr("a/b.go"), Side: model.NoteSideNew, Range: [2]int{1, 1},
		Summary: "x", ContextHash: "h",
	})
	if err != nil {
		t.Fatal(err)
	}

	armed = true
	if c, err := svc.NoteCounts(ctx); err != nil || c.ByPath["a/b.go"] != 1 {
		t.Fatalf("raced NoteCounts = %+v %v", c, err)
	}
	// Mutate BEHIND the Service (no invalidation of its own). If the raced
	// result had been cached, the next call would still say 1.
	if err := inner.Remove(root.ID); err != nil {
		t.Fatal(err)
	}
	if c, _ := svc.NoteCounts(ctx); c.ByPath["a/b.go"] != 0 {
		t.Fatalf("a result raced by a mutation must not be cached: %+v", c)
	}
}

// A shelf note carries a Path but belongs to no worktree: it must not add a
// Files-panel badge to the same path in the checkout.
func TestNoteCountsByPathExcludesShelfNotes(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	ctx := context.Background()
	if _, err := svc.NoteAdd(ctx, model.Note{
		Address: wtAddr("a/b.go"), Side: model.NoteSideNew, Range: [2]int{1, 1},
		Summary: "worktree", ContextHash: "h",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NoteAdd(ctx, model.Note{
		Address: model.FileAddress{State: model.StateShelf, ShelfID: "e1", Path: "a/b.go"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "shelved", ContextHash: "h",
	}); err != nil {
		t.Fatal(err)
	}
	c, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c.ByPath["a/b.go"] != 1 {
		t.Fatalf("a shelf note must not badge the worktree path: ByPath = %+v", c.ByPath)
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

// TestNotesAtScopesToTheCheckout pins the stateless door: a caller that hands
// NotesAt an address with NO Worktree (every HTTP handler — the wire must
// never name a checkout) still gets the notes NoteAdd stamped with this
// Service's own root, resolved and threaded.
func TestNotesAtScopesToTheCheckout(t *testing.T) {
	t.Parallel()
	svc, f := notesSvc(t)
	ctx := context.Background()
	// Both sides of a staged note read through repo.ShowFileInDir.
	f.SetResponse("git -C show", gitexec.Result{Stdout: "alpha\nbeta\n"})

	root, err := svc.NoteAdd(ctx, model.Note{
		Address: model.FileAddress{State: model.StateStaged, Path: "a.go"},
		Side:    model.NoteSideNew,
		Range:   [2]int{2, 2},
		Summary: "tighten this",
	})
	if err != nil {
		t.Fatal(err)
	}
	if root.Address.Worktree != "/wt" {
		t.Fatalf("NoteAdd stamped worktree %q, want /wt", root.Address.Worktree)
	}
	if _, err := svc.NoteReply(ctx, root.ID, model.Note{Summary: "agreed"}); err != nil {
		t.Fatal(err)
	}

	got, err := svc.NotesAt(ctx, model.FileAddress{State: model.StateStaged, Path: "a.go"})
	if err != nil {
		t.Fatalf("NotesAt: %v", err)
	}
	if len(got) != 1 || got[0].Note.ID != root.ID || len(got[0].Replies) != 1 {
		t.Fatalf("NotesAt = %+v, want one root with one reply", got)
	}
	if got[0].Status != model.NoteActive || got[0].Range != [2]int{2, 2} {
		t.Fatalf("resolved %v at %v, want active at {2,2}", got[0].Status, got[0].Range)
	}
	// A different path shares the store but not the target.
	if other, err := svc.NotesAt(ctx, model.FileAddress{State: model.StateStaged, Path: "b.go"}); err != nil || len(other) != 0 {
		t.Fatalf("NotesAt(b.go) = %+v, %v, want none", other, err)
	}
}

func TestNoteAddNormalisesCommitToFullSHA(t *testing.T) {
	t.Parallel()
	dir := noteSideRepo(t)
	svc := svcIn(t, dir)
	svc.UseNotesDir(t.TempDir())
	full := headSHA(t, dir)
	got, err := svc.NoteAdd(context.Background(), model.Note{
		Address: model.FileAddress{State: model.StateCommitted, Commit: full[:7], Path: "a.go"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "short sha in",
	})
	if err != nil {
		t.Fatalf("NoteAdd: %v", err)
	}
	if got.Address.Commit != full {
		t.Fatalf("stored commit = %q, want the full sha %q (a CLI note and a TUI note must share one target)", got.Address.Commit, full)
	}
}

// An annotated tag's own object id is also 40 hex, so a naive "already full
// sha" check would store it verbatim. NoteAdd must PEEL to the commit the tag
// points at, or a CLI note on "v1" would never share a target with a TUI note
// on the same commit.
func TestNoteAddPeelsAnnotatedTagToCommit(t *testing.T) {
	t.Parallel()
	dir := noteSideRepo(t)
	gittest.Run(t, dir, "tag", "-a", "v1", "-m", "v1")
	svc := svcIn(t, dir)
	svc.UseNotesDir(t.TempDir())
	full := headSHA(t, dir)
	got, err := svc.NoteAdd(context.Background(), model.Note{
		Address: model.FileAddress{State: model.StateCommitted, Commit: "v1", Path: "a.go"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "tag in",
	})
	if err != nil {
		t.Fatalf("NoteAdd: %v", err)
	}
	if got.Address.Commit != full {
		t.Fatalf("stored commit = %q, want the peeled commit sha %q, not the tag's own object id", got.Address.Commit, full)
	}
}

// A Service whose runner cannot rev-parse (the FakeRunner suites) must keep the
// commit exactly as given: normalisation is best-effort, never a new failure.
func TestNoteAddKeepsCommitWhenRevParseFails(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	got, err := svc.NoteAdd(context.Background(), model.Note{
		Address:     model.FileAddress{State: model.StateCommitted, Commit: "deadbee", Path: "a.go"},
		Side:        model.NoteSideNew,
		Range:       [2]int{1, 1},
		Summary:     "x",
		ContextHash: "h",
	})
	if err != nil || got.Address.Commit != "deadbee" {
		t.Fatalf("commit = %q err = %v, want the value as given", got.Address.Commit, err)
	}
}

func TestNoteGetAndNoteAddresses(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	ctx := context.Background()
	root, err := svc.NoteAdd(ctx, model.Note{
		Address: wtAddr("a/b.go"), Side: model.NoteSideNew, Range: [2]int{1, 1},
		Summary: "root", ContextHash: "h",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NoteReply(ctx, root.ID, model.Note{Summary: "reply"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NoteAdd(ctx, model.Note{
		Address: model.FileAddress{State: model.StateCommitted, Commit: "c0ffee", Path: "z.go"},
		Side:    model.NoteSideNew, Range: [2]int{2, 2}, Summary: "commit note", ContextHash: "h",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := svc.NoteGet(ctx, root.ID)
	if err != nil || got.Summary != "root" {
		t.Fatalf("NoteGet = %+v %v", got, err)
	}
	if _, err := svc.NoteGet(ctx, "nope1234"); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("NoteGet(missing) = %v, want ErrNoteNotFound", err)
	}

	addrs, err := svc.NoteAddresses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 2 {
		t.Fatalf("addresses = %+v, want 2 (a reply shares its root's address)", addrs)
	}
	var paths []string
	for _, a := range addrs {
		paths = append(paths, a.Path)
	}
	sort.Strings(paths)
	if paths[0] != "a/b.go" || paths[1] != "z.go" {
		t.Fatalf("paths = %v, want [a/b.go z.go]", paths)
	}
}

func TestWaitNotesSweepBounded(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	// No sweep started: the wait group is empty, so this returns immediately.
	if !svc.WaitNotesSweep(context.Background()) {
		t.Fatal("WaitNotesSweep with no sweep running must report done")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if svc.WaitNotesSweep(ctx) {
		t.Fatal("a cancelled ctx must abandon the wait and report not-done")
	}
}

// TestNotesClearRemovesOnlyThisAddress is phase 2's `gg note clear` primitive:
// every note at ONE address goes — roots and replies — and every other
// address's notes survive, with the badge counts refreshed.
func TestNotesClearRemovesOnlyThisAddress(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	ctx := context.Background()
	add := func(path, summary string) model.Note {
		t.Helper()
		n, err := svc.NoteAdd(ctx, model.Note{
			Address: wtAddr(path), Side: model.NoteSideNew, Range: [2]int{1, 1},
			Summary: summary, ContextHash: model.NoteContextHash([]string{"a"}),
		})
		if err != nil {
			t.Fatalf("NoteAdd(%s): %v", path, err)
		}
		return n
	}
	root := add("a/b.go", "first")
	add("a/b.go", "second")
	if _, err := svc.NoteReply(ctx, root.ID, model.Note{Summary: "agreed"}); err != nil {
		t.Fatalf("NoteReply: %v", err)
	}
	keep := add("c/d.go", "elsewhere")

	// Warm the badge cache so the invalidation is observable.
	if c, err := svc.NoteCounts(ctx); err != nil || c.ByPath["a/b.go"] != 2 {
		t.Fatalf("NoteCounts before = %+v, %v, want a/b.go = 2", c, err)
	}

	got, err := svc.NotesClear(ctx, wtAddr("a/b.go"))
	if err != nil {
		t.Fatalf("NotesClear: %v", err)
	}
	if got != 3 {
		t.Fatalf("NotesClear removed %d, want 3 (two roots + one reply)", got)
	}
	if left, err := svc.NotesFor(ctx, wtAddr("a/b.go"), sideDiff("a")); err != nil || len(left) != 0 {
		t.Fatalf("NotesFor(a/b.go) after clear = %+v, %v, want none", left, err)
	}
	other, err := svc.NotesFor(ctx, wtAddr("c/d.go"), sideDiff("a"))
	if err != nil || len(other) != 1 || other[0].Note.ID != keep.ID {
		t.Fatalf("NotesFor(c/d.go) = %+v, %v, want the untouched note", other, err)
	}
	c, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatalf("NoteCounts after: %v", err)
	}
	if c.ByPath["a/b.go"] != 0 || c.ByPath["c/d.go"] != 1 {
		t.Fatalf("NoteCounts after = %+v, want a/b.go gone and c/d.go = 1", c.ByPath)
	}
}

// TestNotesClearOfAnUnusedAddressIsANoOp keeps the popup's "nothing to do"
// path honest: no error, nothing removed, nothing else touched.
func TestNotesClearOfAnUnusedAddressIsANoOp(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	ctx := context.Background()
	if _, err := svc.NoteAdd(ctx, model.Note{
		Address: wtAddr("a/b.go"), Side: model.NoteSideNew, Range: [2]int{1, 1},
		Summary: "keep me", ContextHash: model.NoteContextHash([]string{"a"}),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.NotesClear(ctx, wtAddr("nope.go"))
	if err != nil || got != 0 {
		t.Fatalf("NotesClear(nope.go) = %d, %v, want 0, nil", got, err)
	}
	if left, err := svc.NotesFor(ctx, wtAddr("a/b.go"), sideDiff("a")); err != nil || len(left) != 1 {
		t.Fatalf("NotesFor(a/b.go) = %+v, %v, want the note untouched", left, err)
	}
}
