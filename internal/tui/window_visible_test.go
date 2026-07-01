package tui

import (
	"fmt"
	"strings"
	"testing"
)

func winRowsN(n int) []winRow {
	rows := make([]winRow, n)
	for i := range rows {
		rows[i] = winRow{text: fmt.Sprintf("row-%06d-some-content-here", i)}
	}
	return rows
}

// TestRenderWindowScalesWithVisibleNotTotal pins the O(visible) fast path: in a
// single-line mode (cutoff), rendering a small window over a huge row set must
// not do work proportional to the total. Pre-fix renderWindow built a display
// line for every row every call (the ~1.5s-per-frame / heartbeat freeze on a
// 40k-file panel); post-fix it windows to the visible slice first, so allocs are
// ~flat as the row count grows 40×.
func TestRenderWindowScalesWithVisibleNotTotal(t *testing.T) {
	const h = 20
	measure := func(n int) float64 {
		rows := winRowsN(n)
		o := winOpts{w: 40, h: h, mode: modeCutoff, anchor: n / 2}
		return testing.AllocsPerRun(5, func() { _ = renderWindow(rows, o) })
	}
	small := measure(1000)
	big := measure(40000)
	if big > small*2 {
		t.Fatalf("renderWindow allocs grew %.0f→%.0f (%.1fx) for 40x rows: O(total) not O(visible)", small, big, big/small)
	}
}

// TestRenderWindowVisibleSliceCorrect verifies the fast path shows exactly the
// window the slow path would (centred on the anchor), so the optimisation is
// output-preserving.
func TestRenderWindowVisibleSliceCorrect(t *testing.T) {
	const (
		n = 1000
		h = 20
	)
	rows := winRowsN(n)
	out := strings.Join(renderWindow(rows, winOpts{w: 40, h: h, mode: modeCutoff, anchor: 500}), "\n")
	// windowStart(1000,20,500) = 490 → visible [490,510).
	for _, want := range []string{"row-000490", "row-000500", "row-000509"} {
		if !strings.Contains(out, want) {
			t.Errorf("window missing %q", want)
		}
	}
	for _, notWant := range []string{"row-000489", "row-000510", "row-000000", "row-000999"} {
		if strings.Contains(out, notWant) {
			t.Errorf("off-window row %q leaked in", notWant)
		}
	}
	if got := len(renderWindow(rows, winOpts{w: 40, h: h, mode: modeCutoff, anchor: 500})); got != h {
		t.Errorf("expected exactly %d lines, got %d", h, got)
	}
}
