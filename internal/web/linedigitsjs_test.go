package web

import (
	"regexp"
	"strings"
	"testing"
)

const digitsPureStart = "// --- line-number gutters (pure; guarded against Go) ---"
const digitsPureEnd = "// --- end line-number gutters ---"

// A gutter is as wide as the widest line number its view shows (the TUI's
// gutterWidth): the digit count of the largest number, over both sides.
func TestLineDigitsJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "core.js", digitsPureStart, digitsPureEnd, `
const r = [];
r.push(lineDigits(0), lineDigits(9), lineDigits(10), lineDigits(9999), lineDigits(10455), lineDigits(123456), lineDigits(-3), lineDigits(NaN));
r.push(rowsDigits([], "left_no", "right_no"), rowsDigits(null, "left_no", "right_no"));
r.push(rowsDigits([{left_no: 9998, right_no: 10023}, {left_no: 9999}, {fold: 4}], "left_no", "right_no"));
r.push(rowsDigits([{left_no: 100000, right_no: 0}, {right_no: 7}], "left_no", "right_no"));
r.push(rowsDigits([{old: 12, nu: 0}, {old: 0, nu: 1234}], "old", "nu"));
console.log(r.join("|"));
`)
	if want := "1|1|2|4|5|6|1|1|1|1|5|6|4"; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// The number columns size from --no-digits, never from a fixed width that
// fits four digits (a 10455 wrapped as "1045" over "5", every row doubled).
func TestLineNumberColumnsFollowTheirDigits(t *testing.T) {
	t.Parallel()
	css := readStatic(t, "style.css")
	for _, gone := range []*regexp.Regexp{
		regexp.MustCompile(`td\.no \{ width: 3\.5em`),
		regexp.MustCompile(`col\.no \{ width: 3\.5em`),
		regexp.MustCompile(`\.vno \{[^}]*width: 4em`),
		regexp.MustCompile(`\.bno \{[^}]*width: 4em`),
	} {
		if gone.MatchString(css) {
			t.Errorf("style.css still has a fixed gutter: %s", gone)
		}
	}
	for _, want := range []string{
		"td.no { width: var(--diff-no);",
		"table.diff col.no { width: var(--diff-no); }",
		"--diff-no: max(3.5em, calc(var(--no-digits, 0) * 1ch + 13px));",
		"width: max(4em, calc(var(--no-digits, 0) * 1ch + 11px));",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("style.css lacks %q", want)
		}
	}
	// --diff-no is the gutters' own rule now; a measured root value would
	// be stale the moment a view widened its gutter.
	if strings.Contains(readStatic(t, "core.js"), `setProperty("--diff-no"`) {
		t.Error("core.js still measures --diff-no")
	}
}

// Every view that paints line numbers tells its host how many digits it
// needs; a stack the widest over its files, so their columns line up.
func TestEveryGutterHostSetsItsDigits(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ file, want, why string }{
		{"files.js", `setNoDigits($("diff-pane"), rowsDigits(d.rows, "left_no", "right_no"));`, "the single diff (and the compare / conflict views it hosts)"},
		{"stackview.js", "function stackDigits(", "the stack: the widest over its loaded files"},
		{"stackview.js", "setNoDigits($(\"diff-pane\"), stackDigits(st))", "the stack sets it on the pane its bars share"},
		{"filehist.js", `setNoDigits(host, rowsDigits(d.rows, "left_no", "right_no"));`, "the file-history overlay's diff"},
		{"filehist.js", "setNoDigits(host, lineDigits(lines[lines.length - 1].line));", "blame"},
		{"viewer.js", "setNoDigits(viewerEl(), lineDigits(lines.length));", "the viewer's file"},
		{"viewer.js", `setNoDigits(viewerEl(), rowsDigits(rows, "old", "nu"));`, "the viewer's diff"},
		{"viewer.js", "setNoDigits(viewerEl(), lineDigits(Math.max(0, ...st.slots.map((s) => (s.lines || []).length))));", "the viewer's stack of files: the longest file's"},
		{"viewer.js", "stackGutter(st); // a file arriving may be the longest", "a file arriving in the viewer's stack"},
		{"wtfinder.js", "setNoDigits(body, lineDigits(lines.length));", "the F finder's preview"},
	} {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}
