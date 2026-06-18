package cli

import (
	"strings"
	"testing"
)

func TestRemoteListPrintsRemoteBranches(t *testing.T) {
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
	dir := newRepoDir(t)
	if code, _, _ := runCLI(t, dir, "remote", "bogus"); code == 0 {
		t.Fatal("unknown remote subcommand should fail")
	}
}
