package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Editable-field styling. Every editable popup field is drawn on a subtle
// background so the user can see the editable slot (its extent and that it is
// empty) without having to guess. The focus cursor is a light block that stays
// visible against that background.
var (
	fieldBg          = lipgloss.Color("236")
	fieldStyle       = lipgloss.NewStyle().Background(fieldBg)
	fieldCursorStyle = lipgloss.NewStyle().Background(lipgloss.Color("250")).Foreground(lipgloss.Color("236"))
)

// cursorLineCol locates the cursor as a (line, column) pair within the buffer
// (lines split on '\n'). A cursor sitting just past a trailing '\n' lands at
// column 0 of a new line.
func (f textfield) cursorLineCol() (line, col int) {
	for i := 0; i < f.cursor && i < len(f.runes); i++ {
		if f.runes[i] == '\n' {
			line++
			col = 0
		} else {
			col++
		}
	}
	return line, col
}

// styledLines renders the buffer as one styled string per text line, each
// background-filled to at least width columns so the editable slot is visible
// even when empty. When focused, the cell at the cursor is drawn as a light
// block so it stays visible against the field background. Lines longer than
// width overflow (no truncation, matching the unstyled fields' old behavior).
// Each line is composed of self-contained styled segments (no nesting) so the
// background never bleeds past a reset.
func (f textfield) styledLines(focused bool, width int) []string {
	if width < 1 {
		width = 1
	}
	lines := strings.Split(string(f.runes), "\n")
	curLine, curCol := -1, -1
	if focused {
		curLine, curCol = f.cursorLineCol()
	}
	out := make([]string, len(lines))
	for i, ln := range lines {
		runes := []rune(ln)
		if pad := width - len(runes); pad > 0 {
			runes = append(runes, []rune(strings.Repeat(" ", pad))...)
		}
		if i != curLine {
			out[i] = fieldStyle.Render(string(runes))
			continue
		}
		cc := curCol
		if cc >= len(runes) { // cursor at end of a line already >= width
			runes = append(runes, ' ')
		}
		out[i] = fieldStyle.Render(string(runes[:cc])) +
			fieldCursorStyle.Render(string(runes[cc])) +
			fieldStyle.Render(string(runes[cc+1:]))
	}
	return out
}

// viewField renders a labeled editable row: the (unstyled) prefix, then the
// field's value on a subtle background filling the rest of contentWidth.
// Continuation lines of a multi-line value are indented to the prefix width so
// every value line starts in the same column as the first. Single- and
// multi-line fields share this one code path.
func viewField(prefix string, f textfield, focused bool, contentWidth int) string {
	indentW := lipgloss.Width(prefix)
	lines := f.styledLines(focused, contentWidth-indentW)
	indent := strings.Repeat(" ", indentW)
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(lines[0])
	for _, l := range lines[1:] {
		b.WriteString("\n")
		b.WriteString(indent)
		b.WriteString(l)
	}
	return b.String()
}

// popupContentWidth is the usable text width inside a standard modal popup: the
// inner box width minus the modal's horizontal padding. Field fills target it
// so the editable slot reaches the box's right edge.
func popupContentWidth(w int) int {
	cw := popupInnerWidth(w) - modalStyle.GetHorizontalPadding()
	if cw < 1 {
		cw = 1
	}
	return cw
}
