package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/filelock"
	"github.com/homeend/gigagit/internal/model"
)

// reviewRepo is a real repo on branch "feature" (one commit past main) with
// its own note store; it returns the dir, the service and the feature tip.
func reviewRepo(t *testing.T) (string, *Service, string) {
	t.Helper()
	dir, svc := newRealRepo(t)
	svc.UseNotesDir(t.TempDir())
	runGitIn(t, dir, "checkout", "-b", "feature")
	commitFile(t, dir, "f.txt", "x\n", "feature commit")
	return dir, svc, revParse(t, dir, "feature")
}

func TestBranchReviewTargetFillsCommitAndBranch(t *testing.T) {
	t.Parallel()
	_, svc, tip := reviewRepo(t)
	tg, err := svc.BranchReviewTarget(context.Background(), "feature")
	if err != nil {
		t.Fatal(err)
	}
	if tg.Commit != tip || tg.Branch != "feature" {
		t.Fatalf("Commit=%q Branch=%q, want %q feature", tg.Commit, tg.Branch, tip)
	}
}

func TestBranchReviewTargetShaHasNoBranch(t *testing.T) {
	t.Parallel()
	_, svc, tip := reviewRepo(t)
	tg, err := svc.BranchReviewTarget(context.Background(), tip)
	if err != nil {
		t.Fatal(err)
	}
	if tg.Branch != "" || tg.Commit != tip {
		t.Fatalf("Branch=%q Commit=%q, want an empty branch for a sha", tg.Branch, tg.Commit)
	}
}

