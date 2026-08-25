package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitAmendCLI(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644)
	gitRun(t, dir, "add", ".")
	var out, errb bytes.Buffer
	code := Run(dir, []string{"commit", "--amend", "-m", "reworded"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	subj, _ := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%s").Output()
	if strings.TrimSpace(string(subj)) != "reworded" {
		t.Fatalf("subject = %q, want reworded", strings.TrimSpace(string(subj)))
	}
	// amend must not add a commit: still the single "initial" (now reworded) commit.
	count, _ := exec.Command("git", "-C", dir, "rev-list", "--count", "HEAD").Output()
	if strings.TrimSpace(string(count)) != "1" {
		t.Fatalf("commit count = %q, want 1", strings.TrimSpace(string(count)))
	}
}

func TestCommitAmendNoMessageReusesCLI(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	// newRepoDir's last commit is "initial". Stage a change and amend with no -m.
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644)
	gitRun(t, dir, "add", ".")
	var out, errb bytes.Buffer
	code := Run(dir, []string{"commit", "--amend"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	subj, _ := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%s").Output()
	if strings.TrimSpace(string(subj)) != "initial" {
		t.Fatalf("subject = %q, want 'initial' (reused)", strings.TrimSpace(string(subj)))
	}
}
