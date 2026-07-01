package tui

import (
	"fmt"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func fastPathModel(n int) Model {
	files := make([]model.FileStatus, n)
	for i := range files {
		files[i] = model.FileStatus{Path: fmt.Sprintf("dir/f%06d.txt", i), Kind: model.KindUntracked}
	}
	m := Model{
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{},
		dispModes: map[panel]dispMode{},
		hscroll:   map[panel]int{},
	}
	return m.withStatus(model.WorkingTreeStatus{Files: files})
}

// TestDisplayIndicesFilesFastPathNoAlloc pins the derive-on-write fix: an
// unsorted/unfiltered file panel returns its precomputed membership split in O(1)
// with zero per-call allocation. Pre-fix displayIndices rescanned all 40k files
// and allocated a fresh []int on every call — and it is called many times per
// keystroke, which made scrolling a huge working tree lag for seconds.
func TestDisplayIndicesFilesFastPathNoAlloc(t *testing.T) {
	m := fastPathModel(40000)

	// Correctness: withStatus derived membership equal to a direct recompute, and
	// displayIndices returns exactly that (same result the slow path would give).
	want := m.fileMembership(panelFiles)
	got := m.displayIndices(panelFiles)
	if len(got) != len(want) || len(got) == 0 {
		t.Fatalf("displayIndices(panelFiles) len=%d, want %d (non-zero)", len(got), len(want))
	}

	allocs := testing.AllocsPerRun(20, func() { _ = m.displayIndices(panelFiles) })
	if allocs > 0 {
		t.Fatalf("displayIndices(panelFiles) allocated %.1f/call: derive-on-write fast path not hit", allocs)
	}
}

// TestDisplayIndicesFallbackWhenNotDerived guards the nil-slice safety net: a
// model whose status was set WITHOUT withStatus (e.g. a test assigning m.status
// directly) must still return correct indices via the slow path, not an empty
// fast-path result.
func TestDisplayIndicesFallbackWhenNotDerived(t *testing.T) {
	m := Model{sel: map[panel]int{}, sortModes: map[panel]sortMode{}, dispModes: map[panel]dispMode{}, hscroll: map[panel]int{}}
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "a.txt", Kind: model.KindUntracked},
		{Path: "b.txt", Kind: model.KindUntracked},
	}}
	if m.filesIdx != nil {
		t.Fatal("filesIdx should be nil when withStatus was not used")
	}
	if got := len(m.displayIndices(panelFiles)); got != 2 {
		t.Fatalf("slow-path fallback returned %d, want 2", got)
	}
}
