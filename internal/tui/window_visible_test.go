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

// TestWindowRowBoundsMatchesRenderWindowSlicing pins windowRowBounds — the
// factored-out bound renderWindow itself now uses — against the same
// [490,510) window TestRenderWindowVisibleSliceCorrect checks by rendering,
// so a caller that pre-slices with windowRowBounds (blame's per-row build)
// can never see a different window than renderWindow lays out.
func TestWindowRowBoundsMatchesRenderWindowSlicing(t *testing.T) {
	t.Parallel()
	if lo, hi := windowRowBounds(1000, 20, 500, modeCutoff); lo != 490 || hi != 510 {
		t.Errorf("cutoff bounds = [%d,%d), want [490,510)", lo, hi)
	}
}

// TestWindowRowBounds covers both single-line modes (cutoff/scroll share the
// windowStart formula) and wrap mode (the [a-h,a+h+1) band), including the
// n<=h no-windowing case and clamping at both the start and end of the row
// set.
func TestWindowRowBounds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		n, h, anchor   int
		mode           dispMode
		wantLo, wantHi int
	}{
		{"fits entirely, no windowing", 10, 20, 5, modeCutoff, 0, 10},
		{"fits entirely, wrap mode too", 10, 20, 5, modeWrap, 0, 10},
		{"cutoff centred", 1000, 20, 500, modeCutoff, 490, 510},
		{"scroll uses the same formula as cutoff", 1000, 20, 500, modeScroll, 490, 510},
		{"cutoff clamps at the start", 1000, 20, 0, modeCutoff, 0, 20},
		{"cutoff clamps at the end", 1000, 20, 999, modeCutoff, 980, 1000},
		{"wrap centred band", 1000, 20, 500, modeWrap, 480, 521},
		{"wrap clamps at the start", 1000, 20, 5, modeWrap, 0, 26},
		{"wrap clamps at the end", 1000, 20, 995, modeWrap, 975, 1000},
		{"wrap out-of-range anchor resets to row 0", 1000, 20, -1, modeWrap, 0, 21},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lo, hi := windowRowBounds(c.n, c.h, c.anchor, c.mode)
			if lo != c.wantLo || hi != c.wantHi {
				t.Errorf("windowRowBounds(%d,%d,%d,%v) = [%d,%d), want [%d,%d)", c.n, c.h, c.anchor, c.mode, lo, hi, c.wantLo, c.wantHi)
			}
			if lo < 0 || hi > c.n || lo > hi {
				t.Errorf("bounds [%d,%d) invalid for n=%d", lo, hi, c.n)
			}
		})
	}
}
