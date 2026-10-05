package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

func blob(s string) string { return git.BlobOf("sha1", []byte(s)).ID }

func writeAt(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The spec §9 walk: edit, stage-unchanged, delete, recreate.
func TestWorkingReviewStateFollowsTheFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeAt(t, dir, "a.txt", "A\n")
	writeAt(t, dir, "c.txt", "C\n")
	files := []model.NoteFile{{Path: "a.txt", Blob: blob("A\n")}, {Path: "c.txt", Blob: blob("C\n")}, {Path: "b.txt", Deleted: true}}
	m := WorkingReviewState(dir, files)
	if !m.Current || m.States["a.txt"] != WorkingFileMatches || m.States["b.txt"] != WorkingFileMatches {
		t.Fatalf("fresh: %+v", m)
	}
	writeAt(t, dir, "a.txt", "A edited\n")
	m = WorkingReviewState(dir, files)
	if m.States["a.txt"] != WorkingFileChanged || !m.Current {
		t.Fatalf("after editing a: %+v", m)
	}
	os.Remove(filepath.Join(dir, "c.txt"))
	writeAt(t, dir, "b.txt", "back\n")
	m = WorkingReviewState(dir, files)
	if m.States["c.txt"] != WorkingFileGone || m.States["b.txt"] != WorkingFileChanged || m.Current {
		t.Fatalf("after delete/recreate: %+v, want outdated", m)
	}
}

// Staging an unchanged file keeps the review: the working bytes are the same.
func TestWorkingReviewSurvivesStaging(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	writeAt(t, dir, "a.txt", "reviewed\n")
	runGitIn(t, dir, "add", "a.txt")
	top, _ := svc.TopLevel(context.Background())
	if m := WorkingReviewState(top, []model.NoteFile{{Path: "a.txt", Blob: blob("reviewed\n")}}); !m.Current {
		t.Fatalf("staged unchanged: %+v", m)
	}
}

func TestWorkingReviewStateWorktreeGone(t *testing.T) {
	t.Parallel()
	gone := filepath.Join(t.TempDir(), "removed")
	m := WorkingReviewState(gone, []model.NoteFile{{Path: "x", Deleted: true}})
	if m.Current || m.States["x"] != WorkingFileGone {
		t.Fatalf("a removed worktree: %+v, want every file gone", m)
	}
}

// A sha256 repository's blob ids match too: the algorithm comes from the id.
func TestWorkingReviewStateSha256(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "--object-format=sha256", dir).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	writeAt(t, dir, "a.txt", "x\n")
	out, err := exec.Command("git", "-C", dir, "hash-object", "a.txt").Output()
	if err != nil {
		t.Fatal(err)
	}
	if m := WorkingReviewState(dir, []model.NoteFile{{Path: "a.txt", Blob: strings.TrimSpace(string(out))}}); !m.Current {
		t.Fatalf("sha256 blob did not match: %+v", m)
	}
}

func TestWorkingBlobCacheRehashesOnlyChangedFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	writeAt(t, dir, "a.txt", "one\n")
	old := time.Now().Add(-time.Hour)
	os.Chtimes(p, old, old)
	c := newBlobCache()
	for i := 0; i < 3; i++ {
		if _, err := c.id("sha1", p); err != nil {
			t.Fatal(err)
		}
	}
	if c.hashes != 1 {
		t.Fatalf("hashed %d times for an unchanged file, want 1", c.hashes)
	}
	writeAt(t, dir, "a.txt", "two, longer\n")
	os.Chtimes(p, old.Add(time.Minute), old.Add(time.Minute))
	id, _ := c.id("sha1", p)
	if c.hashes != 2 || id != blob("two, longer\n") {
		t.Fatalf("after a change: hashes %d id %s", c.hashes, id)
	}
}

