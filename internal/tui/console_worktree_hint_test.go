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

// The status row names the console's worktree only when it is NOT the one
// the panels show (the user switched the panels elsewhere under a docked
// console, or the session runs outside the repository); a console in the
// viewed worktree — focused or not — needs no reminder: the header says it.
func TestConsoleWorktreeHintOnlyWhenTheConsoleIsElsewhere(t *testing.T) {
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
	if got := m.consoleWorktreeHint(); got != "" {
		t.Fatalf("unfocused, viewed worktree: hint = %q, want none", got)
	}
	m, _ = m.showConsole(own.Info().ID, true)
	if got := m.consoleWorktreeHint(); got != "" {
		t.Fatalf("focused, viewed worktree: hint = %q, want none", got)
	}
	other := t.TempDir()
	far, err := domain.Sessions().Start(sessionSpecForTest("c", other))
	if err != nil {
		t.Fatal(err)
	}
	m, _ = m.showConsole(far.Info().ID, true)
	if got := m.consoleWorktreeHint(); got != other {
		t.Fatalf("console outside the repository: hint = %q, want %q", got, other)
	}
	if row := statusRowOf(m); !strings.Contains(row, "worktree: ") {
		t.Fatalf("status row %q lacks the worktree label", row)
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

// A console of another worktree of the repository: the panels follow it,
// so there is no hint; enter on home's row under the docked console moves
// the panels and the hint names the console's worktree.
func TestConsoleHintAppearsWhenThePanelsLeaveTheConsolesWorktree(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m, _ = m.showConsole(id, true)
	if got := m.consoleWorktreeHint(); got != "" {
		t.Fatalf("panels show the console's worktree: hint = %q, want none", got)
	}
	nm, _ := m.guardedReRoot(m.home, true)
	m = nm.(Model)
	if got := m.consoleWorktreeHint(); got != filepath.Clean(other) {
		t.Fatalf("panels moved home under the console: hint = %q, want %q", got, other)
	}
}
