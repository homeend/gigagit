package tui

import (
	"fmt"
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

// styledLines renders the buffer as styled display lines, each background-filled
// to exactly width columns so the editable slot is visible even when empty. A
// logical line (split on '\n') longer than width is HARD-WRAPPED into
// consecutive width-wide chunks — one display line each — so viewField's uniform
// continuation indent applies to every wrapped piece (otherwise an over-long
// line overflows the box and the modal re-wraps its tail to column 0, under the
// label, with a stray background segment). When focused, the cell at the cursor
// is drawn as a light block. Each display line is self-contained styled segments
// (no nesting) so the background never bleeds past a reset.
func (f textfield) styledLines(focused bool, width int) []string {
	lines, _ := f.styledLinesCursor(focused, width)
	return lines
}

// styledLinesCursor is styledLines plus the index (within the returned slice)
// of the display line holding the cursor, or -1 when unfocused. Computed by
// the SAME chunk walk that renders the lines, so a windowing caller
// (viewFieldWindow) can never disagree with the render about where the cursor
// sits — the two must share one source of truth or the scroll-to-cursor logic
// drifts the moment the chunking rules change.
func (f textfield) styledLinesCursor(focused bool, width int) ([]string, int) {
	if width < 1 {
		width = 1
	}
	logical := strings.Split(string(f.runes), "\n")
	curLine, curCol := -1, -1
	if focused {
		curLine, curCol = f.cursorLineCol()
	}
	var out []string
	cursorIdx := -1
	for li, ln := range logical {
		runes := []rune(ln)
		n := len(runes)
		// Display chunks: enough width-wide slices to cover the line (at least
		// one, so an empty line still shows the slot), plus one extra when the
		// cursor sits exactly at a full-width line end (it belongs at the start
		// of a fresh chunk, not overflowing the last one by a cell).
		chunks := n / width
		if chunks == 0 || n%width != 0 {
			chunks++
		}
		if li == curLine && curCol == n && n > 0 && n%width == 0 {
			chunks++
		}
		for ci := 0; ci < chunks; ci++ {
			start := ci * width
			var chunk []rune
			if start < n {
				end := start + width
				if end > n {
					end = n
				}
				chunk = append(chunk, runes[start:end]...)
			}
			if pad := width - len(chunk); pad > 0 {
				chunk = append(chunk, []rune(strings.Repeat(" ", pad))...)
			}
			if li == curLine && curCol >= start && curCol < start+width {
				cc := curCol - start
				cursorIdx = len(out)
				out = append(out, fieldStyle.Render(string(chunk[:cc]))+
					fieldCursorStyle.Render(string(chunk[cc:cc+1]))+
					fieldStyle.Render(string(chunk[cc+1:])))
			} else {
				out = append(out, fieldStyle.Render(string(chunk)))
			}
		}
	}
	return out, cursorIdx
}

// viewField renders a labeled editable row: the (unstyled) prefix, then the
// field's value on a subtle background filling the rest of contentWidth.
// Continuation lines of a multi-line value are indented to the prefix width so
// every value line starts in the same column as the first. Single- and
// multi-line fields share this one code path.
func viewField(prefix string, f textfield, focused bool, contentWidth int) string {
	return joinFieldLines(prefix, f.styledLines(focused, contentWidth-lipgloss.Width(prefix)))
}

// viewFieldWindow is viewField with an internal vertical scrolling window:
// at most maxLines display lines are shown (maxLines <= 0 = unlimited). When
// the value overflows, the last window line becomes a dim "(from-to/total)"
// scroll marker and the window is scrolled so the cursor's display line stays
// visible; *scroll is the caller-owned offset (persisted across renders so an
// unfocused field doesn't jump). This is what keeps a popup holding a long
// (AI-generated / squashed) commit body inside the terminal height —
// overlayCenter silently drops rows past termH, so an unwindowed field pushed
// the footer (and its own tail) off-screen with no way to see them.
func viewFieldWindow(prefix string, f textfield, focused bool, contentWidth, maxLines int, scroll *int) string {
	indentW := lipgloss.Width(prefix)
	lines, cursorIdx := f.styledLinesCursor(focused, contentWidth-indentW)
	total := len(lines)
	if maxLines <= 0 || total <= maxLines {
		*scroll = 0
		return joinFieldLines(prefix, lines)
	}
	view := maxLines - 1 // the last line is the scroll marker
	if view < 1 {
		view = 1
	}
	s := *scroll
	if s > total-view {
		s = total - view
	}
	if s < 0 {
		s = 0
	}
	if cursorIdx >= 0 {
		if cursorIdx < s {
			s = cursorIdx
		}
		if cursorIdx >= s+view {
			s = cursorIdx - view + 1
		}
	}
	*scroll = s
	out := joinFieldLines(prefix, lines[s:s+view])
	// Wordless marker (pure numbers) — nothing to translate, so it stays
	// outside the i18n catalog on purpose.
	marker := fmt.Sprintf("(%d-%d/%d)", s+1, s+view, total)
	return out + "\n" + strings.Repeat(" ", indentW) + dimRowStyle.Render(marker)
}

// joinFieldLines lays field display lines out under a label prefix: the first
// line follows the prefix, continuations indent to the prefix width (the
// viewField layout, shared with the windowed variant).
func joinFieldLines(prefix string, lines []string) string {
	indent := strings.Repeat(" ", lipgloss.Width(prefix))
	var b strings.Builder
	b.WriteString(prefix)
	if len(lines) > 0 {
		b.WriteString(lines[0])
	}
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
