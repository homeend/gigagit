package tui

import (
	"fmt"
	"strings"
	"testing"
)

// filesBenchModel builds a Files-panel Model with n untracked rows shaped like
// the real graphify-out case (long, ~92-col hashed paths that always overflow a
// narrow left panel), focus on the Files panel, cutoff display mode, no marks —
// i.e. the natural per-frame Files render path that froze gg.
func filesBenchModel(n int) (Model, []string) {
	m := Model{
		sel:       map[panel]int{panelFiles: n / 2}, // mid-list: exercises both window edges
		sortModes: map[panel]sortMode{},
		dispModes: map[panel]dispMode{}, // zero value == modeCutoff
		hscroll:   map[panel]int{},
		focus:     panelFiles,
		width:     120,
		height:    40,
	}
	rows := make([]string, n)
	for i := range rows {
		rows[i] = fmt.Sprintf("A graphify-out/nodes/%060d/chunk_%08d.json", i, i)
	}
	return m, rows
}

// TestFilesPanelElideWindowed pins the O(files)→O(visible) fix for the Files
// panel render. The per-row cost is dominated by elideFilePath (many allocs per
// call); pre-fix renderPanel elides EVERY row every frame (≈40k paths → the
// ~1.5s freeze on the TypeScript repo's graphify-out/). Post-fix only the ~visible
// window is elided, so allocations-per-row collapse to the cheap winRow floor.
func TestFilesPanelElideWindowed(t *testing.T) {
	const (
		n    = 20000
		boxW = 30 // narrow left column: every 92-col path overflows → full elide path
		boxH = 40 // ~37 visible rows
	)
	m, rows := filesBenchModel(n)
	perRow := testing.AllocsPerRun(3, func() {
		_ = m.renderPanel(panelFiles, "Files", rows, nil, boxW, boxH)
	}) / float64(n)

	// Pre-fix perRow ≈ 10+ (an elideFilePath per row). Post-fix ≈ the winRow
	// floor (≈2). A threshold of 5 sits cleanly between the two.
	if perRow > 5 {
		t.Fatalf("renderPanel allocates %.1f allocs/row: eliding is NOT windowed (O(files) freeze present)", perRow)
	}
}

// TestFilesPanelWindowedOutput verifies the windowed render still shows the
// correct visible slice (centred on the selection, with off-window rows omitted
// and long paths middle-elided so the filename survives) — the output-side
// counterpart to the alloc test.
func TestFilesPanelWindowedOutput(t *testing.T) {
	const (
		n    = 100
		boxW = 30 // innerW 26 → the long paths overflow and are elided
		boxH = 23 // rowsCap = boxH-3 = 20 → visible window [40,60) around sel 50
	)
	m := Model{
		sel:       map[panel]int{panelFiles: 50},
		sortModes: map[panel]sortMode{},
		dispModes: map[panel]dispMode{},
		hscroll:   map[panel]int{},
		focus:     panelFiles,
		width:     120,
		height:    40,
	}
	rows := make([]string, n)
	for i := range rows {
		rows[i] = fmt.Sprintf("A some/nested/dir/file%05d.txt", i)
	}
	out := m.renderPanel(panelFiles, "Files", rows, nil, boxW, boxH)

	// The selection and its window neighbours render; off-window rows do not.
	for _, want := range []string{"file00050", "file00040", "file00059"} {
		if !strings.Contains(out, want) {
			t.Errorf("visible window is missing %q", want)
		}
	}
	for _, notWant := range []string{"file00000", "file00039", "file00060", "file00099"} {
		if strings.Contains(out, notWant) {
			t.Errorf("off-window row %q leaked into the render", notWant)
		}
	}
	// Paths overflow innerW, so the elide keeps the filename via a middle "…".
	if !strings.Contains(out, "…") {
		t.Errorf("expected long paths to be middle-elided (no … found)")
	}

	// A stale out-of-range selection must not blank the panel: it clamps to the
	// tail instead of windowing onto rows we never built.
	m.sel[panelFiles] = 500
	tail := m.renderPanel(panelFiles, "Files", rows, nil, boxW, boxH)
	if !strings.Contains(tail, "file00099") {
		t.Errorf("out-of-range sel blanked the panel (tail row file00099 not shown)")
	}
}

// BenchmarkFilesPanelRender measures the per-frame Files render across sizes.
// After windowing the elide this is ~flat in the expensive dimension.
func BenchmarkFilesPanelRender(b *testing.B) {
	for _, n := range []int{2000, 40000} {
		m, rows := filesBenchModel(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = m.renderPanel(panelFiles, "Files", rows, nil, 30, 40)
			}
		})
	}
}
