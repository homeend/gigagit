package tui

import (
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/textdiff"
)

func rowsDiff(rows ...textdiff.Row) domain.Diff {
	return domain.Diff{Result: textdiff.Result{Rows: rows}}
}

func TestUnifiedDiffRowsPutsARunsDeletionsFirst(t *testing.T) {
	t.Parallel()
	d := rowsDiff(
		textdiff.Row{Kind: textdiff.Same, Left: "a", Right: "a", LeftNo: 1, RightNo: 1},
		textdiff.Row{Kind: textdiff.Changed, Left: "b", Right: "B", LeftNo: 2, RightNo: 2, LeftSpans: []textdiff.Span{{Start: 0, End: 1}}, RightSpans: []textdiff.Span{{Start: 0, End: 1}}},
		textdiff.Row{Kind: textdiff.Del, Left: "c", LeftNo: 3},
		textdiff.Row{Kind: textdiff.Add, Right: "x", RightNo: 3},
		textdiff.Row{Kind: textdiff.Same, Left: "d", Right: "d", LeftNo: 4, RightNo: 4},
	)
	got := unifiedDiffRows(d)
	want := []struct {
		k       diffRowKind
		old, nu int
		text    string
	}{{diffSame, 1, 1, "a"}, {diffDel, 2, 0, "b"}, {diffDel, 3, 0, "c"}, {diffAdd, 0, 2, "B"}, {diffAdd, 0, 3, "x"}, {diffSame, 4, 4, "d"}}
	if len(got) != len(want) {
		t.Fatalf("rows %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if g := got[i]; g.kind != w.k || g.old != w.old || g.nu != w.nu || g.text != w.text || g.raw != w.text || g.at != i {
			t.Fatalf("row %d = %+v, want %+v", i, g, w)
		}
	}
	if got[1].words == nil || got[1].words[0] != emphWord || got[3].words == nil || got[2].words != nil {
		t.Fatal("a changed pair keeps its changed words on both rows; a plain deletion has none")
	}
}

func TestUnifiedDiffRowsExpandsTabsForDisplayOnly(t *testing.T) {
	t.Parallel()
	got := unifiedDiffRows(rowsDiff(textdiff.Row{Kind: textdiff.Add, Right: "\tx", RightNo: 1}))
	if got[0].text != "    x" || got[0].raw != "\tx" {
		t.Fatalf("text %q raw %q", got[0].text, got[0].raw)
	}
}

func TestRowMapsAndTheDeletedRowRule(t *testing.T) {
	t.Parallel()
	rows := unifiedDiffRows(rowsDiff(
		textdiff.Row{Kind: textdiff.Same, Left: "a", Right: "a", LeftNo: 1, RightNo: 1},
		textdiff.Row{Kind: textdiff.Del, Left: "b", LeftNo: 2},
		textdiff.Row{Kind: textdiff.Same, Left: "c", Right: "c", LeftNo: 3, RightNo: 2},
		textdiff.Row{Kind: textdiff.Del, Left: "z", LeftNo: 4},
	))
	if rowOfNew(rows, 2) != 2 || rowOfOld(rows, 2) != 1 || rowOfNew(rows, 9) != -1 || rowOfOld(rows, 9) != -1 {
		t.Fatal("line ↔ row maps")
	}
	if got := rowNewLine(rows, 1); got != 2 { // a deleted row: the next new line below
		t.Fatalf("deleted row → %d, want 2", got)
	}
	if got := rowNewLine(rows, 3); got != 2 { // at the end: the last one above
		t.Fatalf("last deleted row → %d, want 2", got)
	}
	if got := rowNewLine(unifiedDiffRows(rowsDiff(textdiff.Row{Kind: textdiff.Del, Left: "z", LeftNo: 1})), 0); got != 0 {
		t.Fatalf("a deleted file has no new line: %d", got)
	}
}

