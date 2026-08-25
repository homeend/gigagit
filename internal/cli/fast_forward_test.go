package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFastForwardCLIAdvances(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t) // repo on main with an initial commit
	gitRun(t, dir, "branch", "feat")
	gitRun(t, dir, "checkout", "feat")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "ahead")
	featTip := headSHA(t, dir)
	gitRun(t, dir, "checkout", "main")

	var out, errb bytes.Buffer
	code := Run(dir, []string{"fast-forward", featTip}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	if got := headSHA(t, dir); got != featTip {
		t.Fatalf("HEAD = %s, want %s", got, featTip)
	}
}

func TestFastForwardCLIUsage(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"fast-forward"}, strings.NewReader(""), &out, &errb, ""); code != 2 {
		t.Fatalf("missing arg exit = %d, want 2", code)
	}
}

func TestFastForwardCLIRegistered(t *testing.T) {
	t.Parallel()
	if !IsCommand("fast-forward") {
		t.Fatal("fast-forward must be in the commands map")
	}
}
