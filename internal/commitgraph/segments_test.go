package commitgraph

import (
	"slices"
	"testing"
)

func sc(hash string, parents ...string) Commit { return Commit{Hash: hash, Parents: parents} }

func noBoundary(int) bool { return false }

// boundaryAt marks the listed row indexes as boundaries.
func boundaryAt(idx ...int) func(int) bool {
	return func(i int) bool { return slices.Contains(idx, i) }
}

func TestSegmentsLinearIsOneSegment(t *testing.T) {
	t.Parallel()
	got := (&SegmentLayer{}).Append([]Commit{sc("a3", "a2"), sc("a2", "a1"), sc("a1")}, noBoundary)
	if want := []int{0, 0, 0}; !slices.Equal(got, want) {
		t.Fatalf("linear history segments = %v, want %v", got, want)
	}
}

func TestSegmentsBranchOverBase(t *testing.T) {
	t.Parallel()
	// Solo of A: a3..a1 are A's own commits, b2 is branch B's tip (row 3).
	commits := []Commit{sc("a3", "a2"), sc("a2", "a1"), sc("a1", "b2"), sc("b2", "b1"), sc("b1")}
	got := (&SegmentLayer{}).Append(commits, boundaryAt(3))
	if want := []int{0, 0, 0, 1, 1}; !slices.Equal(got, want) {
		t.Fatalf("A-over-B segments = %v, want %v", got, want)
	}
}

func TestSegmentsStackedBoundaries(t *testing.T) {
	t.Parallel()
	commits := []Commit{sc("a1", "b1"), sc("b1", "c1"), sc("c1", "r"), sc("r")}
	got := (&SegmentLayer{}).Append(commits, boundaryAt(1, 2))
	if want := []int{0, 1, 2, 2}; !slices.Equal(got, want) {
		t.Fatalf("stacked territories = %v, want %v", got, want)
	}
}

func TestSegmentsMergeSideLine(t *testing.T) {
	t.Parallel()
	// m merges side (s2,s1) into mainline (c2,c1); the fork point c1 stays
	// mainline-colored (min claim wins), the side line gets its own segment.
	commits := []Commit{sc("m", "c2", "s2"), sc("c2", "c1"), sc("s2", "s1"), sc("s1", "c1"), sc("c1")}
	got := (&SegmentLayer{}).Append(commits, noBoundary)
	if want := []int{0, 0, 1, 1, 0}; !slices.Equal(got, want) {
		t.Fatalf("merge segments = %v, want %v", got, want)
	}
}

func TestSegmentsMergeForkPointMinClaimWinsEitherOrder(t *testing.T) {
	t.Parallel()
	// The side line pages in BEFORE the mainline child of the fork point — the
	// fork point must still take the smaller (mainline) id.
	commits := []Commit{sc("m", "c2", "s2"), sc("s2", "s1"), sc("s1", "c1"), sc("c2", "c1"), sc("c1")}
	got := (&SegmentLayer{}).Append(commits, noBoundary)
	if want := []int{0, 1, 1, 0, 0}; !slices.Equal(got, want) {
		t.Fatalf("fork point should keep the mainline segment: %v, want %v", got, want)
	}
}

func TestSegmentsBoundaryOverridesClaim(t *testing.T) {
	t.Parallel()
	got := (&SegmentLayer{}).Append([]Commit{sc("a1", "b1"), sc("b1")}, boundaryAt(1))
	if want := []int{0, 1}; !slices.Equal(got, want) {
		t.Fatalf("boundary should override the inherited claim: %v, want %v", got, want)
	}
}

func TestSegmentsNonTopologicalInput(t *testing.T) {
	t.Parallel()
	// A parent listed before its child must not panic; the orphaned child
	// simply starts a fresh segment.
	got := (&SegmentLayer{}).Append([]Commit{sc("p", "g"), sc("c", "p"), sc("g")}, noBoundary)
	if len(got) != 3 || got[0] != 0 || got[2] != 0 || got[1] == got[0] {
		t.Fatalf("non-topological segments = %v", got)
	}
}

func TestSegmentsIncrementalAppendMatchesFull(t *testing.T) {
	t.Parallel()
	commits := []Commit{sc("m", "c2", "s2"), sc("c2", "c1"), sc("s2", "s1"), sc("s1", "c1"), sc("c1", "c0"), sc("c0")}
	full := (&SegmentLayer{}).Append(commits, boundaryAt(4))
	var l SegmentLayer
	paged := l.Append(commits[:2], boundaryAt(4))
	paged = append(paged, l.Append(commits[2:], func(i int) bool { return i+2 == 4 })...)
	if !slices.Equal(full, paged) {
		t.Fatalf("paged append %v differs from full walk %v", paged, full)
	}
}

func TestSegmentsClaimsPruned(t *testing.T) {
	t.Parallel()
	var l SegmentLayer
	l.Append([]Commit{sc("a3", "a2"), sc("a2", "a1"), sc("a1")}, noBoundary)
	if len(l.claims) != 0 {
		t.Fatalf("claims not pruned after processing: %v", l.claims)
	}
}
