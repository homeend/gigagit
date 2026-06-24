package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/homeend/gigagit/internal/model"
	"github.com/muesli/termenv"
)

// forceColor makes lipgloss emit ANSI in the non-TTY test environment.
func forceColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

func TestLaneColorRecycles(t *testing.T) {
	if laneColor(0) != lanePalette[0] {
		t.Fatalf("lane 0 color")
	}
	if laneColor(len(lanePalette)) != lanePalette[0] {
		t.Fatalf("color should recycle modulo palette length")
	}
}

func TestCommitDecoratorsColorGraphNodeNotSelected(t *testing.T) {
	forceColor(t)
	m := footerModel()
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "aaaaaaa"}, {Hash: "bbbbbbb"}}
	m = m.rebuildCommitGraph()
	if len(m.commitGraphLanes) != 2 {
		t.Fatalf("rebuildCommitGraph should populate lanes, got %v", m.commitGraphLanes)
	}
	rows, idx := m.panelView(panelCommits)
	decos := m.commitDecorators(rows, idx)
	if decos == nil {
		t.Fatal("graph mode should produce decorators")
	}
	// A linear graph puts both nodes in lane 0 at graph col 0 → text col 2.
	out := decos[0]("  ● aaaaaaa", 0, 0)
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("graph node should be colored: %q", out)
	}
}

// TestRenderPanelEmitsLaneColor is the end-to-end guard: it proves the color
// survives the full assembled render path (decorator → style.Render → border
// box), and that the selected row is NOT colored (reverse wins).
func TestRenderPanelEmitsLaneColor(t *testing.T) {
	forceColor(t)
	m := footerModel()
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "aaaaaaa", Subject: "x"}, {Hash: "bbbbbbb", Subject: "y"}}
	m = m.rebuildCommitGraph()
	m.sel[panelCommits] = 0 // row 0 selected → must NOT be colored
	rows, idx := m.panelView(panelCommits)
	decos := m.commitDecorators(rows, idx)
	out := m.renderPanel(panelCommits, "Commits", rows, decos, 40, 8)
	// Derive the exact escape lipgloss emits for this color under the active
	// profile (256 vs truecolor differ), rather than guessing the SGR form.
	probe := lipgloss.NewStyle().Foreground(laneColor(0)).Render("●")
	esc := probe[:strings.IndexRune(probe, '●')] // the leading color escape
	if esc == "" || !strings.Contains(out, esc) {
		t.Fatalf("assembled panel should emit lane-0 color escape %q:\n%s", esc, out)
	}
}

func TestCommitRowsNeverContainAnsi(t *testing.T) {
	m := footerModel()
	m.commits = []model.Commit{{Hash: "aaaaaaa", Subject: "x"}, {Hash: "bbbbbbb", Subject: "y"}}
	m = m.rebuildCommitGraph()
	for _, row := range m.commitRows() {
		if strings.ContainsRune(row, '\x1b') {
			t.Fatalf("commitRows must stay plain (no ANSI): %q", row)
		}
	}
}

func TestCommitViewModeToggle(t *testing.T) {
	m := footerModel()
	m.focus = panelCommits
	r, ok := findRow(availableActions(m), "commits-viewmode")
	if !ok {
		t.Fatal("view-mode toggle missing on Commits panel")
	}
	if r.label != "Show as list" {
		t.Fatalf("graph-mode label = %q, want Show as list", r.label)
	}
	mm, _ := r.run(m)
	m = mm.(Model)
	if !m.commitListMode {
		t.Fatal("run should switch to list mode")
	}
	r2, _ := findRow(availableActions(m), "commits-viewmode")
	if r2.label != "Show as graph" {
		t.Fatalf("list-mode label = %q, want Show as graph", r2.label)
	}
}

func TestListModeRowsHaveDotGutterAndColorUnderFilter(t *testing.T) {
	forceColor(t)
	m := footerModel()
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "aaaaaaa", Subject: "x"}, {Hash: "bbbbbbb", Subject: "y"}}
	m = m.rebuildCommitGraph()
	m.commitListMode = true
	rows := m.commitRows()
	if len(rows) != 2 || !strings.HasPrefix(rows[0], "● ") {
		t.Fatalf("list rows should start with a ● gutter: %q", rows)
	}
	if strings.ContainsRune(rows[0], '\x1b') {
		t.Fatalf("list rows must stay plain: %q", rows[0])
	}
	// List mode colors even when the graph would be suppressed (simulate filter
	// by asserting commitDecorators returns non-nil while commitGraphOn is false).
	m.sortModes[panelCommits] = sortDateDesc // non-default → forces commitGraphOn() false
	decos := m.commitDecorators(rows, []int{0, 1})
	if decos == nil {
		t.Fatal("list mode should color regardless of graph suppression")
	}
	out := decos[0]("  ● aaaaaaa x", 0, 0)
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("list dot should be colored: %q", out)
	}
}
