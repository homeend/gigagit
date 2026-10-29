package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// A rebase paused in a worktree lists it detached; /api/worktrees names
// the branch it rewrites (model.Worktree.Rebasing), so the page can keep
// the tree under that branch as the TUI does.
func TestWorktreesEndpointNamesARebasingBranch(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	second := filepath.Join(t.TempDir(), "wt2")
	gitRun(t, dir, "worktree", "add", "-b", "w2", second)
	gitRun(t, second, "checkout", "-q", "--detach")
	gitDir := strings.TrimSpace(gitRun(t, second, "rev-parse", "--path-format=absolute", "--absolute-git-dir"))
	if err := os.MkdirAll(filepath.Join(gitDir, "rebase-merge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "rebase-merge", "head-name"), []byte("refs/heads/w2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	var body struct {
		Worktrees []struct {
			Path     string `json:"path"`
			Branch   string `json:"branch"`
			Detached bool   `json:"detached"`
			Rebasing string `json:"rebasing"`
		} `json:"worktrees"`
	}
	if code := getJSON(t, ts, "/api/worktrees", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	found := false
	for _, w := range body.Worktrees {
		if filepath.Base(w.Path) == "wt2" {
			found = true
			if !w.Detached || w.Branch != "" || w.Rebasing != "w2" {
				t.Fatalf("rebasing worktree = %+v, want detached, branch \"\", rebasing w2", w)
			}
		}
	}
	if !found {
		t.Fatalf("wt2 not listed: %+v", body.Worktrees)
	}
}

// heldBranch is the TUI's HeldBranch: every branch → worktree lookup on the
// page (the branch row's path, switch's "go to worktree", the delete row,
// the walk's landing row) and the Worktrees row label use it.
func TestRebasingWorktreeJS(t *testing.T) {
	t.Parallel()
	var mod strings.Builder
	for _, fn := range []string{"pathKey", "heldBranch", "worktreeBranch", "worktreeLabel"} {
		mod.WriteString(jsFunc(t, "walk.js", fn) + "\n")
	}
	src := mod.String() + `
const wts = [{path: "/r", branch: "main"}, {path: "/r/wt-a", branch: "", detached: true, rebasing: "feat"}, {path: "/r/wt-b", branch: "", detached: true}, {path: "/r/bare", bare: true}];
console.log(JSON.stringify([
  wts.map(heldBranch),
  worktreeBranch(wts, "/r/wt-a"), // the walk lands on the branch a paused rebase rewrites
  worktreeBranch(wts, "/r/wt-b"),
  wts.map(worktreeLabel),
]));
`
	want := `[["main","feat","",""],"feat","",["main","feat (rebasing)","(detached)","(bare)"]]`
	if got := nodeRun(t, src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	side := readStaticSrc(t, "sidebar.js")
	ops := readStaticSrc(t, "ops.js")
	cm := readStaticSrc(t, "commits.js")
	for _, pin := range []struct{ src, want, why string }{
		{side, "state.worktrees.find((x) => heldBranch(x) === name)", "a branch row finds its worktree through a paused rebase"},
		{side, "worktreeLabel(w)", "the Worktrees row is labelled by worktreeLabel"},
		{side, "(w) => heldBranch(w) || w.path", "the Worktrees sort name is the held branch"},
		{ops, "heldBranch(w) === branch && w.path !== state.worktree", "switch offers to go to a worktree a rebase holds the branch in"},
		{cm, "some((w) => heldBranch(w) === ref.name)", "no delete row for a branch a paused rebase holds"},
	} {
		if !strings.Contains(pin.src, pin.want) {
			t.Errorf("%s: missing %q", pin.why, pin.want)
		}
	}
}
