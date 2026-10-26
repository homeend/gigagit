package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The blame search paints only the rows near the screen (the TUI paints only
// the rows it draws): rebuilding the whole body on every keystroke cost 2–4 s
// per key on a 13k-line file. These pin the pieces that keep a keystroke
// bounded by the screen, not by the file.

// findHits must match against a line's cached fold when the host supplies
// one: folding every line again on every keystroke is most of a re-find.
func TestFindHitsUsesCachedFoldJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	script := jsFunc(t, "inviewsearch.js", "foldRunes") + "\n" + jsFunc(t, "inviewsearch.js", "findHits") + `
// The fold deliberately disagrees with the text: a hit proves the cache is read.
const hits = findHits([{ row: 0, side: 0, text: "abc", fold: ["x", "y", "z"] }, { row: 1, side: 0, text: "XY" }], "xy");
console.log(JSON.stringify(hits.map((h) => [h.row, h.start, h.end])));
`
	out, err := exec.Command(node, "-e", script).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	if got, want := strings.TrimSpace(string(out)), "[[0,0,2],[1,0,2]]"; got != want {
		t.Fatalf("findHits = %s, want %s", got, want)
	}
}

// blameRowsToPaint picks, inside the window, the rows whose paint is stale
// (painted for an older query) AND that carry or carried hits; a row that was
// plain and stays plain is never redrawn.
func TestBlameRowsToPaintJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	script := jsFunc(t, "inviewsearch.js", "rowsToPaint") + `
const gen = 3;
const paintGen = [3, 2, 2, 2, 0, 2, 2, 1];       // row 0 is fresh
const painted = [true, true, false, false, false, true, false, true];
const hits = new Set([0, 2, 4, 6]);
const has = (i) => hits.has(i);
// Window 0..6: row 0 fresh, 1 carried hits, 2 has hits, 3 plain both times,
// 4 has hits, 5 carried hits, 6 has hits; row 7 is outside the window.
console.log(JSON.stringify(rowsToPaint(0, 6, gen, paintGen, painted, has)));
console.log(JSON.stringify(rowsToPaint(2, 3, gen, paintGen, painted, has)));
`
	out, err := exec.Command(node, "-e", script).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	if got, want := strings.TrimSpace(string(out)), "[1,2,4,5,6]\n[2]"; got != want {
		t.Fatalf("rowsToPaint =\n%s\nwant\n%s", got, want)
	}
}

// firstRowBelow finds the first row whose bottom reaches y by bisection —
// the reader's position and the paint window read a handful of rows, not
// every row above the screen.
func TestFirstRowBelowJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	script := jsFunc(t, "inviewsearch.js", "firstRowBelow") + `
const bottoms = [10, 20, 45, 50, 80];
let reads = 0;
const at = (i) => (reads++, bottoms[i]);
const out = [firstRowBelow(5, at, 0), firstRowBelow(5, at, 20), firstRowBelow(5, at, 21), firstRowBelow(5, at, 80), firstRowBelow(5, at, 81), firstRowBelow(0, at, 5)];
console.log(JSON.stringify(out));
const big = Array.from({ length: 13000 }, (_, i) => (i + 1) * 17);
reads = 0;
firstRowBelow(big.length, (i) => (reads++, big[i]), 17 * 9000);
console.log(reads <= 15);
`
	out, err := exec.Command(node, "-e", script).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	if got, want := strings.TrimSpace(string(out)), "[0,1,2,4,5,0]\ntrue"; got != want {
		t.Fatalf("firstRowBelow =\n%s\nwant\n%s", got, want)
	}
}

// A keystroke repaints the window; the full body build stays the open's.
func TestBlameSearchNeverRebuildsTheBody(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "filehist.js"))
	if err != nil {
		t.Fatal(err)
	}
	fh := string(b)
	i := strings.Index(fh, `bindSearchBar("blame-search"`)
	if i < 0 {
		t.Fatal("filehist.js: the blame host no longer mounts the shared bar")
	}
	j := strings.Index(fh[i:], "\n});\n")
	if j < 0 {
		t.Fatal("filehist.js: the blame bar's host literal has no end")
	}
	host := fh[i : i+j]
	if strings.Contains(host, "renderBlame(") {
		t.Error("filehist.js: the blame bar's render rebuilds the whole body on every keystroke; repaint the window instead")
	}
	for _, want := range []string{"repaintBlameSearch()", "firstRowBelow("} {
		if !strings.Contains(host, want) {
			t.Errorf("filehist.js: the blame bar's host must use %s", want)
		}
	}
	for _, want := range []string{
		`blameEl().addEventListener("scroll"`, // rows scrolled into view get their paint
		"fold: foldRunes(",                    // the fold is cached once per open file
	} {
		if !strings.Contains(fh, want) {
			t.Errorf("filehist.js: missing %q", want)
		}
	}
}
