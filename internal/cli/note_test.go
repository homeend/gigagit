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
