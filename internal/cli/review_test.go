package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateReviewEnv points XDG_CONFIG_HOME/XDG_STATE_HOME at fresh temp dirs so
// tests never read the real machine's global gg config (which could carry a
// "review" category tool and silently change candidate counts) nor write
// review reports outside the test sandbox.
func isolateReviewEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

// writeReviewTool appends a category="review" [[tools.command]] block to
// dir's .gg.toml (creating the file if it doesn't exist yet).
func writeReviewTool(t *testing.T, dir, name, command string) {
	t.Helper()
	block := fmt.Sprintf("\n[[tools.command]]\ncategory = \"review\"\nname = %q\nmode = \"capture\"\ncommand = %q\n", name, command)
	f, err := os.OpenFile(filepath.Join(dir, ".gg.toml"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(block); err != nil {
		t.Fatal(err)
	}
}

func TestReviewTargetForArgRangeUsedAsIs(t *testing.T) {
	tgt := reviewTargetForArg("main..HEAD")
	if tgt.Range != "main..HEAD" || tgt.Diff.Rev != "main..HEAD" {
		t.Fatalf("got %+v, want Range/Diff.Rev = main..HEAD", tgt)
	}
}

func TestReviewTargetForArgSingleCommitDiffsOwnChange(t *testing.T) {
	tgt := reviewTargetForArg("abc123")
	want := "abc123^..abc123"
	if tgt.Range != want || tgt.Diff.Rev != want {
		t.Fatalf("got %+v, want Range/Diff.Rev = %q (a bare rev would diff the working tree against it, not the commit's own change)", tgt, want)
	}
}

func TestReviewNoToolConfigured(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	code, out, errb := runCLI(t, dir, "review", "--working")
	if code != 1 {
		t.Fatalf("exit=%d out=%s stderr=%s, want 1", code, out, errb)
	}
	if !strings.Contains(errb, "no review tool configured") {
		t.Fatalf("stderr = %q, want it to mention the missing [[tools.command]]", errb)
	}
}

func TestReviewToolNotFound(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	writeReviewTool(t, dir, "Echo", `printf "hi\n"`)
	code, _, errb := runCLI(t, dir, "review", "--tool", "Nope", "--working")
	if code != 1 {
		t.Fatalf("exit=%d stderr=%s, want 1", code, errb)
	}
	if !strings.Contains(errb, `no review tool named "Nope"`) {
		t.Fatalf("stderr = %q", errb)
	}
}

func TestReviewAmbiguousToolListsNames(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	writeReviewTool(t, dir, "Echo1", `printf "one\n"`)
	writeReviewTool(t, dir, "Echo2", `printf "two\n"`)
	code, _, errb := runCLI(t, dir, "review", "--working")
	if code != 1 {
		t.Fatalf("exit=%d stderr=%s, want 1", code, errb)
	}
	if !strings.Contains(errb, "multiple review tools") || !strings.Contains(errb, "Echo1") || !strings.Contains(errb, "Echo2") {
		t.Fatalf("stderr = %q, want both candidate names listed", errb)
	}
}

func TestReviewInvalidToolIgnoredFallsBackToOther(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	// An unknown category makes this block inert at load (config.ValidateToolCommand),
	// so it must not count toward "ambiguous" nor be pickable.
	block := "\n[[tools.command]]\ncategory = \"bogus\"\nname = \"Bad\"\nmode = \"capture\"\ncommand = \"true\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(block), 0o644); err != nil {
		t.Fatal(err)
	}
	writeReviewTool(t, dir, "Echo", `printf "only one\n"`)
	code, out, errb := runCLI(t, dir, "review", "--working")
	if code != 0 {
		t.Fatalf("exit=%d out=%s stderr=%s, want 0 (the sole valid review candidate)", code, out, errb)
	}
	if !strings.Contains(out, "only one") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestReviewWorkingAndPositionalMutuallyExclusive(t *testing.T) {
	dir := newRepoDir(t)
	code, _, errb := runCLI(t, dir, "review", "--working", "HEAD")
	if code != 2 {
		t.Fatalf("exit=%d stderr=%s, want 2 (usage error)", code, errb)
	}
}

func TestReviewTooManyPositionals(t *testing.T) {
	dir := newRepoDir(t)
	code, _, errb := runCLI(t, dir, "review", "HEAD", "main")
	if code != 2 {
		t.Fatalf("exit=%d stderr=%s, want 2 (usage error)", code, errb)
	}
}

func TestReviewRangePositionalPrintsAndPersists(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	runGit(t, dir, "commit", "--allow-empty", "-m", "second")
	writeReviewTool(t, dir, "Echo", `printf "FAKE REVIEW of <range>\n"`)
	code, out, errb := runCLI(t, dir, "review", "--tool", "Echo", "HEAD~1..HEAD")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "FAKE REVIEW of HEAD~1..HEAD") {
		t.Fatalf("stdout = %q", out)
	}
	if !strings.Contains(errb, "report:") {
		t.Fatalf("stderr missing persisted report path: %q", errb)
	}
}

// TestReviewFlagsMustPrecedePositional pins the documented usage contract:
// `--tool` takes a value, so (like `gg log [-n N] [<rev>]`, unlike bool-only
// `gg show <commit> [--patch]`) it must come BEFORE the positional.
// flag.Parse stops at the first non-flag argument, so a positional-first
// invocation leaves "--tool"/"Echo" as stray positionals — a usage error,
// not a silently-ignored --tool.
func TestReviewFlagsMustPrecedePositional(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	runGit(t, dir, "commit", "--allow-empty", "-m", "second")
	writeReviewTool(t, dir, "Echo", `printf "FAKE REVIEW of <range>\n"`)

	code, out, errb := runCLI(t, dir, "review", "--tool", "Echo", "HEAD~1..HEAD")
	if code != 0 {
		t.Fatalf("flags-first: exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "FAKE REVIEW of HEAD~1..HEAD") {
		t.Fatalf("flags-first: stdout = %q", out)
	}

	code, _, errb = runCLI(t, dir, "review", "HEAD~1..HEAD", "--tool", "Echo")
	if code != 2 {
		t.Fatalf("positional-first: exit=%d stderr=%s, want 2 (flags must precede the positional)", code, errb)
	}
}

func TestReviewSingleCommitPositionalDiffsOwnChange(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	runGit(t, dir, "commit", "--allow-empty", "-m", "second")
	sha := runGit(t, dir, "rev-parse", "HEAD")
	writeReviewTool(t, dir, "Echo", `printf "FAKE REVIEW of <range>\n"`)
	code, out, errb := runCLI(t, dir, "review", "--tool", "Echo", sha)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	want := "FAKE REVIEW of " + sha + "^.." + sha
	if !strings.Contains(out, want) {
		t.Fatalf("stdout = %q, want contains %q", out, want)
	}
}
