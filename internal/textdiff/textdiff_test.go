package textdiff

import (
	"bytes"
	"fmt"
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

func kindsEqual(a, b []Kind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCompareEqualInputs(t *testing.T) {
	res := Compare([]byte("a\nb\n"), []byte("a\nb\n"))
	want := []Kind{Same, Same}
	if !kindsEqual(rowsKinds(res.Rows), want) {
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
	if !kindsEqual(rowsKinds(res.Rows), want) {
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
	if !kindsEqual(rowsKinds(res.Rows), want) {
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
	if !kindsEqual(rowsKinds(res.Rows), want) {
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
	if !kindsEqual(rowsKinds(res.Rows), want) {
		t.Fatalf("kinds = %v, want %v", rowsKinds(res.Rows), want)
	}
	if len(res.Blocks) != 2 || res.Blocks[0] != 1 || res.Blocks[1] != 4 {
		t.Fatalf("blocks = %v, want [1 4]", res.Blocks)
	}
}

func TestCompareEmptySides(t *testing.T) {
	res := Compare(nil, []byte("a\nb\n"))
	if !kindsEqual(rowsKinds(res.Rows), []Kind{Add, Add}) {
		t.Fatalf("new file: kinds = %v", rowsKinds(res.Rows))
	}
	res = Compare([]byte("a\nb\n"), nil)
	if !kindsEqual(rowsKinds(res.Rows), []Kind{Del, Del}) {
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
