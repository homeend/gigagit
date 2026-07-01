package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/model"
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
	m.sel[panelWorktrees] = 0      // …and selected (selInWin == 0)
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

// The selected row under the reveal is drawn in reverse video padded across the
// panel's inner width. A reveal only as wide as a short full-text would leave
// that highlight peeking out to its right (two backgrounds on one line). The
// reveal must be padded to cover the whole row — at least the panel inner width.
func TestTooltipFillsSelectedRowHighlight(t *testing.T) {
	m := footerModel() // width 120; Commits is the right panel
	m.focus = panelCommits
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	// Trimmed-branch reveal whose full text is shorter than the panel's row width.
	m.commits = []model.Commit{{Hash: "abcdef0", Subject: "added tzName to item metadata",
		Refs: []model.Ref{{Name: "features/new-data-source-v2", Kind: model.RefLocal}}}}
	m.sel[panelCommits] = 0

	lines, _, _, ok := m.tooltip()
	if !ok {
		t.Fatal("want a tooltip")
	}
	g := m.layout()
	innerW := g.rightW - 4 // mirrors renderPanel: border (2) + padding (2)
	if w := ansi.StringWidth(lines[0]); w < innerW {
		t.Errorf("reveal is %d cols, must fill at least the panel inner width %d so the "+
			"selected-row highlight does not peek out", w, innerW)
	}
}

// A right-hand panel (Commits) has little room to its right, so a row wider than
// that room — but narrower than the whole TERMINAL — must overflow LEFT, over the
// panels to its left, and still show its full text (not clip). The reveal may use
// the whole terminal width, not just the commit window; its start column shifts
// left of the panel's content edge.
func TestTooltipOverflowsLeftFromRightPanel(t *testing.T) {
	m := footerModel() // width 120; Commits is the right panel
	m.focus = panelCommits
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	// A subject wider than the Commits panel's room-to-the-right (~half the
	// screen) but well within the full 120-col width. (The compact text-only
	// reveal carries no graph/identity padding, so the subject itself must be
	// long enough to overflow the right edge and force the leftward shift.)
	subj := "channel api rework: split the v2 source adapter into separate reader and writer halves cleanly"
	m.commits = []model.Commit{{Hash: "abcdef0", Subject: subj}}
	m.sel[panelCommits] = 0

	lines, x, _, ok := m.tooltip()
	if !ok {
		t.Fatal("want a tooltip for the truncated commit row")
	}
	plain := ansi.Strip(lines[0])
	if !strings.Contains(plain, subj) {
		t.Fatalf("reveal must show the full subject (no clip), got %q", plain)
	}
	if strings.HasSuffix(strings.TrimRight(plain, " "), "…") {
		t.Errorf("a row that fits the terminal must not be clipped with …, got %q", plain)
	}
	contentEdge := m.layout().pos[panelCommits].x + 2
	if x >= contentEdge {
		t.Errorf("reveal x = %d, want < content edge %d (shifted left over the panels)", x, contentEdge)
	}
}

// treeRevealModel opens the commit files tree (a separate left-column slot) with
// a long directory heading selected and the tree side focused, so the reveal
// must show the full path the truncated row hides.
func treeRevealModel(sel int) Model {
	m := filesModel() // width 80
	m.filesTitle = "Files a190f4b changes"
	m.filesHash = "a190f4b"
	m.filesView = &contentPopup{
		lines: []contentLine{
			{text: "M  jooq-gen.bat"},
			{text: treeLongDir, heading: true},
			{text: "  M  AbstractUtils.java", path: "x/AbstractUtils.java"},
		},
		sel:  sel,
		mode: modeCutoff,
	}
	m.filesTreeFocused = true
	return m
}

const treeLongDir = "src/com/netstellar/feedmill/db/dao/util/"

