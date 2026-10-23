package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLineJumpTyping(t *testing.T) {
	t.Parallel()
	var j lineJump
	j.open()
	for _, k := range []string{"4", "2"} {
		if ev, _ := j.key(runeKey(k)); ev != jumpChanged {
			t.Fatalf("digit %s: event %v, want jumpChanged", k, ev)
		}
	}
	if b := j.badge(); b != ":42█" {
		t.Fatalf("badge %q, want :42█", b)
	}
	if ev, _ := j.key(tea.KeyMsg{Type: tea.KeyBackspace}); ev != jumpChanged {
		t.Fatalf("backspace: event %v", ev)
	}
	if b := j.badge(); b != ":4█" {
		t.Fatalf("after backspace badge %q, want :4█", b)
	}
	// A letter is swallowed: the jump owns the keyboard while it is typed.
	if ev, _ := j.key(runeKey("x")); ev != jumpIgnored {
		t.Fatalf("x: event %v, want jumpIgnored", ev)
	}
	if b := j.badge(); b != ":4█" {
		t.Fatalf("x changed the badge: %q", b)
	}
	ev, n := j.key(tea.KeyMsg{Type: tea.KeyEnter})
	if ev != jumpGo || n != 4 {
		t.Fatalf("enter: (%v, %d), want (jumpGo, 4)", ev, n)
	}
	if j.typing || j.badge() != "" {
		t.Fatalf("enter must close the prompt: typing %v badge %q", j.typing, j.badge())
	}
}

func TestLineJumpCancel(t *testing.T) {
	t.Parallel()
	var j lineJump
	j.open()
	j.key(runeKey("7"))
	if ev, _ := j.key(tea.KeyMsg{Type: tea.KeyEsc}); ev != jumpCancelled {
		t.Fatalf("esc: event %v, want jumpCancelled", ev)
	}
	if j.typing || j.digits != "" {
		t.Fatalf("esc must clear: %+v", j)
	}
	j.open()
	if ev, _ := j.key(tea.KeyMsg{Type: tea.KeyEnter}); ev != jumpCancelled {
		t.Fatalf("enter on nothing: event %v, want jumpCancelled", ev)
	}
	j.open()
	j.key(runeKey("0"))
	if ev, _ := j.key(tea.KeyMsg{Type: tea.KeyEnter}); ev != jumpCancelled {
		t.Fatalf("enter on 0: event %v, want jumpCancelled", ev)
	}
}

func TestLineJumpCaps(t *testing.T) {
	t.Parallel()
	var j lineJump
	j.open()
	for _, k := range []string{"0", "0", "0", "1", "2"} {
		j.key(runeKey(k))
	}
	if ev, n := j.key(tea.KeyMsg{Type: tea.KeyEnter}); ev != jumpGo || n != 12 {
		t.Fatalf("00012: (%v, %d), want (jumpGo, 12)", ev, n)
	}
	j.open()
	for _, k := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"} {
		j.key(runeKey(k))
	}
	if j.digits != "1234567" {
		t.Fatalf("digits %q, want the first 7", j.digits)
	}
	// A pasted run of digits arrives as one KeyRunes: the cap still holds.
	j.open()
	j.key(runeKey("98765432"))
	if j.digits != "9876543" {
		t.Fatalf("pasted digits %q, want 9876543", j.digits)
	}
}

func TestLineJumpNotTyping(t *testing.T) {
	t.Parallel()
	var j lineJump
	if ev, _ := j.key(runeKey("4")); ev != jumpNotOurs {
		t.Fatalf("closed jump: event %v, want jumpNotOurs", ev)
	}
	if !jumpOpenKey(runeKey(":")) {
		t.Fatal(": must open the jump")
	}
	if jumpOpenKey(runeKey("a")) {
		t.Fatal("a must not open the jump")
	}
}
