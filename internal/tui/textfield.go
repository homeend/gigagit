package tui

import (
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// Value returns the buffer as a string. Value receiver: read-only and callable
// on non-addressable values (e.g. a map element like worktreePopup.inputs[l]).
func (f textfield) Value() string { return string(f.runes) }

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

// InsertNewline inserts a line break at the cursor. The popup calls this for
// the commit description's Enter (single-line fields never call it).
func (f *textfield) InsertNewline() { f.insert([]rune{'\n'}) }

// Up moves the cursor to the same column on the previous line (best effort,
// clamped to that line's length). No-op on the first line.
func (f *textfield) Up() {
	ls := f.lineStart(f.cursor)
	if ls == 0 {
		return
	}
	col := f.cursor - ls
	prevStart := f.lineStart(ls - 1)
	prevEnd := ls - 1 // index of the '\n' ending the previous line
	if prevStart+col > prevEnd {
		f.cursor = prevEnd
	} else {
		f.cursor = prevStart + col
	}
}

// Down moves the cursor to the same column on the next line (best effort,
// clamped). No-op on the last line.
func (f *textfield) Down() {
	le := f.lineEnd(f.cursor)
	if le >= len(f.runes) {
		return
	}
	col := f.cursor - f.lineStart(f.cursor)
	nextStart := le + 1
	nextEnd := f.lineEnd(nextStart)
	if nextStart+col > nextEnd {
		f.cursor = nextEnd
	} else {
		f.cursor = nextStart + col
	}
}

// cursorCell renders the character under the cursor in reverse video.
var cursorCell = lipgloss.NewStyle().Reverse(true)

// View renders the field's text. Unfocused: plain text. Focused: the rune at
// the cursor is shown reverse-video; at end-of-buffer (or end-of-line) a
// reverse space marks the insertion point so it is always visible. The caller
// owns surrounding labels and any per-line indentation; for a multi-line buffer
// the caller may split View(true) on "\n" (the reverse cell never spans lines).
// Value receiver: read-only and callable on non-addressable map values.
func (f textfield) View(focused bool) string {
	if !focused {
		return string(f.runes)
	}
	if f.cursor >= len(f.runes) {
		return string(f.runes) + cursorCell.Render(" ")
	}
	at := f.runes[f.cursor]
	if at == '\n' {
		return string(f.runes[:f.cursor]) + cursorCell.Render(" ") + "\n" + string(f.runes[f.cursor+1:])
	}
	return string(f.runes[:f.cursor]) + cursorCell.Render(string(at)) + string(f.runes[f.cursor+1:])
}

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
