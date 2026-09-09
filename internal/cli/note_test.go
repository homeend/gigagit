package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noteRepo is a real repo with a committed file, a working-tree edit and an
// untracked file — the three default target states of §4.5.
// It sets XDG_STATE_HOME, so it must NOT be used from a t.Parallel() test.
func noteRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	body := "alpha\nbravo\ncharlie\ndelta\n"
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "seed")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\nBRAVO\ncharlie\ndelta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fresh.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestNoteAddByNewLinePrintsID(t *testing.T) {
	dir := noteRepo(t)
	code, out, errb := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "2", "--summary", "shouty")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	id := strings.TrimSpace(out)
	if len(id) != 8 {
		t.Fatalf("stdout = %q, want the new 8-hex note id on one line", out)
	}
}

// A line past the end of the file is refused, not silently clamped:
// domain.NoteAdd's bounds check (Task 7's controller ruling) must fire for
// this CLI caller exactly as it does for the batch importer.
func TestNoteAddNewLinePastEndOfFileFails(t *testing.T) {
	dir := noteRepo(t)
	code, _, errb := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "100", "--summary", "too far")
	if code != 1 {
		t.Fatalf("exit=%d stderr=%s, want 1", code, errb)
	}
	if !strings.Contains(errb, "past the end") {
		t.Fatalf("stderr = %q, want a message naming the out-of-range anchor", errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--file", "a.txt")
	if strings.TrimSpace(list) != "" {
		t.Fatalf("a refused add must store nothing, got:\n%s", list)
	}
}

func TestNoteAddJSONCarriesWireShape(t *testing.T) {
	dir := noteRepo(t)
	code, out, errb := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "2",
		"--summary", "shouty", "--rationale", "why", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	var got struct {
		ID        string `json:"id"`
		Source    string `json:"source"`
		Author    string `json:"author"`
		Path      string `json:"path"`
		Side      string `json:"side"`
		Line      int    `json:"line"`
		Range     [2]int `json:"range"`
		Summary   string `json:"summary"`
		Rationale string `json:"rationale"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not a wire note: %v\n%s", err, out)
	}
	if got.Source != "agent" || got.Author != "agent" {
		t.Errorf("CLI notes default to source/author agent: %+v", got)
	}
	if got.Path != "a.txt" || got.Side != "new" || got.Range != [2]int{2, 2} || got.Line != 2 {
		t.Errorf("anchor = %+v, want a.txt new 2-2", got)
	}
	if got.Summary != "shouty" || got.Rationale != "why" {
		t.Errorf("text = %+v", got)
	}
}

func TestNoteAddSourceUserAndAgentEnvAuthor(t *testing.T) {
	dir := noteRepo(t)
	t.Setenv("GG_AGENT", "sonnet")
	code, out, errb := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1",
		"--summary", "s", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, `"author":"sonnet"`) {
		t.Fatalf("$GG_AGENT must become the default author: %s", out)
	}
	code, out, errb = runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "3",
		"--summary", "human", "--source", "user", "--author", "ada", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, `"source":"user"`) || !strings.Contains(out, `"author":"ada"`) {
		t.Fatalf("explicit --source/--author must win: %s", out)
	}
}

func TestNoteAddByHunkAnchorsTheWholeHunk(t *testing.T) {
	dir := noteRepo(t)
	code, out, errb := runCLI(t, dir, "note", "add", "--file", "a.txt", "--hunk", "1", "--summary", "hunk note", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	var got struct {
		Side  string `json:"side"`
		Range [2]int `json:"range"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Side != "new" || got.Range[0] < 1 || got.Range[1] < got.Range[0] {
		t.Fatalf("hunk anchor = %+v, want the hunk's whole new-side span", got)
	}
}

func TestNoteAddHunkPastEndIsExit1(t *testing.T) {
	dir := noteRepo(t)
	code, _, errb := runCLI(t, dir, "note", "add", "--file", "a.txt", "--hunk", "9", "--summary", "s")
	if code != 1 {
		t.Fatalf("exit=%d stderr=%s, want 1", code, errb)
	}
	if !strings.Contains(errb, "has 1 hunks") {
		t.Fatalf("stderr = %q, want it to name the file's hunk count", errb)
	}
}

func TestNoteAddUntrackedFileIsUntrackedState(t *testing.T) {
	dir := noteRepo(t)
	code, _, errb := runCLI(t, dir, "note", "add", "--file", "fresh.txt", "--new-line", "1", "--summary", "s")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s (an untracked file has no index side; the state must absorb that)", code, errb)
	}
}

func TestNoteAddRevStoresFullSHA(t *testing.T) {
	dir := noteRepo(t)
	sha := runGit(t, dir, "rev-parse", "HEAD")
	code, out, errb := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1",
		"--rev", sha[:7], "--summary", "commit note", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, `"rev":"`+sha+`"`) {
		t.Fatalf("a commit note must store the FULL sha: %s", out)
	}
}

