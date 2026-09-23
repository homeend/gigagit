package web

import (
	"testing"

	"github.com/homeend/gigagit/internal/hunkpick"
	"github.com/homeend/gigagit/internal/textdiff"
)

// The web stages ROWS, the unit a side-by-side diff shows: a modified row is
// one change (old line replaced by new), a removed-only row its old line, an
// added-only row its new line. A row that is not selected keeps its current
// version — exactly git add -p's line semantics — whichever lane it is in.

func rowDoc(t *testing.T, old, nw string) (*hunkpick.Doc, [][]blockRow) {
	t.Helper()
	o, n := []byte(old), []byte(nw)
	return hunkpick.FromDiff(o, n), hunkBlockRows(textdiff.Compare(o, n, textdiff.Options{}).Rows)
}

func resolved(t *testing.T, doc *hunkpick.Doc) string {
	t.Helper()
	got, ok := doc.Resolved()
	if !ok {
		t.Fatal("the doc must resolve once every block has a decision")
	}
	return string(got)
}

func TestStagingOneModifiedRowReplacesOnlyThatLine(t *testing.T) {
	t.Parallel()
	// one block of two modified rows
	doc, rows := rowDoc(t, "keep\nold1\nold2\ntail\n", "keep\nnew1\nnew2\ntail\n")
	if len(rows) != 1 || len(rows[0]) != 2 {
		t.Fatalf("fixture: want one block of two rows, got %v", rows)
	}
	err := applyRowSelection(doc, rows, laneUnstaged, []stageBlockPick{{Block: 0, Rows: []int{0}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := resolved(t, doc); got != "keep\nnew1\nold2\ntail\n" {
		t.Fatalf("staged content = %q: the first row must be replaced, the second kept", got)
	}
}

func TestStagingAnAddedRowAndARemovedRow(t *testing.T) {
	t.Parallel()
	// a removal and an addition in separate blocks
	doc, rows := rowDoc(t, "a\ngone\nb\nc\n", "a\nb\nc\nadded\n")
	if len(rows) != 2 {
		t.Fatalf("fixture: want two blocks, got %d", len(rows))
	}
	// stage the addition only: the removal stays unstaged (the line remains)
	if err := applyRowSelection(doc, rows, laneUnstaged, []stageBlockPick{{Block: 1, Rows: []int{0}}}); err != nil {
		t.Fatal(err)
	}
	if got := resolved(t, doc); got != "a\ngone\nb\nc\nadded\n" {
		t.Fatalf("staging the addition alone gave %q", got)
	}

	// and staging the removal alone drops the line, leaves the addition out
	doc2, rows2 := rowDoc(t, "a\ngone\nb\nc\n", "a\nb\nc\nadded\n")
	if err := applyRowSelection(doc2, rows2, laneUnstaged, []stageBlockPick{{Block: 0, Rows: []int{0}}}); err != nil {
		t.Fatal(err)
	}
	if got := resolved(t, doc2); got != "a\nb\nc\n" {
		t.Fatalf("staging the removal alone gave %q", got)
	}
}

func TestStagingAWholeHunk(t *testing.T) {
	t.Parallel()
	doc, rows := rowDoc(t, "keep\nold1\nold2\ntail\n", "keep\nnew1\nnew2\ntail\n")
	if err := applyRowSelection(doc, rows, laneUnstaged, []stageBlockPick{{Block: 0, Whole: true}}); err != nil {
		t.Fatal(err)
	}
	if got := resolved(t, doc); got != "keep\nnew1\nnew2\ntail\n" {
		t.Fatalf("the whole hunk staged as %q", got)
	}
}

// The STAGED diff shows HEAD → index. Unstaging a row puts HEAD's version of
// it back into the index; unselected rows keep the index's version.
func TestUnstagingOneRowRestoresHEADsLine(t *testing.T) {
	t.Parallel()
	head, index := "keep\nold1\nold2\ntail\n", "keep\nnew1\nnew2\ntail\n"
	doc, rows := rowDoc(t, head, index)
	if err := applyRowSelection(doc, rows, laneStaged, []stageBlockPick{{Block: 0, Rows: []int{1}}}); err != nil {
		t.Fatal(err)
	}
	if got := resolved(t, doc); got != "keep\nnew1\nold2\ntail\n" {
		t.Fatalf("the new index after unstaging row 2 = %q", got)
	}
}

func TestUnstagingAWholeHunkRestoresHEAD(t *testing.T) {
	t.Parallel()
	doc, rows := rowDoc(t, "keep\nold1\ntail\n", "keep\nnew1\ntail\n")
	if err := applyRowSelection(doc, rows, laneStaged, []stageBlockPick{{Block: 0, Whole: true}}); err != nil {
		t.Fatal(err)
	}
	if got := resolved(t, doc); got != "keep\nold1\ntail\n" {
		t.Fatalf("unstaging the whole hunk left %q in the index", got)
	}
}

func TestRowSelectionRefusesOutOfRange(t *testing.T) {
	t.Parallel()
	doc, rows := rowDoc(t, "a\nb\n", "a\nB\n")
	if err := applyRowSelection(doc, rows, laneUnstaged, []stageBlockPick{{Block: 3, Rows: []int{0}}}); err == nil {
		t.Fatal("an unknown block must be refused")
	}
	doc, rows = rowDoc(t, "a\nb\n", "a\nB\n")
	if err := applyRowSelection(doc, rows, laneUnstaged, []stageBlockPick{{Block: 0, Rows: []int{5}}}); err == nil {
		t.Fatal("an unknown row must be refused")
	}
}
