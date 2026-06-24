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
	if !strings.HasPrefix(got, "M i") {
		t.Errorf("got %q, want the glyph %q AND the path's beginning kept", got, "M ")
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

// elideFileMiddle fills the column with the path head, then "…/<filename>".
func TestElideFileMiddle(t *testing.T) {
	// "aa/bb/cc/dd/view.go" is 19 cols; at 16 the dirs nearest the file go.
	got := elideFileMiddle("aa/bb/cc/dd/view.go", 16)
	if w := lipgloss.Width(got); w > 16 {
		t.Errorf("got %q width %d, want <= 16", got, w)
	}
	if !strings.HasPrefix(got, "aa/") {
		t.Errorf("got %q, want the path's beginning kept", got)
	}
	if !strings.HasSuffix(got, "…/view.go") {
		t.Errorf("got %q, want it to end with …/<filename>", got)
	}
	// A bare filename (no directory) has no middle to elide.
	if got := elideFileMiddle("averylongfilename.go", 8); lipgloss.Width(got) > 8 {
		t.Errorf("bare filename: got %q overflows 8", got)
	}
}

// The Files panel (middle of the left column) middle-elides long paths in
// cutoff mode: the path's beginning and the filename stay; the directories
// nearest the file are dropped.
func TestFilesPanelMiddleElidesPath(t *testing.T) {
	m := New(nil)
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "internal/tui/some/deep/view.go", Kind: model.KindTracked, Unstaged: 'M'},
	}}
	rows := m.statusRows(panelFiles)
	out := m.renderPanel(panelFiles, "Files", rows, nil, 28, 6)
	if !strings.Contains(out, "view.go") {
		t.Errorf("Files panel must keep the filename; got:\n%s", out)
	}
	if !strings.Contains(out, "internal/tu") {
		t.Errorf("Files panel must keep the path's beginning; got:\n%s", out)
	}
	if !strings.Contains(out, "…/") {
		t.Errorf("Files panel must mark the dropped middle with …/; got:\n%s", out)
	}
	if strings.Contains(out, "deep") || strings.Contains(out, "some") {
		t.Errorf("Files panel should drop the dirs nearest the file; got:\n%s", out)
	}
}

// Scope guard: the change is Files-only. The Staged panel below keeps the old
// tail-truncation (basename falls off the right edge at the same narrow width).
func TestStagedPanelUnaffected(t *testing.T) {
	m := New(nil)
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "internal/tui/some/deep/view.go", Kind: model.KindTracked, Staged: 'M'},
	}}
	rows := m.statusRows(panelStaged)
	out := m.renderPanel(panelStaged, "Staged", rows, nil, 20, 6)
	if strings.Contains(out, "view.go") {
		t.Errorf("Staged panel should still tail-truncate (basename dropped); got:\n%s", out)
	}
}
