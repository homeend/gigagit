package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Plan 4c, web half: hunk staging inside a stack. The module-level diffHunks
// was exactly the "current file" global that notes (4a) and search (4b) had
// already had to lose — a stack paints one file's table while another file's
// picks are live, so the picks a table paints must arrive as a parameter and
// the bar must act on ONE file: the one being read.
func TestHunkContextIsExplicit(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")

	if !strings.Contains(files, "function hunkCls(r, kctx)") || !strings.Contains(files, "function hunkAttr(r, kctx)") {
		t.Fatal("files.js: hunkCls/hunkAttr must take the hunk context, not read diffHunks")
	}
	// Their bodies may not reach for the global at all.
	for _, fn := range []string{"hunkCls", "hunkAttr"} {
		if body := jsFunc(t, "files.js", fn); strings.Contains(body, "diffHunks") {
			t.Fatalf("files.js: %s still reads the module-level diffHunks:\n%s", fn, body)
		}
	}
	if !strings.Contains(files, "function diffHTML(d, paneWidth, notesOn = false, open = state.diffFolds, nctx = null, hctx = null, kctx = null)") {
		t.Fatal("files.js: diffHTML must take the hunk context as its own parameter")
	}
	// The single-file view keeps working by passing its own globals in.
	if !strings.Contains(files, "diffHunks ? { picks: diffHunks.picks } : null") {
		t.Fatal("files.js: renderDiff must hand the single-file picks to diffHTML")
	}
}

// Each slot arms its own staging state from its own diff, and a re-fetch drops
// its picks: they are POSITIONAL against the bytes the server hashed.
func TestSlotCarriesItsOwnHunks(t *testing.T) {
	t.Parallel()
	view := readStatic(t, "stackview.js")
	stack := readStatic(t, "stack.js")

	if !strings.Contains(stack, "hunks: null,") {
		t.Fatal("stack.js: buildSlots must give every slot its own hunks hook")
	}
	if !strings.Contains(jsFunc(t, "stack.js", "reconcileSlots"), "o.hunksPrev = o.hunks;") {
		t.Fatal("stack.js: a kept slot re-fetches, so its picks must be parked for the re-arm to judge")
	}
	if !strings.Contains(view, "s.hunksPrev.hash === s.hunks.hash") {
		t.Fatal("stackview.js: picks survive a re-fetch only while the freshness hash is unchanged")
	}
	if !strings.Contains(view, "s.hunks = d.hunks && hunkEligible(s.f)") {
		t.Fatal("stackview.js: a slot must arm its staging state from its own diff's tags")
	}
	if !strings.Contains(view, "const kctx = s.hunks ? { picks: s.hunks.picks } : null;") {
		t.Fatal("stackview.js: a slot must paint with its OWN picks")
	}
	if !strings.Contains(view, "function hunkSlotAt(el)") || !strings.Contains(view, "function activeSlotHunks()") {
		t.Fatal("stackview.js: the click and the bar need the two slot accessors")
	}
}

// The bar is a one-file gesture (D1) that follows the file just picked in (D2),
// and a staging round inside a stack reconciles in place rather than re-opening
// the file as a single diff (D3).
func TestHunkBarActsOnTheActiveFile(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")

	if !strings.Contains(files, "function activeHunks()") {
		t.Fatal("files.js: the bar needs one accessor for whose picks it acts on")
	}
	for _, fn := range []string{"renderHunkBar", "stageHunksPicked"} {
		if !strings.Contains(jsFunc(t, "files.js", fn), "activeHunks()") {
			t.Fatalf("files.js: %s must go through activeHunks(), not the global", fn)
		}
	}
	if !strings.Contains(files, "const scope = hunkSlotAt(tr);") {
		t.Fatal("files.js: a click must toggle the picks of the row's OWN slot")
	}
	if !strings.Contains(files, "if (scope) pickOnSlot(scope.k);") {
		t.Fatal("files.js: picking in a file must make it the one the bar stages")
	}
	// The anchor is derived from SCROLL, so it cannot be what the bar stages:
	// a pick followed by any scroll would stage the file at the pane top.
	if !strings.Contains(jsFunc(t, "stackview.js", "activeSlotHunks"), "picked.hunks.picks.size") {
		t.Fatal("stackview.js: a slot with live picks must own the bar, not the scroll anchor")
	}
	// …and the listener may no longer bail out on the single-file global, which
	// is null in a stack (the same dead gate 4a and 4b each had to remove).
	if strings.Contains(files, `$("diff-body").addEventListener("click", (e) => {
  if (!diffHunks) return;`) {
		t.Fatal("files.js: the click listener still returns early on diffHunks — dead in a stack")
	}
	if !strings.Contains(jsFunc(t, "files.js", "paintHunkPicks"), `(scope && scope.el) || $("diff-body")`) {
		t.Fatal("files.js: a repaint must be scoped to one file — other slots hold their own picks")
	}
	if !strings.Contains(jsFunc(t, "files.js", "stageHunksPicked"), "if (!state.stack) reopenAfterHunkStage(") {
		t.Fatal("files.js: in a stack the status re-read reconciles in place; re-opening would tear the stack down")
	}
	if !strings.Contains(files, "if (!state.stack && diffHunks && !state.statusEntries.some(") {
		t.Fatal("files.js: the gone-file guard is the single-file lane's; a stack's slots answer for themselves")
	}
}

// The picks a table paints really are the slot's own: two slots, different
// picks, and only the matching rows carry the class. Node-imported so the
// assertion sees the rendered HTML, not the source.
const hunkPaintHarness = `
import * as H from "./hunks.mjs";
const rows = [
  { kind: "change", left: "a", right: "A", left_no: 1, right_no: 1, hunk: 0 },
  { kind: "same", left: "b", right: "b", left_no: 2, right_no: 2 },
  { kind: "change", left: "c", right: "C", left_no: 3, right_no: 3, hunk: 1 },
];
const out = {};
out.picked0 = rows.map((r) => H.hunkCls(r, { picks: new Set([0]) })).join("|");
out.picked1 = rows.map((r) => H.hunkCls(r, { picks: new Set([1]) })).join("|");
out.none = rows.map((r) => H.hunkCls(r, null)).join("|");
out.attr = rows.map((r) => H.hunkAttr(r, { picks: new Set() })).join("|");
out.attrNone = rows.map((r) => H.hunkAttr(r, null)).join("|");
console.log(JSON.stringify(out));
`

func TestHunkClassesFollowTheirOwnPicks(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src := readStatic(t, "files.js")
	// The two helpers are pure and self-contained: lift them out so the guard
	// runs without files.js's DOM imports.
	mod := jsFunc(t, "files.js", "hunkCls") + "\n" + jsFunc(t, "files.js", "hunkAttr") + "\nexport { hunkCls, hunkAttr };\n"
	if !strings.Contains(src, "function hunkCls(r, kctx)") {
		t.Fatal("files.js: hunkCls must take the context")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hunks.mjs"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(hunkPaintHarness), 0o644); err != nil {
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
		"picked0":  " hk picked|| hk",
		"picked1":  " hk|| hk picked",
		"none":     "||",
		"attr":     ` data-hunk="0"|| data-hunk="1"`,
		"attrNone": "||",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
}
