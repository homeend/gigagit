package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
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
	if !regexp.MustCompile(`(?m)^note: [0-9a-f]{8}$`).MatchString(errb) {
		t.Fatalf("stderr missing the stored note id: %q", errb)
	}
	if strings.Contains(errb, "report:") {
		t.Fatalf("stderr still names a report file: %q", errb)
	}
}

// A working-changes review is stored as a note and listed like any review.
func TestReviewWorkingStoresANote(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	writeReviewTool(t, dir, "Echo", `printf "FAKE WORKING REVIEW\n"`)
	code, out, errb := runCLI(t, dir, "review", "--tool", "Echo", "--working")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "FAKE WORKING REVIEW") || !strings.Contains(errb, "note: ") {
		t.Fatalf("stdout=%q stderr=%q, want the review printed and its note id", out, errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list")
	if !strings.Contains(list, "] review working changes") {
		t.Fatalf("note list = %q, want the working review row", list)
	}
}

func TestReviewWorkingWithNotesIsAUsageError(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	writeReviewTool(t, dir, "Echo", `printf "x\n"`)
	code, _, errb := runCLI(t, dir, "review", "--tool", "Echo", "--working", "--notes")
	if code != 2 || !strings.Contains(errb, "a working review is stored; its notes show on the files") {
		t.Fatalf("exit=%d stderr=%q, want 2 and the documented message", code, errb)
	}
}

// The stored review's notes show on a matching file's `note list --file`, and
// leave once the file is edited.
func TestNoteListFileShowsAWorkingReviewsNotesWhileTheFileMatches(t *testing.T) {
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
		`printf '{"version":1,"summary":"R","files":[{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"shouty"}]}]}' > "$GG_MESSAGE_FILE"`)
	if code, _, errb := runCLI(t, dir, "review", "--tool", "Echo", "--working"); code != 0 {
		t.Fatalf("review exit=%d stderr=%s", code, errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--file", "a.txt")
	if !strings.Contains(list, "shouty") || !strings.Contains(list, "review:") {
		t.Fatalf("note list --file = %q, want the review's note", list)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, list, _ = runCLI(t, dir, "note", "list", "--file", "a.txt"); strings.Contains(list, "shouty") {
		t.Fatalf("an edited file still lists the review's note: %q", list)
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

// A tool that writes the review document to $GG_MESSAGE_FILE has its notes
// imported and the ids listed on stderr; the review itself still prints.
func TestReviewNotesImportsTheDocument(t *testing.T) {
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
	runGit(t, dir, "commit", "-am", "change")
	writeReviewTool(t, dir, "Echo",
		`printf '{"version":1,"summary":"THE REPORT","files":[{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"shouty"}]}]}' > "$GG_MESSAGE_FILE"`)

	code, out, errb := runCLI(t, dir, "review", "--tool", "Echo", "--notes", "HEAD")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "THE REPORT") {
		t.Fatalf("the review must still print: %q", out)
	}
	if !strings.Contains(errb, "notes:") {
		t.Fatalf("stderr must list the imported ids: %q", errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--rev", "HEAD", "--file", "a.txt")
	if !strings.Contains(list, "shouty") || !strings.Contains(list, "new:2-2") {
		t.Fatalf("the note must be stored against the working tree:\n%s", list)
	}
}

// A tool with only stdout prints the document there; that is imported too.
func TestReviewNotesReadsTheDocumentFromStdout(t *testing.T) {
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
	runGit(t, dir, "commit", "-am", "change")
	writeReviewTool(t, dir, "Echo",
		`printf '{"version":1,"summary":"ok","files":[{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"from the report"}]}]}\n'`)

	code, _, errb := runCLI(t, dir, "review", "--tool", "Echo", "--notes", "HEAD")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--rev", "HEAD", "--file", "a.txt")
	if !strings.Contains(list, "from the report") {
		t.Fatalf("a document on stdout must be imported:\n%s", list)
	}
}

// A prose review is no document: exit 1, naming the contract.
func TestReviewNotesNoNotesIsExit1(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	runGit(t, dir, "commit", "--allow-empty", "-m", "second")
	writeReviewTool(t, dir, "Echo", `printf 'just prose, no JSON\n'`)
	code, _, errb := runCLI(t, dir, "review", "--tool", "Echo", "--notes", "HEAD")
	if code != 1 {
		t.Fatalf("exit=%d stderr=%s, want 1", code, errb)
	}
	if !strings.Contains(errb, "not a gg review document") {
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
		`printf '{"version":1,"summary":"R","files":[{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"kept"},{"oldRange":[2,2],"summary":"dropped"}]}]}' > "$GG_MESSAGE_FILE"`)

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

// A --notes run that imported NOTHING (the document has only an overview)
// must not wake the window: the reload post exists to show
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
		`printf '{"version":1,"summary":"nothing anchored","files":[]}' > "$GG_MESSAGE_FILE"`)

	code, _, errb := runCLI(t, dir, "review", "--tool", "Echo", "--notes", "HEAD")
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

// The whole lane from the CLI: a branch review becomes a note on the branch's
// tip, and deleting the branch in gg takes the review with it.
func TestReviewBranchNoteGoesWithTheBranch(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	writeReviewTool(t, dir, "Echo", `printf "BRANCH REVIEW\n"`)
	runGit(t, dir, "add", ".gg.toml")
	runGit(t, dir, "commit", "-m", "tool")
	runGit(t, dir, "checkout", "-b", "feature")
	runGit(t, dir, "commit", "--allow-empty", "-m", "feature work")

	code, _, errb := runCLI(t, dir, "review", "--tool", "Echo")
	if code != 0 {
		t.Fatalf("review: exit=%d stderr=%s", code, errb)
	}
	ctx := context.Background()
	revs, err := domain.Open(dir).Reviews(ctx)
	if err != nil || len(revs) != 1 || revs[0].Branch != "feature" || revs[0].Kind != domain.ReviewOnBranch {
		t.Fatalf("reviews after the run = %+v (%v)", revs, err)
	}

	runGit(t, dir, "checkout", "-")
	if code, _, errb := runCLI(t, dir, "branch", "delete", "--force", "feature"); code != 0 {
		t.Fatalf("branch delete: exit=%d stderr=%s", code, errb)
	}
	if revs, _ := domain.Open(dir).Reviews(ctx); len(revs) != 0 {
		t.Fatalf("the review outlived its branch: %+v", revs)
	}
}

// A structured review prints as its overview, then one line per note —
// never as the JSON document.
func TestReviewPrintsTheStructuredReview(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	runGit(t, dir, "commit", "--allow-empty", "-m", "second")
	writeReviewTool(t, dir, "Echo",
		`printf '{"version":1,"summary":"## Overview\\nall good","meta":{"verdict":"approve"},"files":[{"path":"f.go","annotations":[{"newRange":[3,4],"summary":"S","meta":{"severity":"bug"}},{"oldRange":[7,7],"summary":"gone"}]}]}' > "$GG_MESSAGE_FILE"`)
	code, out, errb := runCLI(t, dir, "review", "--tool", "Echo", "HEAD")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	want := "## Overview\nall good\n\nverdict: approve\n\nf.go:3-4 — S (severity: bug)\nf.go:-7 — gone\n"
	if out != want {
		t.Fatalf("stdout:\n%q\nwant\n%q", out, want)
	}
	if !strings.Contains(errb, "note: ") || strings.Contains(errb, "warning") {
		t.Fatalf("stderr = %q", errb)
	}
}

// A prose review prints as it came, with a warning that it is not the
// document.
func TestReviewWarnsAboutAProseReview(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	runGit(t, dir, "commit", "--allow-empty", "-m", "second")
	writeReviewTool(t, dir, "Echo", `printf 'The advisor confirms four findings.\n'`)
	code, out, errb := runCLI(t, dir, "review", "--tool", "Echo", "HEAD")
	if code != 0 || out != "The advisor confirms four findings.\n" {
		t.Fatalf("exit=%d stdout=%q", code, out)
	}
	if !strings.Contains(errb, "warning: the review is not in gg review format; stored as text") {
		t.Fatalf("stderr = %q", errb)
	}
}

// A stored review listed by `gg note list` names itself as a review of its
// commit — never as a line note on "new:1-1" gone stale.
func TestNoteListLabelsAReview(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	runGit(t, dir, "commit", "--allow-empty", "-m", "second")
	writeReviewTool(t, dir, "Echo", `printf "FAKE REVIEW\n"`)
	if code, _, errb := runCLI(t, dir, "review", "--tool", "Echo", "HEAD~1..HEAD"); code != 0 {
		t.Fatalf("review exit=%d stderr=%s", code, errb)
	}
	code, out, errb := runCLI(t, dir, "note", "list")
	if code != 0 {
		t.Fatalf("note list exit=%d stderr=%s", code, errb)
	}
	head := strings.TrimSpace(runGit(t, dir, "rev-parse", "--short=7", "HEAD"))
	if !strings.Contains(out, "] review "+head) {
		t.Errorf("note list = %q, want the row to say \"review %s\"", out, head)
	}
	for _, bad := range []string{"new:1-1", "stale"} {
		if strings.Contains(out, bad) {
			t.Errorf("note list = %q, must not show %q for a review", out, bad)
		}
	}
}

// A branch review's row names the commit and the branch with no stray colon.
func TestRenderNoteLineBranchReview(t *testing.T) {
	n := model.Note{ID: "d8665f79", Source: model.NoteSourceAgent, Tags: []string{model.ReviewTag},
		Address: model.FileAddress{State: model.StateCommitted, Commit: "49cd78100000000000000000000000000000000", Branch: "feature"},
		Summary: "Review: feature"}
	var b strings.Builder
	renderNoteLine(&b, domain.ResolvedNote{Note: n}, false, "")
	if got, want := b.String(), "d8665f79 [agent] review 49cd781 (feature)  Review: feature\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A working review's notes on a file are read-only and not the store's:
// `note clear --type` passes them by instead of failing on them.
func TestNoteClearByTypeSkipsAWorkingReviewsNotes(t *testing.T) {
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
		`printf '{"version":1,"summary":"R","files":[{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"shouty"}]}]}' > "$GG_MESSAGE_FILE"`)
	if code, _, errb := runCLI(t, dir, "review", "--tool", "Echo", "--working"); code != 0 {
		t.Fatalf("review exit=%d stderr=%s", code, errb)
	}
	code, out, errb := runCLI(t, dir, "note", "clear", "--file", "a.txt", "--type", "agent", "--yes")
	if code != 0 || !strings.Contains(out, "removed 0 notes") {
		t.Fatalf("exit=%d out=%q stderr=%q, want 0 and nothing removed", code, out, errb)
	}
	if _, list, _ := runCLI(t, dir, "note", "list", "--file", "a.txt"); !strings.Contains(list, "shouty") {
		t.Fatalf("the review's note must still show: %q", list)
	}
}