func TestFilesTreeRevealsTruncatedRow(t *testing.T) {
	m := treeRevealModel(1) // the long heading
	lines, x, y, ok := m.tooltip()
	if !ok {
		t.Fatal("want a reveal for the truncated tree heading")
	}
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(plain, treeLongDir) {
		t.Fatalf("reveal must show the full dir path, got %q", plain)
	}
	if x != 2 {
		t.Errorf("reveal x = %d, want 2 (the left column's content edge)", x)
	}
	if y != 4 { // box top (1) + border + title = 3, plus selInWin (1)
		t.Errorf("reveal y = %d, want 4 (the heading's own line)", y)
	}
}

func TestFilesTreeRevealHiddenWhenRowFits(t *testing.T) {
	m := treeRevealModel(0) // "M  jooq-gen.bat" fits the column
	if _, _, _, ok := m.tooltip(); ok {
		t.Fatal("no reveal when the tree row fits")
	}
}

func TestFilesTreeRevealSuppressedWhenListFocused(t *testing.T) {
	m := treeRevealModel(1)
	m.filesTreeFocused = false // the commits side owns the selection now
	// A long commit subject so the commits-side reveal DOES fire — the test then
	// proves the tree's path is absent regardless of the panel reveal.
	m.commits = []model.Commit{{Hash: "1111111aaaa",
		Subject: "one — a deliberately long commit subject to trigger the commits-side reveal"}}
	lines, _, _, _ := m.tooltip()
	if strings.Contains(ansi.Strip(strings.Join(lines, "\n")), treeLongDir) {
		t.Fatal("the tree's path must not be revealed while the list side is focused")
	}
}

// The render() path only reaches the reveal overlay on the non-layer/non-diff
// branch; assert the tree reveal survives the assembled view, not just tooltip().
func TestFilesTreeRevealRenderedInView(t *testing.T) {
	m := treeRevealModel(1)
	if !strings.Contains(ansi.Strip(m.render()), treeLongDir) {
		t.Fatal("rendered view must composite the tree reveal's full path")
	}
}

