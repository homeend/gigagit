package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/textdiff"
)

// loadedModelTwoFileCompare builds a real repo with three commits — the root,
// one adding a.txt, one adding b.txt (distinct content) — and returns the
// loaded model plus root/tip endpoints for a commit↔commit comparison whose
// changed set is exactly {a.txt, b.txt}.
func loadedModelTwoFileCompare(t *testing.T) (Model, model.Endpoint, model.Endpoint) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "c0")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha-content\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "c1")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bravo-content\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "c2")

	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	mm := loaded.(Model)
	if len(mm.commits) < 3 {
		t.Fatalf("expected 3 commits, got %d", len(mm.commits))
	}
	left := model.Endpoint{Kind: model.EndpointCommit, Hash: mm.commits[2].Hash}  // root
	right := model.Endpoint{Kind: model.EndpointCommit, Hash: mm.commits[0].Hash} // tip
	return mm, left, right
}

// staleRowsText flattens the new side of aligned rows for content assertions.
func staleRowsText(rows []textdiff.Row) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r.Right)
		b.WriteString("\n")
	}
	return b.String()
}

// A superseded compare-diff load must not touch the live diff view. Regression:
// loadCompareDiffCmd captured the live layer (v := m.diffLayer()) and its async
// closure mutated it with no staleness gate, so when file-stepping (N/P/home/
// end) outran a slow load — easy in a commit↔commit compare, where already-seen
// files answer from the LRU cache instantly — the stale result overwrote the
// file the view had since stepped to. The tag-gated diffMsg was correctly
// dropped, but the mutation had already happened: the view showed one file's
// content under another file's title, so stepping looked like it did nothing.
func TestStaleCompareDiffLoadCannotClobberLiveView(t *testing.T) {
	m, left, right := loadedModelTwoFileCompare(t)
	m.width, m.height = 100, 30
	m, _ = m.openCompareFiles(left, right)
	m.filesView.lines = []contentLine{
		{text: "A  a.txt", path: "a.txt", status: "A"},
		{text: "A  b.txt", path: "b.txt", status: "A"},
	}
	m.filesView.sel = 0

	// enter opens a.txt's diff; keep its load command pending (the slow load).
	u, cmdA := m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.diffLayer() == nil || cmdA == nil {
		t.Fatal("enter must open the diff view with a load command")
	}

	// Step to b.txt before a.txt's load lands (what N N at the last change
	// resolves to); b's load completes first and is applied.
	u2, cmdB := m.stepDiffFile(1)
	m = u2.(Model)
	if cmdB == nil {
		t.Fatal("file step must fire b.txt's load")
	}
	u3, _ := m.Update(cmdB())
	m = u3.(Model)
	v := m.diffLayer()
	if v.title != "b.txt" || v.loading {
		t.Fatalf("precondition: view must show b.txt loaded (title=%q loading=%v)", v.title, v.loading)
	}
	if got := staleRowsText(v.full); !strings.Contains(got, "bravo-content") {
		t.Fatalf("precondition: b.txt rows missing, got %q", got)
	}

	// The stale a.txt closure now completes; its message is dropped by the tag
	// gate — and the live view must be left untouched.
	u4, _ := m.Update(cmdA())
	m = u4.(Model)
	v = m.diffLayer()
	got := staleRowsText(v.full)
	if strings.Contains(got, "alpha-content") || !strings.Contains(got, "bravo-content") {
		t.Errorf("stale load clobbered the live view: rows = %q", got)
	}
	if v.title != "b.txt" {
		t.Errorf("title = %q, want b.txt", v.title)
	}
}
