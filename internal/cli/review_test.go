package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
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

// writeReviewToolFrontends is writeReviewTool but with an explicit
// frontends = [...] tag, for testing the "cli" visibility filter.
func writeReviewToolFrontends(t *testing.T, dir, name, command string, frontends []string) {
	t.Helper()
	quoted := make([]string, len(frontends))
	for i, f := range frontends {
		quoted[i] = fmt.Sprintf("%q", f)
	}
	block := fmt.Sprintf("\n[[tools.command]]\ncategory = \"review\"\nname = %q\nmode = \"capture\"\nfrontends = [%s]\ncommand = %q\n",
		name, strings.Join(quoted, ", "), command)
	f, err := os.OpenFile(filepath.Join(dir, ".gg.toml"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(block); err != nil {
		t.Fatal(err)
	}
}

// A review block tagged frontends=["web"] must never be visible to the CLI:
// as the sole candidate it must NOT be auto-picked (falls through to "no
// review tool configured"), and alongside other candidates it must not
// appear in the ambiguous-tool name list.
func TestReviewFrontendsWebOnlyHiddenFromCLISoleCandidate(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	writeReviewToolFrontends(t, dir, "WebOnly", `printf "web only\n"`, []string{"web"})
	code, _, errb := runCLI(t, dir, "review", "--working")
	if code != 1 {
		t.Fatalf("exit=%d stderr=%s, want 1 (web-only tool must not be picked by the CLI)", code, errb)
	}
	if !strings.Contains(errb, "no review tool configured") {
		t.Fatalf("stderr = %q, want it to report no review tool configured", errb)
	}
}

func TestReviewFrontendsWebOnlyExcludedFromAmbiguousList(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	writeReviewTool(t, dir, "Echo1", `printf "one\n"`)
	writeReviewTool(t, dir, "Echo2", `printf "two\n"`)
	writeReviewToolFrontends(t, dir, "WebOnly", `printf "web only\n"`, []string{"web"})
	code, _, errb := runCLI(t, dir, "review", "--working")
	if code != 1 {
		t.Fatalf("exit=%d stderr=%s, want 1", code, errb)
	}
	if !strings.Contains(errb, "multiple review tools") || !strings.Contains(errb, "Echo1") || !strings.Contains(errb, "Echo2") {
		t.Fatalf("stderr = %q, want Echo1 and Echo2 listed", errb)
	}
	if strings.Contains(errb, "WebOnly") {
		t.Fatalf("stderr = %q, WebOnly must not appear in the CLI candidate list", errb)
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

// A tool that writes $GG_NOTES_FILE has its notes imported and the ids listed
// on stderr; the report itself still prints and is still persisted.
func TestReviewNotesImportsSidecarFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "seed")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeReviewTool(t, dir, "Echo",
		`printf 'THE REPORT\n'; printf '{"version":1,"files":[{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"shouty"}]}]}' > "$GG_NOTES_FILE"`)

	code, out, errb := runCLI(t, dir, "review", "--tool", "Echo", "--working", "--notes")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "THE REPORT") {
		t.Fatalf("the freeform report must still print: %q", out)
	}
	if !strings.Contains(errb, "notes:") {
		t.Fatalf("stderr must list the imported ids: %q", errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--file", "a.txt")
	if !strings.Contains(list, "shouty") || !strings.Contains(list, "new:2-2") {
		t.Fatalf("the note must be stored against the working tree:\n%s", list)
	}
}

// When the notes file stays empty but the REPORT itself is agent-context v1,
// that is imported instead (the report body is still the JSON).
func TestReviewNotesFallsBackToJSONReport(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "seed")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeReviewTool(t, dir, "Echo",
		`printf '{"version":1,"files":[{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"from the report"}]}]}\n'`)

	code, _, errb := runCLI(t, dir, "review", "--tool", "Echo", "--working", "--notes")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--file", "a.txt")
	if !strings.Contains(list, "from the report") {
		t.Fatalf("a JSON report must be imported when the notes file is empty:\n%s", list)
	}
}

// Neither channel carried notes: exit 1, naming the contract.
func TestReviewNotesNoNotesIsExit1(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	runGit(t, dir, "commit", "--allow-empty", "-m", "second")
	writeReviewTool(t, dir, "Echo", `printf 'just prose, no JSON\n'`)
	code, _, errb := runCLI(t, dir, "review", "--tool", "Echo", "--working", "--notes")
	if code != 1 {
		t.Fatalf("exit=%d stderr=%s, want 1", code, errb)
	}
	if !strings.Contains(errb, "review tool wrote no notes") || !strings.Contains(errb, "GG_NOTES_FILE") {
		t.Fatalf("stderr = %q, want the documented message", errb)
	}
}

// A RANGE review's base is not a note-addressable side: old-side annotations
// are skipped with one warning, and the notes land on the tip commit.
func TestReviewNotesRangeAnchorsTipNewSideOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "seed")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "shout")
	sha := runGit(t, dir, "rev-parse", "HEAD")
	writeReviewTool(t, dir, "Echo",
		`printf 'R\n'; printf '{"version":1,"files":[{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"kept"},{"oldRange":[2,2],"summary":"dropped"}]}]}' > "$GG_NOTES_FILE"`)

	code, _, errb := runCLI(t, dir, "review", "--tool", "Echo", "--notes", "HEAD~1..HEAD")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(errb, "old-side") {
		t.Fatalf("stderr = %q, want one old-side warning", errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--rev", sha, "--file", "a.txt")
	if !strings.Contains(list, "kept") || strings.Contains(list, "dropped") {
		t.Fatalf("only the new-side note may land on the tip commit:\n%s", list)
	}
}

// A --notes run that imported NOTHING (the tool wrote a batch carrying only a
// top-level context) must not wake the window: the reload post exists to show
// new notes, and a stray wire command with none to show is noise.
func TestReviewNotesStoringNothingPostsNoReload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	runGit(t, dir, "commit", "--allow-empty", "-m", "second")
	inbox := steerDirFor(domain.Open(dir))
	if inbox == "" {
		t.Fatal("setup: no inbox resolved for the test repo")
	}
	livePresence(t, inbox)
	writeReviewTool(t, dir, "Echo",
		`printf 'THE REPORT\n'; printf '{"version":1,"summary":"nothing anchored","files":[]}' > "$GG_NOTES_FILE"`)

	code, _, errb := runCLI(t, dir, "review", "--tool", "Echo", "--working", "--notes")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if got := steer.Drain(inbox); len(got) != 0 {
		t.Fatalf("posted %+v, want nothing — no note was imported", got)
	}
}

func TestReviewSkipsInteractiveRows(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	// An interactive row waits for a human: gg review (headless) must not
	// run it, so with only that row configured there is no review tool.
	block := "\n[[tools.command]]\ncategory = \"review\"\nname = \"Claude (interactive)\"\nmode = \"interactive\"\ncommand = \"true\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(block), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errb := runCLI(t, dir, "review", "--working")
	if code != 1 || !strings.Contains(errb, "no review tool configured") {
		t.Fatalf("exit=%d out=%s stderr=%s, want 1 + no review tool", code, out, errb)
	}
}
