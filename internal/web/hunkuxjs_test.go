package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The commit message is the thing being written, so it takes the sidebar's
// whole width and its buttons sit under it; ⤢ swaps the everyday three lines
// for a tall box and the choice is remembered server-side (a random port makes
// browser storage useless).
func TestCommitBoxIsAColumnWithAGrowControl(t *testing.T) {
	t.Parallel()
	html := readStatic(t, "index.html")
	css := readStatic(t, "style.css")
	ops := readStatic(t, "ops.js")

	if !strings.Contains(html, `<div id="commit-actions">`) {
		t.Fatal("index.html: the commit buttons must live in their own row under the message")
	}
	if !strings.Contains(html, `id="commit-grow"`) {
		t.Fatal("index.html: the message box needs its grow control")
	}
	// the textarea must come BEFORE the actions row in the markup
	if strings.Index(html, `id="commit-msg"`) > strings.Index(html, `<div id="commit-actions">`) {
		t.Fatal("index.html: the message must be above its buttons")
	}
	if !strings.Contains(css, "#commit-box { padding: 4px 8px; border-bottom: 1px solid var(--border); display: flex; flex-direction: column;") {
		t.Fatal("style.css: the commit box must stack its rows, not sit them side by side")
	}
	if !strings.Contains(css, "#commit-box textarea {\n  width: 100%;") {
		t.Fatal("style.css: the message must take the whole column")
	}
	if !strings.Contains(ops, "function applyCommitRows()") || !strings.Contains(ops, "saveUI({ commit_tall: tall })") {
		t.Fatal("ops.js: the grow control must apply and remember the size")
	}
	if !strings.Contains(readStatic(t, "app.js"), "applyCommitRows();") {
		t.Fatal("app.js: a stored size must be applied at startup")
	}
}

// The marks follow the unit: the whole row is selected or not, the hunk's
// frame shows what "Stage hunk" takes, and nothing marks a single cell.
func TestSelectionMarksTheWholeRow(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	css := readStatic(t, "style.css")

	if !strings.Contains(files, "const hkAt = kctx ? hunkRunEdges(items) : null;") {
		t.Fatal("files.js: the hunk's frame must come from the rows actually painted")
	}
	for _, want := range []string{
		"tr.hk.hk-top td { border-top: 1px solid var(--border); }",
		"tr.hk.hk-bot td { border-bottom: 1px solid var(--border); }",
		"tr.hk.hk-sel td {",
		"tr.hk:hover td {",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("style.css: missing %s", want)
		}
	}
	// the earlier rounds' per-cell marks and checkmarks must be gone, all of
	// them — one round's rules once survived next to the next round's
	for _, gone := range []string{"pick-l", "pick-w", "tr.hk.picked", "#hunk-bar", `content: "✓`} {
		if strings.Contains(css, gone) {
			t.Fatalf("style.css: a stale rule survives: %s", gone)
		}
	}
}

// hunkRunEdges is pure: one pass, the painted list, contiguous runs.
const hunkEdgesHarness = `
import * as E from "./edges.mjs";
const R = (h) => ({ hunk: h });
const rows = [R(null), R(0), R(0), R(null), R(1), R(null), R(2), R(2), R(2)];
const { first, last } = E.hunkRunEdges(rows);
const out = {};
out.firsts = rows.map((r, i) => (first.has(r) ? i + ":" + first.get(r) : "")).filter(Boolean).join(",");
out.lasts = rows.map((r, i) => (last.has(r) ? i + ":" + last.get(r) : "")).filter(Boolean).join(",");
// a single-row hunk opens AND closes its own run
const one = [R(null), R(7), R(null)];
const e1 = E.hunkRunEdges(one);
out.single = (e1.first.get(one[1]) === 7) + "/" + (e1.last.get(one[1]) === 7);
// no hunks at all: empty maps, never a crash
const none = E.hunkRunEdges([R(null), R(null)]);
out.none = none.first.size + "/" + none.last.size;
console.log(JSON.stringify(out));
`

func TestHunkRunEdgesJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	mod := jsFunc(t, "files.js", "hunkRunEdges") + "\nexport { hunkRunEdges };\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "edges.mjs"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(hunkEdgesHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &got); err != nil {
		t.Fatalf("not the harness JSON: %v\n%s", err, out)
	}
	want := map[string]string{
		"firsts": "1:0,4:1,6:2",
		"lasts":  "2:0,4:1,8:2",
		"single": "true/true",
		"none":   "0/0",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
}
