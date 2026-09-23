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

// A reader must see the block's extent (hk-top / hk-bot cap its frame), which
// CELL is taken (the ✓ and the tint sit on that side's cell, never across both
// panes), and what a click would take (hovering outlines the whole run).
func TestAPickedBlockIsMarkedAsAUnit(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	css := readStatic(t, "style.css")

	if !strings.Contains(files, "function hunkRunEdges(items)") {
		t.Fatal("files.js: the paint must know where each hunk's run begins and ends")
	}
	if !strings.Contains(files, "const hkAt = kctx ? hunkRunEdges(items) : null;") {
		t.Fatal("files.js: the edges must come from the rows actually painted (the fold can hide a hunk's first row)")
	}
	cls := jsFunc(t, "files.js", "hunkCls")
	for _, want := range []string{"hk-top", "hk-bot", "picked", "pick-l", "pick-w"} {
		if !strings.Contains(cls, want) {
			t.Fatalf("files.js: hunkCls must emit %q:\n%s", want, cls)
		}
	}
	for _, want := range []string{
		"tr.hk.hk-top td { border-top: 1px solid var(--border); }",
		"tr.hk.hk-bot td { border-bottom: 1px solid var(--border); }",
		"tr.hk.pick-l td.side.l, tr.hk.pick-w td.side.r { background: var(--sel); }",
		`tr.hk.pick-l td.no.l::before, tr.hk.pick-w td.no.r::before { content: "✓";`,
		"tr.hk.hk-hover td {",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("style.css: missing the block's own mark: %s", want)
		}
	}
	if !strings.Contains(files, "function paintHunkHover(key)") || !strings.Contains(files, "function hunkRunKey(tr)") {
		t.Fatal("files.js: hovering one row must outline the whole run (no CSS selector can)")
	}
}

// The right-click menu is where the click-to-pick mechanic is written down —
// and the one-shot way to stage the block under the pointer.
func TestDiffMenuOffersBlockStaging(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	for _, want := range []string{
		"this ${side === \"index\" ? \"index\" : \"working\"} line`",
		`label: "take this block's working side",`,
		`label: "take this block's index side",`,
		`label: "clear this block",`,
		`label: "stage this block",`,
		"`stage selected (${n} line",
	} {
		if !strings.Contains(files, want) {
			t.Fatalf("files.js: the diff's context menu lacks %s", want)
		}
	}
	// it must act on the row's OWN file, like every other per-row gesture
	if !strings.Contains(files, "const hk = hkRow ? hunkSlotAt(hkRow) ||") {
		t.Fatal("files.js: the menu must resolve the clicked row's own slot")
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