func TestFoldDiffRowsKeepsContextAndKeptRows(t *testing.T) {
	t.Parallel()
	var src []textdiff.Row
	for i := 1; i <= 20; i++ {
		src = append(src, textdiff.Row{Kind: textdiff.Same, Left: "s", Right: "s", LeftNo: i, RightNo: i})
	}
	src[10] = textdiff.Row{Kind: textdiff.Changed, Left: "o", Right: "n", LeftNo: 11, RightNo: 11}
	all := unifiedDiffRows(rowsDiff(src...))
	keep := func(i int) bool { return all[i].nu == 2 } // a banded / remarked line
	got := foldDiffRows(all, keep, nil)
	// fold(1: line 1) · line 2 (kept) · fold(5: lines 3..7) · 8 9 10 · −11 · +11 · 12 13 14 · fold(6: 15..20)
	if len(got) != 12 {
		t.Fatalf("%d rows, want 12: %+v", len(got), got)
	}
	if got[0].kind != diffFold || got[0].hidden != 1 || got[1].nu != 2 || got[2].kind != diffFold || got[2].hidden != 5 {
		t.Fatalf("head = %+v %+v %+v", got[0], got[1], got[2])
	}
	if last := got[len(got)-1]; last.kind != diffFold || last.hidden != 6 {
		t.Fatalf("tail = %+v", last)
	}
	open := map[int]bool{got[2].at: true}
	if again := foldDiffRows(all, keep, open); len(again) != len(got)+4 {
		t.Fatalf("an opened fold shows its 5 rows: %d rows, want %d", len(again), len(got)+4)
	}
	if rowOfNew(got, 4) != -1 || rowNewLine(got, 2) != 8 {
		t.Fatal("a folded line has no row; a fold stands for the next shown line")
	}
}

func TestDiffNoticeTexts(t *testing.T) {
	t.Parallel()
	changed := domain.ReviewFileDiff{Changed: true, Diff: rowsDiff(textdiff.Row{Kind: textdiff.Add, Right: "x", RightNo: 1})}
	for _, c := range []struct {
		res   domain.ReviewFileDiff
		err   error
		agent bool
		want  string
	}{
		{changed, nil, false, ""},
		{changed, nil, true, ""},
		{domain.ReviewFileDiff{}, nil, false, "no change to this file in the review — D shows the file"},
		{domain.ReviewFileDiff{}, nil, true, "no changes in the working tree — D shows the file"},
		{domain.ReviewFileDiff{Changed: true, Diff: domain.Diff{Binary: true}}, nil, false, "binary file — D shows the file"},
		{domain.ReviewFileDiff{Changed: true, Diff: domain.Diff{TooLarge: true}}, nil, false, "diff too large — D shows the file"},
		{domain.ReviewFileDiff{}, domain.ErrReviewNotFound, false, "the review is gone — D shows the file"},
		{domain.ReviewFileDiff{}, errors.New("boom"), false, "could not read the diff — D shows the file"},
	} {
		if got := diffNoticeText(c.res, c.err, c.agent); got != c.want {
			t.Fatalf("notice(changed %v, %v, agent %v) = %q, want %q", c.res.Changed, c.err, c.agent, got, c.want)
		}
	}
}

func TestDiffTitleNamesTheRange(t *testing.T) {
	t.Parallel()
	res := domain.ReviewFileDiff{Left: "3a4d954", Right: "774ace5"}
	if got := diffTitle("a.go", res); got != "Diff a.go (3a4d954 → 774ace5)" {
		t.Fatalf("title %q", got)
	}
	res.OldPath = "old.go"
	if got := diffTitle("a.go", res); got != "Diff old.go → a.go (3a4d954 → 774ace5)" {
		t.Fatalf("rename title %q", got)
	}
}

func TestSameText(t *testing.T) {
	t.Parallel()
	if !sameText([]string{"a", "b"}, []string{"a", "b"}) || sameText([]string{"a"}, []string{"b"}) || sameText(nil, nil) || sameText([]string{"a"}, []string{"a", "b"}) {
		t.Fatal("sameText: equal non-empty line lists only")
	}
}
