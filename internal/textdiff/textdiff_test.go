package textdiff

import (
	"bytes"
	"fmt"
	"slices"
	"testing"
	"time"
)

// rowsKinds compacts kinds for table comparisons.
func rowsKinds(rows []Row) []Kind {
	out := make([]Kind, len(rows))
	for i, r := range rows {
		out[i] = r.Kind
	}
	return out
}

func TestCompareEqualInputs(t *testing.T) {
	res := Compare([]byte("a\nb\n"), []byte("a\nb\n"))
	want := []Kind{Same, Same}
	if !slices.Equal(rowsKinds(res.Rows), want) {
		t.Fatalf("kinds = %v, want %v", rowsKinds(res.Rows), want)
	}
	if len(res.Blocks) != 0 {
		t.Fatalf("equal inputs must have no blocks, got %v", res.Blocks)
	}
	if res.Rows[1].LeftNo != 2 || res.Rows[1].RightNo != 2 {
		t.Fatalf("line numbers wrong: %+v", res.Rows[1])
	}
}

func TestComparePureInsert(t *testing.T) {
	res := Compare([]byte("a\nc\n"), []byte("a\nb\nc\n"))
	want := []Kind{Same, Add, Same}
	if !slices.Equal(rowsKinds(res.Rows), want) {
		t.Fatalf("kinds = %v, want %v", rowsKinds(res.Rows), want)
	}
	r := res.Rows[1]
	if r.Left != "" || r.Right != "b" || r.LeftNo != 0 || r.RightNo != 2 {
		t.Fatalf("add row wrong: %+v", r)
	}
	if len(res.Blocks) != 1 || res.Blocks[0] != 1 {
		t.Fatalf("blocks = %v, want [1]", res.Blocks)
	}
}

func TestComparePureDelete(t *testing.T) {
	res := Compare([]byte("a\nb\nc\n"), []byte("a\nc\n"))
	want := []Kind{Same, Del, Same}
	if !slices.Equal(rowsKinds(res.Rows), want) {
		t.Fatalf("kinds = %v, want %v", rowsKinds(res.Rows), want)
	}
	r := res.Rows[1]
	if r.Left != "b" || r.Right != "" || r.LeftNo != 2 || r.RightNo != 0 {
		t.Fatalf("del row wrong: %+v", r)
	}
}

func TestCompareReplaceUnequalRuns(t *testing.T) {
	// 2 deleted lines replaced by 3 added lines: zip 2 Changed pairs, the
	// third added line gets an Add row with a left gap.
	res := Compare([]byte("keep\nx1\nx2\nkeep2\n"), []byte("keep\ny1\ny2\ny3\nkeep2\n"))
	want := []Kind{Same, Changed, Changed, Add, Same}
	if !slices.Equal(rowsKinds(res.Rows), want) {
		t.Fatalf("kinds = %v, want %v", rowsKinds(res.Rows), want)
	}
	if res.Rows[1].Left != "x1" || res.Rows[1].Right != "y1" {
		t.Fatalf("first changed pair wrong: %+v", res.Rows[1])
	}
	if res.Rows[3].RightNo != 4 || res.Rows[3].LeftNo != 0 {
		t.Fatalf("tail add row wrong: %+v", res.Rows[3])
	}
}

func TestCompareMultipleBlocks(t *testing.T) {
	res := Compare([]byte("a\nb\nc\nd\ne\n"), []byte("a\nB\nc\nd\nE\n"))
	want := []Kind{Same, Changed, Same, Same, Changed}
	if !slices.Equal(rowsKinds(res.Rows), want) {
		t.Fatalf("kinds = %v, want %v", rowsKinds(res.Rows), want)
	}
	if len(res.Blocks) != 2 || res.Blocks[0] != 1 || res.Blocks[1] != 4 {
		t.Fatalf("blocks = %v, want [1 4]", res.Blocks)
	}
}

func TestCompareEmptySides(t *testing.T) {
	res := Compare(nil, []byte("a\nb\n"))
	if !slices.Equal(rowsKinds(res.Rows), []Kind{Add, Add}) {
		t.Fatalf("new file: kinds = %v", rowsKinds(res.Rows))
	}
	res = Compare([]byte("a\nb\n"), nil)
	if !slices.Equal(rowsKinds(res.Rows), []Kind{Del, Del}) {
		t.Fatalf("deleted file: kinds = %v", rowsKinds(res.Rows))
	}
	res = Compare(nil, nil)
	if len(res.Rows) != 0 || len(res.Blocks) != 0 {
		t.Fatalf("both empty: %+v", res)
	}
}

func TestCompareTrailingNewlineBothSidesDropped(t *testing.T) {
	// Both sides end with \n: the phantom empty last line appears on neither.
	res := Compare([]byte("a\n"), []byte("a\n"))
	if len(res.Rows) != 1 || res.Rows[0].Kind != Same {
		t.Fatalf("rows = %+v", res.Rows)
	}
}

