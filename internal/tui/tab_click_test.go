package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TestTabLabelsByteCompatible pins the header strings to their pre-refactor form,
// so the joinTabSegs rewrite can never silently change what the panels draw (and
// the fit_test.go substring checks stay valid).
func TestTabLabelsByteCompatible(t *testing.T) {
	t.Parallel()
	cases := []struct{ got, want string }{
		{tabBarLabel(panelBranches), "[Branches] R W"},
		{tabBarLabel(panelRemotes), "B [Remotes] W"},
		{tabBarLabel(panelWorktrees), "B R [Worktrees]"},
		{filesTabLabel(panelFiles, 3, 5), "[Files 3] Tags 5"},
		{filesTabLabel(panelTags, 3, 5), "Files 3 [Tags 5]"},
		{bottomTabLabel(panelStaged, 2, 4), "[Staged 2] Reflog 4"},
		{bottomTabLabel(panelReflog, 2, 4), "Staged 2 [Reflog 4]"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("label = %q, want %q", c.got, c.want)
		}
	}
}

// TestTabSegSyncInvariant is the load-bearing check: for every slot, the column a
// tab's text occupies in joinTabSegs maps back to that exact tab via tabSegAt,
// and the single-space separators map to nothing. If the render layout and the
// hit-test ever drift, this fails.
func TestTabSegSyncInvariant(t *testing.T) {
	t.Parallel()
	slots := [][]tabSeg{
		topTabSegs(panelBranches),
		topTabSegs(panelRemotes),
		topTabSegs(panelWorktrees),
		filesTabSegs(panelFiles, 3, 5),
		filesTabSegs(panelTags, 3, 5),
		bottomTabSegs(panelStaged, 2, 4),
		bottomTabSegs(panelReflog, 2, 4),
	}
	for _, segs := range slots {
		// Independently reconstruct the per-column owner: each segment's text
		// occupies its width, then one separator column owned by nobody.
		col := 0
		for i, s := range segs {
			w := lipgloss.Width(s.text)
			for c := col; c < col+w; c++ {
				p, ok := tabSegAt(segs, c)
				if !ok || p != s.p {
					t.Errorf("%q col %d -> (%v,%v), want (%v,true)", joinTabSegs(segs), c, p, ok, s.p)
				}
			}
			col += w
			if i < len(segs)-1 { // separator space between this tab and the next
				if _, ok := tabSegAt(segs, col); ok {
					t.Errorf("%q separator col %d should map to no tab", joinTabSegs(segs), col)
				}
				col++
			}
		}
		// One past the last tab (the sort/filter decoration area) maps to nothing.
		if _, ok := tabSegAt(segs, col); ok {
			t.Errorf("%q col %d past last tab should map to no tab", joinTabSegs(segs), col)
		}
		// Negative column (left of the header) never matches.
		if _, ok := tabSegAt(segs, -1); ok {
			t.Errorf("%q col -1 should map to no tab", joinTabSegs(segs))
		}
	}
}

// TestTabClickAtGeometry verifies the click→tab mapping on a real laid-out model:
// the top slot's header sits on row pos.y+1, text starting at pos.x+2.
func TestTabClickAtGeometry(t *testing.T) {
	t.Parallel()
	m := Model{width: 90, height: 30, activeLeftTab: panelBranches}
	pos := m.layout().pos[panelBranches] // {0, 1}
	labelY := pos.y + 1
	base := pos.x + 2 // left border + Padding(0,1) left
	// "[Branches] R W": [Branches]=cols 0-9, space 10, R=11, space 12, W=13.
	cases := []struct {
		name string
		x, y int
		want panel
		ok   bool
	}{
		{"branches text", base + 0, labelY, panelBranches, true},
		{"branches end", base + 9, labelY, panelBranches, true},
		{"separator", base + 10, labelY, 0, false},
		{"remotes marker", base + 11, labelY, panelRemotes, true},
		{"worktrees marker", base + 13, labelY, panelWorktrees, true},
		{"past tabs", base + 30, labelY, 0, false},
		{"left of header", pos.x, labelY, 0, false},
		{"data row, not label", base + 0, labelY + 1, 0, false},
		{"top border, not label", base + 0, pos.y, 0, false},
	}
	for _, c := range cases {
		got, ok := m.tabClickAt(panelBranches, c.x, c.y)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%s: tabClickAt(%d,%d) = (%v,%v), want (%v,%v)", c.name, c.x, c.y, got, ok, c.want, c.ok)
		}
	}
	// Commits is not a tab slot.
	if _, ok := m.tabClickAt(panelCommits, base, labelY); ok {
		t.Error("Commits should have no clickable tabs")
	}
}

