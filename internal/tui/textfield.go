package tui

import (
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// textfield is a cursor-aware editable text buffer shared by the editable
// popups. cursor is a rune index in [0, len(runes)]. A '\n' is an ordinary
// rune, so the same type backs single-line fields and the multi-line commit
// description. The quick-switcher FILTER inputs deliberately do NOT use it.
type textfield struct {
	runes  []rune
	cursor int
}

// newTextField makes a field pre-filled with s, cursor at the end.
func newTextField(s string) textfield {
	r := []rune(s)
	return textfield{runes: r, cursor: len(r)}
}

// Value returns the buffer as a string.
func (f *textfield) Value() string { return string(f.runes) }

// SetValue replaces the buffer and puts the cursor at the end.
func (f *textfield) SetValue(s string) {
	f.runes = []rune(s)
	f.cursor = len(f.runes)
}

// insert puts rs at the cursor and advances past them.
func (f *textfield) insert(rs []rune) {
	out := make([]rune, 0, len(f.runes)+len(rs))
	out = append(out, f.runes[:f.cursor]...)
	out = append(out, rs...)
	out = append(out, f.runes[f.cursor:]...)
	f.runes = out
	f.cursor += len(rs)
}

func (f *textfield) backspace() {
	if f.cursor == 0 {
		return
	}
	f.runes = append(f.runes[:f.cursor-1], f.runes[f.cursor:]...)
	f.cursor--
}

func (f *textfield) deleteFwd() {
	if f.cursor >= len(f.runes) {
		return
	}
	f.runes = append(f.runes[:f.cursor], f.runes[f.cursor+1:]...)
}

func (f *textfield) left() {
	if f.cursor > 0 {
		f.cursor--
	}
}

func (f *textfield) right() {
	if f.cursor < len(f.runes) {
		f.cursor++
	}
}

// wordLeft moves to the start of the word before the cursor (skipping any
// spaces first, then the word's runes).
func (f *textfield) wordLeft() {
	i := f.cursor
	for i > 0 && unicode.IsSpace(f.runes[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(f.runes[i-1]) {
		i--
	}
	f.cursor = i
}

// wordRight moves to the end of the word after the cursor.
func (f *textfield) wordRight() {
	i, n := f.cursor, len(f.runes)
	for i < n && unicode.IsSpace(f.runes[i]) {
		i++
	}
	for i < n && !unicode.IsSpace(f.runes[i]) {
		i++
	}
	f.cursor = i
}

// deleteWord removes the word before the cursor.
func (f *textfield) deleteWord() {
	start := f.cursor
	f.wordLeft()
	f.runes = append(f.runes[:f.cursor], f.runes[start:]...)
}

// lineStart returns the index just after the previous '\n' before i (or 0).
func (f *textfield) lineStart(i int) int {
	for i > 0 && f.runes[i-1] != '\n' {
		i--
	}
	return i
}

// lineEnd returns the index of the next '\n' at or after i (or len).
func (f *textfield) lineEnd(i int) int {
	n := len(f.runes)
	for i < n && f.runes[i] != '\n' {
		i++
	}
	return i
}

func (f *textfield) home() { f.cursor = f.lineStart(f.cursor) }
func (f *textfield) end()  { f.cursor = f.lineEnd(f.cursor) }

// HandleEditKey applies one editing key. Returns true if it consumed the key,
// false for any key the popup must handle itself (Enter, Tab, Esc, Up, Down,
// Ctrl+S, …). Navigation/submit semantics stay with the popup.
func (f *textfield) HandleEditKey(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyRunes:
		f.insert(msg.Runes)
	case tea.KeySpace:
		f.insert([]rune{' '})
	case tea.KeyBackspace, tea.KeyCtrlH:
		f.backspace()
	case tea.KeyDelete:
		f.deleteFwd()
	case tea.KeyLeft:
		if msg.Alt {
			f.wordLeft()
		} else {
			f.left()
		}
	case tea.KeyRight:
		if msg.Alt {
			f.wordRight()
		} else {
			f.right()
		}
	case tea.KeyCtrlLeft:
		f.wordLeft()
	case tea.KeyCtrlRight:
		f.wordRight()
	case tea.KeyHome:
		f.home()
	case tea.KeyEnd:
		f.end()
	case tea.KeyCtrlW:
		f.deleteWord()
	default:
		return false
	}
	return true
}
