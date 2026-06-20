package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gigagit/gg/internal/model"
)

const longPath = "/very/long/path/that/will/definitely/not/fit/in/a/narrow/panel"

// tooltipModel: 80-col terminal → left panels ~22 cols inner, so the second
// worktree row (79 chars) is guaranteed to truncate, while row 0 ("* main  /repo")
// fits comfortably (16 chars < innerW=22), and the full row fits on one tooltip
// line (79 < g.w=80) so longPath is never split across lines.
func tooltipModel() Model {
	return Model{
		width: 80, height: 24,
		focus:           panelWorktrees,
		activeLeftTab:   panelWorktrees, // Worktrees must be the active tab to be visible
		sel:             map[panel]int{panelWorktrees: 1},
		currentWorktree: "/repo",
		worktrees: []model.Worktree{
			{Path: "/repo", Branch: "main"},
			{Path: longPath, Branch: "feature/login"},
		},
	}
}

func TestTooltipShowsFullRowAboveSelection(t *testing.T) {
	m := tooltipModel()
	lines, _, y, ok := m.tooltip()
	if !ok {
		t.Fatal("want a tooltip for the truncated selected row")
	}
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(plain, longPath) {
		t.Fatalf("tooltip must contain the full path, got %q", plain)
	}
	g := m.layout()
	_, selInWin, _ := windowRows(mustRows(t, m, panelWorktrees), g.boxH[panelWorktrees]-3, 1)
	rowY := g.pos[panelWorktrees].y + 2 + selInWin
	if want := rowY - len(lines); y != want {
		t.Errorf("tooltip y = %d, want %d (directly above row line %d)", y, want, rowY)
	}
}

func mustRows(t *testing.T, m Model, p panel) []string {
	t.Helper()
	rows, _ := m.panelView(p)
	if len(rows) == 0 {
		t.Fatal("expected rows")
	}
	return rows
}

func TestTooltipHiddenWhenRowFits(t *testing.T) {
	m := tooltipModel()
	m.sel[panelWorktrees] = 0 // "* main  /repo" fits comfortably
	if _, _, _, ok := m.tooltip(); ok {
		t.Fatal("no tooltip when the selected row fits")
	}
}

func TestTooltipHiddenOnEmptyPanel(t *testing.T) {
	m := tooltipModel()
	m.worktrees = nil
	if _, _, _, ok := m.tooltip(); ok {
		t.Fatal("no tooltip for an empty panel")
	}
}

func TestTooltipWrapsAndCaps(t *testing.T) {
	m := tooltipModel()
	m.worktrees[1].Path = strings.Repeat("x", 500) // wider than 3 terminal lines
	lines, _, _, ok := m.tooltip()
	if !ok {
		t.Fatal("want a tooltip")
	}
	if len(lines) != 3 {
		t.Fatalf("want 3 wrapped lines, got %d", len(lines))
	}
	last := ansi.Strip(lines[len(lines)-1])
	if !strings.HasSuffix(strings.TrimRight(last, " "), "…") {
		t.Errorf("capped tooltip must end with …, got %q", last)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > m.width {
			t.Errorf("line %d is %d cols, wider than the terminal (%d)", i, w, m.width)
		}
	}
}

func TestTooltipRenderedInView(t *testing.T) {
	m := tooltipModel()
	out := ansi.Strip(m.render())
	if !strings.Contains(out, longPath) {
		t.Fatal("rendered view must contain the tooltip's full path")
	}
}

func TestTooltipSuppressedByModal(t *testing.T) {
	m := tooltipModel()
	m.modal = &decisionState{} // minimal valid value — render() early-returns on modal != nil
	out := ansi.Strip(m.render())
	if strings.Contains(out, longPath) {
		t.Fatal("modal view must not contain the tooltip")
	}
}

func TestTooltipSuppressedByPopup(t *testing.T) {
	m := tooltipModel()
	m = m.pushOverlay(&repoPopup{}) // any open popup owns the screen
	out := ansi.Strip(m.render())
	if strings.Contains(out, longPath) {
		t.Fatal("popup view must not contain the tooltip")
	}
}

func TestWrapWidth(t *testing.T) {
	got := wrapWidth("abcdef", 3, 3)
	if len(got) != 2 || got[0] != "abc" || got[1] != "def" {
		t.Fatalf("wrapWidth = %q", got)
	}
	capped := wrapWidth(strings.Repeat("a", 10), 3, 2)
	if len(capped) != 2 || !strings.HasSuffix(capped[1], "…") {
		t.Fatalf("capped wrap = %q", capped)
	}
}

func TestTooltipY(t *testing.T) {
	if y := tooltipY(5, 2); y != 3 {
		t.Errorf("tooltipY(5,2) = %d, want 3 (above)", y)
	}
	if y := tooltipY(1, 3); y != 2 {
		t.Errorf("tooltipY(1,3) = %d, want 2 (flips below)", y)
	}
}

func TestTooltipSuppressedByContentPopup(t *testing.T) {
	m := tooltipModel()
	m.contentPopup = newContentPopup("T", contentLines(2))
	out := ansi.Strip(m.render())
	if strings.Contains(out, longPath) {
		t.Fatal("content popup view must not contain the tooltip")
	}
}

func TestTooltipSuppressedOutsideCutoffMode(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.focus = panelBranches
	m.branches = []model.Branch{{Name: strings.Repeat("x", 80)}}
	// cutoff (default): the long selected row is truncated -> tooltip appears.
	if _, _, _, ok := m.tooltip(); !ok {
		t.Fatal("cutoff mode: expected a reveal tooltip for the truncated row")
	}
	// wrap: the row is fully visible across wrapped lines -> no tooltip.
	m.dispModes[panelBranches] = modeWrap
	if _, _, _, ok := m.tooltip(); ok {
		t.Error("wrap mode: tooltip must be suppressed (row already visible)")
	}
	// scroll: the user pans to read the row -> no tooltip.
	m.dispModes[panelBranches] = modeScroll
	if _, _, _, ok := m.tooltip(); ok {
		t.Error("scroll mode: tooltip must be suppressed")
	}
}
