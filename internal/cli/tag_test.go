package cli

import (
	"os/exec"
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

func TestTagCreateLightweightAndAnnotated(t *testing.T) {
	dir := newRepoDir(t)
	if code, _, errb := runCLI(t, dir, "tag", "create", "v1.0.0"); code != 0 {
		t.Fatalf("lightweight create exit %d: %s", code, errb)
	}
	if code, _, errb := runCLI(t, dir, "tag", "create", "-m", "rel2", "v2.0.0"); code != 0 {
		t.Fatalf("annotated create exit %d: %s", code, errb)
	}
	_, out, _ := runCLI(t, dir, "tag", "ls")
	if !strings.Contains(out, "v1.0.0") || !strings.Contains(out, "v2.0.0") {
		t.Fatalf("created tags not listed:\n%s", out)
	}
}

func TestTagCreateRequiresName(t *testing.T) {
	dir := newRepoDir(t)
	if code, _, _ := runCLI(t, dir, "tag", "create"); code == 0 {
		t.Fatal("create with no name must fail")
	}
}

func TestTagRmDeletes(t *testing.T) {
	dir := newRepoDir(t)
	gitRun(t, dir, "tag", "v1.0.0")
	if code, _, errb := runCLI(t, dir, "tag", "rm", "v1.0.0"); code != 0 {
		t.Fatalf("rm exit %d: %s", code, errb)
	}
	_, out, _ := runCLI(t, dir, "tag", "ls")
	if strings.Contains(out, "v1.0.0") {
		t.Fatalf("tag still listed:\n%s", out)
	}
}

func TestTagRmRequiresName(t *testing.T) {
	dir := newRepoDir(t)
	if code, _, _ := runCLI(t, dir, "tag", "rm"); code == 0 {
		t.Fatal("rm with no name must fail")
	}
}

func TestTagCheckoutDetached(t *testing.T) {
	dir := newRepoDir(t)
	gitRun(t, dir, "tag", "v1.0.0")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "c2")
	if code, _, errb := runCLI(t, dir, "tag", "checkout", "v1.0.0"); code != 0 {
		t.Fatalf("checkout exit %d: %s", code, errb)
	}
	out, _ := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("expected detached HEAD, on %q", out)
	}
}

func TestTagCheckoutToBranch(t *testing.T) {
	dir := newRepoDir(t)
	gitRun(t, dir, "tag", "v1.0.0")
	if code, _, errb := runCLI(t, dir, "tag", "checkout", "--branch", "rel", "v1.0.0"); code != 0 {
		t.Fatalf("checkout --branch exit %d: %s", code, errb)
	}
	out, _ := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if strings.TrimSpace(string(out)) != "rel" {
		t.Fatalf("on %q, want rel", out)
	}
}

func TestTagPushToOrigin(t *testing.T) {
	clone := cloneWithRemoteFoo(t)
	gitRun(t, clone, "tag", "v1.0.0")
	if code, _, errb := runCLI(t, clone, "tag", "push", "v1.0.0", "origin"); code != 0 {
		t.Fatalf("push exit %d: %s", code, errb)
	}
	out, _ := exec.Command("git", "-C", clone, "ls-remote", "--tags", "origin").Output()
	if !strings.Contains(string(out), "refs/tags/v1.0.0") {
		t.Fatalf("tag not pushed:\n%s", out)
	}
}

func TestTagPushRequiresName(t *testing.T) {
	dir := newRepoDir(t)
	if code, _, _ := runCLI(t, dir, "tag", "push"); code == 0 {
		t.Fatal("push with no name must fail")
	}
}

func TestTagRmRemoteDeletesTagOnOrigin(t *testing.T) {
	clone := cloneWithRemoteFoo(t)
	origin := runGit(t, clone, "config", "--get", "remote.origin.url")
	runGit(t, clone, "tag", "v1.0.0")
	runGit(t, clone, "push", "origin", "v1.0.0")

	if code, _, errb := runCLI(t, clone, "tag", "rm", "--remote", "v1.0.0", "origin"); code != 0 {
		t.Fatalf("tag rm --remote exit = %d (stderr: %s)", code, errb)
	}
	if out := runGit(t, origin, "tag", "-l", "v1.0.0"); strings.TrimSpace(out) != "" {
		t.Fatalf("origin still has tag v1.0.0: %q", out)
	}
}

func TestTagRmLocalStillLocalOnly(t *testing.T) {
	clone := cloneWithRemoteFoo(t)
	origin := runGit(t, clone, "config", "--get", "remote.origin.url")
	runGit(t, clone, "tag", "v1.0.0")
	runGit(t, clone, "push", "origin", "v1.0.0")

	if code, _, errb := runCLI(t, clone, "tag", "rm", "v1.0.0"); code != 0 {
		t.Fatalf("tag rm exit = %d (stderr: %s)", code, errb)
	}
	// local gone, origin untouched
	if out := runGit(t, clone, "tag", "-l", "v1.0.0"); strings.TrimSpace(out) != "" {
		t.Fatalf("local tag v1.0.0 not deleted: %q", out)
	}
	if out := runGit(t, origin, "tag", "-l", "v1.0.0"); strings.TrimSpace(out) == "" {
		t.Fatal("local rm must not touch the origin tag")
	}
}

func TestTagAnnotateMakesAnnotated(t *testing.T) {
	dir := newRepoDir(t)
	runGit(t, dir, "tag", "v1.0.0") // lightweight
	if code, _, errb := runCLI(t, dir, "tag", "annotate", "-m", "release one", "v1.0.0"); code != 0 {
		t.Fatalf("tag annotate exit = %d (stderr: %s)", code, errb)
	}
	if typ := strings.TrimSpace(runGit(t, dir, "cat-file", "-t", "v1.0.0")); typ != "tag" {
		t.Fatalf("tag type = %q, want tag (annotated)", typ)
	}
}

func TestTagAnnotateRequiresMessage(t *testing.T) {
	dir := newRepoDir(t)
	runGit(t, dir, "tag", "v1.0.0")
	if code, _, _ := runCLI(t, dir, "tag", "annotate", "v1.0.0"); code != 2 {
		t.Fatalf("missing -m exit = %d, want 2", code)
	}
}

func TestTagAnnotateUnknownTag(t *testing.T) {
	dir := newRepoDir(t)
	if code, _, _ := runCLI(t, dir, "tag", "annotate", "-m", "x", "nope"); code == 0 {
		t.Fatal("unknown tag must exit non-zero")
	}
}
