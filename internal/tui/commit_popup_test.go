package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCommitPopupMessageAssembly(t *testing.T) {
	if got := (&commitPopup{title: "subj"}).message(); got != "subj" {
		t.Fatalf("title-only = %q", got)
	}
	if got := (&commitPopup{title: "subj", desc: "body line"}).message(); got != "subj\n\nbody line" {
		t.Fatalf("title+body = %q", got)
	}
	if got := (&commitPopup{title: "  spaced  "}).message(); got != "spaced" {
		t.Fatalf("title should be trimmed: %q", got)
	}
}

func TestSplitMessage(t *testing.T) {
	ti, de := splitMessage("subject\n\nbody one\nbody two\n")
	if ti != "subject" || de != "body one\nbody two" {
		t.Fatalf("split = (%q, %q)", ti, de)
	}
	if ti, de := splitMessage("only subject"); ti != "only subject" || de != "" {
		t.Fatalf("single-line split = (%q, %q)", ti, de)
	}
}

func TestCommitPopupTypingAndFieldSwitch(t *testing.T) {
	m := Model{sel: map[panel]int{}}
	m = m.pushLayer(&commitPopup{})
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")})
	m = tm.(Model)
	if layerOf[*commitPopup](m).title != "hi" {
		t.Fatalf("title = %q", layerOf[*commitPopup](m).title)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // title → description
	m = tm.(Model)
	if layerOf[*commitPopup](m).field != 1 {
		t.Fatal("enter in title must move to description")
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("body")})
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // newline in description
	m = tm.(Model)
	if layerOf[*commitPopup](m).desc != "body\n" {
		t.Fatalf("desc = %q, want \"body\\n\"", layerOf[*commitPopup](m).desc)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab}) // back to title
	m = tm.(Model)
	if layerOf[*commitPopup](m).field != 0 {
		t.Fatal("tab must switch field")
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(Model)
	if layerOf[*commitPopup](m) != nil {
		t.Fatal("esc must close the popup")
	}
}

func TestCommitPopupEmptyTitleRefused(t *testing.T) {
	m := Model{sel: map[panel]int{}}
	m = m.pushLayer(&commitPopup{})
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = tm.(Model)
	if cmd != nil {
		t.Fatal("ctrl+s with an empty title must not start a commit")
	}
	if layerOf[*commitPopup](m) == nil {
		t.Fatal("empty-title submit must keep the popup open")
	}
}
