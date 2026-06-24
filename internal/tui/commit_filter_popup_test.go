package tui

import (
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCommitFilterPopupOpensOnBackslash(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelCommits
	m2, _ := m.Update(keyMsg("\\"))
	mm := m2.(Model)
	if _, ok := mm.topLayer().(*commitFilterPopup); !ok {
		t.Fatalf("backslash on Commits should open the filter popup, top=%T", mm.topLayer())
	}
}

func TestCommitFilterPopupApplySetsFilter(t *testing.T) {
	m := loadedModel(t)
	p := &commitFilterPopup{}
	p.focus = cfGrep
	for _, r := range "race" { // drive the focused field through HandleEditKey
		p.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = m.pushLayer(p)
	// Enter applies.
	m2, _ := m.Update(keyMsg("enter"))
	mm := m2.(Model)
	if mm.commitFilter.Grep != "race" {
		t.Fatalf("apply should set Grep=race, got %q", mm.commitFilter.Grep)
	}
	if _, ok := mm.topLayer().(*commitFilterPopup); ok {
		t.Fatal("apply should pop the popup")
	}
}

func TestCommitFilterPopupCtrlRClearsAllFilters(t *testing.T) {
	m := loadedModel(t)
	m.commitFilter = commitFilterFields{Paths: []string{"sub"}, Author: "alice", Grep: "race"}
	p := newCommitFilterPopup(m.commitFilter)
	m = m.pushLayer(p)
	// ctrl+r removes every filter axis at once and closes the popup.
	m2, _ := m.Update(keyMsg("ctrl+r"))
	mm := m2.(Model)
	if mm.commitFilter.filtered() {
		t.Fatalf("ctrl+r should remove all filters, got %+v", mm.commitFilter)
	}
	if _, ok := mm.topLayer().(*commitFilterPopup); ok {
		t.Fatal("ctrl+r should pop the popup")
	}
}

func TestCommitFilterPopupEscCancels(t *testing.T) {
	m := loadedModel(t)
	m = m.pushLayer(&commitFilterPopup{})
	beforeAuthor := m.commitFilter.Author
	beforeGrep := m.commitFilter.Grep
	beforeSince := m.commitFilter.Since
	beforeUntil := m.commitFilter.Until
	beforePaths := append([]string(nil), m.commitFilter.Paths...)
	m2, _ := m.Update(keyMsg("esc"))
	mm := m2.(Model)
	if _, ok := mm.topLayer().(*commitFilterPopup); ok {
		t.Fatal("esc should pop the popup")
	}
	if mm.commitFilter.Author != beforeAuthor ||
		mm.commitFilter.Grep != beforeGrep ||
		mm.commitFilter.Since != beforeSince ||
		mm.commitFilter.Until != beforeUntil ||
		!slices.Equal(mm.commitFilter.Paths, beforePaths) {
		t.Fatal("esc must not change the filter")
	}
}

func TestCommitFilterPopupSwallowsGlobalKeys(t *testing.T) {
	m := loadedModel(t)
	m = m.pushLayer(&commitFilterPopup{})
	m2, cmd := m.Update(keyMsg("p")) // 'p' is pull globally; must NOT fire here
	mm := m2.(Model)
	if mm.running {
		t.Fatal("global key leaked through the popup")
	}
	_ = cmd
}
