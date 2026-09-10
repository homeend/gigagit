package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// forceColor (TrueColor save/restore) lives in commit_color_test.go.

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

// TestTextFieldInsertDropsNULRunes guards against a real crash: pasting
// clipboard text into a Windows console can synthesize a stray NUL
// (U+0000) key event at the end of the burst. A NUL surviving into a tag
// name/message reaches git's argv unfiltered (internal/git/mutate.go's
// CreateTag), and Go's Windows exec layer rejects any arg containing a NUL
// with syscall.EINVAL — surfacing as the opaque "fork/exec ...: invalid
// argument" reported after copy-pasting a tag name into the create-tag
// popup. No legitimate keystroke or paste needs a literal NUL, so it's
// dropped at the field's own insertion boundary rather than validated
// downstream.
func TestTextFieldInsertDropsNULRunes(t *testing.T) {
	var f textfield
	f.HandleEditKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v', '0', '.', '1', '.', '9', 0}})
	if f.Value() != "v0.1.9" {
		t.Fatalf("Value = %q, want %q (NUL rune must be dropped)", f.Value(), "v0.1.9")
	}
	if f.cursor != len([]rune("v0.1.9")) {
		t.Fatalf("cursor = %d, want %d", f.cursor, len([]rune("v0.1.9")))
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

func TestTextFieldNewlineAndHomeEnd(t *testing.T) {
	var f textfield
	f.HandleEditKey(keyMsg("ab"))
	f.InsertNewline()
	f.HandleEditKey(keyMsg("cd"))
	if f.Value() != "ab\ncd" {
		t.Fatalf("Value = %q, want 'ab\\ncd'", f.Value())
	}
	f.HandleEditKey(keyMsg("home")) // start of 2nd line
	if f.cursor != 3 {
		t.Fatalf("home cursor = %d, want 3 (start of 2nd line)", f.cursor)
	}
	f.HandleEditKey(keyMsg("end")) // end of 2nd line
	if f.cursor != 5 {
		t.Fatalf("end cursor = %d, want 5", f.cursor)
	}
}

func TestTextFieldUpDown(t *testing.T) {
	f := newTextField("abcd\nxy") // cursor 7 (end, col 2 on line 2)
	f.Up()                        // to line 1, col 2 -> index 2
	if f.cursor != 2 {
		t.Fatalf("Up cursor = %d, want 2", f.cursor)
	}
	f.Down() // back to line 2, col 2 -> index 7
	if f.cursor != 7 {
		t.Fatalf("Down cursor = %d, want 7", f.cursor)
	}
}

func TestTextFieldUpDownColumnClamp(t *testing.T) {
	f := newTextField("a\nlongline") // cursor at end of line 2
	f.Up()                           // line 1 only has 1 col -> clamp to index 1
	if f.cursor != 1 {
		t.Fatalf("Up clamp cursor = %d, want 1", f.cursor)
	}
}

func TestTextFieldUpOnFirstLineNoOp(t *testing.T) {
	f := newTextField("abc")
	f.HandleEditKey(keyMsg("home"))
	f.Up()
	if f.cursor != 0 {
		t.Fatalf("Up on first line cursor = %d, want 0", f.cursor)
	}
	f.HandleEditKey(keyMsg("end"))
	f.Down()
	if f.cursor != 3 {
		t.Fatalf("Down on last line cursor = %d, want 3", f.cursor)
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

func TestTextFieldViewUnfocusedPlain(t *testing.T) {
	f := newTextField("abc")
	if got := f.View(false); got != "abc" {
		t.Fatalf("unfocused View = %q, want plain 'abc'", got)
	}
}

func TestTextFieldViewFocusedCursorAtRune(t *testing.T) {
	forceColor(t)
	f := newTextField("ab")
	f.HandleEditKey(keyMsg("left")) // cursor on 'b'
	got := f.View(true)
	if !strings.Contains(got, "\x1b[7mb") {
		t.Fatalf("focused View = %q, want reverse cell on 'b'", got)
	}
}

func TestTextFieldViewFocusedCursorAtEnd(t *testing.T) {
	forceColor(t)
	f := newTextField("ab") // cursor at end
	got := f.View(true)
	if !strings.HasPrefix(got, "ab") || !strings.Contains(got, "\x1b[7m") {
		t.Fatalf("focused-at-end View = %q, want 'ab' + reverse block", got)
	}
}

// --- Delete key: forward-delete mid-buffer, backspace at the end. ---
//
// Some keyboards (and some terminals' Backspace mappings) deliver ^[[3~,
// i.e. tea.KeyDelete, for the key the user erases with. At the end of the
// buffer a forward-delete has nothing to remove and was a silent no-op —
// the "cannot delete the last character" report. The field turns that
// case into a backspace so every gg text field erases on Delete too.

func TestTextFieldDeleteAtEndActsAsBackspace(t *testing.T) {
	f := newTextField("abc") // cursor at the end
	if !f.HandleEditKey(tea.KeyMsg{Type: tea.KeyDelete}) {
		t.Fatal("Delete must be consumed")
	}
	if f.Value() != "ab" || f.cursor != 2 {
		t.Fatalf("Delete at end = %q/%d, want ab/2 (backspace)", f.Value(), f.cursor)
	}
	for range 5 {
		f.HandleEditKey(tea.KeyMsg{Type: tea.KeyDelete})
	}
	if f.Value() != "" || f.cursor != 0 {
		t.Fatalf("repeated Delete must empty the field, got %q/%d", f.Value(), f.cursor)
	}
}

func TestTextFieldDeleteMidBufferForwardDeletes(t *testing.T) {
	f := textfield{runes: []rune("abcd"), cursor: 1}
	f.HandleEditKey(tea.KeyMsg{Type: tea.KeyDelete})
	if f.Value() != "acd" || f.cursor != 1 {
		t.Fatalf("Delete mid-buffer = %q/%d, want acd/1 (forward-delete, cursor stays)", f.Value(), f.cursor)
	}
	// The end of a LINE is not the end of the buffer: the newline ahead of
	// the cursor is still forward-deleted (multi-line commit description).
	f = textfield{runes: []rune("ab\ncd"), cursor: 2}
	f.HandleEditKey(tea.KeyMsg{Type: tea.KeyDelete})
	if f.Value() != "abcd" || f.cursor != 2 {
		t.Fatalf("Delete before \\n = %q/%d, want abcd/2", f.Value(), f.cursor)
	}
}

func TestTextFieldDeleteOnEmptyIsNoop(t *testing.T) {
	var f textfield
	if !f.HandleEditKey(tea.KeyMsg{Type: tea.KeyDelete}) {
		t.Fatal("Delete must be consumed even on an empty field")
	}
	if f.Value() != "" || f.cursor != 0 {
		t.Fatalf("got %q/%d", f.Value(), f.cursor)
	}
}
