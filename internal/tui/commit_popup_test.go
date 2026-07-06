package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPackHintsKeepsKeyPairsIntact(t *testing.T) {
	pairs := []string{
		"[tab] switch field", "[enter] newline/next",
		"[ctrl+g] generate", "[ctrl+s] commit", "[esc] cancel",
	}
	// A narrow width forces wrapping; every pair must stay on one line and no
	// line may exceed the width — the bug was "[ctrl+g]" wrapping away from its
	// "generate" label, reading as a dangling key.
	out := packHints(pairs, 52)
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 52 {
			t.Fatalf("line exceeds width 52: %q (%d)", line, len(line))
		}
		for _, p := range pairs {
			key := p[:strings.IndexByte(p, ']')+1] // e.g. "[ctrl+g]"
			if strings.Contains(line, key) && !strings.Contains(line, p) {
				t.Fatalf("%q split from its label on line %q", key, line)
			}
		}
	}
	for _, p := range pairs { // nothing dropped
		if !strings.Contains(out, p) {
			t.Fatalf("missing pair %q in %q", p, out)
		}
	}
	if strings.Contains(packHints(pairs, 200), "\n") {
		t.Fatal("a wide width should keep all pairs on one line")
	}
}

func TestCommitPopupMessageAssembly(t *testing.T) {
	if got := (&commitPopup{title: newTextField("subj")}).message(); got != "subj" {
		t.Fatalf("title-only = %q", got)
	}
	if got := (&commitPopup{title: newTextField("subj"), desc: newTextField("body line")}).message(); got != "subj\n\nbody line" {
		t.Fatalf("title+body = %q", got)
	}
	if got := (&commitPopup{title: newTextField("  spaced  ")}).message(); got != "spaced" {
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
	if layerOf[*commitPopup](m).title.Value() != "hi" {
		t.Fatalf("title = %q", layerOf[*commitPopup](m).title.Value())
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
	if layerOf[*commitPopup](m).desc.Value() != "body\n" {
		t.Fatalf("desc = %q, want \"body\\n\"", layerOf[*commitPopup](m).desc.Value())
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
