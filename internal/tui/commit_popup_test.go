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

// applyCommitKey routes a key through updateCommitPopupKey (the popup owns input).
func applyCommitKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	updated, cmd := m.updateCommitPopupKey(k)
	return updated.(Model), cmd
}

func TestCommitPopupTypingAndFieldSwitch(t *testing.T) {
	m := Model{commitPopup: &commitPopup{}}
	m, _ = applyCommitKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")})
	if m.commitPopup.title != "hi" {
		t.Fatalf("title = %q", m.commitPopup.title)
	}
	m, _ = applyCommitKey(m, tea.KeyMsg{Type: tea.KeyEnter}) // title → description
	if m.commitPopup.field != 1 {
		t.Fatal("enter in title must move to description")
	}
	m, _ = applyCommitKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("body")})
	m, _ = applyCommitKey(m, tea.KeyMsg{Type: tea.KeyEnter}) // newline in description
	if m.commitPopup.desc != "body\n" {
		t.Fatalf("desc = %q, want \"body\\n\"", m.commitPopup.desc)
	}
	m, _ = applyCommitKey(m, tea.KeyMsg{Type: tea.KeyTab}) // back to title
	if m.commitPopup.field != 0 {
		t.Fatal("tab must switch field")
	}
	m, _ = applyCommitKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.commitPopup != nil {
		t.Fatal("esc must close the popup")
	}
}

func TestCommitPopupEmptyTitleRefused(t *testing.T) {
	m := Model{commitPopup: &commitPopup{}}
	m, cmd := applyCommitKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd != nil {
		t.Fatal("ctrl+s with an empty title must not start a commit")
	}
	if m.commitPopup == nil {
		t.Fatal("empty-title submit must keep the popup open")
	}
}
