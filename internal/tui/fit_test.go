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