func TestNoteAddUsageErrors(t *testing.T) {
	dir := noteRepo(t)
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"range rev", []string{"note", "add", "--file", "a.txt", "--new-line", "1", "--rev", "main..HEAD", "--summary", "s"}, "one commit"},
		{"cached with rev", []string{"note", "add", "--file", "a.txt", "--new-line", "1", "--cached", "--rev", "HEAD", "--summary", "s"}, "mutually exclusive"},
		{"no target", []string{"note", "add", "--file", "a.txt", "--summary", "s"}, "exactly one of"},
		{"two targets", []string{"note", "add", "--file", "a.txt", "--new-line", "1", "--old-line", "1", "--summary", "s"}, "exactly one of"},
		{"no summary", []string{"note", "add", "--file", "a.txt", "--new-line", "1"}, "--summary"},
		{"bad source", []string{"note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "s", "--source", "robot"}, "user or agent"},
	} {
		code, _, errb := runCLI(t, dir, c.args...)
		if code != 2 {
			t.Errorf("%s: exit=%d stderr=%s, want 2", c.name, code, errb)
			continue
		}
		if !strings.Contains(errb, c.want) {
			t.Errorf("%s: stderr = %q, want it to contain %q", c.name, errb, c.want)
		}
	}
}

func TestNoteReplyInheritsAnchorAndRmRemovesThread(t *testing.T) {
	dir := noteRepo(t)
	_, out, _ := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "2", "--summary", "root")
	root := strings.TrimSpace(out)

	code, out, errb := runCLI(t, dir, "note", "reply", root, "--summary", "addressed", "--json")
	if code != 0 {
		t.Fatalf("reply exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, `"parent_id":"`+root+`"`) || !strings.Contains(out, `"range":[2,2]`) {
		t.Fatalf("a reply must inherit the parent's anchor: %s", out)
	}

	if code, _, errb = runCLI(t, dir, "note", "rm", root); code != 0 {
		t.Fatalf("rm exit=%d stderr=%s", code, errb)
	}
	if code, _, errb = runCLI(t, dir, "note", "rm", root); code != 1 {
		t.Fatalf("removing a gone note = %d stderr=%s, want exit 1", code, errb)
	}
}

func TestNoteUnknownSubcommandIsUsage(t *testing.T) {
	dir := noteRepo(t)
	code, _, errb := runCLI(t, dir, "note", "frobnicate")
	if code != 2 || !strings.Contains(errb, "unknown subcommand") {
		t.Fatalf("exit=%d stderr=%q, want 2 + an unknown-subcommand message", code, errb)
	}
}

func TestNoteIsAKnownCommand(t *testing.T) {
	t.Parallel()
	if !IsCommand("note") {
		t.Error(`"note" must be in the commands map (cmd/gg routing, help, gg batch)`)
	}
}

func TestNoteListTextFormat(t *testing.T) {
	dir := noteRepo(t)
	_, out, _ := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "2", "--summary", "shouty")
	root := strings.TrimSpace(out)
	if code, _, errb := runCLI(t, dir, "note", "reply", root, "--summary", "addressed", "--source", "user"); code != 0 {
		t.Fatalf("reply: %s", errb)
	}

	code, out, errb := runCLI(t, dir, "note", "list", "--file", "a.txt")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want a root line and one indented reply:\n%s", out)
	}
	for _, want := range []string{root, "[agent]", "a.txt", "new:2-2", "active", "shouty"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("root line %q missing %q", lines[0], want)
		}
	}
	if !strings.HasPrefix(lines[1], "  ") || !strings.Contains(lines[1], "[user] reply") ||
		!strings.Contains(lines[1], "addressed") {
		t.Errorf("reply line = %q, want two-space indent + [user] reply + the text", lines[1])
	}
}

