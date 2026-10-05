package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// prepareWorking runs a working review's Prepare and returns the inputs and
// the diff and context files it wrote.
func prepareWorking(t *testing.T, dir string, repo *git.Repo) (TaskInputs, string, string) {
	t.Helper()
	in, err := ReviewChanges{Command: "true", Dir: dir, Diff: model.DiffSpec{Rev: "HEAD"},
		RangeLabel: "working changes", Working: true}.Prepare(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(in.Cleanup)
	env := map[string]string{}
	for _, kv := range in.Env {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	return in, readFile(t, env["GG_REVIEW_DIFF"]), readFile(t, env["GG_CONTEXT_FILE"])
}

func fileOf(files []model.NoteFile, path string) (model.NoteFile, bool) {
	for _, f := range files {
		if f.Path == path {
			return f, true
		}
	}
	return model.NoteFile{}, false
}

func gitHash(t *testing.T, dir, path string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "hash-object", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReviewChangesWorkingIncludesUntrackedFiles(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, "a.txt", "one\n", "c1")
	write(t, dir, "a.txt", "one\nTWO\n")
	write(t, dir, "my new.txt", "x\ny\n")
	in, diff, ctxDoc := prepareWorking(t, dir, repo)
	for _, want := range []string{"+TWO", "diff --git a/my new.txt b/my new.txt", "new file mode 100644",
		"--- /dev/null", "+++ b/my new.txt", "@@ -0,0 +1,2 @@", "+x", "+y"} {
		if !strings.Contains(diff, want) {
			t.Errorf("diff lacks %q:\n%s", want, diff)
		}
	}
	if !strings.Contains(ctxDoc, "2\t0\tmy new.txt") {
		t.Errorf("context numstat lacks the untracked file:\n%s", ctxDoc)
	}
	for _, p := range []string{"a.txt", "my new.txt"} {
		f, ok := fileOf(in.ReviewFiles, p)
		if !ok || f.Blob != gitHash(t, dir, p) || f.Deleted {
			t.Errorf("%s fingerprint = %+v (found %v), want git's blob", p, f, ok)
		}
	}
}

func TestReviewChangesWorkingSkipsIgnoredFiles(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, ".gitignore", "*.log\n", "ignore")
	write(t, dir, "x.log", "noise\n")
	write(t, dir, "keep.txt", "k\n")
	in, diff, _ := prepareWorking(t, dir, repo)
	if strings.Contains(diff, "x.log") {
		t.Errorf("an ignored file reached the diff:\n%s", diff)
	}
	if _, ok := fileOf(in.ReviewFiles, "x.log"); ok {
		t.Error("an ignored file was fingerprinted")
	}
	if _, ok := fileOf(in.ReviewFiles, "keep.txt"); !ok {
		t.Error("the untracked file was not fingerprinted")
	}
}

// The fingerprint is of the bytes Prepare read: a file rewritten while the
// agent runs keeps the reviewed blob.
func TestReviewChangesWorkingFingerprintsArePrepareTime(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, "a.txt", "one\n", "c1")
	write(t, dir, "a.txt", "reviewed\n")
	in, _, _ := prepareWorking(t, dir, repo)
	write(t, dir, "a.txt", "edited later\n")
	f, _ := fileOf(in.ReviewFiles, "a.txt")
	if f.Blob != git.BlobOf("sha1", []byte("reviewed\n")).ID {
		t.Fatalf("blob %s is not the reviewed bytes'", f.Blob)
	}
}

func TestReviewChangesWorkingRecordsADeletion(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, "b.txt", "b\n", "c1")
	if err := os.Remove(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatal(err)
	}
	in, _, _ := prepareWorking(t, dir, repo)
	if f, ok := fileOf(in.ReviewFiles, "b.txt"); !ok || !f.Deleted || f.Blob != "" {
		t.Fatalf("b.txt = %+v (found %v), want Deleted", f, ok)
	}
}

// Review Focus 2: an unstaged rename is a deletion plus an untracked file.
func TestReviewChangesWorkingUnstagedRename(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, "old.txt", "same\n", "c1")
	if err := os.Rename(filepath.Join(dir, "old.txt"), filepath.Join(dir, "new.txt")); err != nil {
		t.Fatal(err)
	}
	in, _, _ := prepareWorking(t, dir, repo)
	if f, _ := fileOf(in.ReviewFiles, "old.txt"); !f.Deleted {
		t.Errorf("old.txt = %+v, want Deleted", f)
	}
	if f, _ := fileOf(in.ReviewFiles, "new.txt"); f.Blob == "" {
		t.Errorf("new.txt = %+v, want a blob", f)
	}
}

func TestReviewChangesWorkingBinaryUntracked(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	write(t, dir, "bin.dat", "a\x00b")
	_, diff, ctxDoc := prepareWorking(t, dir, repo)
	if !strings.Contains(diff, "Binary files /dev/null and b/bin.dat differ") {
		t.Errorf("diff:\n%s", diff)
	}
	if !strings.Contains(ctxDoc, "-\t-\tbin.dat") {
		t.Errorf("numstat:\n%s", ctxDoc)
	}
}

// Review Focus 3: past the cap the diff is truncated as today, and the big
// file is still fingerprinted.
func TestReviewChangesWorkingUntrackedPastTheCap(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	write(t, dir, "huge.txt", strings.Repeat("x\n", MaxDiffBytes))
	in, diff, ctxDoc := prepareWorking(t, dir, repo)
	if !strings.Contains(diff, "(diff truncated") || !strings.Contains(ctxDoc, "truncated") {
		t.Errorf("the diff must say it was truncated:\n%.200s", diff)
	}
	if f, ok := fileOf(in.ReviewFiles, "huge.txt"); !ok || f.Blob != gitHash(t, dir, "huge.txt") {
		t.Errorf("huge.txt fingerprint = %+v", f)
	}
}

// Review Focus 4: a nested repository is listed as "sub/" — not a file.
func TestReviewChangesWorkingSkipsADirectoryEntry(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	sub := filepath.Join(dir, "sub")
	if out, err := exec.Command("git", "init", "-q", sub).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	write(t, dir, "sub/inner.txt", "i\n")
	in, _, _ := prepareWorking(t, dir, repo)
	for _, f := range in.ReviewFiles {
		if strings.HasPrefix(f.Path, "sub") {
			t.Fatalf("a directory entry was fingerprinted: %+v", f)
		}
	}
}

// Only a working review reads untracked files and fingerprints.
func TestReviewChangesRangeHasNoFingerprints(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, "a.txt", "one\n", "c1")
	stageAndCommit(t, dir, repo, "a.txt", "two\n", "c2")
	write(t, dir, "u.txt", "u\n")
	in, err := ReviewChanges{Command: "true", Dir: dir, Diff: model.DiffSpec{Rev: "HEAD~1..HEAD"}}.
		Prepare(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Cleanup()
	if in.ReviewFiles != nil {
		t.Fatalf("ReviewFiles = %+v, want none", in.ReviewFiles)
	}
}

func TestReviewChangesCollectCarriesTheFingerprints(t *testing.T) {
	t.Parallel()
	files := []model.NoteFile{{Path: "a", Blob: "b"}}
	res, _ := ReviewChanges{Working: true}.Collect(TaskInputs{ReviewFiles: files}, []byte("r"))
	if len(res.ReviewFiles) != 1 || res.ReviewFiles[0] != files[0] {
		t.Fatalf("ReviewFiles = %+v", res.ReviewFiles)
	}
}
