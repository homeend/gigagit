package tui

import (
	"strings"

	"github.com/homeend/gigagit/internal/i18n"
)

// Painting a stack's two synthetic row kinds: the per-file header and the
// one-line placeholder that stands in for a body there is nothing to show for.
// Everything else on the screen is the ordinary diff renderer.

// stackRow renders one header or placeholder display row, padded to the full
// width. A header reads "▾ M  path  +12 −3" (▸ when the file is folded, and
// "old → new" for a rename); with the cursor on it, it takes the cursor-row
// style so j/k landing on a header is visible.
func (m Model) stackRow(v *diffView, dr dRow, w int, onCursor bool) string {
	f, ok := v.stackFileAt(dr.line)
	if !ok {
		return ""
	}
	s := st()
	switch dr.kind {
	case lineGap: // the blank line above a file's header
		return ""
	case lineRule: // the rule under it, the full width of the screen
		return s.diffFold.Render(strings.Repeat("─", w))
	}
	if dr.kind == linePlace {
		return truncate(s.diffFold.Render(placeText(f)), w)
	}
	mark := "▾"
	if f.collapsed {
		mark = "▸"
	}
	name := f.path
	if f.oldPath != "" {
		name = f.oldPath + " → " + f.path
	}
	text := mark + " " + f.status + "  " + name
	switch {
	case f.bin || (f.d != nil && f.d.binary):
		text += "  " + i18n.T("bin")
	case f.counted:
		text += "  " + i18n.T("+%d −%d", f.add, f.del)
	}
	style := s.pickerLabel
	if onCursor {
		style = s.diffCursorRow
	}
	return style.Render(padRight(truncate(text, w), w))
}

// placeText is the single line a file shows in place of a body: why there is
// nothing to read there. The wording matches the single-file view's own body
// states, so a stack says exactly what one file at a time would say.
func placeText(f stackFile) string {
	switch {
	case f.conflict:
		return i18n.T("  conflict — enter opens the resolver")
	case f.d == nil && f.load == stackLoading:
		return i18n.T("  (loading…)")
	case f.d == nil:
		return i18n.T("  (not loaded yet)")
	case f.d.err != nil:
		return i18n.T("  error: %s", f.d.err.Error())
	case f.d.binary:
		return i18n.T("  (binary file)")
	case f.d.tooLarge:
		return i18n.T("  (file too large)")
	}
	return i18n.T("  (no content difference)")
}
