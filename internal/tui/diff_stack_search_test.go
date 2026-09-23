package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/textdiff"
)

// searchRows builds n Same rows whose right side is plain text, with the
// listed rows carrying the word "needle" — the thing a stacked search looks
// for. Left and right differ so the row is searched on the right only (the
// Same rule) exactly as a real diff's identical rows are.
func searchRows(n int, needleAt ...int) []textdiff.Row {
	rows := make([]textdiff.Row, n)
	for i := range rows {
		rows[i] = textdiff.Row{Kind: textdiff.Same, Left: "plain line", Right: "plain line", LeftNo: i + 1, RightNo: i + 1}
	}
	for _, c := range needleAt {
		rows[c] = textdiff.Row{Kind: textdiff.Same, Left: "a needle here", Right: "a needle here", LeftNo: c + 1, RightNo: c + 1}
	}
	return rows
}

// A stack is ONE line stream, so the in-view search already spans every file
// whose diff has arrived and is unfolded: searchLines() walks v.lines and
// skips only the lines that are not bodies (headers, rules, placeholders).
// This is the baseline plan 4b rests on — it passes before any of 4b is
// written, and the rest of the plan is about the files that are NOT in the
// stream (folded, or never fetched).
func TestStackSearchAlreadySpansLoadedFiles(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, searchRows(8, 2), searchRows(8), searchRows(8, 5))
	v.search.query = "needle"
	v.search.refindFrom(v.searchLines(), v.searchPos())

	files := map[int]bool{}
	for _, h := range v.search.hits {
		files[v.lines[h.row].file] = true
	}
	if !files[0] || !files[2] {
		t.Fatalf("a stacked search must find hits in the first AND the last file, got files %v (%d hits)", files, len(v.search.hits))
	}
	if files[1] {
		t.Fatalf("file 1 carries no needle, yet a hit was attributed to it: %v", files)
	}
}

// A file arriving ABOVE the reader lengthens the stream and shifts every line
// index below it. applyStackFile rebuilds the stream, then remaps the cursor
// through a stack anchor — so the search must be re-found AFTER the remap. Run
// inside rebuild() it measures from a line index the new stream no longer
// means, and the current hit jumps to another file's hit.
func TestStackSearchKeepsTheCurrentHitWhenAFileLoadsAbove(t *testing.T) {
	t.Parallel()
	// Dense hits: a stale line index must snap to a DIFFERENT hit, or the test
	// cannot see the bug (two far-apart hits re-snap to the same one anyway).
	v := stackViewOf(t, nil, searchRows(9, 1, 3, 5, 7), searchRows(9, 1, 3, 5, 7)) // file 0 not fetched
	m := diffModel()
	m.height, m.width = 24, 120
	m = m.pushLayer(v)
	body := m.diffBodyRows()
	v.search.query = "needle"
	v.search.refindFrom(v.searchLines(), v.searchPos())
	if len(v.search.hits) != 8 {
		t.Fatalf("fixture: want four hits in each loaded file, got %d", len(v.search.hits))
	}
	v.goToHit(len(v.search.hits)-1, body) // the hit in the LAST file
	wantFile := v.lines[v.search.hits[v.search.cur].row].file
	wantNo := v.lines[v.curLine].Row.RightNo

	u, _ := m.Update(stackFileMsg{gen: v.stk.gen, idx: 0, view: diffViewWith(searchRows(9), nil)})
	mm := u.(Model)
	nv := mm.diffLayer()

	if nv.lines[nv.curLine].Row.RightNo != wantNo || nv.curFile() != wantFile {
		t.Fatalf("the cursor left file %d line %d for file %d line %d", wantFile, wantNo, nv.curFile(), nv.lines[nv.curLine].Row.RightNo)
	}
	h := nv.search.hits[nv.search.cur]
	if nv.lines[h.row].file != wantFile {
		t.Fatalf("the current hit moved to file %d, want %d", nv.lines[h.row].file, wantFile)
	}
	if h.row != nv.curLine {
		t.Fatalf("the current hit (line %d) parted from the cursor (line %d)", h.row, nv.curLine)
	}
}

// D1: the count is over the whole stack, and a trailing + says the stack has
// files it has not searched yet — folded, or never fetched.
func TestStackSearchBadgeMarksUnsearchedFiles(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, searchRows(8, 2), searchRows(8, 5), nil) // file 2 never fetched
	v.search.query = "needle"
	v.search.refindFrom(v.searchLines(), v.searchPos())
	if got := v.searchBadge(); !strings.HasSuffix(got, "+") {
		t.Fatalf("badge %q must end in + while a file is unsearched", got)
	}
	v.stk.files[2].collapsed = true
	v.stk.files[2].d, v.stk.files[2].load = diffViewWith(searchRows(8), nil), stackLoaded
	v.rebuild()
	if got := v.searchBadge(); !strings.HasSuffix(got, "+") {
		t.Fatalf("badge %q must end in + while a loaded file is still FOLDED", got)
	}
}

func TestStackSearchBadgeDropsThePlusWhenEverythingIsSearched(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, searchRows(8, 2), searchRows(8, 5), searchRows(8))
	v.search.query = "needle"
	v.search.refindFrom(v.searchLines(), v.searchPos())
	if got := v.searchBadge(); strings.HasSuffix(got, "+") {
		t.Fatalf("badge %q must not claim unsearched files when every file is loaded and open", got)
	}
}

// A file that can never hold a hit — conflicted, binary, too large, errored —
// does not hold the + open, or it would never clear and ] would cascade into a
// dead end.
func TestStackSearchBadgeIgnoresFilesThatCanNeverHoldAHit(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, searchRows(8, 2), searchRows(8, 5), nil)
	v.stk.files[2].conflict, v.stk.files[2].collapsed = true, true
	v.rebuild()
	v.search.query = "needle"
	v.search.refindFrom(v.searchLines(), v.searchPos())
	if got := v.searchBadge(); strings.HasSuffix(got, "+") {
		t.Fatalf("badge %q counted a conflicted file as unsearched", got)
	}
}

// Single-file the badge is untouched: no stack, no +.
func TestSearchBadgeUnchangedWithoutAStack(t *testing.T) {
	t.Parallel()
	v := diffViewWith(searchRows(8, 2), nil)
	v.search.query = "needle"
	v.search.refindFrom(v.searchLines(), v.searchPos())
	if got, want := v.searchBadge(), v.search.badge(); got != want {
		t.Fatalf("single-file badge %q must be the search's own %q", got, want)
	}
}
