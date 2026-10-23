package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/workspace"
)

// Probe round 2: the Workspaces popup's, the R switcher's and the launch
// dialog's text and cursor.

// the add picker's filter cannot take a space (tea.KeySpace is not
// KeyRunes), unlike R's filter; a path or name with a space cannot be typed.
func TestPickerFilterTakesSpace(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo().mainPath
	m, p := openWSPopup(t, m, []workspace.Entry{{Name: "shop", Repos: []string{home}}})
	m, _ = wsPress(t, m, "enter")
	m, _ = wsPress(t, m, "a")
	if p.level != wsLevelPicker {
		t.Fatalf("level = %v", p.level)
	}
	m = wsType(t, m, "my")
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	m = nm.(Model)
	m = wsType(t, m, "repo")
	if p.pquery != "my repo" {
		t.Fatalf("picker query = %q, want %q", p.pquery, "my repo")
	}
}

// R opens on the current repository's row ("R, enter" = no-op), but
// any filter edit (a typed rune then backspace, or esc clearing it) resets the
// cursor to row 0 — the first WORKSPACE row — so "R x ⌫ enter" opens a
// workspace.
func TestRFilterClearedLandsOnWorkspaceRow(t *testing.T) {
	m, _ := seededWorkspaces(t)
	m.width, m.height = 120, 40
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	p := layerOf[*repoPopup](m)
	start := p.sel
	m = wsType(t, m, "q")
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = nm.(Model)
	if p.query != "" {
		t.Fatalf("query = %q", p.query)
	}
	if p.sel != start {
		t.Fatalf("after typing and clearing the filter the cursor is on row %d (workspace rows: %d), not the current repository's row %d", p.sel, len(p.wsVisible()), start)
	}
}

// the launch dialog's prose is hard-wrapped by runes (wrapWidth), so
// a word is cut in two ("a wo" / "rkspace,") — pinned in the
// tui_workspace_launch* goldens.
func TestLaunchDialogWrapsMidWord(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	home := m.homeRepo().mainPath
	m = withWorkspacesFile(t, m, []workspace.Entry{{Name: "shop", Repos: []string{home, secondRepo(t)}}})
	nm, _ := m.maybeAskWorkspace()
	p, ok := nm.topLayer().(*workspaceLaunchPopup)
	if !ok {
		t.Fatalf("top = %T", nm.topLayer())
	}
	out := p.render(nm, "")
	if !strings.Contains(out, "workspace,") {
		t.Fatalf("a word of the dialog's prose is split across lines:\n%s", out)
	}
}

// the Workspaces popup's rows are not elided: a workspace row (name
// + member names, p.row(mark+text) never cut) and a member row (base name +
// a path elided to textW-24 whatever the base's width) wrap onto
// continuation lines. The selected row's own line must end with its tail.
func TestPopupRowsWrap(t *testing.T) {
	m := loadedModel(t)
	long := []string{
		"/r/alpha-service-with-a-long-name", "/r/beta-frontend-application-xyz",
		"/r/gamma-infrastructure-terraform", "/r/delta", "/r/epsilon",
	}
	m, p := openWSPopup(t, m, []workspace.Entry{{Name: "big", Repos: long}})
	selectedLine := func(out string) string {
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, "> ") {
				return l
			}
		}
		return ""
	}
	if row := selectedLine(p.render(m, "")); !strings.Contains(row, "epsilon") {
		t.Errorf("list: the selected workspace row wrapped — its line %q lacks the last member \"epsilon\"", strings.TrimSpace(row))
	}
	m, _ = wsPress(t, m, "enter")
	out := p.render(m, "")
	if row := selectedLine(out); !strings.Contains(row, "alpha-service-with-a-long-name") {
		t.Errorf("members: the selected member row lost its name: %q", strings.TrimSpace(row))
	}
	if n := strings.Count(out, "alpha-service"); n != 1 {
		t.Errorf("members: the selected member row wrapped onto another line (%d lines name it):\n%s", n, out)
	}
}

// The Workspaces popup's hint lines wrap between keys, never between a key
// and its label ("[d]" on one line, "delete" on the next).
func TestWorkspacePopupHintsWrapBetweenKeys(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	home := m.homeRepo().mainPath
	m, p := openWSPopup(t, m, []workspace.Entry{{Name: "shop", Repos: []string{home}}})
	check := func(out string, pairs map[string]string) {
		t.Helper()
		for _, l := range strings.Split(out, "\n") {
			for key, label := range pairs {
				if strings.Contains(l, key) && !strings.Contains(l, label) {
					t.Errorf("hint split between %q and %q:\n%s", key, label, out)
				}
			}
		}
	}
	check(p.render(m, ""), map[string]string{"[enter]": "repositories", "[o]": "open now", "[d]": "delete"})
	m, _ = wsPress(t, m, "enter")
	check(p.render(m, ""), map[string]string{"[s]": "settings repository", "[d]": "remove", "[K/J]": "move"})
}
