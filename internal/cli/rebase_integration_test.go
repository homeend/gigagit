package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/rebaseplan"
)

// runGit runs git in dir with a frozen identity, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gg rebase -i --plan, run as a real built binary (so it can serve as the
// rebase sequence editor), rewords the oldest commit and drops the middle one.
func TestRebaseInteractiveCLIEndToEnd(t *testing.T) {
	ggBin := filepath.Join(t.TempDir(), "gg-test-bin")
	if out, err := exec.Command("go", "build", "-o", ggBin, "github.com/gigagit/gg/cmd/gg").CombinedOutput(); err != nil {
		t.Fatalf("build gg: %v\n%s", err, out)
	}

	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "r.txt"), []byte("r\n"), 0o644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
	runGit(t, dir, "checkout", "-b", "work")
	for _, n := range []string{"wip1", "wip2", "wip3"} {
		os.WriteFile(filepath.Join(dir, n+".txt"), []byte(n+"\n"), 0o644)
		runGit(t, dir, "add", ".")
		runGit(t, dir, "commit", "-m", n)
	}

	plan := rebaseplan.Plan{Entries: []rebaseplan.Entry{
		{Sha: runGit(t, dir, "rev-parse", "work~2"), Action: rebaseplan.Reword, Orig: "wip1", NewMsg: "wip1 reworded"},
		{Sha: runGit(t, dir, "rev-parse", "work~1"), Action: rebaseplan.Drop, Orig: "wip2"},
		{Sha: runGit(t, dir, "rev-parse", "work"), Action: rebaseplan.Pick, Orig: "wip3"},
	}}
	b, _ := rebaseplan.Marshal(plan)
	planPath := filepath.Join(dir, "plan.json")
	os.WriteFile(planPath, b, 0o644)

	cmd := exec.Command(ggBin, "rebase", "-i", "--plan", planPath, "main")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gg rebase -i: %v\n%s", err, out)
	}

	got := runGit(t, dir, "log", "--pretty=%s", "main..work") // newest-first
	want := "wip3\nwip1 reworded"
	if got != want {
		t.Fatalf("subjects =\n%q\nwant\n%q", got, want)
	}
}