func TestFilesTreeRevealSuppressedOutsideCutoff(t *testing.T) {
	m := treeRevealModel(1)
	m.filesView.mode = modeWrap // the wrapped row is already fully visible
	if _, _, _, ok := m.tooltip(); ok {
		t.Fatal("no reveal in wrap mode")
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
	// The … must sit a margin short of the right edge, not hug it: the visible
	// text is trimmed to terminal width − revealClipMargin.
	text := strings.TrimRight(only, " ")
	if tw := lipgloss.Width(text); tw > m.width-revealClipMargin {
		t.Errorf("clipped text is %d cols, want ≤ terminal width − margin (%d)", tw, m.width-revealClipMargin)
	}
	// The highlight must HUG the clipped text — no trailing blank yellow padding it
	// out to the full screen width. The strip is sized to its text, so the line has
	// no trailing spaces and is strictly narrower than the terminal.
	if strings.HasSuffix(only, " ") {
		t.Errorf("clipped reveal must not trail blank filler to the edge, got %q", only)
	}
	if w := ansi.StringWidth(lines[0]); w >= m.width {
		t.Errorf("clipped reveal is %d cols, must be narrower than the terminal (%d) — sized to its text, not the full screen", w, m.width)
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

// In file-preview mode the right column is the preview pager, not the Commits
// panel. The hidden commit row behind it must not surface its reveal — a long
// subject that shifts left and lands over the file tree (the reported bug). The
// pair shares one model and differs only in filesPreview, so a vacuous pass
// (nothing truncates / degenerate geometry) can't masquerade as a fix.
func TestTooltipSuppressedByFilePreview(t *testing.T) {
	base := footerModel() // width 120; Commits is the right panel
	base.focus = panelCommits
	if base.sel == nil {
		base.sel = map[panel]int{}
	}
	const subj = "Merge tag 'firewire-updates-7.2' of git://git.kernel.org/pub/scm/linux/kernel/git/ieee1394/linux1394"
	base.commits = []model.Commit{{Hash: "aff3ca3aaaa", Subject: subj}}
	base.sel[panelCommits] = 0

	// Precondition: with no preview the long commit row DOES reveal. This proves
	// the geometry is live and the reveal machinery fires, so the suppressed case
	// below isolates the guard (not a row that simply never truncates).
	lines, _, _, ok := base.tooltip()
	if !ok {
		t.Fatal("precondition: the long commit row must reveal when no preview is open")
	}
	if plain := ansi.Strip(strings.Join(lines, "\n")); !strings.Contains(plain, "firewire-updates") {
		t.Fatalf("precondition: reveal should show the commit subject, got %q", plain)
	}

	// Same model, but the file preview now owns the right column (tree NOT focused,
	// so the tree-path reveal branch is skipped too).
	m := base
	m.filesView = &contentPopup{lines: []contentLine{{text: "M  mod_devicetable.h", path: "mod_devicetable.h"}}, mode: modeCutoff}
	m.filesPreview = &contentPopup{title: "mod_devicetable.h", lines: []contentLine{{text: "..."}}}
	m.filesTreeFocused = false
	if _, _, _, ok := m.tooltip(); ok {
		t.Fatal("the commit reveal must be suppressed while the file preview owns the right column")
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

// The commit reveal must show only the readable text — the full branch label +
// subject — with NO graph glyphs and NO fixed-width padding gap. (The graph is
// positional; revealing its lanes in a horizontal strip is meaningless, and the
// 16-col identity padding leaves an ugly gap between a short branch and the
// subject.)
func TestCommitRevealIsTextOnlyNoGraph(t *testing.T) {
	m := footerModel()
	m.focus = panelCommits
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	subject := "mfd: MAINTAINERS: Remove a really long subject line that overflows the panel width by quite a lot indeed yes"
	m.commits = []model.Commit{{Hash: "abcdef0", Subject: subject,
		Refs: []model.Ref{{Name: "master", Kind: model.RefLocal, Head: true}}}}
	m = m.rebuildCommitGraph()
	m.sel[panelCommits] = 0
	lines, _, _, ok := m.tooltip()
	if !ok {
		t.Fatal("expected a reveal for the width-truncated commit row")
	}
	plain := ansi.Strip(strings.Join(lines, "\n"))
	for _, g := range []string{"●", "│", "┼", "╮", "╯", "╭", "╰", "⋯"} {
		if strings.Contains(plain, g) {
			t.Errorf("reveal must not contain graph glyph %q: %q", g, plain)
		}
	}
	// Branch label immediately followed by a single space then the subject — no
	// fixed-width padding gap.
	if !strings.Contains(plain, "master mfd:") {
		t.Errorf("reveal should be compact 'branch subject' with no padding gap: %q", plain)
	}
}

// The branch-identity column sizes to the widest loaded branch label (capped at
// commitIdentW), so a short common name like "master" leaves no fixed-width gap.
func TestCommitIdentColumnFitsToLongestName(t *testing.T) {
	m := footerModel()
	m.commits = []model.Commit{
		{Hash: "aaaaaa0", Subject: "s0", Refs: []model.Ref{{Name: "master", Kind: model.RefLocal}}},
		{Hash: "bbbbbb0", Subject: "s1", Refs: []model.Ref{{Name: "dev", Kind: model.RefLocal}}},
	}
	if w := m.commitIdentWidth(); w != lipgloss.Width("master") {
		t.Fatalf("ident width = %d, want %d (widest loaded label)", w, lipgloss.Width("master"))
	}
	rows := m.commitRows()
	// "master" row: label padded only to 6 → exactly one space before the subject.
	if !strings.Contains(rows[0], "master s0") {
		t.Errorf("short-name row should have no padding gap: %q", rows[0])
	}
	// A name longer than the cap still trims and the column caps at commitIdentW.
	m.commits = append(m.commits, model.Commit{Hash: "cccccc0", Subject: "s2",
		Refs: []model.Ref{{Name: "b/this-name-is-way-too-long-to-fit", Kind: model.RefLocal}}})
	if w := m.commitIdentWidth(); w != commitIdentW {
		t.Fatalf("ident width = %d, want cap %d when a long name is loaded", w, commitIdentW)
	}
}
