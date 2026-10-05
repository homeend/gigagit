package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
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
