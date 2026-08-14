package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/homeend/gigagit/internal/model"
)

// elideFilePath keeps the leading status glyph + space and middle-elides the
// path so BOTH the path's beginning and the filename survive a narrow column.
func TestElideFilePath(t *testing.T) {
	if got := elideFilePath("M short.go", 20); got != "M short.go" {
		t.Errorf("fits: got %q, want unchanged", got)
	}
	got := elideFilePath("M internal/tui/view.go", 12)
	if w := lipgloss.Width(got); w > 12 {
		t.Errorf("got %q width %d, want <= 12", got, w)
	}
	if !strings.HasPrefix(got, "M ") {
		t.Errorf("got %q, want the glyph column %q kept", got, "M ")
	}
	if !strings.Contains(got, "view.go") {
		t.Errorf("got %q, want the filename %q kept", got, "view.go")
	}
	if !strings.Contains(got, "…/") {
		t.Errorf("got %q, want …/ marking the dropped middle before the filename", got)
	}
	// Degenerate widths never overflow the budget.
	for _, n := range []int{0, 1, 2, 3} {
		if w := lipgloss.Width(elideFilePath("M internal/tui/view.go", n)); w > n {
			t.Errorf("n=%d: width %d overflows budget", n, w)
		}
	}
}

// The file-row elision keeps the path's beginning AND the directory nearest
// the file around the "…" (elidePath's alternating priority).
func TestElideFileMiddle(t *testing.T) {
	// "aa/bb/cc/dd/view.go" is 19 cols; at 16 the middle dirs (bb, cc) go.
	got := elidePath("aa/bb/cc/dd/view.go", 16)
	if got != "aa/…/dd/view.go" {
		t.Errorf("got %q, want %q", got, "aa/…/dd/view.go")
	}
	// A bare filename (no directory) is cut inside the name.
	if got := elidePath("averylongfilename.go", 8); lipgloss.Width(got) > 8 {
		t.Errorf("bare filename: got %q overflows 8", got)
	}
}

// The Files panel (middle of the left column) middle-elides long paths in
// cutoff mode: the path's beginning, the directory nearest the file, and the
// filename stay; the middle directories are dropped.
func TestFilesPanelMiddleElidesPath(t *testing.T) {
	m := New(nil)
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "internal/tui/some/deep/view.go", Kind: model.KindTracked, Unstaged: 'M'},
	}}
	rows := m.statusRows(panelFiles)
	out := m.renderPanel(panelFiles, "Files", rows, nil, 32, 6)
	if !strings.Contains(out, "view.go") {
		t.Errorf("Files panel must keep the filename; got:\n%s", out)
	}
	if !strings.Contains(out, "internal") {
		t.Errorf("Files panel must keep the path's beginning; got:\n%s", out)
	}
	if !strings.Contains(out, "…/") {
		t.Errorf("Files panel must mark the dropped middle with …/; got:\n%s", out)
	}
	if !strings.Contains(out, "deep") {
		t.Errorf("Files panel should keep the dir nearest the file; got:\n%s", out)
	}
	if strings.Contains(out, "some") || strings.Contains(out, "tui/") {
		t.Errorf("Files panel should drop the MIDDLE dirs; got:\n%s", out)
	}
}

// The Staged panel (bottom of the left column) middle-elides long paths the
// same way the Files panel does.
func TestStagedPanelMiddleElidesPath(t *testing.T) {
	m := New(nil)
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "internal/tui/some/deep/view.go", Kind: model.KindTracked, Staged: 'M'},
	}}
	rows := m.statusRows(panelStaged)
	out := m.renderPanel(panelStaged, "Staged", rows, nil, 32, 6)
	if !strings.Contains(out, "view.go") {
		t.Errorf("Staged panel must keep the filename; got:\n%s", out)
	}
	if !strings.Contains(out, "internal") {
		t.Errorf("Staged panel must keep the path's beginning; got:\n%s", out)
	}
	if !strings.Contains(out, "…/") {
		t.Errorf("Staged panel must mark the dropped middle with …/; got:\n%s", out)
	}
	if strings.Contains(out, "some") || strings.Contains(out, "tui/") {
		t.Errorf("Staged panel should drop the MIDDLE dirs; got:\n%s", out)
	}
}
