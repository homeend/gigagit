package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The @ highlight's ctrl+↑/↓ (the TUI's jump prev/next match): from the
// cursor's feed row to the next / previous match, wrapping at the ends; a
// cursor between matches steps to the neighbour in that direction.
func TestHighlightStepJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	mod := jsFunc(t, "commits.js", "highlightStep") + "\nexport { highlightStep };\n"
	harness := `
import { highlightStep } from "./h.mjs";
const m = [2, 5, 9];
console.log(JSON.stringify([
  highlightStep(m, 2, 1), highlightStep(m, 9, 1), highlightStep(m, 2, -1),
  highlightStep(m, 3, 1), highlightStep(m, 3, -1), highlightStep([], 3, 1),
]));
`
	dir := t.TempDir()
	for name, body := range map[string]string{"h.mjs": mod, "run.mjs": harness} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got []int
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := []int{5, 2, 9, 5, 2, -1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("steps = %v, want %v", got, want)
		}
	}
}

// The @ highlight is wired: the key, the dimmed rows, the steps, the deep
// search and recall share its query.
func TestHighlightWired(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ file, want, why string }{
		{"keys.js", "openCommitHighlight()", "@ opens the highlight on the commit list"},
		{"commits.js", `state.chl && !state.chl.set.has(row.hash) ? " dim" : ""`, "non-matching rows dim, every row stays"},
		{"commits.js", "stepHighlight(e.key === \"ArrowDown\" ? 1 : -1)", "ctrl+↑/↓ step between matches"},
		{"commits.js", "(state.chl && state.chl.q)", "ctrl+f digs deeper with the highlight's query"},
		{"style.css", ".crow.dim {", "dimmed rows are styled"},
	} {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}

// The conflict picker searches like the diff (the TUI's / @ ] [ in its hunk
// picker): the key gate lets it in, and the diff search host paints the
// picker's own rows while it is up.
func TestConflictPickerSearchWired(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	for _, c := range []struct{ want, why string }{
		{`(!state.lastDiff && !state.stack && !conflictPick)`, "/ @ ] [ reach the search while the picker is up"},
		{"conflictPick ? cfSearchHere() :", "the host asks the picker where the reader is"},
		{"conflictPick ? cfSearchRender() :", "…re-finds over the picker's rows"},
		{"conflictPick ? cfSearchGoTo(i) :", "…and scrolls to a hit in the picker"},
		{`querySelectorAll("#cf-doc pre.cf-text, #cf-doc .cf-ln")`, "the picker's rows are its text blocks and side lines"},
	} {
		if !strings.Contains(files, c.want) {
			t.Errorf("files.js lacks %q: %s", c.want, c.why)
		}
	}
	if !strings.Contains(readStatic(t, "helpsearch.js"), "export { paintRow") {
		t.Error("helpsearch.js does not share its hit painter")
	}
}