// Review Focus 1: a same-size rewrite within the mtime tick is never served stale.
func TestWorkingBlobCacheNeverTrustsAFreshMtime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	writeAt(t, dir, "a.txt", "AAAA\n")
	c := newBlobCache()
	if id, _ := c.id("sha1", p); id != blob("AAAA\n") {
		t.Fatal("first hash")
	}
	fi, _ := os.Stat(p)
	writeAt(t, dir, "a.txt", "BBBB\n")
	os.Chtimes(p, fi.ModTime(), fi.ModTime()) // same size, same mtime
	if id, _ := c.id("sha1", p); id != blob("BBBB\n") {
		t.Fatalf("a fresh same-size rewrite was served from the cache")
	}
}

// Own worktree only: a sibling worktree lists none of this one's reviews.
func TestWorkingReviewsOnlyInTheirWorktree(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	notesDir := t.TempDir()
	svc.UseNotesDir(notesDir)
	ctx := context.Background()
	if _, _, err := svc.SaveReview(ctx, SaveReview{Target: WorkingReviewTarget(), Text: "x",
		Files: []model.NoteFile{{Path: "gone", Deleted: true}}}); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "wt2")
	runGitIn(t, dir, "worktree", "add", "-b", "side", other)
	svc2 := Open(other)
	svc2.UseNotesDir(notesDir)
	if got, _ := svc2.WorkingReviews(ctx); len(got) != 0 {
		t.Fatalf("the sibling worktree sees %d reviews", len(got))
	}
	got, err := svc.WorkingReviews(ctx)
	if err != nil || len(got) != 1 || !got[0].Current || got[0].Kind != ReviewOnWorktree {
		t.Fatalf("own worktree: %+v %v", got, err)
	}
}

// putWorkingReview stores a working review of dir's a.txt (blob of reviewed)
// created at created, directly through the store.
func putWorkingReview(t *testing.T, svc *Service, top, reviewed string, created time.Time) string {
	t.Helper()
	st := svc.notesStore(context.Background())
	n := model.Note{ID: "rv" + reviewed[:1], Source: model.NoteSourceAgent, Tags: []string{model.ReviewTag},
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: top},
		Summary: "Review: working changes", Files: []model.NoteFile{{Path: "a.txt", Blob: blob(reviewed)}},
		Created: created, Updated: created}
	if err := st.Put(n); err != nil {
		t.Fatal(err)
	}
	return n.ID
}

func TestSweepWorkingReviews(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		onDisk   string // a.txt now
		age      time.Duration
		wantKept bool
	}{
		{"current and old: kept", "A\n", 400 * 24 * time.Hour, true},
		{"outdated and young: kept", "edited\n", time.Hour, true},
		{"outdated and old: dropped", "edited\n", 400 * 24 * time.Hour, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, svc := newRealRepo(t)
			svc.UseNotesDir(t.TempDir())
			svc.SetNotesPolicy(30, 0)
			top, _ := svc.TopLevel(context.Background())
			writeAt(t, dir, "a.txt", c.onDisk)
			id := putWorkingReview(t, svc, top, "A\n", time.Now().UTC().Add(-c.age))
			if _, err := svc.sweepNotes(context.Background()); err != nil {
				t.Fatal(err)
			}
			all, _ := svc.notesStore(context.Background()).LoadAll()
			kept := false
			for _, n := range all {
				kept = kept || n.ID == id
			}
			if kept != c.wantKept {
				t.Fatalf("kept = %v, want %v", kept, c.wantKept)
			}
		})
	}
}

func TestNoteCountsListWorkingReviewsApart(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	svc.UseNotesDir(t.TempDir())
	top, _ := svc.TopLevel(context.Background())
	writeAt(t, dir, "a.txt", "A\n")
	putWorkingReview(t, svc, top, "A\n", time.Now().UTC())
	c, err := svc.NoteCounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.WorkingReviews) != 1 || len(c.Reviews) != 0 || len(c.ByPath) != 0 {
		t.Fatalf("counts: working %d reviews %d byPath %v", len(c.WorkingReviews), len(c.Reviews), c.ByPath)
	}
}

