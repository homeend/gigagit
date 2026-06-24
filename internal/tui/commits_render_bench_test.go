package tui

import (
	"fmt"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// benchModel builds a Commits-panel Model with n commits, each carrying a
// `lanes`-wide cached graph row, focus on the Commits panel, default sort, no
// filter — i.e. the natural-order graph render path.
func benchModel(n, lanes, cols int) Model {
	m := Model{
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{},
		dispModes: map[panel]dispMode{},
		hscroll:   map[panel]int{},
		focus:     panelCommits,
		width:     120,
		height:    40,
	}
	m.commits = make([]model.Commit, n)
	m.commitGraphRows = make([]string, n)
	m.commitGraphLanes = make([]int, n)
	cells := cellsWithNode(lanes, lanes/2)
	for i := range m.commits {
		m.commits[i] = model.Commit{
			Hash:    fmt.Sprintf("%040x", i),
			Subject: "some commit subject line here",
			Source:  "main",
		}
		m.commitGraphRows[i] = cells
		m.commitGraphLanes[i] = lanes / 2
	}
	m.commitGraphCols = cols
	return m
}

// TestCommitDecoratorsAllocScaleLinear pins the O(n²)→O(n) fix: doubling the
// feed must not ~quadruple the allocations of building decorators. Pre-fix this
// fails (≈4× growth); post-fix it is ≈2×.
func TestCommitDecoratorsAllocScaleLinear(t *testing.T) {
	measure := func(n int) float64 {
		m := benchModel(n, 303, 8)
		rows, idx := m.panelView(panelCommits)
		return testing.AllocsPerRun(3, func() {
			_ = m.commitDecorators(rows, idx)
		})
	}
	a := measure(400)
	b := measure(800)
	ratio := b / a
	if ratio > 2.6 { // linear ≈2.0; allow headroom. O(n²) would be ≈4.0.
		t.Fatalf("decorator allocs grew %.2fx for 2x feed (want ≤2.6x, O(n^2) bug present)", ratio)
	}
}

// BenchmarkCommitsRender measures the per-frame Commits render hot path
// (commitBody: filter+sort indices, then style only the visible window) across
// feed sizes and graph widths. After window-then-style this is ~flat in n.
func BenchmarkCommitsRender(b *testing.B) {
	const boxH = 40 // ~37 visible rows
	for _, n := range []int{1000, 5000} {
		for _, cols := range []int{8, 80} {
			m := benchModel(n, 303, cols)
			m.sel[panelCommits] = n / 2 // mid-feed: exercises both window edges
			b.Run(fmt.Sprintf("n=%d/cols=%d", n, cols), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					_, _, _ = m.commitBody(boxH)
				}
			})
		}
	}
}

// TestFilteredDisplayIndicesSkipsRowStyling guards that filtering a large feed
// matches via the cheap haystack, never Row(i) (which now styles the graph
// window). The whole filtered walk must therefore cost LESS than styling every
// row once; if Row styling leaked back into the filter path it would cost the
// row styling PLUS the haystack and exceed it.
func TestFilteredDisplayIndicesSkipsRowStyling(t *testing.T) {
	const n = 2000
	m := benchModel(n, 303, 8)
	m.filterPanel = panelCommits
	m.filterQuery = "subject" // matches every commit's subject via the haystack
	l := m.listFor(panelCommits)

	rowStyling := testing.AllocsPerRun(3, func() {
		for i := 0; i < n; i++ {
			_ = l.Row(i)
		}
	})
	filtered := testing.AllocsPerRun(3, func() {
		_ = m.displayIndices(panelCommits)
	})
	if filtered >= rowStyling {
		t.Fatalf("filtered displayIndices allocs=%.0f >= per-row styling allocs=%.0f: Row styling leaked into the filter path", filtered, rowStyling)
	}
}

// BenchmarkCommitsRenderFull keeps the pre-windowing measurement (full
// materialization via panelView) so the O(visible) win stays visible in numbers.
func BenchmarkCommitsRenderFull(b *testing.B) {
	for _, n := range []int{1000, 5000} {
		m := benchModel(n, 303, 8)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				rows, idx := m.panelView(panelCommits)
				_ = m.commitDecorators(rows, idx)
			}
		})
	}
}
