package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTextFieldInsertAndValue(t *testing.T) {
	var f textfield
	f.HandleEditKey(keyMsg("abc"))
	if f.Value() != "abc" {
		t.Fatalf("Value = %q, want abc", f.Value())
	}
	if f.cursor != 3 {
		t.Fatalf("cursor = %d, want 3", f.cursor)
	}
}

func TestTextFieldInsertMidBuffer(t *testing.T) {
	f := newTextField("ac")
	f.HandleEditKey(keyMsg("left")) // cursor: 2 -> 1
	f.HandleEditKey(keyMsg("b"))    // insert at 1
	if f.Value() != "abc" {
		t.Fatalf("Value = %q, want abc", f.Value())
	}
	if f.cursor != 2 {
		t.Fatalf("cursor = %d, want 2", f.cursor)
	}
}

func TestTextFieldBackspaceAndDelete(t *testing.T) {
	f := newTextField("abc")
	f.HandleEditKey(keyMsg("backspace")) // "ab", cursor 2
	if f.Value() != "ab" {
		t.Fatalf("after backspace Value = %q, want ab", f.Value())
	}
	f.HandleEditKey(keyMsg("home"))   // cursor 0
	f.HandleEditKey(keyMsg("delete")) // delete 'a' -> "b"
	if f.Value() != "b" {
		t.Fatalf("after delete Value = %q, want b", f.Value())
	}
}

func TestTextFieldArrowClamp(t *testing.T) {
	f := newTextField("ab")
	for i := 0; i < 5; i++ {
		f.HandleEditKey(keyMsg("left"))
	}
	if f.cursor != 0 {
		t.Fatalf("cursor = %d, want clamp at 0", f.cursor)
	}
	for i := 0; i < 5; i++ {
		f.HandleEditKey(keyMsg("right"))
	}
	if f.cursor != 2 {
		t.Fatalf("cursor = %d, want clamp at 2", f.cursor)
	}
}

func TestTextFieldHomeEndSpace(t *testing.T) {
	f := newTextField("hi")
	f.HandleEditKey(keyMsg("home"))
	if f.cursor != 0 {
		t.Fatalf("home cursor = %d, want 0", f.cursor)
	}
	f.HandleEditKey(keyMsg("end"))
	if f.cursor != 2 {
		t.Fatalf("end cursor = %d, want 2", f.cursor)
	}
	f.HandleEditKey(keyMsg("space"))
	if f.Value() != "hi " {
		t.Fatalf("Value = %q, want 'hi '", f.Value())
	}
}

func TestTextFieldWordJumpsAndDeleteWord(t *testing.T) {
	f := newTextField("foo bar baz") // cursor 11
	f.HandleEditKey(keyMsg("ctrl+left"))
	if f.cursor != 8 { // start of "baz"
		t.Fatalf("word-left cursor = %d, want 8", f.cursor)
	}
	f.HandleEditKey(keyMsg("alt+left")) // alt+left = word-left
	if f.cursor != 4 {                  // start of "bar"
		t.Fatalf("alt word-left cursor = %d, want 4", f.cursor)
	}
	f.HandleEditKey(keyMsg("ctrl+w")) // delete the word before the cursor ("foo")
	if f.Value() != "bar baz" {
		t.Fatalf("after ctrl+w Value = %q, want 'bar baz'", f.Value())
	}
}

func TestTextFieldWordRight(t *testing.T) {
	f := newTextField("foo bar")
	f.HandleEditKey(keyMsg("home"))
	f.HandleEditKey(keyMsg("ctrl+right"))
	if f.cursor != 3 { // end of "foo"
		t.Fatalf("word-right cursor = %d, want 3", f.cursor)
	}
}

func TestTextFieldSetValue(t *testing.T) {
	var f textfield
	f.SetValue("hello")
	if f.Value() != "hello" || f.cursor != 5 {
		t.Fatalf("SetValue Value=%q cursor=%d, want hello/5", f.Value(), f.cursor)
	}
}

func TestTextFieldHandleEditKeyReturnsFalse(t *testing.T) {
	var f textfield
	for _, kt := range []tea.KeyType{tea.KeyEnter, tea.KeyTab, tea.KeyEsc, tea.KeyUp, tea.KeyDown, tea.KeyCtrlS} {
		if f.HandleEditKey(tea.KeyMsg{Type: kt}) {
			t.Fatalf("HandleEditKey consumed %v, want false", kt)
		}
	}
}
