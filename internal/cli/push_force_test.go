package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cloneDiverged builds a bare origin at v1 and a clone whose tip is rewritten
// (amended) so it no longer fast-forwards origin. Returns the clone and origin
// paths.
func cloneDiverged(t *testing.T) (clone, origin string) {
	t.Helper()
	root := t.TempDir()
	origin = filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone = filepath.Join(root, "clone")
	gitIn(t, root, "init", "--bare", origin)
	gitIn(t, root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644)
	gitIn(t, seed, "checkout", "-b", "main")
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "v1")
	gitIn(t, seed, "push", "-u", "origin", "main")
	gitIn(t, root, "clone", origin, clone)
	gitIn(t, clone, "checkout", "main")
	// Rewrite the tip in place so it diverges from origin.
	os.WriteFile(filepath.Join(clone, "f.txt"), []byte("rewritten\n"), 0o644)
	gitIn(t, clone, "add", ".")
	gitIn(t, clone, "commit", "--amend", "-m", "v1 rewritten")
	return clone, origin
}

// originContent clones origin afresh and returns f.txt, proving what the remote
// actually holds.
func originContent(t *testing.T, origin string) string {
	t.Helper()
	verify := filepath.Join(t.TempDir(), "verify")
	gitIn(t, filepath.Dir(verify), "clone", origin, verify)
	gitIn(t, verify, "checkout", "main")
	b, err := os.ReadFile(filepath.Join(verify, "f.txt"))
	if err != nil {
		t.Fatalf("read verify f.txt: %v", err)
	}
	return string(b)
}

func TestPushPlainRejectedOnDivergence(t *testing.T) {
	clone, origin := cloneDiverged(t)
	if code, _, _ := runCLI(t, clone, "push"); code == 0 {
		t.Fatal("plain push of diverged history should exit non-zero")
	}
	if got := originContent(t, origin); got != "v1\n" {
		t.Fatalf("origin content = %q, want unchanged v1 after a rejected push", got)
	}
}

func TestPushForceOverwritesRemote(t *testing.T) {
	clone, origin := cloneDiverged(t)
	code, out, errb := runCLI(t, clone, "push", "--force")
	if code != 0 {
		t.Fatalf("push --force exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if !strings.Contains(out, "pushed") {
		t.Fatalf("expected 'pushed' in output:\n%s", out)
	}
	if got := originContent(t, origin); got != "rewritten\n" {
		t.Fatalf("origin content = %q, want rewritten after --force", got)
	}
}

func TestPushForceWithLeaseOverwritesRemote(t *testing.T) {
	clone, origin := cloneDiverged(t)
	code, _, errb := runCLI(t, clone, "push", "--force-with-lease")
	if code != 0 {
		t.Fatalf("push --force-with-lease exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if got := originContent(t, origin); got != "rewritten\n" {
		t.Fatalf("origin content = %q, want rewritten after --force-with-lease", got)
	}
}

func TestPushForceFlagsMutuallyExclusive(t *testing.T) {
	dir := newRepoDir(t)
	code, _, errb := runCLI(t, dir, "push", "--force", "--force-with-lease")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errb, "at most one") {
		t.Fatalf("stderr = %q, want an 'at most one' message", errb)
	}
}
