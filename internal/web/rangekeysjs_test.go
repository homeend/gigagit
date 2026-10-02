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