func TestNotesOverviewListsWorkingReviewsWithTheirState(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	svc.UseNotesDir(t.TempDir())
	top, _ := svc.TopLevel(context.Background())
	writeAt(t, dir, "a.txt", "A\n")
	putWorkingReview(t, svc, top, "A\n", time.Now().UTC())                   // current
	putWorkingReview(t, svc, top, "B\n", time.Now().UTC().Add(-time.Minute)) // outdated
	ov, err := svc.NotesOverview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.WorkingReviews) != 2 || !ov.WorkingReviews[0].Current || ov.WorkingReviews[1].Current {
		t.Fatalf("working reviews = %+v", ov.WorkingReviews)
	}
	if len(ov.Unstaged) != 0 || len(ov.Commits) != 0 || ov.Count() != 2 {
		t.Fatalf("leaked: unstaged %d commits %d count %d", len(ov.Unstaged), len(ov.Commits), ov.Count())
	}
	if got := ov.ShownOn([]string{"main"}); len(got.WorkingReviews) != 2 {
		t.Fatalf("ShownOn dropped the working reviews")
	}
}

// `gg note list` reaches a working review through NoteAddresses → NotesAt.
func TestNotesAtShowsAWorkingReviewItself(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	top, _ := svc.TopLevel(ctx)
	writeAt(t, dir, "a.txt", "A\n")
	id := putWorkingReview(t, svc, top, "A\n", time.Now().UTC())
	addrs, _ := svc.NoteAddresses(ctx)
	found := false
	for _, a := range addrs {
		rs, err := svc.NotesAt(ctx, a)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rs {
			found = found || (r.Note.ID == id && r.Status == model.NoteActive)
		}
	}
	if !found {
		t.Fatal("the working review is not listed")
	}
}

// workingReviewOf commits a.txt/c.txt, edits both, stores a structured
// working review with one note on each (new side) and one old-side note on a.txt.
func workingReviewOf(t *testing.T) (string, *Service, string) {
	t.Helper()
	dir, svc := newRealRepo(t)
	svc.UseNotesDir(t.TempDir())
	commitFile(t, dir, "a.txt", "a1\na2\n", "a")
	commitFile(t, dir, "c.txt", "c1\n", "c")
	writeAt(t, dir, "a.txt", "a1\nA2\n")
	writeAt(t, dir, "c.txt", "c1\nC2\n")
	writeAt(t, dir, "u.txt", "u1\n")
	doc := `{"version":1,"summary":"ok","files":[` +
		`{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"on a"},{"oldRange":[2,2],"summary":"old a"}]},` +
		`{"path":"c.txt","annotations":[{"newRange":[2,2],"summary":"on c"}]}]}`
	files := []model.NoteFile{{Path: "a.txt", Blob: blob("a1\nA2\n")}, {Path: "c.txt", Blob: blob("c1\nC2\n")}, {Path: "u.txt", Blob: blob("u1\n")}}
	id, _, err := svc.SaveReview(context.Background(), SaveReview{Target: WorkingReviewTarget(), Text: doc, Files: files})
	if err != nil {
		t.Fatal(err)
	}
	return dir, svc, id
}

func TestReviewFilesOfAWorkingReviewIsHeadToWorktree(t *testing.T) {
	t.Parallel()
	_, svc, id := workingReviewOf(t)
	ctx := context.Background()
	r, _ := svc.Review(ctx, id)
	files, err := svc.ReviewFiles(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f.Path] = true
	}
	if !got["a.txt"] || !got["c.txt"] || !got["u.txt"] {
		t.Fatalf("files = %+v, want the modified and the untracked ones", files)
	}
	base, tip, isRange := svc.ReviewRevs(ctx, r)
	if len(base) < 40 || tip != "" || isRange {
		t.Fatalf("revs = %q %q %v, want HEAD's sha, no tip, no range", base, tip, isRange)
	}
}

