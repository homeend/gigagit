package cli

import (
	"os/exec"
	"strings"
	"testing"
)

// A stale exact fetch refspec (its branch deleted on the remote) makes every
// fetch exit 128. --on-stale-mapping=remove answers the fetch_mapping.stale
// fork: the mapping is removed and the pull completes.
func TestPullOnStaleMappingRemove(t *testing.T) {
	t.Parallel()
	clone := cloneBehind(t)
	gitIn(t, clone, "config", "--add", "remote.origin.fetch",
		"+refs/heads/gone:refs/remotes/origin/gone")

	code, out, errb := runCLI(t, clone, "pull", "--on-stale-mapping", "remove")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if !strings.Contains(out, "pulled") {
		t.Fatalf("expected 'pulled' in output:\n%s", out)
	}
	cmd := exec.Command("git", "config", "--get-all", "remote.origin.fetch")
	cmd.Dir = clone
	specs, err := cmd.Output()
	if err != nil {
		t.Fatalf("config --get-all: %v", err)
	}
	if strings.Contains(string(specs), "gone") {
		t.Fatalf("stale mapping survived:\n%s", specs)
	}
}

// --on-stale-mapping=abort keeps the mapping; the pull fails with the
// original fetch error. A piped run without the flag behaves the same (the
// fork stays unanswered; config is never mutated unseen).
func TestPullOnStaleMappingAbort(t *testing.T) {
	t.Parallel()
	clone := cloneBehind(t)
	gitIn(t, clone, "config", "--add", "remote.origin.fetch",
		"+refs/heads/gone:refs/remotes/origin/gone")

	code, _, errb := runCLI(t, clone, "pull", "--on-stale-mapping", "abort")
	if code == 0 {
		t.Fatal("exit = 0, want failure")
	}
	if !strings.Contains(errb, "couldn't find remote ref") {
		t.Fatalf("stderr should carry the fetch failure:\n%s", errb)
	}
}

func TestPullOnStaleMappingBadValue(t *testing.T) {
	t.Parallel()
	clone := cloneBehind(t)
	code, _, errb := runCLI(t, clone, "pull", "--on-stale-mapping", "bogus")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr: %s)", code, errb)
	}
}