func TestCompareNewlineAtEOFOnlyChangeIsVisible(t *testing.T) {
	// Old ends with \n, new does not: git reports the file modified, so the
	// viewer must show a block, not two identical panes. The side WITH the
	// newline keeps one extra (empty) row.
	res := Compare([]byte("a\n"), []byte("a"))
	if len(res.Blocks) == 0 {
		t.Fatalf("newline-at-EOF change produced no block: rows %+v", res.Rows)
	}
	res = Compare([]byte("a"), []byte("a\n"))
	if len(res.Blocks) == 0 {
		t.Fatalf("reverse newline-at-EOF change produced no block: rows %+v", res.Rows)
	}
}

func TestCompareTrimOverlapPrefixOfOther(t *testing.T) {
	// One side is a prefix of the other: prefix+suffix trim must not
	// double-count the shared line ("a" must not be consumed twice).
	res := Compare([]byte("a\na\n"), []byte("a\n"))
	got := rowsKinds(res.Rows)
	// Same+Del or Del+Same are both valid alignments; the invariant is one
	// Same row and one Del row totalling the right line counts.
	var same, del int
	for _, k := range got {
		switch k {
		case Same:
			same++
		case Del:
			del++
		default:
			t.Fatalf("unexpected kind %v in %v", k, got)
		}
	}
	if same != 1 || del != 1 {
		t.Fatalf("kinds = %v, want one Same and one Del", got)
	}
}

func TestCompareLineCapTruncates(t *testing.T) {
	var oldB, newB bytes.Buffer
	for i := 0; i < maxLines+10; i++ {
		fmt.Fprintf(&oldB, "old%d\n", i)
		fmt.Fprintf(&newB, "new%d\n", i)
	}
	res := Compare(oldB.Bytes(), newB.Bytes())
	if !res.Truncated {
		t.Fatal("expected Truncated for over-cap fully-different inputs")
	}
	// Fallback shape: all Del rows then all Add rows, one block.
	if len(res.Blocks) != 1 {
		t.Fatalf("blocks = %v, want exactly one replace block", res.Blocks)
	}
}

func TestCompareLineCapAfterTrim(t *testing.T) {
	// A one-line change in a huge file: the cap applies to the TRIMMED
	// middle, so this must align perfectly and not be Truncated.
	var oldB, newB bytes.Buffer
	for i := 0; i < maxLines+10; i++ {
		fmt.Fprintf(&oldB, "line%d\n", i)
		if i == 31337 {
			fmt.Fprintf(&newB, "CHANGED\n")
		} else {
			fmt.Fprintf(&newB, "line%d\n", i)
		}
	}
	res := Compare(oldB.Bytes(), newB.Bytes())
	if res.Truncated {
		t.Fatal("small change in a big file must not be Truncated")
	}
	if len(res.Blocks) != 1 {
		t.Fatalf("blocks = %v, want one", res.Blocks)
	}
}

func TestCompareEditBudgetBailsFast(t *testing.T) {
	// Two large, completely different inputs (no trim help): the edit
	// budget must bail out quickly with the replace fallback.
	var oldB, newB bytes.Buffer
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&oldB, "alpha%d\n", i*7)
		fmt.Fprintf(&newB, "omega%d\n", i*13)
	}
	start := time.Now()
	res := Compare(oldB.Bytes(), newB.Bytes())
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("Compare took %v; the edit budget must bound the worst case", d)
	}
	if !res.Truncated {
		t.Fatal("expected Truncated when the edit budget is exceeded")
	}
}

func TestIsBinary(t *testing.T) {
	if IsBinary([]byte("plain text\nwith lines\n")) {
		t.Fatal("text misclassified as binary")
	}
	if !IsBinary([]byte("PNG\x00\x01\x02")) {
		t.Fatal("NUL content not classified as binary")
	}
	if IsBinary(nil) {
		t.Fatal("empty content must not be binary")
	}
}

func TestCompareSingleLineNoNewline(t *testing.T) {
	res := Compare([]byte("hello"), []byte("world"))
	if !slices.Equal(rowsKinds(res.Rows), []Kind{Changed}) {
		t.Fatalf("kinds = %v, want [Changed]", rowsKinds(res.Rows))
	}
	if res.Rows[0].Left != "hello" || res.Rows[0].Right != "world" {
		t.Fatalf("row = %+v", res.Rows[0])
	}
}

func TestCompareCRLFLinesPreserved(t *testing.T) {
	// Lines keep their \r — comparison is raw; display sanitizing is the
	// TUI's job. Equal CRLF content must compare as Same.
	res := Compare([]byte("a\r\nb\r\n"), []byte("a\r\nb\r\n"))
	if !slices.Equal(rowsKinds(res.Rows), []Kind{Same, Same}) {
		t.Fatalf("kinds = %v, want [Same Same]", rowsKinds(res.Rows))
	}
	if res.Rows[0].Left != "a\r" {
		t.Fatalf("CRLF stripped from line content: %q", res.Rows[0].Left)
	}
}

