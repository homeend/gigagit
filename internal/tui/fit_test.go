package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/model"
)

// The rendered frame must never exceed the terminal: at most height lines, and
// no line wider than width — even with far more commits than fit and an
// over-long subject. This guards against the overflow that broke the layout.
func TestRenderNeverExceedsTerminal(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 80, 24

	// Many commits (must be windowed, not dumped) and a too-wide subject.
	m.commits = make([]model.Commit, 100)
	for i := range m.commits {
		m.commits[i] = model.Commit{Hash: "abcdef0123", Subject: "commit subject"}
	}
	m.commits[0].Subject = strings.Repeat("x", 400)

	out := m.View()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > m.height {
		t.Fatalf("render produced %d lines, want <= %d", len(lines), m.height)
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w > m.width {
			t.Fatalf("line %d is %d cols wide, want <= %d: %q", i, w, m.width, ln)
		}
	}
}

// A tiny terminal must still render without panic and stay within bounds.
func TestRenderTinyTerminal(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 20, 8
	out := m.View()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w > m.width {
			t.Fatalf("line %d is %d cols wide, want <= %d", i, w, m.width)
		}
	}
}

// The 3-panel left column must also respect terminal bounds, even with many
// worktrees and a medium height where each left panel is near its 3-row floor.
func TestRenderThreePanelLeftFits(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 60, 12 // bodyH = 9 -> three left panels of 3 rows each

	m.worktrees = make([]model.Worktree, 40)
	for i := range m.worktrees {
		m.worktrees[i] = model.Worktree{Path: "/very/long/path/to/worktree/number", Branch: "branch-name"}
	}
	m.branches = make([]model.Branch, 40)
	for i := range m.branches {
		m.branches[i] = model.Branch{Name: "branch-name"}
	}

	out := m.View()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > m.height {
		t.Fatalf("render produced %d lines, want <= %d", len(lines), m.height)
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w > m.width {
			t.Fatalf("line %d is %d cols wide, want <= %d: %q", i, w, m.width, ln)
		}
	}
}

// A visible tooltip must not push the frame beyond the terminal bounds.
func TestRenderWithTooltipStaysInBounds(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 50, 24
	m.focus = panelWorktrees
	m.worktrees = []model.Worktree{
		{Path: "/repo", Branch: "main"},
		{Path: "/" + strings.Repeat("deep/", 40) + "end", Branch: "feature/x"},
	}
	m.sel[panelWorktrees] = 1

	out := m.View()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > m.height {
		t.Fatalf("render produced %d lines, want <= %d", len(lines), m.height)
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w > m.width {
			t.Fatalf("line %d is %d cols wide, want <= %d: %q", i, w, m.width, ln)
		}
	}
}

// The running spinner (⏳, a 2-column glyph) must not push the status line one
// column past the terminal edge: truncate measures display columns, not runes.
func TestRenderRunningSpinnerStatusFits(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 80, 24
	m.running = true
	m.statusMsg = strings.Repeat("x", 300)

	out := m.View()
	for i, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if w := lipgloss.Width(ln); w > m.width {
			t.Fatalf("line %d is %d cols wide, want <= %d: %q", i, w, m.width, ln)
		}
	}
}

func TestRenderPanelWrapModeExpandsRow(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.focus = panelBranches
	m.dispModes[panelBranches] = modeWrap
	m.branches = []model.Branch{{Name: strings.Repeat("x", 60)}}
	out := m.renderPanel(panelBranches, "Branches", m.branchRows(), nil, 20, 6)
	// In wrap mode the 60-char branch name occupies more than one body line.
	if strings.Count(out, "x") < 30 {
		t.Errorf("wrap mode did not expand the long row:\n%s", out)
	}
}

func TestRenderPanelCutoffStaysOneLine(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.focus = panelBranches
	m.branches = []model.Branch{{Name: strings.Repeat("x", 60)}}
	out := m.renderPanel(panelBranches, "Branches", m.branchRows(), nil, 20, 6)
	// Default cutoff: the row is truncated to one line (innerW=16) -> few x's.
	if strings.Count(out, "x") > 16 {
		t.Errorf("cutoff mode should truncate, got too many chars:\n%s", out)
	}
}

func TestLayoutTabbedLeftColumn(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	g := m.layout()
	if g.boxH[panelBranches] <= 0 {
		t.Error("active Branches tab box missing")
	}
	if g.boxH[panelFiles] <= 0 {
		t.Error("Status box missing")
	}
	if g.boxH[panelWorktrees] != 0 {
		t.Errorf("inactive Worktrees tab must be hidden, got boxH %d", g.boxH[panelWorktrees])
	}
	if g.pos[panelFiles].y != g.pos[panelBranches].y+g.boxH[panelBranches] {
		t.Error("Status is not positioned directly below the tab slot")
	}
	m.activeLeftTab = panelWorktrees
	g = m.layout()
	if g.boxH[panelWorktrees] <= 0 || g.boxH[panelBranches] != 0 {
		t.Error("switching activeLeftTab did not move the visible box")
	}
}

func TestRenderShowsActiveTabBar(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	out := m.renderInterface()
	if !strings.Contains(out, "[Branches] R W") {
		t.Errorf("tab bar (Branches active) missing:\n%s", out)
	}
	m.activeLeftTab = panelRemotes
	out = m.renderInterface()
	if !strings.Contains(out, "B [Remotes] W") {
		t.Errorf("tab bar (Remotes active) missing:\n%s", out)
	}
	// Populated Remotes tab: the ref list must appear in the panel body.
	m.remoteBranches = []model.RemoteBranch{{Name: "origin/main", Remote: "origin", Branch: "main"}}
	out = m.renderInterface()
	if !strings.Contains(out, "origin/main") {
		t.Errorf("Remotes tab body missing the ref:\n%s", out)
	}
	m.activeLeftTab = panelWorktrees
	out = m.renderInterface()
	if !strings.Contains(out, "B R [Worktrees]") {
		t.Errorf("tab bar (Worktrees active) missing:\n%s", out)
	}
}

func TestRemoteRowsContent(t *testing.T) {
	m := New(nil)
	m.remoteBranches = []model.RemoteBranch{
		{Name: "origin/main", Remote: "origin", Branch: "main"},
		{Name: "origin/feature/x", Remote: "origin", Branch: "feature/x"},
	}
	rows := m.remoteRows()
	if len(rows) != 2 || !strings.Contains(rows[0], "origin/main") || !strings.Contains(rows[1], "origin/feature/x") {
		t.Fatalf("remoteRows = %#v", rows)
	}
}

func TestTabBarLabelThreeWay(t *testing.T) {
	if got := tabBarLabel(panelRemotes); !strings.Contains(got, "[Remotes]") {
		t.Fatalf("active Remotes: %q", got)
	}
	if got := tabBarLabel(panelBranches); !strings.Contains(got, "[Branches]") || !strings.Contains(got, " R ") {
		t.Fatalf("active Branches: %q", got)
	}
	if got := tabBarLabel(panelWorktrees); !strings.Contains(got, "[Worktrees]") {
		t.Fatalf("active Worktrees: %q", got)
	}
}

func TestBranchRowsBehindIndicator(t *testing.T) {
	m := New(nil)
	m.branches = []model.Branch{
		{Name: "feature", Behind: 3},
		{Name: "clean", Behind: 0},
	}
	rows := m.branchRows()
	if !strings.Contains(rows[0], "(↓3)") {
		t.Fatalf("behind row missing indicator: %q", rows[0])
	}
	if strings.Contains(rows[1], "↓") {
		t.Fatalf("clean row should have no indicator: %q", rows[1])
	}
}