func TestNoteListJSONAndTypeFilter(t *testing.T) {
	dir := noteRepo(t)
	runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "by agent")
	runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "3", "--summary", "by human", "--source", "user")

	code, out, errb := runCLI(t, dir, "note", "list", "--file", "a.txt", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	var all []struct {
		Source  string `json:"source"`
		Status  string `json:"status"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(out), &all); err != nil {
		t.Fatalf("--json must be an array of wire notes: %v\n%s", err, out)
	}
	if len(all) != 2 {
		t.Fatalf("json = %+v, want both notes", all)
	}
	for _, n := range all {
		if n.Status == "" {
			t.Errorf("every wire note carries a resolution status: %+v", n)
		}
	}

	_, out, _ = runCLI(t, dir, "note", "list", "--file", "a.txt", "--type", "user")
	if strings.Contains(out, "by agent") || !strings.Contains(out, "by human") {
		t.Fatalf("--type user must keep only user notes:\n%s", out)
	}
	_, out, _ = runCLI(t, dir, "note", "list", "--file", "a.txt", "--type", "agent")
	if !strings.Contains(out, "by agent") || strings.Contains(out, "by human") {
		t.Fatalf("--type agent must keep only agent notes:\n%s", out)
	}
	if code, _, errb := runCLI(t, dir, "note", "list", "--file", "a.txt", "--type", "robot"); code != 2 {
		t.Fatalf("exit=%d stderr=%s, want 2 for a bad --type", code, errb)
	}
}

// Without --file, list enumerates every address this checkout can see.
func TestNoteListWithoutFileCoversEveryTarget(t *testing.T) {
	dir := noteRepo(t)
	sha := runGit(t, dir, "rev-parse", "HEAD")
	runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "2", "--summary", "worktree note")
	runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--rev", sha, "--summary", "commit note")

	code, out, errb := runCLI(t, dir, "note", "list")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "worktree note") || !strings.Contains(out, "commit note") {
		t.Fatalf("bare list must show worktree AND commit notes:\n%s", out)
	}
}

// TestNoteListCachedRevWithoutFileIsUsage guards against --cached/--rev being
// silently ignored when --file is absent: a bare `note list --cached` (or
// --rev) has no single target to resolve them against, so it must fail
// loudly rather than fall back to "every address" as a bare `note list`
// would.
func TestNoteListCachedRevWithoutFileIsUsage(t *testing.T) {
	dir := noteRepo(t)
	for _, args := range [][]string{
		{"note", "list", "--cached"},
		{"note", "list", "--rev", "HEAD"},
	} {
		code, _, errb := runCLI(t, dir, args...)
		if code != 2 {
			t.Fatalf("%v: exit=%d stderr=%s, want exit 2", args, code, errb)
		}
		if !strings.Contains(errb, "note list: --cached/--rev need --file <path>") {
			t.Fatalf("%v: stderr=%q, want the --cached/--rev usage message", args, errb)
		}
	}
}

// TestNoteClearCachedRevWithoutFileIsUsage mirrors the list guard for clear.
func TestNoteClearCachedRevWithoutFileIsUsage(t *testing.T) {
	dir := noteRepo(t)
	for _, args := range [][]string{
		{"note", "clear", "--all", "--yes", "--cached"},
		{"note", "clear", "--all", "--yes", "--rev", "HEAD"},
	} {
		code, _, errb := runCLI(t, dir, args...)
		if code != 2 {
			t.Fatalf("%v: exit=%d stderr=%s, want exit 2", args, code, errb)
		}
		if !strings.Contains(errb, "note clear: --cached/--rev need --file <path>") {
			t.Fatalf("%v: stderr=%q, want the --cached/--rev usage message", args, errb)
		}
	}
}

func TestNoteClearGuardsAndCount(t *testing.T) {
	dir := noteRepo(t)
	runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "one")
	_, out, _ := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "2", "--summary", "two")
	root := strings.TrimSpace(out)
	runCLI(t, dir, "note", "reply", root, "--summary", "r")

	for _, c := range []struct {
		name string
		args []string
	}{
		{"no --yes", []string{"note", "clear", "--all"}},
		{"neither file nor all", []string{"note", "clear", "--yes"}},
		{"both file and all", []string{"note", "clear", "--all", "--file", "a.txt", "--yes"}},
	} {
		if code, _, errb := runCLI(t, dir, c.args...); code != 2 {
			t.Errorf("%s: exit=%d stderr=%s, want 2", c.name, code, errb)
		}
	}

	// A bad --type must be reported even without --yes: the type guard runs
	// before the --yes guard.
	if code, _, errb := runCLI(t, dir, "note", "clear", "--all", "--type", "robot"); code != 2 || !strings.Contains(errb, "--type") {
		t.Fatalf("clear --all --type robot: exit=%d stderr=%q, want exit 2 and a --type error", code, errb)
	}

	code, out, errb := runCLI(t, dir, "note", "clear", "--all", "--yes")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "removed 3 notes") {
		t.Fatalf("stdout = %q, want the removed RECORD count (two roots + one reply)", out)
	}
	_, out, _ = runCLI(t, dir, "note", "list")
	if strings.TrimSpace(out) != "" {
		t.Fatalf("clear --all must empty the store: %q", out)
	}
}

// A note whose anchor file no longer exists is orphaned (NotesAt — and so
// `list` — would show nothing for it). `clear` must still find and count it:
// the --type all path removes through NotesClear directly, never consulting
// the orphan-hiding NotesAt read.
//
// This deliberately does NOT run `note list` in between: every `gg note`
// subcommand's own withNotesHousekeeping starts a bounded startup sweep
// AFTER it runs, and that general sweep also drops orphaned notes on its own.
// Calling `list` first would race that sweep and could remove the note
// before `clear` ever sees it, defeating the point of this test — which is
// that `clear` itself, not a lucky sweep, is what counts an orphaned note
// correctly.
func TestNoteClearCountsOrphanedNotes(t *testing.T) {
	dir := noteRepo(t)
	runCLI(t, dir, "note", "add", "--file", "fresh.txt", "--new-line", "1", "--summary", "orphan me")
	if err := os.Remove(filepath.Join(dir, "fresh.txt")); err != nil {
		t.Fatal(err)
	}
	code, out, errb := runCLI(t, dir, "note", "clear", "--file", "fresh.txt", "--yes")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "removed 1 notes") {
		t.Fatalf("stdout = %q, want the orphaned note cleared and counted", out)
	}
}

// --type user / --type agent on clear narrows to matching ROOTS only: a
// matched root takes its own replies with it (they count too), and threads of
// the other type are untouched.
func TestNoteClearTypeNarrowing(t *testing.T) {
	dir := noteRepo(t)
	_, out, _ := runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "agent note")
	agentRoot := strings.TrimSpace(out)
	if code, _, errb := runCLI(t, dir, "note", "reply", agentRoot, "--summary", "agent reply"); code != 0 {
		t.Fatalf("reply: exit=%d stderr=%s", code, errb)
	}
	runCLI(t, dir, "note", "add", "--file", "a.txt", "--new-line", "2", "--summary", "user note", "--source", "user")

	code, out, errb := runCLI(t, dir, "note", "clear", "--all", "--type", "agent", "--yes")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "removed 2 notes") {
		t.Fatalf("stdout = %q, want the agent root + its reply counted", out)
	}
	_, out, _ = runCLI(t, dir, "note", "list")
	if strings.Contains(out, "agent note") || !strings.Contains(out, "user note") {
		t.Fatalf("clear --type agent must remove only the agent thread, leaving the user thread listable:\n%s", out)
	}

	code, out, errb = runCLI(t, dir, "note", "clear", "--all", "--type", "user", "--yes")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "removed 1 notes") {
		t.Fatalf("stdout = %q, want the user root counted (no replies)", out)
	}
	_, out, _ = runCLI(t, dir, "note", "list")
	if strings.TrimSpace(out) != "" {
		t.Fatalf("both threads should now be gone: %q", out)
	}
}