// TestTabClickAtTruncated guards the narrow-column case: when renderPanel
// truncates a header that overflows innerW (= leftW-4), the cut-off tab is not on
// screen, so its (now dead) cells must not be clickable — otherwise a click in
// the right-edge padding would switch to an invisible tab.
func TestTabClickAtTruncated(t *testing.T) {
	t.Parallel()
	// width 48 -> leftW = 48/3 = 16 -> innerW = 12. "[Branches] R W" is 14 cells,
	// so the trailing " W" (cols 12-13) is truncated away.
	m := Model{width: 48, height: 30, activeLeftTab: panelBranches}
	pos := m.layout().pos[panelBranches]
	labelY := pos.y + 1
	base := pos.x + 2
	// The visible "R" marker (col 11 < innerW) still switches.
	if got, ok := m.tabClickAt(panelBranches, base+11, labelY); !ok || got != panelRemotes {
		t.Errorf("visible R: tabClickAt = (%v,%v), want (panelRemotes,true)", got, ok)
	}
	// The truncated-away "W" marker (col 13 >= innerW=12) must not be clickable.
	if got, ok := m.tabClickAt(panelBranches, base+13, labelY); ok {
		t.Errorf("truncated W at col 13 should not be clickable, got (%v,%v)", got, ok)
	}
}

// TestActivateTab covers the shared activation path used by both ctrl+←/→ and
// mouse clicks: the right slot field is set, focus moves, the top slot updates
// lastLeftPanel, and a maximized column re-pins to the new tab.
func TestActivateTab(t *testing.T) {
	t.Parallel()
	m := Model{activeLeftTab: panelBranches}
	m = m.activateTab(panelWorktrees)
	if m.activeLeftTab != panelWorktrees || m.focus != panelWorktrees || m.lastLeftPanel != panelWorktrees {
		t.Errorf("top slot: activeLeftTab=%v focus=%v lastLeftPanel=%v", m.activeLeftTab, m.focus, m.lastLeftPanel)
	}
	m = m.activateTab(panelTags)
	if m.activeFilesTab != panelTags || m.focus != panelTags {
		t.Errorf("middle slot: activeFilesTab=%v focus=%v", m.activeFilesTab, m.focus)
	}
	m = m.activateTab(panelReflog)
	if m.activeBottomTab != panelReflog || m.focus != panelReflog {
		t.Errorf("bottom slot: activeBottomTab=%v focus=%v", m.activeBottomTab, m.focus)
	}
	// A maximized column re-pins to the newly activated tab.
	m = Model{leftMaxed: true, leftMax: panelBranches}
	m = m.activateTab(panelRemotes)
	if m.leftMax != panelRemotes {
		t.Errorf("maximized column did not re-pin: leftMax=%v, want panelRemotes", m.leftMax)
	}
}

// TestMouseClickSwitchesTab exercises the full path: a left-click on the Remotes
// marker in the top tab bar switches to and focuses the Remotes tab; a click on a
// data row below does not switch tabs.
func TestMouseClickSwitchesTab(t *testing.T) {
	t.Parallel()
	m := Model{width: 90, height: 30, activeLeftTab: panelBranches, focus: panelCommits, sel: map[panel]int{}}
	pos := m.layout().pos[panelBranches]
	x := pos.x + 2 + 11 // the "R" marker
	y := pos.y + 1      // the tab-bar row
	u, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
	nm := u.(Model)
	if nm.activeLeftTab != panelRemotes || nm.focus != panelRemotes {
		t.Fatalf("click on Remotes tab: activeLeftTab=%v focus=%v, want panelRemotes", nm.activeLeftTab, nm.focus)
	}

	// A click on a data row (not the tab bar) leaves the active tab unchanged
	// and just focuses the panel.
	m2 := Model{width: 90, height: 30, activeLeftTab: panelBranches, focus: panelCommits, sel: map[panel]int{}}
	u2, _ := m2.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: pos.x + 2, Y: pos.y + 3})
	nm2 := u2.(Model)
	if nm2.activeLeftTab != panelBranches {
		t.Errorf("data-row click changed active tab to %v", nm2.activeLeftTab)
	}
	if nm2.focus != panelBranches {
		t.Errorf("data-row click did not focus the panel: focus=%v", nm2.focus)
	}
}
