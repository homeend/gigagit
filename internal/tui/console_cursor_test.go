package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// While a console is bound (alt+a / alt+t landed on it: the console has
// the keyboard, the Commits column the focus), the Branches tab still
// shows WHICH session row the walk is on: its cursor is drawn dimmed on
// the session's sub-row instead of vanishing with the focus.
func TestBoundConsoleShowsTheBranchesCursorOnItsSessionRow(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m = pressAlt(t, m, 'a')
	if m.console == nil || !m.console.focused || m.focus != panelCommits || m.console.id != id {
		t.Fatalf("precondition: console=%+v focus=%v", m.console, m.focus)
	}
	out := ansi.Strip(m.renderPanel(panelBranches, "Branches", m.branchRows(), nil, 60, 12))
	var cursorRow string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "> ") {
			cursorRow = l
			break
		}
	}
	if cursorRow == "" || !strings.Contains(cursorRow, "└ ●") {
		t.Fatalf("the bound console's session row carries no cursor:\n%s", out)
	}
}

// Without a console the unfocused Branches tab stays cursorless, as every
// unfocused panel does.
func TestAnUnfocusedBranchesTabWithoutAConsoleShowsNoCursor(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m.focus = panelFiles
	out := ansi.Strip(m.renderPanel(panelBranches, "Branches", m.branchRows(), nil, 60, 12))
	if strings.Contains(out, "> ") {
		t.Fatalf("an unfocused panel drew a cursor:\n%s", out)
	}
}
