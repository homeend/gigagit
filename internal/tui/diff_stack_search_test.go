package tui

import (
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
