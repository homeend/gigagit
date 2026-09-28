package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mergeFixture: main and feat with a non-conflicting extra file on feat.
func mergeFixture(t *testing.T) string {
	t.Helper()
	dir := newRepoDir(t)
	gitRun(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("f\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "feat change")
	gitRun(t, dir, "checkout", "main")
	return dir
}

// conflictFixture: main and feat both edit shared.txt.
func conflictFixture(t *testing.T) string {
	t.Helper()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("base\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")
	gitRun(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("feat\n"), 0o644)
	gitRun(t, dir, "commit", "-am", "feat change")
	gitRun(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("main\n"), 0o644)
	gitRun(t, dir, "commit", "-am", "main change")
	return dir
}

func TestMergeIntoCurrent(t *testing.T) {
	t.Parallel()
	dir := mergeFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"merge", "feat"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "merged feat into main") {
		t.Fatalf("stdout: %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "feat.txt")); err != nil {
		t.Fatal("feat.txt missing after merge")
	}
}

func TestMergeIntoExplicitTarget(t *testing.T) {
	t.Parallel()
	dir := mergeFixture(t)
	gitRun(t, dir, "branch", "target")
	var out, errb bytes.Buffer
	code := Run(dir, []string{"merge", "--into", "target", "feat"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	// SmartMerge rung 3 ends on the target branch.
	cur, _ := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if strings.TrimSpace(string(cur)) != "target" {
		t.Fatalf("on %q, want target", strings.TrimSpace(string(cur)))
	}
}

func TestMergeConflictUnansweredNonTTY(t *testing.T) {
	t.Parallel()
	dir := conflictFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"merge", "feat"}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "keep-conflicts") || !strings.Contains(errb.String(), "abort") {
		t.Fatalf("stderr must list the options: %q", errb.String())
	}
}

func TestMergeConflictAbortFlag(t *testing.T) {
	t.Parallel()
	dir := conflictFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"merge", "--on-conflict=abort", "feat"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	got, _ := os.ReadFile(filepath.Join(dir, "shared.txt"))
	if string(got) != "main\n" {
		t.Fatalf("shared.txt = %q after abort", got)
	}
}

func TestMergeConflictKeepFlag(t *testing.T) {
	t.Parallel()
	dir := conflictFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"merge", "--on-conflict=keep", "feat"}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit %d, want 1 (conflicts kept)", code)
	}
	if err := exec.Command("git", "-C", dir, "rev-parse", "-q", "--verify", "MERGE_HEAD").Run(); err != nil {
		t.Fatal("expected the merge left in progress")
	}
}

func TestMergeUsageErrors(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	for _, args := range [][]string{
		{"merge"},                                // missing source
		{"merge", "a", "b"},                      // too many positionals
		{"merge", "--on-conflict=bogus", "feat"}, // invalid policy value
	} {
		var out, errb bytes.Buffer
		if code := Run(dir, args, strings.NewReader(""), &out, &errb, ""); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}

// mergeParents returns HEAD's parent count in dir.
func mergeParents(t *testing.T, dir string) int {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%P").Output()
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Fields(string(out)))
}

func mergeBody(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%B").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(string(out), "\n")
}

// -m sets the merge commit's message and forces a merge commit where a
// fast-forward was possible (mergeFixture fast-forwards without it).
func TestMergeMessageFlag(t *testing.T) {
	t.Parallel()
	dir := mergeFixture(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"merge", "-m", "Merge feat: the thing", "feat"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if n := mergeParents(t, dir); n != 2 {
		t.Fatalf("HEAD has %d parents, want a merge commit", n)
	}
	if got := mergeBody(t, dir); got != "Merge feat: the thing" {
		t.Fatalf("message = %q", got)
	}
}

// -F - reads a multi-line message (body + trailers) from stdin.
func TestMergeMessageFromStdin(t *testing.T) {
	t.Parallel()
	dir := mergeFixture(t)
	msg := "Merge feat: stdin\n\nWhy it shipped.\n\nCo-Authored-By: bot <b@x>\n"
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"merge", "-F", "-", "feat"}, strings.NewReader(msg), &out, &errb, ""); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if got := mergeBody(t, dir); got != strings.TrimRight(msg, "\n") {
		t.Fatalf("message = %q", got)
	}
}

func TestMergeMessageFromFile(t *testing.T) {
	t.Parallel()
	dir := mergeFixture(t)
	f := filepath.Join(t.TempDir(), "msg.txt")
	os.WriteFile(f, []byte("Merge feat: from file\n"), 0o644)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"merge", "-F", f, "feat"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if got := mergeBody(t, dir); got != "Merge feat: from file" {
		t.Fatalf("message = %q", got)
	}
}

// --no-ff alone: a merge commit with git's own message.
func TestMergeNoFF(t *testing.T) {
	t.Parallel()
	dir := mergeFixture(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"merge", "--no-ff", "feat"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if n := mergeParents(t, dir); n != 2 {
		t.Fatalf("HEAD has %d parents, want a merge commit", n)
	}
	if got := mergeBody(t, dir); !strings.HasPrefix(got, "Merge branch 'feat'") {
		t.Fatalf("message = %q, want git's default", got)
	}
}

func TestMergeMessageUsageErrors(t *testing.T) {
	t.Parallel()
	dir := mergeFixture(t)
	before := headSha(t, dir)
	for name, args := range map[string][]string{
		"both -m and -F": {"merge", "-m", "x", "-F", "-", "feat"},
		"empty -m":       {"merge", "-m", "  \n", "feat"},
		"missing file":   {"merge", "-F", filepath.Join(dir, "nope.txt"), "feat"},
	} {
		var out, errb bytes.Buffer
		if code := Run(dir, args, strings.NewReader(""), &out, &errb, ""); code != 2 {
			t.Errorf("%s: exit %d, want 2 (stderr %q)", name, code, errb.String())
		}
		if got := headSha(t, dir); got != before {
			t.Errorf("%s: merged anyway (HEAD moved to %s)", name, got)
		}
	}
}
