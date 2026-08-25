package cli

import (
	"strings"
	"testing"
)

func TestRemoteListPrintsRemoteBranches(t *testing.T) {
	t.Parallel()
	clone := cloneWithRemoteFoo(t) // from ops_test.go (same package)
	code, out, errb := runCLI(t, clone, "remote", "ls")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if !strings.Contains(out, "origin/foo") || !strings.Contains(out, "origin/main") {
		t.Fatalf("remote ls output missing refs:\n%s", out)
	}
	if strings.Contains(out, "origin/HEAD") {
		t.Fatalf("origin/HEAD symref should be filtered:\n%s", out)
	}
}

func TestRemoteUnknownSubcommand(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	if code, _, _ := runCLI(t, dir, "remote", "bogus"); code == 0 {
		t.Fatal("unknown remote subcommand should fail")
	}
}

func TestRemoteFetchUpdatesTrackingRefs(t *testing.T) {
	t.Parallel()
	clone := cloneWithRemoteFoo(t)
	if code, _, errb := runCLI(t, clone, "remote", "fetch"); code != 0 {
		t.Fatalf("remote fetch exit = %d (stderr: %s)", code, errb)
	}
	// foo is checkoutable as a local tracking branch after the fetch.
	if code, _, errb := runCLI(t, clone, "checkout", "origin/foo"); code != 0 {
		t.Fatalf("checkout after fetch exit = %d (stderr: %s)", code, errb)
	}
}

func TestRemotePruneDropsDeletedRef(t *testing.T) {
	t.Parallel()
	clone := cloneWithRemoteFoo(t)
	origin := runGit(t, clone, "config", "--get", "remote.origin.url")
	runGit(t, origin, "branch", "-D", "foo") // delete foo on the (bare) origin
	if code, _, errb := runCLI(t, clone, "remote", "prune"); code != 0 {
		t.Fatalf("remote prune exit = %d (stderr: %s)", code, errb)
	}
	_, out, _ := runCLI(t, clone, "remote", "ls")
	if strings.Contains(out, "origin/foo") {
		t.Fatalf("origin/foo should be pruned:\n%s", out)
	}
}

func TestRemoteRmDeletesBranchOnOrigin(t *testing.T) {
	t.Parallel()
	clone := cloneWithRemoteFoo(t)
	origin := runGit(t, clone, "config", "--get", "remote.origin.url")
	if code, _, errb := runCLI(t, clone, "remote", "rm", "origin/foo"); code != 0 {
		t.Fatalf("remote rm exit = %d (stderr: %s)", code, errb)
	}
	if out := runGit(t, origin, "branch", "--list", "foo"); strings.TrimSpace(out) != "" {
		t.Fatalf("origin still has foo: %q", out)
	}
}

func TestRemoteRmRejectsArgWithoutSlash(t *testing.T) {
	t.Parallel()
	clone := cloneWithRemoteFoo(t)
	code, _, errb := runCLI(t, clone, "remote", "rm", "noslash")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errb, "usage") {
		t.Fatalf("stderr = %q, want a usage message", errb)
	}
}
