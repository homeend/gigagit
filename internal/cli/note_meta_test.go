package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runGG is runCLI with the outputs first.
func runGG(t *testing.T, dir string, args ...string) (string, string, int) {
	t.Helper()
	code, out, errb := runCLI(t, dir, args...)
	return out, errb, code
}

func TestNoteAddWithMetaAndDisplay(t *testing.T) {
	dir := noteRepo(t) // a repo with a.txt modified (unstaged), as note_test.go builds
	out, errOut, code := runGG(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "s",
		"--meta", "kind=issue", "--meta", "severity=high",
		"--display", `{"kind":{"show":["badge"],"values":{"issue":{"colour":"red"}}}}`, "--json")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "colour") {
		t.Fatalf("the unknown field must be named on stderr: %q", errOut)
	}
	var w struct {
		Meta    []struct{ Key, Value string } `json:"meta"`
		Display struct {
			Badges []struct{ Key, Value string } `json:"badges"`
		} `json:"display"`
	}
	if err := json.Unmarshal([]byte(out), &w); err != nil {
		t.Fatal(err)
	}
	if len(w.Meta) != 2 || len(w.Display.Badges) != 1 {
		t.Fatalf("wire = %s", out)
	}
}

func TestNoteAddBadMetaIsAUsageError(t *testing.T) {
	dir := noteRepo(t)
	for _, bad := range []string{"kind", "=x", "intent=" + strings.Repeat("v", 300)} {
		_, errOut, code := runGG(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "s", "--meta", bad)
		if code != 2 {
			t.Fatalf("--meta %q: code %d (%s)", bad, code, errOut)
		}
	}
	_, _, code := runGG(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "s", "--display", `{"kind":"x"}`)
	if code != 2 {
		t.Fatalf("malformed display: code %d", code)
	}
}

func TestNoteMetaSetUnsetDisplay(t *testing.T) {
	dir := noteRepo(t)
	id, _, _ := runGG(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "s", "--author", "A")
	id = strings.TrimSpace(id)
	if _, e, c := runGG(t, dir, "note", "meta", id, "set", "status=disagreed", "--author", "MR"); c != 0 {
		t.Fatalf("set: %d %s", c, e)
	}
	show, _, _ := runGG(t, dir, "note", "show", id)
	if !strings.Contains(show, "status: disagreed (MR)") {
		t.Fatalf("show = %q", show)
	}
	df := filepath.Join(t.TempDir(), "d.json")
	os.WriteFile(df, []byte(`{"status":{"show":["line"]}}`), 0o600)
	if _, e, c := runGG(t, dir, "note", "meta", id, "display", "@"+df, "--author", "A"); c != 0 {
		t.Fatalf("display: %d %s", c, e)
	}
	if _, e, c := runGG(t, dir, "note", "meta", id, "unset", "status", "--author", "MR"); c != 0 {
		t.Fatalf("unset: %d %s", c, e)
	}
	if show, _, _ := runGG(t, dir, "note", "show", id); strings.Contains(show, "disagreed") {
		t.Fatalf("unset left %q", show)
	}
}

func TestNoteListPrintsAMetaLine(t *testing.T) {
	dir := noteRepo(t)
	runGG(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "s", "--meta", "kind=issue")
	out, _, _ := runGG(t, dir, "note", "list", "--file", "a.txt")
	if !strings.Contains(out, "\n    meta: kind: issue\n") {
		t.Fatalf("list = %q", out)
	}
}

func TestNoteShowListsAFrameOnlyKey(t *testing.T) {
	dir := noteRepo(t)
	id, _, _ := runGG(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "s",
		"--meta", "severity=high", "--display", `{"severity":{"show":["frame"]}}`)
	show, _, _ := runGG(t, dir, "note", "show", strings.TrimSpace(id))
	if !strings.Contains(show, "severity: high") {
		t.Fatalf("show = %q", show)
	}
}

func TestReviewSaveNamesDisplayProblems(t *testing.T) {
	dir, link := reviewSaveRepo(t)
	doc := `{"version":1,"summary":"fine","display":{"kind":{"show":["badge"],"colour":"x"}},
	  "files":[{"path":"f.txt","annotations":[{"newRange":[2,2],"summary":"one"}]}]}`
	code, _, errb := runCLIStdin(t, dir, doc, "review", "save", link, "--agent", "A", "--stdin")
	if code != 0 || !strings.Contains(errb, "colour") {
		t.Fatalf("save = %d %q", code, errb)
	}
}
