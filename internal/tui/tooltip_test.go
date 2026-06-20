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

func TestTooltipShowsFullRowInline(t *testing.T) {
	m := tooltipModel()
	m.width = 120 // wide enough that the full row fits when overflowing to the screen edge
	lines, _, y, ok := m.tooltip()
	if !ok {
		t.Fatal("want a tooltip for the truncated selected row")
	}
	if len(lines) != 1 {
		t.Fatalf("inline reveal is a single line, got %d", len(lines))
	}
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(plain, longPath) {
		t.Fatalf("tooltip must contain the full path, got %q", plain)
	}
	g := m.layout()
	_, selInWin, _ := windowRows(mustRows(t, m, panelWorktrees), g.boxH[panelWorktrees]-3, 1)
	rowY := g.pos[panelWorktrees].y + 2 + selInWin
	if y != rowY {
		t.Errorf("tooltip y = %d, want %d (on the row's own line)", y, rowY)
	}
}

// The filed bug: the FIRST visible row (selInWin == 0) selected and truncated.
// The old floating strip placed itself at rowY-n, landing on the panel's border
// (origin.y) and label (origin.y+1) lines — covering the title bar. The inline
// reveal must land on the row itself (origin.y+2), leaving the bar untouched.
func TestTooltipOnTopRowKeepsTitleBar(t *testing.T) {
	m := tooltipModel()
	m.width = 120
	m.worktrees[0].Path = longPath // make the top row long…
	m.sel[panelWorktrees] = 0       // …and selected (selInWin == 0)
	_, _, y, ok := m.tooltip()
	if !ok {
		t.Fatal("want a tooltip for the truncated top row")
	}
	origin := m.layout().pos[panelWorktrees]
	if y != origin.y+2 {
		t.Fatalf("reveal y = %d, want %d (the row line); it must not touch the border %d or label %d",
			y, origin.y+2, origin.y, origin.y+1)
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

// A trimmed branch-name column reveals the full name via the tooltip even when
// the row otherwise fits (the displayed row carries the … so the plain
// truncation check alone would miss it).
func TestTooltipRevealsTrimmedBranchName(t *testing.T) {
	m := footerModel()
	m.focus = panelCommits
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	long := "b/from-feat-cherry-pick-very-long" // > commitIdentW
	m.commits = []model.Commit{{Hash: "abcdef0", Subject: "x",
		Refs: []model.Ref{{Name: long, Kind: model.RefLocal}}}}
	m.sel[panelCommits] = 0
	lines, _, _, ok := m.tooltip()
	if !ok {
		t.Fatal("a trimmed branch name should reveal a tooltip even if the row fits")
	}
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(plain, long) {
		t.Fatalf("tooltip must show the full branch name, got %q", plain)
	}
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

func TestTooltipOverflowsSingleLineCappedAtScreen(t *testing.T) {
	m := tooltipModel()
	m.worktrees[1].Path = strings.Repeat("x", 500) // wider than the whole screen
	lines, _, _, ok := m.tooltip()
	if !ok {
		t.Fatal("want a tooltip")
	}
	if len(lines) != 1 {
		t.Fatalf("inline reveal stays a single line, got %d", len(lines))
	}
	only := ansi.Strip(lines[0])
	if !strings.HasSuffix(strings.TrimRight(only, " "), "…") {
		t.Errorf("a reveal too wide for the screen must end with …, got %q", only)
	}
	if w := ansi.StringWidth(lines[0]); w > m.width {
		t.Errorf("reveal is %d cols, wider than the terminal (%d)", w, m.width)
	}
}

func TestTooltipRenderedInView(t *testing.T) {
	m := tooltipModel()
	m.width = 120 // the inline reveal overflows to the screen edge; give it room for the full path
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
	m = m.pushLayer(&repoPopup{}) // any open popup owns the screen
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

func TestTooltipSuppressedByContentPopup(t *testing.T) {
	m := tooltipModel()
	m = m.pushLayer(newContentPopup("T", contentLines(2)))
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
