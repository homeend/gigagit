package cli

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

func TestCommitRewordCLIHead(t *testing.T) {
	dir := newRepoDir(t) // single commit "initial" (HEAD == root)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"commit", "reword", "HEAD", "-m", "new message"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	subj, _ := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%B").Output()
	if !strings.Contains(string(subj), "new message") {
		t.Fatalf("message = %q, want it reworded", string(subj))
	}
	// reword must not add a commit.
	count, _ := exec.Command("git", "-C", dir, "rev-list", "--count", "HEAD").Output()
	if strings.TrimSpace(string(count)) != "1" {
		t.Fatalf("commit count = %q, want 1", strings.TrimSpace(string(count)))
	}
}

func TestCommitRewordCLIUsage(t *testing.T) {
	dir := newRepoDir(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"commit", "reword", "HEAD"}, strings.NewReader(""), &out, &errb, ""); code != 2 {
		t.Fatalf("want usage exit 2 (missing -m), got %d", code)
	}
}