func TestWorkingReviewCountsOnlyMatchingFiles(t *testing.T) {
	t.Parallel()
	dir, svc, id := workingReviewOf(t)
	ctx := context.Background()
	writeAt(t, dir, "c.txt", "c1\nC2 edited\n")
	counts, err := svc.ReviewFileCounts(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if counts["a.txt"] != 2 || counts["c.txt"] != 0 {
		t.Fatalf("counts = %v, want a.txt:2 (both sides fit) and nothing on the changed c.txt", counts)
	}
	other, _ := svc.ReviewOtherNotes(ctx, id)
	if len(other) != 1 || other[0].Path != "c.txt" || !other[0].Changed {
		t.Fatalf("other = %+v, want c.txt's note marked changed", other)
	}
}

func TestReviewNotesForAWorkingReviewUseTheWorktreeAddress(t *testing.T) {
	t.Parallel()
	dir, svc, id := workingReviewOf(t)
	ctx := context.Background()
	d := Diff{Result: textdiff.Result{Rows: []textdiff.Row{
		{LeftNo: 1, RightNo: 1, Left: "a1", Right: "a1"}, {LeftNo: 2, RightNo: 2, Left: "a2", Right: "A2"}}}}
	ns, err := svc.ReviewNotesFor(ctx, id, "a.txt", d)
	if err != nil || len(ns) != 2 {
		t.Fatalf("notes = %+v %v", ns, err)
	}
	if a := ns[0].Note.Address; a.State != model.StateUnstaged || a.Path != "a.txt" || a.Worktree == "" {
		t.Fatalf("address = %+v", a)
	}
	writeAt(t, dir, "a.txt", "changed\n")
	if ns, _ = svc.ReviewNotesFor(ctx, id, "a.txt", d); len(ns) != 0 {
		t.Fatalf("a changed file still shows %d notes", len(ns))
	}
}

// Spec §7: a matching file's working-tree diff draws the review's notes —
// new side only (the old side the review read is HEAD, not the index).
func TestNotesAtAddsAMatchingWorkingReviewsNotes(t *testing.T) {
	t.Parallel()
	dir, svc, id := workingReviewOf(t)
	ctx := context.Background()
	addr := model.FileAddress{State: model.StateUnstaged, Path: "a.txt"}
	ns, err := svc.NotesAt(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 1 || ns[0].Note.ID != model.ReviewNoteIDPrefix+id+":0" || ns[0].Note.Side != model.NoteSideNew {
		t.Fatalf("notes = %+v, want only the new-side review note", ns)
	}
	writeAt(t, dir, "a.txt", "edited\n")
	if ns, _ = svc.NotesAt(ctx, addr); len(ns) != 0 {
		t.Fatalf("an edited file still shows %+v", ns)
	}
}

// A staged diff's new side is the index, not the working bytes a review
// read: a working review's notes are drawn on the working-tree diff only.
func TestNotesAtLeavesAWorkingReviewsNotesOffTheStagedDiff(t *testing.T) {
	t.Parallel()
	_, svc, _ := workingReviewOf(t)
	ns, err := svc.NotesAt(context.Background(), model.FileAddress{State: model.StateStaged, Path: "a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 0 {
		t.Fatalf("staged diff notes = %+v, want none", ns)
	}
}

// A read that merely FAILS (permissions, an unmounted drive) proves nothing:
// the review is unreadable, not outdated, and the sweep keeps it.
func TestSweepKeepsAWorkingReviewItCannotRead(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	dir, svc := newRealRepo(t)
	svc.UseNotesDir(t.TempDir())
	svc.SetNotesPolicy(30, 0)
	top, _ := svc.TopLevel(context.Background())
	writeAt(t, dir, "a.txt", "A\n")
	id := putWorkingReview(t, svc, top, "A\n", time.Now().UTC().Add(-400*24*time.Hour))
	locked := filepath.Join(top)
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	m := WorkingReviewState(top, []model.NoteFile{{Path: "a.txt", Blob: blob("A\n")}})
	if !m.Unreadable || m.Current {
		os.Chmod(locked, 0o755)
		t.Fatalf("match = %+v, want unreadable", m)
	}
	_, err := svc.sweepNotes(context.Background())
	os.Chmod(locked, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	all, _ := svc.notesStore(context.Background()).LoadAll()
	for _, n := range all {
		if n.ID == id {
			return
		}
	}
	t.Fatal("the sweep dropped a review it could not read")
}
