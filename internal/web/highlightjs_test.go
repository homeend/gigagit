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
		{"commits.js", "while (state.layout !== \"list\" && guard++ < 3) drillOut(); // @ is the commit list's", "@ works on the commit list, as the TUI's Commits panel key"},
		{"commits.js", "highlightStep(matches, cur - 1, 1)", "typing snaps the cursor to the nearest match at or after it"},
	} {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}

// ctrl+↑/↓ over a kept highlight must be routed before the plain ↓ / ↑
// cursor keys, or they only move the cursor one row.
func TestHighlightStepRoutedBeforeArrows(t *testing.T) {
	t.Parallel()
	keys := readStatic(t, "keys.js")
	step := strings.Index(keys, "state.chl && state.pane === \"commits\" && (e.ctrlKey || e.metaKey)")
	arrow := strings.Index(keys, `if (e.key === "j" || e.key === "ArrowDown") {`)
	if step < 0 || arrow < 0 {
		t.Fatalf("keys.js lacks the highlight step (%d) or the arrow branch (%d)", step, arrow)
	}
	if step > arrow {
		t.Errorf("the highlight's ctrl+↑/↓ is routed after the plain arrows: it never fires")
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
		{`querySelectorAll("#cf-doc pre.cf-text, #cf-doc .cf-ln .cf-lt")`, "the picker's rows are its text blocks and side lines — the line text, never its [ ] tick"},
		{`<span class="cf-lt">${cfLineHTML(ln)}</span>`, "a side line's text is its own span"},
		{"function closeConflictPick() {\n  conflictPick = null;\n  diffSearchBar.reset();", "a query typed in the picker leaves with it"},
		{`else { diffSearchBar.reset(); setDiffTitle(""); diffBodyEl().innerHTML = "";`, "…and with the last resolved conflict"},
	} {
		if !strings.Contains(files, c.want) {
			t.Errorf("files.js lacks %q: %s", c.want, c.why)
		}
	}
	if !strings.Contains(readStatic(t, "docsearch.js"), "export function paintRow(") {
		t.Error("docsearch.js does not share its hit painter")
	}
}

// Search recall walks the ring like the TUI's dropdown: alt+↓ opens it at
// the newest phrase and goes older, alt+↑ goes newer and, above the newest,
// gives back what was being typed (-1); alt+↑ with nothing recalled is a no-op.
func TestRecallStepJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	mod := jsFunc(t, "searchhist.js", "recallStep") + "\nexport { recallStep };\n"
	harness := `
import { recallStep } from "./h.mjs";
console.log(JSON.stringify([
  recallStep(-1, 3, "ArrowDown"), recallStep(0, 3, "ArrowDown"), recallStep(2, 3, "ArrowDown"),
  recallStep(1, 3, "ArrowUp"), recallStep(0, 3, "ArrowUp"), recallStep(-1, 3, "ArrowUp"),
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
	if got, want := strings.TrimSpace(string(out)), "[0,1,2,0,-1,-1]"; got != want {
		t.Fatalf("recall steps = %s, want %s", got, want)
	}
}

// A walk belongs to one typing session: every field starts it afresh when
// it opens (and warms the rings so the first alt+↓ is not a round trip).
func TestRecallResetOnOpen(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ file, fn, end string }{
		{"searchbar.js", "const open = (backward) => {", "\n  };"},
		{"commits.js", "function openCommitFilter() {", "\n}"},
		{"commits.js", "function openCommitHighlight() {", "\n}"},
		{"wtfinder.js", "function openFinder() {", "\n}"},
	} {
		src := readStatic(t, c.file)
		i := strings.Index(src, c.fn)
		if i < 0 {
			t.Fatalf("%s lacks %q", c.file, c.fn)
		}
		end := strings.Index(src[i:], c.end)
		if end < 0 || !strings.Contains(src[i:i+end], "recallOpen(") {
			t.Errorf("%s %s does not start a fresh recall walk", c.file, c.fn)
		}
	}
}

// Every key the branch adds is in the ? help.
func TestSearchParityHelp(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ file, want string }{
		{"index.html", `<span class="hkey">@</span>`},
		{"index.html", "ctrl+↑ / ctrl+↓"},
		{"index.html", "<b>↓</b> / <b>↑</b> step through the links you copied"},
		{"searchhist.js", "alt+↓ · alt+↑ · search history"},
		{"filehist.js", "file history · older commits"},
		{"searchbar.js", "in the conflict picker"},
	} {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s: the ? help lacks %q", c.file, c.want)
		}
	}
}

// The # prompt's link list: a late fetch never lands in a prompt typed on or
// reopened since, ↑ on the first row gives back what was typed (the TUI's ↑
// returns to the field), and the "nothing recent" row is not a pick.
func TestPromptHistoryGuards(t *testing.T) {
	t.Parallel()
	src := readStatic(t, "layers.js")
	for _, c := range []struct{ want, why string }{
		{"if (gen !== promptHistGen) return; // typed on, or another prompt opened", "a late fetch is dropped"},
		{"if (h.sel === 0 && d < 0) {", "↑ on the first row leaves the list"},
		{"promptField().value = h.draft;", "…restoring what was typed"},
		{`if (s.classList.contains("none")) return;`, "the empty-history row is not a pick"},
	} {
		if !strings.Contains(src, c.want) {
			t.Errorf("layers.js lacks %q: %s", c.want, c.why)
		}
	}
}

// A loaded page of file history moves the cursor only when it is still on
// the last row (the TUI's advanceAt), and a failed load says so.
func TestFileHistoryPageGuards(t *testing.T) {
	t.Parallel()
	src := readStatic(t, "filehist.js")
	for _, want := range []string{
		"if (h.rows.length > first && h.sel === first - 1) openHistoryDiff(first);",
		`opLine("older history: " + (e.message || e), true);`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("filehist.js lacks %q", want)
		}
	}
}
