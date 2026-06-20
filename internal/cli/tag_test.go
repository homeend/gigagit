package cli

import (
	"strings"
	"testing"
)

func TestTagListPrintsTags(t *testing.T) {
	dir := newRepoDir(t)
	gitRun(t, dir, "tag", "v1.0.0")
	gitRun(t, dir, "tag", "-a", "v2.0.0", "-m", "rel2")

	code, out, errb := runCLI(t, dir, "tag", "ls")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if !strings.Contains(out, "v1.0.0") || !strings.Contains(out, "v2.0.0") {
		t.Fatalf("tag ls output missing tags:\n%s", out)
	}
}

func TestTagUnknownSubcommand(t *testing.T) {
	dir := newRepoDir(t)
	if code, _, _ := runCLI(t, dir, "tag", "bogus"); code == 0 {
		t.Fatal("unknown tag subcommand should fail")
	}
}
