package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
)

// statusRowOf is the bottom line of the rendered interface — the status row.
func statusRowOf(m Model) string {
	lines := strings.Split(m.renderInterface(), "\n")
	return lines[len(lines)-1]
}

// The docked console's worktree path is in the status row while the console
// is unfocused (what alt+a leaves) or runs in another worktree than gg's own;
// a focused console on gg's own worktree needs no reminder.
func TestConsoleWorktreeHintWhenUnfocusedOrForeign(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	if m.currentWorktree == "" {
		t.Fatal("loaded model has no current worktree")
	}
	if got := m.consoleWorktreeHint(); got != "" {
		t.Fatalf("no console: hint = %q, want none", got)
	}
	own := startTestSession(t, m, `sleep 5`)
	m, _ = m.showConsole(own.Info().ID, false)
	if got := m.consoleWorktreeHint(); filepath.Clean(got) != filepath.Clean(m.currentWorktree) {
		t.Fatalf("unfocused, own worktree: hint = %q, want %q", got, m.currentWorktree)
	}
	if row := statusRowOf(m); !strings.Contains(row, m.currentWorktree) {
		t.Fatalf("status row %q lacks the worktree path %q", row, m.currentWorktree)
	}
	m, _ = m.showConsole(own.Info().ID, true)
	if got := m.consoleWorktreeHint(); got != "" {
		t.Fatalf("focused, own worktree: hint = %q, want none", got)
	}
	other := t.TempDir()
	far, err := domain.Sessions().Start(sessionSpecForTest("c", other))
	if err != nil {
		t.Fatal(err)
	}
	m, _ = m.showConsole(far.Info().ID, true)
	if got := m.consoleWorktreeHint(); got != other {
		t.Fatalf("focused, other worktree: hint = %q, want %q", got, other)
	}
}

// A path longer than the room left in the row is cut in the MIDDLE: the
// directory name survives, the row never overflows; alt+t terminals count.
func TestConsoleWorktreeHintElidesToTheRoomLeft(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 60, 30
	startTestSession(t, m, `sleep 5`) // swaps in the test manager
	deep := filepath.Join(t.TempDir(), "a-rather-long-directory", "another-long-directory", "yet-more-nesting", "wt-feature-x")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := sessionSpecForTest("Terminal", deep)
	spec.Terminal = true
	term, err := domain.Sessions().Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	m, _ = m.showConsole(term.Info().ID, false)
	m.statusMsg = "Terminal in wt-feature-x — 1 of 1 by last use"
	row := statusRowOf(m)
	if w := lipgloss.Width(row); w > m.width {
		t.Fatalf("status row is %d wide, over %d: %q", w, m.width, row)
	}
	if !strings.Contains(row, "…") || !strings.HasSuffix(strings.TrimRight(row, " "), "wt-feature-x") {
		t.Fatalf("status row %q: want the path middle-elided, ending in its directory name", row)
	}
}

// The Commits row hint ("working tree · 3 files", "⎇ main · # …") describes
// a row the docked console covers: it gives the status row to the console's
// worktree instead of reading as a second "worktree".
func TestDockedConsoleHidesCommitRowHint(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m.focus = panelCommits
	if m.commitBranchHint() == "" {
		t.Fatal("precondition: the Commits row hint shows without a console")
	}
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.showConsole(s.Info().ID, false)
	if m.focus != panelCommits {
		t.Fatalf("precondition: focus = %v, want the Commits column", m.focus)
	}
	if got := m.commitBranchHint(); got != "" {
		t.Fatalf("docked console: Commits row hint = %q, want none", got)
	}
}
