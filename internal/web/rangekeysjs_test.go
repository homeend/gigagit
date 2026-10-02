package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The keyboard's range marking, run under node: one shift+↓ / shift+↑ moves
// the band's end away from the mark (the diff's stepRange) or from the cursor
// (the viewer's viewerStep), which stays put.
const rangeKeysHarness = `
import { stepRange, viewerStep } from "./rk.mjs";
const rows = [
  { left_no: 1, right_no: 1 },
  { left_no: 2, right_no: 2 },
  { left_no: 3, right_no: 0 },
  { left_no: 0, right_no: 3 },
  { left_no: 4, right_no: 4 },
  { left_no: 5, right_no: 5 },
];
const hunks = [{ left_no: 1, right_no: 1 }, { left_no: 2, right_no: 2 }, { left_no: 40, right_no: 41 }];
const at = (side, no) => ({ side, no });
const rg = (side, first, last) => ({ side, first, last });
console.log(JSON.stringify([
  stepRange(rows, at("new", 2), null, 1),
  stepRange(rows, at("new", 2), rg("new", 2, 4), 1),
  stepRange(rows, at("new", 2), rg("new", 2, 4), -1),
  stepRange(rows, at("new", 2), rg("new", 2, 3), -1),
  stepRange(rows, at("new", 2), null, -1),
  stepRange(rows, at("new", 4), rg("new", 2, 4), -1),
  stepRange(rows, at("new", 5), null, 1),
  stepRange(rows, at("old", 2), null, 1),
  stepRange(rows, at("new", 2), rg("old", 2, 3), 1),
  stepRange(rows, null, null, 1),
  stepRange(hunks, at("new", 2), null, 1),
  viewerStep(null, 5, 10, 1),
  viewerStep({ start: 5, end: 6 }, 5, 10, 1),
  viewerStep({ start: 5, end: 7 }, 5, 10, -1),
  viewerStep({ start: 5, end: 6 }, 5, 10, -1),
  viewerStep(null, 5, 10, -1),
  viewerStep({ start: 3, end: 5 }, 5, 10, -1),
  viewerStep(null, 10, 10, 1),
  viewerStep(null, 0, 10, 1),
  viewerStep({ start: 3, end: 7 }, 5, 10, 1),
]));
`

