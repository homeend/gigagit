package web

import (
	"strings"
	"testing"
)

const sdPureStart = "// --- split diff model (pure; guarded against Go) ---"
const sdPureEnd = "// --- end split diff model ---"

// Spec 2026-10-11-overview-split-diff §4–§7: /api/diff's aligned pairs read
// as one column (a run's deleted lines, then its added ones), every row ↔
// new line question, bands on deleted rows, boxes on both sides, the
// notices, the title and the same-content rule.
func TestSplitDiffModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "splitdiff.js", sdPureStart, sdPureEnd, `
const d = { rows: [
  { kind: "same", left: "a", right: "a", left_no: 1, right_no: 1 },
  { kind: "change", left: "b", right: "B", left_no: 2, right_no: 2, left_spans: [[0, 1]], right_spans: [[0, 1]] },
  { kind: "del", left: "c", right: "", left_no: 3, right_no: 0 },
  { kind: "same", left: "d", right: "d", left_no: 4, right_no: 3 },
  { kind: "add", left: "", right: "e", left_no: 0, right_no: 4 },
] };
const rows = unifiedRows(d), by = lineRows(rows);
const tail = unifiedRows({ rows: [{ kind: "same", left: "a", right: "a", left_no: 1, right_no: 1 }, { kind: "del", left: "z", left_no: 2, right_no: 0 }] });
const lead = unifiedRows({ rows: [{ kind: "del", left: "x", left_no: 1, right_no: 0 }, { kind: "same", left: "a", right: "a", left_no: 2, right_no: 1 }] });
const gone = unifiedRows({ rows: [{ kind: "del", left: "x", left_no: 1, right_no: 0 }, { kind: "del", left: "y", left_no: 2, right_no: 0 }] });
const bands = [{ start: 1, end: 2, i: 0 }, { start: 3, end: 3, i: 1 }];
const placed = placeNotes(rows, [{ id: "n1", start: 2, end: 3 }, { id: "r1", side: "old", start: 3, end: 3 }, { id: "r2", side: "old", start: 9, end: 9 }, { id: "r3", start: 1, end: 1 }]);
console.log([
  rows.map((r) => r.kind + ":" + r.old + ":" + r.nu + ":" + r.text).join(","),
  JSON.stringify(rows[1].spans) + JSON.stringify(rows[2].spans) + JSON.stringify(rows[3].spans),
  [by[1], by[2], by[3], by[4]].join(","),
  rowOfLine(rows, by, 2), rowOfLine(rows, by, 9), rowOfLine([], [], 1),
  rowNewLine(rows, 1), rowNewLine(rows, 2), rowNewLine(rows, 4), rowNewLine(tail, 1), rowNewLine(rows, 99),
  rows.map((r) => rowBand(rows, r.i, bands, 1)).join(","),
  rowBand(rows, 1, bands, 0), rowBand(lead, 0, [{ start: 1, end: 1, i: 0 }], 0) || "none",
  placed.map((p) => p.n.id + ":" + p.first + "-" + p.last).join(","),
  nextNoteRow(placed, 0, 1), nextNoteRow(placed, 3, 1), nextNoteRow(placed, 3, -1), nextNoteRow(placed, 0, -1),
  noteAtRow(placed, 4).id, noteAtRow(placed, 1),
  rowOfLine(gone, lineRows(gone), 0), rowNewLine(gone, 1), rowBand(gone, 0, bands, 0) || "none",
].join("|"));
console.log([
  diffSource({ stored: { id: "review-overview:abc" } }, "a b.txt"), diffSource({ id: "f3" }, "x.go"),
  diffSource(null, "x") || "none", diffSource({ stored: { id: "" } }, "x") || "none",
].join("|"));
console.log([
  diffNotice(d, 0, false) || "rows",
  diffNotice({ rows: [{ kind: "same" }] }, 0, true),
  diffNotice({ changed: false, rows: [] }, 0, false),
  diffNotice(null, 404, false), diffNotice(null, 500, true),
  diffNotice({ binary: true }, 0, false), diffNotice({ too_large: true }, 0, false),
].join("|"));
const h1 = diffHead({ left_label: "3a4d954", right_label: "774ace5", old_path: "old.txt" }, "new.txt", false), h2 = diffHead({}, "a.txt", true);
const s = (key, path, lines) => ({ key, path, lines });
console.log([
  h1.name, h1.range, h2.name, h2.range,
  sameContent(s("o", "a", ["x"]), s("o", "b", ["x"])), sameContent(s("o", "a", ["x"]), s("o", "a", ["x"])),
  sameContent(s("o", "a", ["x"]), s("p", "b", ["x"])), sameContent(s("o", "a", []), s("o", "b", [])),
  sameContent(s("o", "a", ["x"]), s("o", "b", ["y"])), sameContent(null, s("o", "b", ["x"])),
].join("|"));
`)
	want := "same:1:1:a,del:2:0:b,del:3:0:c,add:0:2:B,same:4:3:d,add:0:4:e|[[0,1]]null[[0,1]]|0,3,4,5|3|0|-1|2|2|3|1|0|other,other,other,other,cur,|cur|none|r3:0-0,r1:2-2,n1:3-4|2|-1|2|-1|n1||0|0|none\n" +
		"/api/diff?review=abc&path=a+b.txt|/api/diff?wt=head&path=x.go|none|none\n" +
		"rows|no changes in the working tree — D shows the file|no change to this file in the review — D shows the file|the review is gone — D shows the file|could not read the diff — D shows the file|binary file — D shows the file|diff too large — D shows the file\n" +
		"old.txt → new.txt|3a4d954 → 774ace5|a.txt|HEAD → working tree|true|false|false|false|false|false"
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// Spec §6 / §8: the viewer reads a diff for a file an overview's link
// opened when that overview's choice says so, before the first paint; D and
// the title's switch flip it; every close forgets the choice; a late read
// never lands on a newer file or a flipped-back choice.
func TestSplitDiffIsWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		`from "./splitdiff.js"`,
		`let diffPref = null;`,
		`function canFlip()`,
		`function diffWanted(from = view.from)`,
		`diffWanted(from) ? fetchDiff(from, reg.file.path) : null`,
		`view.diff = dres ? buildDiff(dres, from) : null;`,
		`if (!from) diffPref = null;`,
		`if (seq !== loadSeq || id !== view.id || view.from !== from || !diffWanted()) return;`,
		`} else if (view.diff) {`,
		`<div id="viewer-mode" class="hidden"><button data-mode="file">file</button><button data-mode="diff">diff</button></div>`,
		`case "D": if (!canFlip()) return false; flipDiff(); break;`,
		"diffPref = null;\n  lastStep = null;",
		`if (view.diff) refreshDiff();`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	if !strings.Contains(readStatic(t, "style.css"), "#viewer-mode.hidden { display: none; }") {
		t.Error("style.css: #viewer-mode.hidden has no rule (gg web hides by id)")
	}
	if !strings.Contains(readStatic(t, "files.js"), "collapseDiffRows,") {
		t.Error("files.js does not export collapseDiffRows")
	}
}

