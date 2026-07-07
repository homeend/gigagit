package engine

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repogate"
)

func TestReviewChangesLockModeRead(t *testing.T) {
	if (ReviewChanges{}).LockMode() != repogate.Read {
		t.Fatal("want Read")
	}
}

// A task-agent writes the report to $GG_MESSAGE_FILE; that content wins over stdout.
func TestReviewChangesPrefersMessageFile(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	dir, repo := newRepo(t)
	// two commits so HEAD~1..HEAD is a real range
	stageAndCommit(t, dir, repo, "a.txt", "one\n", "c1")
	stageAndCommit(t, dir, repo, "a.txt", "one\ntwo\n", "c2")

	cmd := `printf 'stdout is a report, ignore me\n'; ` +
		`printf 'REVIEW: looks fine\n' > "$GG_MESSAGE_FILE"`
	res, err := ReviewChanges{Command: cmd, Dir: dir, Diff: model.DiffSpec{Rev: "HEAD~1..HEAD"}, RangeLabel: "HEAD~1..HEAD"}.
		Run(context.Background(), OpDeps{Repo: repo, CaptureRunner: ShellCaptureRunner{}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Captured != "REVIEW: looks fine\n" {
		t.Fatalf("captured=%q, want the message-file content (file wins over stdout)", res.Captured)
	}
}

// A stdout tool (Claude) leaves the file empty; stdout is used.
func TestReviewChangesFallsBackToStdout(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, "a.txt", "one\n", "c1")

	res, err := ReviewChanges{Command: `printf 'the whole review on stdout\n'`, Dir: dir, Diff: model.DiffSpec{Rev: "HEAD"}, RangeLabel: "HEAD"}.
		Run(context.Background(), OpDeps{Repo: repo, CaptureRunner: ShellCaptureRunner{}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Captured != "the whole review on stdout\n" {
		t.Fatalf("captured=%q, want stdout", res.Captured)
	}
}

// $GG_REVIEW_DIFF holds the RANGE diff (not --cached); the context file names the range.
func TestReviewChangesWritesRangeDiffAndContext(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/cp")
	}
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, "a.txt", "one\n", "c1")
	stageAndCommit(t, dir, repo, "a.txt", "one\nTWO\n", "c2")

	// Copy the two provisioned files out so we can assert on them post-run.
	cmd := `cp "$GG_REVIEW_DIFF" "` + dir + `/seen.diff"; cp "$GG_CONTEXT_FILE" "` + dir + `/seen.ctx"; printf x > "$GG_MESSAGE_FILE"`
	_, err := ReviewChanges{Command: cmd, Dir: dir, Diff: model.DiffSpec{Rev: "HEAD~1..HEAD"}, RangeLabel: "HEAD~1..HEAD"}.
		Run(context.Background(), OpDeps{Repo: repo, CaptureRunner: ShellCaptureRunner{}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	diffSeen := readFile(t, dir+"/seen.diff")
	if !strings.Contains(diffSeen, "TWO") {
		t.Fatalf("review diff must be the range diff (contain TWO):\n%s", diffSeen)
	}
	ctxSeen := readFile(t, dir+"/seen.ctx")
	if !strings.Contains(ctxSeen, "HEAD~1..HEAD") {
		t.Fatalf("context file must name the range:\n%s", ctxSeen)
	}
}

func stageAndCommit(t *testing.T, dir string, repo *git.Repo, name, content, msg string) {
	t.Helper()
	stageFile(t, dir, repo, name, content) // stageFile already exists in generate_message_test.go
	if err := repo.Commit(context.Background(), msg, false, false); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
