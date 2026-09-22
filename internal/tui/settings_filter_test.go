package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// settingsType opens the , menu and types s into it, one rune per keypress.
func settingsType(t *testing.T, s string) (Model, *settingsPopup) {
	t.Helper()
	m, _ := settingsModel(t)
	m, _ = send(m, keyMsg(","))
	for _, r := range s {
		if r == ' ' {
			m, _ = send(m, keyType(tea.KeySpace))
			continue
		}
		m, _ = send(m, keyRunes(string(r)))
	}
	p := layerOf[*settingsPopup](m)
	if p == nil {
		t.Fatalf("typing %q must leave the settings menu open", s)
	}
	return m, p
}

// visibleEntries maps the visible menu indices back to their entries.
func visibleEntries(p *settingsPopup) []string {
	var out []string
	for _, i := range p.visibleMenu() {
		out = append(out, settingsMenu[i])
	}
	return out
}

// Letters type into the filter (j, k and q included) like the ctrl+p palette.
func TestSettingsMenuLettersTypeIntoFilter(t *testing.T) {
	t.Parallel()
	_, p := settingsType(t, "jkq")
	if p.query != "jkq" {
		t.Fatalf("query = %q, want %q", p.query, "jkq")
	}
	if p.menuSel != 0 {
		t.Fatalf("letters must not move the selection, menuSel = %d", p.menuSel)
	}
}

// Typing narrows the rows case-insensitively on the title, and the rows'
// live state ("on"/"off") is not part of what matches.
func TestSettingsMenuTypingFiltersRows(t *testing.T) {
	t.Parallel()
	m, p := settingsType(t, "THEME c")
	if got := visibleEntries(p); len(got) != 1 || got[0] != settingsMenuThemeColours {
		t.Fatalf("visible = %v, want only %q", got, settingsMenuThemeColours)
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "THEME c█") {
		t.Fatalf("header must echo the query:\n%s", out)
	}
	if strings.Contains(out, "External tools") {
		t.Fatalf("filtered-out rows must not render:\n%s", out)
	}
	_, p = settingsType(t, "off")
	if got := visibleEntries(p); len(got) != 0 {
		t.Fatalf("state text must not match, visible = %v", got)
	}
}

// enter acts on the selected row of the FILTERED list, and arrows move within it.
func TestSettingsMenuEnterRunsFilteredRow(t *testing.T) {
	t.Parallel()
	m, _ := settingsType(t, "refresh")
	// Auto-refresh, Auto remote-tag refresh, Refresh rates → down twice = rates.
	m, _ = send(m, keyType(tea.KeyDown))
	m, _ = send(m, keyType(tea.KeyDown))
	m, _ = send(m, keyType(tea.KeyEnter))
	if p := layerOf[*settingsPopup](m); p == nil || !p.ratesView {
		t.Fatal("enter on the filtered Refresh rates row should open the rates editor")
	}
}

// First esc clears the filter, the second closes; no match shows a placeholder.
func TestSettingsMenuEscClearsFilterFirst(t *testing.T) {
	t.Parallel()
	m, p := settingsType(t, "zzz")
	if !strings.Contains(ansi.Strip(m.View()), "(no match)") {
		t.Fatal("an empty filter result must say (no match)")
	}
	m, _ = send(m, keyType(tea.KeyEnter)) // no row: a no-op, not a panic
	m, _ = send(m, keyType(tea.KeyEsc))
	if p = layerOf[*settingsPopup](m); p == nil || p.query != "" {
		t.Fatal("first esc must clear the filter and keep the menu open")
	}
	m, _ = send(m, keyType(tea.KeyEsc))
	if layerOf[*settingsPopup](m) != nil {
		t.Fatal("esc with no filter must close the menu")
	}
}

// Backspace trims the query; the filter survives a round trip into a sub-screen.
func TestSettingsMenuBackspaceAndSubscreenKeepFilter(t *testing.T) {
	t.Parallel()
	m, p := settingsType(t, "ratesx")
	m, _ = send(m, keyType(tea.KeyBackspace))
	if p.query != "rates" {
		t.Fatalf("backspace: query = %q, want %q", p.query, "rates")
	}
	m, _ = send(m, keyType(tea.KeyEnter))
	if !p.ratesView {
		t.Fatal("enter should open the rates editor")
	}
	m, _ = send(m, keyType(tea.KeyEsc))
	if p.ratesView || p.query != "rates" {
		t.Fatalf("esc from the sub-screen returns to the filtered menu; ratesView=%v query=%q", p.ratesView, p.query)
	}
	_ = m
}