// Spec §5: in a diff the cursor walks every shown row, a click or a hit
// lands on its row, } / { step the boxes by row (old-side remarks too),
// d / r and the footer act on the box at the cursor's row, L and the menu
// refuse a deleted line, f folds, and the footer and help advertise D / f.
func TestSplitDiffKeysAreWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		`function onDeletedRow()`,
		`function noteAtCursor()`,
		`"a deleted line has no link in the reviewed version"`,
		`if (here && !(onDeletedRow() && !view.range)) items.push`,
		`case "f": if (!view.diff) return false; if (!view.diff.notice) toggleDiffFold(); break;`,
		`const line = view.diff ? 0 : nextNoteLine(shownNotes(), view.cur, dir);`,
		`nextNoteRow(placeNotes(dv.rows, diffNotes()), dv.row, dir)`,
		`view.diff.row = h.row;`,
		`const fold = e.target.closest(".vfold");`,
		`e.target.closest(".vline[data-i], .vline[data-r]")`,
		`case "dflip": flipDiff(); break;`,
		`case "dfold": if (view.diff && !view.diff.notice) toggleDiffFold(); break;`,
		`<b>D</b>`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	if n := strings.Count(v, "noteAtLine(shownNotes(), view.cur)"); n != 1 { // noteAtCursor's own file branch only
		t.Errorf("noteAtLine(shownNotes(), view.cur) appears %d times: every d / r path must read noteAtCursor()", n)
	}
}

// Spec §7: a link step whose file holds the previous step's very text, at
// another path, says so — after the open's own notices, which win.
func TestSameContentLineIsWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		`const step = { key: from.id, path: f.path, lines: view.placeholder || view.image ? [] : view.lines.map((l) => l.text || "") };`,
		`if (!landed.notice && !reg.evicted && sameContent(lastStep, step)) opLine("same content as " + elidePath(lastStep.path, 60), false);`,
		`lastStep = step;`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
}
