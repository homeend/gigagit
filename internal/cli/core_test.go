package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

func newRepoDir(t *testing.T) string {
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
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")
	return dir
}

func TestCLIDeciderPolicyAnswers(t *testing.T) {
	d := cliDecider{policy: map[string]string{"non-fast-forward": "rebase"}}
	resp, err := d.Decide(context.Background(), engine.DecisionRequest{ID: "non-fast-forward"})
	if err != nil || resp.Option != "rebase" {
		t.Fatalf("resp=%v err=%v, want rebase", resp, err)
	}
}

func TestCLIDeciderNonInteractiveUnansweredErrors(t *testing.T) {
	d := cliDecider{policy: map[string]string{}, interactive: false}
	_, err := d.Decide(context.Background(), engine.DecisionRequest{ID: "non-fast-forward", Options: []string{"rebase", "abort"}})
	if err == nil {
		t.Fatal("non-interactive unanswered decision must error")
	}
}

func TestRunOperationCommitsAndStreamsProgress(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	svc := domain.Open(dir)

	var prog bytes.Buffer
	res, err := runOperation(context.Background(), svc, engine.Commit{Message: "second", All: true}, cliDecider{}, &prog)
	if err != nil {
		t.Fatalf("runOperation: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if !strings.Contains(prog.String(), "committing") {
		t.Fatalf("progress missing 'committing':\n%s", prog.String())
	}
}