func TestExpandWrapsRows1to1(t *testing.T) {
	rows := []Row{{Kind: Same, Left: "a"}, {Kind: Changed, Left: "b", Right: "B"}}
	lines := Expand(rows)
	if len(lines) != 2 {
		t.Fatalf("len = %d, want 2", len(lines))
	}
	for i, l := range lines {
		if l.Fold != 0 || l.Row != rows[i] {
			t.Fatalf("line %d = %+v, want Row %+v Fold 0", i, l, rows[i])
		}
	}
}

// sameRows builds n Same rows then sets the given indices to Changed.
func sameRows(n int, changed ...int) []Row {
	rows := make([]Row, n)
	for i := range rows {
		rows[i] = Row{Kind: Same, LeftNo: i + 1, RightNo: i + 1}
	}
	for _, c := range changed {
		rows[c] = Row{Kind: Changed, Left: "x", Right: "y", LeftNo: c + 1, RightNo: c + 1}
	}
	return rows
}

func TestCollapseSingleBlockMidFile(t *testing.T) {
	// 20 rows, change at 10, context 3 → keep [7..13].
	rows := sameRows(20, 10)
	lines, blocks := Collapse(rows, []int{10}, 3)
	if len(lines) != 9 { // Fold(0..6) + 7 kept rows + Fold(14..19)
		t.Fatalf("len(lines) = %d, want 9:\n%+v", len(lines), lines)
	}
	if lines[0].Fold != 7 {
		t.Fatalf("leading fold = %d, want 7", lines[0].Fold)
	}
	if lines[8].Fold != 6 {
		t.Fatalf("trailing fold = %d, want 6", lines[8].Fold)
	}
	if len(blocks) != 1 || blocks[0] != 4 {
		t.Fatalf("blocks = %v, want [4]", blocks)
	}
	if lines[4].Fold != 0 || lines[4].Row.Kind != Changed {
		t.Fatalf("block line = %+v, want the Changed row", lines[4])
	}
}

func TestCollapseChangeAtStartNoLeadingFold(t *testing.T) {
	rows := sameRows(20, 0)
	lines, blocks := Collapse(rows, []int{0}, 3)
	if lines[0].Fold != 0 || lines[0].Row.Kind != Changed {
		t.Fatalf("first line must be the change, got %+v", lines[0])
	}
	if blocks[0] != 0 {
		t.Fatalf("blocks = %v, want [0]", blocks)
	}
	if last := lines[len(lines)-1]; last.Fold != 16 { // rows 4..19
		t.Fatalf("trailing fold = %d, want 16", last.Fold)
	}
}

func TestCollapseChangeAtEndNoTrailingFold(t *testing.T) {
	rows := sameRows(20, 19)
	lines, _ := Collapse(rows, []int{19}, 3)
	if last := lines[len(lines)-1]; last.Fold != 0 || last.Row.Kind != Changed {
		t.Fatalf("last line must be the change, got %+v", last)
	}
}

func TestCollapseTwoBlocksFarApartFoldBetween(t *testing.T) {
	// changes at 5 and 15, context 3 → keep [2..8] and [12..18], gap 9..11.
	rows := sameRows(21, 5, 15)
	lines, blocks := Collapse(rows, []int{5, 15}, 3)
	if len(blocks) != 2 {
		t.Fatalf("blocks = %v, want 2 entries", blocks)
	}
	var foldBetween bool
	for i := blocks[0] + 1; i < blocks[1]; i++ {
		if lines[i].Fold == 3 {
			foldBetween = true
		}
	}
	if !foldBetween {
		t.Fatalf("expected a Fold:3 between the blocks:\n%+v", lines)
	}
}

func TestCollapseAdjacentBlocksMerge(t *testing.T) {
	// changes at 5 and 9, context 3 → windows [2..8] and [6..12] overlap:
	// no fold between them.
	rows := sameRows(20, 5, 9)
	lines, blocks := Collapse(rows, []int{5, 9}, 3)
	for i := blocks[0]; i <= blocks[1]; i++ {
		if lines[i].Fold != 0 {
			t.Fatalf("merged region must have no fold, found one at %d:\n%+v", i, lines)
		}
	}
}

func TestCollapseContextExceedsGapKeepsAll(t *testing.T) {
	rows := sameRows(10, 5)
	lines, _ := Collapse(rows, []int{5}, 100)
	if len(lines) != 10 {
		t.Fatalf("len = %d, want all 10 rows kept", len(lines))
	}
	for _, l := range lines {
		if l.Fold != 0 {
			t.Fatalf("no fold expected with huge context:\n%+v", lines)
		}
	}
}

func TestCollapseNoBlocksEmpty(t *testing.T) {
	lines, blocks := Collapse(sameRows(10), nil, 3)
	if len(lines) != 0 || len(blocks) != 0 {
		t.Fatalf("no-change collapse must be empty, got %d lines %d blocks", len(lines), len(blocks))
	}
}