func TestSaveReviewBranchNoteAndKinds(t *testing.T) {
	t.Parallel()
	dir, svc, tip := reviewRepo(t)
	ctx := context.Background()
	tg, _ := svc.BranchReviewTarget(ctx, "feature")
	id, warn, err := svc.SaveReview(ctx, SaveReview{Target: tg, Agent: "Claude Code", Text: "# Looks good\nbody"})
	if err != nil || warn != "" {
		t.Fatalf("SaveReview: %v %q", err, warn)
	}
	r, err := svc.Review(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != ReviewOnBranch || r.Commit != tip || r.Branch != "feature" || r.Text != "# Looks good\nbody" ||
		r.Scope != tg.Range || r.Agent != "Claude Code" || !strings.HasPrefix(r.Summary, "Review: feature") {
		t.Fatalf("review = %+v", r)
	}
	if got, _ := svc.ReviewsForBranch(ctx, "feature"); len(got) != 1 {
		t.Fatalf("ReviewsForBranch = %d, want 1", len(got))
	}
	// The branch moves on: the review stays on its commit as "was the tip".
	commitFile(t, dir, "g.txt", "y\n", "second")
	r, _ = svc.Review(ctx, id)
	if r.Kind != ReviewWasTip {
		t.Fatalf("after the branch moved: kind %v, want ReviewWasTip", r.Kind)
	}
	if got, _ := svc.ReviewsForBranch(ctx, "feature"); len(got) != 0 {
		t.Fatalf("ReviewsForBranch after the move = %d, want 0 (current tip only)", len(got))
	}
	if got, _ := svc.ReviewsForCommit(ctx, tip); len(got) != 1 {
		t.Fatalf("ReviewsForCommit = %d, want 1", len(got))
	}
}

func TestSaveReviewCommitRangeAnchorsOnTheLastCommit(t *testing.T) {
	t.Parallel()
	dir, svc, tip := reviewRepo(t)
	base := revParse(t, dir, "main")
	tg := ReviewTarget{Kind: ReviewRange, Range: base + ".." + tip, Label: "range"}
	id, _, err := svc.SaveReview(context.Background(), SaveReview{Target: tg, Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := svc.Review(context.Background(), id)
	if r.Commit != tip || r.Branch != "" || r.Kind != ReviewOnCommit {
		t.Fatalf("review = %+v, want a commit review on %s", r, tip)
	}
}

func TestSaveReviewRefusesWorkingChanges(t *testing.T) {
	t.Parallel()
	_, svc, _ := reviewRepo(t)
	_, _, err := svc.SaveReview(context.Background(), SaveReview{Target: WorkingReviewTarget(), Text: "x"})
	if !errors.Is(err, ErrNoReviewCommit) {
		t.Fatalf("err = %v, want ErrNoReviewCommit", err)
	}
}

func TestSaveReviewUpdatesInPlace(t *testing.T) {
	t.Parallel()
	_, svc, _ := reviewRepo(t)
	ctx := context.Background()
	tg, _ := svc.BranchReviewTarget(ctx, "feature")
	id, _, _ := svc.SaveReview(ctx, SaveReview{Target: tg, Text: "one"})
	id2, _, err := svc.SaveReview(ctx, SaveReview{Target: tg, Text: "two", NoteID: id})
	if err != nil || id2 != id {
		t.Fatalf("update: id %q→%q err %v", id, id2, err)
	}
	all, _ := svc.Reviews(ctx)
	if len(all) != 1 || all[0].Text != "two" {
		t.Fatalf("reviews = %+v, want one with text two", all)
	}
}

// Serial: swaps the package's retry seams.
func TestSaveReviewRetriesAHeldLock(t *testing.T) {
	_, svc, _ := reviewRepo(t)
	ctx := context.Background()
	tg, _ := svc.BranchReviewTarget(ctx, "feature")
	var sleeps []time.Duration
	restore := setReviewRetrySeams(func(d time.Duration) { sleeps = append(sleeps, d) })
	defer restore()
	fails := 2
	reviewPutHook = func() error {
		if fails > 0 {
			fails--
			return filelock.ErrHeld
		}
		return nil
	}
	if _, _, err := svc.SaveReview(ctx, SaveReview{Target: tg, Text: "x"}); err != nil {
		t.Fatalf("SaveReview: %v", err)
	}
	if len(sleeps) != 2 {
		t.Fatalf("slept %d times, want 2", len(sleeps))
	}
	for _, d := range sleeps {
		if d < 500*time.Millisecond || d > 2*time.Second {
			t.Fatalf("sleep %v outside 500ms–2s", d)
		}
	}
}

// Serial: swaps the package's retry seams.
func TestSaveReviewGivesUpAfterTheBudget(t *testing.T) {
	_, svc, _ := reviewRepo(t)
	ctx := context.Background()
	tg, _ := svc.BranchReviewTarget(ctx, "feature")
	var total time.Duration
	restore := setReviewRetrySeams(func(d time.Duration) { total += d })
	defer restore()
	reviewClock = func() time.Duration { return total } // elapsed = time slept
	reviewPutHook = func() error { return filelock.ErrHeld }
	_, _, err := svc.SaveReview(ctx, SaveReview{Target: tg, Text: "x"})
	if !errors.Is(err, filelock.ErrHeld) {
		t.Fatalf("err = %v, want ErrHeld", err)
	}
	if total > reviewRetryBudget || total < reviewRetryBudget-2*time.Second {
		t.Fatalf("slept %v, want just under the %v budget", total, reviewRetryBudget)
	}
}

func TestSaveReviewQuarantinesACorruptStore(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	notesDir := t.TempDir()
	svc.UseNotesDir(notesDir)
	runGitIn(t, dir, "checkout", "-b", "feature")
	commitFile(t, dir, "f.txt", "x\n", "c")
	if err := os.WriteFile(filepath.Join(notesDir, "notes.toml"), []byte("[[[ broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	tg, _ := svc.BranchReviewTarget(context.Background(), "feature")
	id, warn, err := svc.SaveReview(context.Background(), SaveReview{Target: tg, Text: "x"})
	if err != nil || id == "" {
		t.Fatalf("SaveReview: %q %v", id, err)
	}
	if !strings.Contains(warn, "notes.toml.corrupt-") {
		t.Fatalf("warn = %q, want the quarantine path", warn)
	}
	m, _ := filepath.Glob(filepath.Join(notesDir, "notes.toml.corrupt-*"))
	if len(m) != 1 {
		t.Fatalf("quarantined files = %v", m)
	}
	if r, err := svc.Review(context.Background(), id); err != nil || r.Text != "x" {
		t.Fatalf("review after quarantine: %+v %v", r, err)
	}
}

func TestNoteCountsListsReviewHeads(t *testing.T) {
	t.Parallel()
	_, svc, tip := reviewRepo(t)
	ctx := context.Background()
	tg, _ := svc.BranchReviewTarget(ctx, "feature")
	id, _, _ := svc.SaveReview(ctx, SaveReview{Target: tg, Agent: "A", Text: "x"})
	c, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Reviews) != 1 || c.Reviews[0].ID != id || c.Reviews[0].Branch != "feature" || c.Reviews[0].Commit != tip {
		t.Fatalf("Reviews = %+v", c.Reviews)
	}
	// A review has its own marker (✎); the ◆ badge counts the notes a diff
	// can show, so a review alone leaves the commit unbadged.
	if n := c.ByCommit[tip]; n != 0 {
		t.Fatalf("ByCommit[tip] = %d, want 0 (the ◆ badge never counts a review)", n)
	}
}

var deleteAnyway = engine.MapDecider{"delete-branch": "delete", "branch-unmerged": "force-delete"}

func TestDeleteBranchDeletesItsReviews(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	ctx := context.Background()
	tg, _ := svc.BranchReviewTarget(ctx, "feature")
	if _, _, err := svc.SaveReview(ctx, SaveReview{Target: tg, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "checkout", "main")
	if _, err := svc.Execute(ctx, engine.DeleteBranch{Name: "feature"}, nil, deleteAnyway); err != nil {
		t.Fatal(err)
	}
	if all, _ := svc.Reviews(ctx); len(all) != 0 {
		t.Fatalf("reviews after the delete = %+v", all)
	}
}

// The TUI fills Address.Branch on working-tree line notes too: deleting the
// branch must take its REVIEWS only.
func TestDeleteBranchKeepsLineNotes(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	ctx := context.Background()
	st := svc.notesStore(ctx)
	line := model.Note{ID: "line1", Address: model.FileAddress{State: model.StateUnstaged, Worktree: dir, Branch: "feature", Path: "f.txt"},
		Summary: "keep me", Created: time.Now(), ContextHash: "h"}
	if err := st.Put(line); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "checkout", "main")
	if _, err := svc.Execute(ctx, engine.DeleteBranch{Name: "feature"}, nil, deleteAnyway); err != nil {
		t.Fatal(err)
	}
	all, _ := st.Load()
	if len(all) != 1 || all[0].ID != "line1" {
		t.Fatalf("the line note went with the branch: %v", all)
	}
}

func TestRenameBranchRenamesItsReviews(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	ctx := context.Background()
	tg, _ := svc.BranchReviewTarget(ctx, "feature")
	id, _, _ := svc.SaveReview(ctx, SaveReview{Target: tg, Text: "x"})
	runGitIn(t, dir, "checkout", "main")
	if _, err := svc.Execute(ctx, engine.RenameBranch{Old: "feature", New: "feat2"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	r, _ := svc.Review(ctx, id)
	if r.Branch != "feat2" || r.Kind != ReviewOnBranch {
		t.Fatalf("after the rename: %+v", r)
	}
}

// A range review's notes are shown on the branch they were written on: a
// rename carries them to the new name (they would be hidden on both names
// otherwise), and deleting the branch leaves them in the store.
func TestRangeReviewNotesFollowABranchRename(t *testing.T) {
	t.Parallel()
	dir, svc, tip := reviewRepo(t)
	ctx := context.Background()
	st := svc.notesStore(ctx)
	if err := st.Put(model.Note{ID: "rng1", Preview: "aaaaaaa..bbbbbbb", PreviewBranch: "feature",
		Address: model.FileAddress{State: model.StateCommitted, Commit: tip, Path: "f.txt"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "range", Created: time.Now(), ContextHash: "h"}); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "checkout", "main")
	if _, err := svc.Execute(ctx, engine.RenameBranch{Old: "feature", New: "feat2"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	c, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ScopesShownOn(tip, []string{"feat2"}); len(got) != 1 {
		t.Fatalf("on the renamed branch the review must show: %+v (all %+v)", got, c.ScopesByCommit[tip])
	}
	if got := c.ScopesShownOn(tip, []string{"main"}); len(got) != 0 {
		t.Fatalf("on main it must not: %+v", got)
	}
	if _, err := svc.Execute(ctx, engine.DeleteBranch{Name: "feat2"}, nil, deleteAnyway); err != nil {
		t.Fatal(err)
	}
	if all, _ := st.Load(); len(all) != 1 {
		t.Fatalf("deleting the branch must leave its range notes in the store: %v", all)
	}
	// The branch is gone: its review is no other branch's to show.
	svc.InvalidateNoteCounts()
	if c, _ := svc.NoteCounts(ctx); len(c.ScopesShownOn(tip, []string{"main"})) != 0 {
		t.Fatal("a deleted branch's range review must not surface on main")
	}
}

func TestNotesOverviewListsReviewsUnderTheirCommit(t *testing.T) {
	t.Parallel()
	_, svc, tip := reviewRepo(t)
	ctx := context.Background()
	tg, _ := svc.BranchReviewTarget(ctx, "feature")
	if _, _, err := svc.SaveReview(ctx, SaveReview{Target: tg, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	gone := strings.Repeat("d", 40)
	if err := svc.notesStore(ctx).Put(model.Note{ID: "old1", Source: model.NoteSourceAgent,
		Address: model.FileAddress{State: model.StateCommitted, Commit: gone},
		Tags:    []string{model.ReviewTag}, Summary: "Review: gone", Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	ov, err := svc.NotesOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Count() != 2 {
		t.Fatalf("Count = %d, want 2", ov.Count())
	}
	byHash := map[string]NoteCommitNotes{}
	for _, c := range ov.Commits {
		byHash[c.Hash] = c
	}
	if c := byHash[tip]; len(c.Reviews) != 1 || len(c.Files) != 0 || c.Reviews[0].Kind != ReviewOnBranch {
		t.Fatalf("tip entry = %+v", c)
	}
	if c := byHash[gone]; !c.Missing || len(c.Reviews) != 1 {
		t.Fatalf("missing-commit entry = %+v", c)
	}
}

// TestNoReviewReportFilesAnywhere pins the removal: no domain source asks for
// the old "reviews" state dir — a review lives in its note only.
func TestNoReviewReportFilesAnywhere(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(src), `stateBaseDir("reviews")`) {
			t.Fatalf("%s still writes <state>/gg/reviews", f)
		}
	}
}

// "Review the current branch" is asked as HEAD; its note carries the branch.
func TestBranchReviewTargetHeadNamesTheCheckedOutBranch(t *testing.T) {
	t.Parallel()
	_, svc, tip := reviewRepo(t)
	tg, err := svc.BranchReviewTarget(context.Background(), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if tg.Branch != "feature" || tg.Commit != tip {
		t.Fatalf("Branch=%q Commit=%q, want feature at %s", tg.Branch, tg.Commit, tip)
	}
}

// With the DEFAULT store (resolved from the git common dir, which takes a
// Read reservation) the follow-up must run after Execute releases the repo:
// inside it, the delete deadlocked on its own reservation. Serial: Setenv.
func TestDeleteBranchFollowUpRunsOutsideTheReservation(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir, svc := newRealRepo(t)
	runGitIn(t, dir, "checkout", "-b", "feature")
	commitFile(t, dir, "f.txt", "x\n", "feature commit")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tg, _ := svc.BranchReviewTarget(ctx, "feature")
	if _, _, err := svc.SaveReview(ctx, SaveReview{Target: tg, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "checkout", "main")
	fresh := Open(dir) // a new Service resolves its store lazily, inside Execute
	done := make(chan error, 1)
	go func() {
		_, err := fresh.Execute(ctx, engine.DeleteBranch{Name: "feature"}, nil, deleteAnyway)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("DeleteBranch deadlocked on its own reservation")
	}
	if all, _ := fresh.Reviews(ctx); len(all) != 0 {
		t.Fatalf("reviews after the delete = %+v", all)
	}
}

func TestReviewNoteIsReadOnly(t *testing.T) {
	_, svc, _ := reviewRepo(t)
	ctx := context.Background()
	if err := svc.NoteEdit(ctx, "review:abc:0", "x", ""); !errors.Is(err, ErrReadOnlyNote) {
		t.Fatalf("edit: %v", err)
	}
	if _, err := svc.NoteReply(ctx, "review:abc:0", model.Note{Summary: "r"}); !errors.Is(err, ErrReadOnlyNote) {
		t.Fatalf("reply: %v", err)
	}
	if err := svc.NoteRemove(ctx, "review:abc:0"); !errors.Is(err, ErrReadOnlyNote) {
		t.Fatalf("remove: %v", err)
	}
}