func TestRangeKeySteps(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	mod := jsFunc(t, "files.js", "stepRange") + "\n" +
		jsFunc(t, "viewer.js", "viewerRange") + "\n" +
		jsFunc(t, "viewer.js", "viewerStep") + "\n" +
		"export { stepRange, viewerStep };\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rk.mjs"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(rangeKeysHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := `[` +
		// no band: it starts at the mark
		`{"side":"new","first":2,"last":3,"end":3},` +
		// grows at the end away from the mark…
		`{"side":"new","first":2,"last":5,"end":5},` +
		// …and shrinks there
		`{"side":"new","first":2,"last":3,"end":3},` +
		// back to the one marked line
		`{"side":"new","first":2,"last":2,"end":2},` +
		// upward from the mark
		`{"side":"new","first":1,"last":2,"end":1},` +
		// the mark is the band's LAST line: the first one moves
		`{"side":"new","first":1,"last":4,"end":1},` +
		// the file's last line: nowhere to go
		`null,` +
		// the old side walks its own numbers
		`{"side":"old","first":2,"last":3,"end":3},` +
		// the band's side wins over a mark on the other side
		`{"side":"old","first":2,"last":4,"end":4},` +
		// nothing marked
		`null,` +
		// the next line the rows hold (a defensive case: a diff's rows hold
		// every line of both files, so a real gap does not occur)
		`{"side":"new","first":2,"last":41,"end":41},` +
		// the viewer: from the cursor, which stays
		`{"range":{"start":5,"end":6},"end":6},` +
		`{"range":{"start":5,"end":7},"end":7},` +
		`{"range":{"start":5,"end":6},"end":6},` +
		`{"range":null,"end":5},` +
		`{"range":{"start":4,"end":5},"end":4},` +
		// the cursor is the band's last line: its first one moves
		`{"range":{"start":2,"end":5},"end":2},` +
		`null,null,` +
		// the cursor walked off the band's ends: marking starts over from it
		`{"range":{"start":5,"end":6},"end":6}` +
		`]`
	if got := strings.TrimSpace(string(out)); got != want {
		t.Errorf("range key steps:\n got %s\nwant %s", got, want)
	}
}

// The wiring the helpers cannot see.
func TestRangeKeysWiring(t *testing.T) {
	t.Parallel()
	keys := readStatic(t, "keys.js")
	files := readStatic(t, "files.js")
	viewer := readStatic(t, "viewer.js")
	// Before the arrows scroll the diff (diffScrollKey would take them).
	rk, scroll := strings.Index(keys, "if (rangeKey(e)) return;"), strings.Index(keys, "if (diffScrollKey(e)) return;")
	if rk < 0 || scroll < rk {
		t.Error("keys.js: rangeKey must run before diffScrollKey")
	}
	for _, c := range []struct{ src, pin, why string }{
		{files, `if (e.shiftKey && (e.key === "ArrowDown" || e.key === "ArrowUp")) {`, "shift+↓/↑ mark in the diff"},
		{files, `const got = row ? diffRowLink(row, e.target.closest("td")) : null;`, "the menu and L share one link builder"},
		{files, `const got = row ? diffRowLink(row, row.querySelector(side === "old" ? "td.no.l" : "td.no.r")) : null;`, "L reads the mark's own side"},
		{viewer, "stepViewerRange(e.key === \"ArrowDown\" ? 1 : -1);", "shift+↓/↑ mark in the viewer"},
		{viewer, "const here = viewerLinkHere();", "the viewer's menu and L share one link builder"},
		{files, "const s = st.slots.find((o) => o.range) || st.slots[st.anchor];", "the keys start in c's file: the band's, else the cursor's"},
		{files, "if (mark.side === \"old\" && ctx.preview && !ctx.preview.pair) {", "a preview starts on the new side"},
		{files, "are not all in this diff: no link names them", "L says so for a partly held range"},
		{files, "|| !!(rngRows && rngRows.has(r)) || marked(r);", "the marked row is never folded away"},
	} {
		if !strings.Contains(c.src, c.pin) {
			t.Errorf("%s: lost %q", c.why, c.pin)
		}
	}
	// The viewer's shift branch comes before its plain ↓/↑ cursor keys.
	sh, plain := strings.Index(viewer, "stepViewerRange(e.key"), strings.Index(viewer, `case "ArrowDown": case "j": moveCursor(1); break;`)
	if sh < 0 || plain < sh {
		t.Error("viewer.js: shift+↓/↑ must be taken before the cursor keys")
	}
}

// The band's class (shared by the render and the in-place step) and the
// scroll that keeps the moving end out from under the sticky headers, run
// under node.
const bandSightHarness = `
import { bandCls, sightScroll, bandOnly } from "./bs.mjs";
console.log(JSON.stringify([
  bandOnly(true, "same"), bandOnly(true, "del"), bandOnly(true, "add"), bandOnly(false, "del"),
  bandCls(true, "new", ""),
  bandCls(true, "old", ""),
  bandCls(false, "new", ""),
  bandCls(true, "new", "old"),
  bandCls(true, "old", "old"),
  bandCls(true, "new", "new"),
  sightScroll(100, 120, 65, 500),
  sightScroll(50, 70, 65, 500),
  sightScroll(36, 56, 94, 500),
  sightScroll(490, 510, 65, 500),
  sightScroll(100, 700, 65, 500),
]));
`

func TestBandClassAndSight(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	mod := jsFunc(t, "files.js", "bandCls") + "\n" + jsFunc(t, "files.js", "sightScroll") + "\n" +
		jsFunc(t, "files.js", "bandOnly") + "\n" +
		"export { bandCls, sightScroll, bandOnly };\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bs.mjs"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(bandSightHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := `[` +
		// unified: a context row shows both sides, a del row the old, an add
		// row the new; every row of the other layouts shows both
		`"","old","new","",` +
		// a band row wears its side's class; a row outside wears none
		`"rng-r","rng-l","",` +
		// a unified one-side row: only when it shows the band's side
		`"","rng-l","rng-r",` +
		// already in sight: no scroll
		`0,` +
		// under #diff-top (bottom 65): up by what it hides
		`-15,` +
		// under a stack file's header too (bottom 94)
		`-58,` +
		// under the bottom bars: down
		`10,` +
		// taller than the gap: its top shows, it is not pushed above it
		`35` +
		`]`
	if got := strings.TrimSpace(string(out)); got != want {
		t.Errorf("band class / sight:\n got %s\nwant %s", got, want)
	}
}

// The cheap step's wiring: a keyboard step bands in place, leaves a repaint
// to the next frame, and keeps the end clear of the sticky headers.
func TestRangeKeysCheapStepWiring(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	viewer := readStatic(t, "viewer.js")
	for _, c := range []struct{ src, pin, why string }{
		{files, "setDiffRange(any, { side: next.side, first: next.first, last: next.last }, true);", "a keyboard step may leave its repaint to the next frame"},
		{files, "if (bandPendingHere() || !paintBandInPlace($(\"diff-body\"), (state.lastDiff || {}).rows, prev, r)) {", "the single diff bands in place first"},
		{files, "if (got.has(i) && !got.get(i).every((t) => rowShows(t, rows[i]))) return false;", "no in-place band on a table drawn from other rows"},
		{files, "return rows.length > 0 || pending;", "esc takes a band whose repaint is still pending (and nothing else)"},
		{files, "if (bandPending) bandPending.end = end;", "the end is shown after a pending repaint"},
		{files, "pane.scrollTop += sightScroll(r.top, r.bottom, from, to);", "the end is kept clear of the sticky headers"},
		{files, "data-cols=\"${cols}\"${notesOn ? \" data-band\" : \"\"}", "the table says how it was drawn"},
		{viewer, "if (!paintViewerBand(prev, view.range)) rerenderKeepingScroll();", "the viewer bands in place first"},
	} {
		if !strings.Contains(c.src, c.pin) {
			t.Errorf("%s: lost %q", c.why, c.pin)
		}
	}
	if strings.Contains(files, `if (end) end.scrollIntoView({ block: "nearest" });`) {
		t.Error("files.js: the keyboard step must not scrollIntoView (it parks the end under the sticky headers)")
	}
}

// The deferred band repaint's bookkeeping, run under node with the page's
// repaints stubbed: a repaint is for the diff it was made on — the reader
// switching files or opening the stack drops it, and its end is shown only
// there; a repaint that a newer one superseded does nothing.
const bandLaterHarness = `
import { bandPendingHere, flushBandRepaint, setPending, getPending, state, calls } from "./bl.mjs";
const out = [];
const A = { rows: [] }, B = { rows: [] }, S1 = { slots: [] }, S2 = { slots: [] };
// 1. made on diff A, still on A: kept, flushed, its end shown
state.lastDiff = A; state.stack = null;
let p = { single: true, slots: new Set(), diff: A, stack: null, end: { no: 5 } };
setPending(p);
out.push(bandPendingHere() === p);
flushBandRepaint(p);
out.push(calls.splice(0).join(","), getPending());
// 2. the reader switched to diff B meanwhile: dropped; its flush does nothing
p = { single: true, slots: new Set(), diff: A, stack: null, end: { no: 5 } };
setPending(p);
state.lastDiff = B;
out.push(bandPendingHere());
flushBandRepaint(p);
out.push(calls.splice(0).join(","));
// 3. still pending when the file changes: the flush neither repaints nor scrolls
p = { single: true, slots: new Set(), diff: A, stack: null, end: { no: 5 } };
setPending(p);
flushBandRepaint(p);
out.push(calls.splice(0).join(","));
// 4. superseded by a newer repaint: the old timer does nothing
state.lastDiff = A;
const old = { single: true, slots: new Set(), diff: A, stack: null, end: null };
const cur = { single: true, slots: new Set(), diff: A, stack: null, end: null };
setPending(cur);
flushBandRepaint(old);
out.push(calls.splice(0).join(","), getPending() === cur);
// 5. made in stack S1, now S2: dropped; made on the single diff, now a stack: dropped
state.stack = S2;
setPending({ single: false, slots: new Set(["s"]), diff: A, stack: S1, end: null });
out.push(bandPendingHere());
setPending({ single: true, slots: new Set(), diff: A, stack: null, end: null });
out.push(bandPendingHere());
// 6. a stack's own: kept, flushed with its slots and end
p = { single: false, slots: new Set(["s"]), diff: A, stack: S2, end: { k: 1 } };
setPending(p);
out.push(bandPendingHere() === p);
flushBandRepaint(p);
out.push(calls.splice(0).join(","));
console.log(JSON.stringify(out));
`

func TestBandRepaintLaterBookkeeping(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	mod := "export const state = { lastDiff: null, stack: null };\n" +
		"export const calls = [];\n" +
		"const rerenderDiffKeepingPlace = () => calls.push(\"rerender\");\n" +
		"const repaintStackSlots = (s) => calls.push(\"slots:\" + s.join(\"+\"));\n" +
		"const showBandEnd = () => calls.push(\"end\");\n" +
		"let bandPending = null;\nconst bandPaint = { cost: 0, end: 0 };\n" +
		"export const setPending = (p) => (bandPending = p);\nexport const getPending = () => bandPending;\n" +
		jsFunc(t, "files.js", "bandPendingHere") + "\n" + jsFunc(t, "files.js", "flushBandRepaint") + "\n" +
		"export { bandPendingHere, flushBandRepaint };\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bl.mjs"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(bandLaterHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := `[true,"rerender,end",null,null,"","","",true,null,null,true,"slots:s,end"]`
	if got := strings.TrimSpace(string(out)); got != want {
		t.Errorf("band repaint bookkeeping:\n got %s\nwant %s", got, want)
	}
}
