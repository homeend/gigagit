package tui

import (
	"fmt"
	"testing"

	"github.com/gigagit/gg/internal/model"
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

// BenchmarkCommitsRender measures the per-frame Commits render hot path
// (filter+sort+materialize via panelView, then decorators) across feed sizes
// and graph widths.
func BenchmarkCommitsRender(b *testing.B) {
	for _, n := range []int{1000, 5000} {
		for _, cols := range []int{8, 80} {
			m := benchModel(n, 303, cols)
			b.Run(fmt.Sprintf("n=%d/cols=%d", n, cols), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					rows, idx := m.panelView(panelCommits)
					_ = m.commitDecorators(rows, idx)
				}
			})
		}
	}
}
