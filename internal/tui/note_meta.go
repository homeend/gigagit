package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/homeend/gigagit/internal/notemeta"
	"github.com/homeend/gigagit/internal/syntax"
)

// The TUI's rendering of note meta (spec 2026-10-12-note-meta §6): every
// surface paints a notemeta.Resolved through ONE mechanism — the meta
// pseudo-classes below ride a syntax.Class mask (like the markdown classes),
// so a badge is coloured the same on a note box, a send-panel row or a file
// row. Agent keys and values are data: shown verbatim, never translated.

// metaColours is the declaration's colour vocabulary in class order.
var metaColours = [7]string{"red", "orange", "yellow", "green", "blue", "purple", "grey"}

// The meta pseudo-classes: the text style's seven, then the filled style's.
const (
	metaClassStart syntax.Class = 200
	metaClassEnd   syntax.Class = metaClassStart + 14
)

// metaClass is colour in style as a mask class; 0 (plain) for a colour
// outside the vocabulary.
func metaClass(color, style string) syntax.Class {
	for i, c := range metaColours {
		if c == color {
			if style == notemeta.StyleFilled {
				return metaClassStart + 7 + syntax.Class(i)
			}
			return metaClassStart + syntax.Class(i)
		}
	}
	return 0
}

// metaStyle paints c over base: a text class colours the foreground, a
// filled one the background with the theme's ground as its text.
func (s *styles) metaStyle(base lipgloss.Style, c syntax.Class) lipgloss.Style {
	i := int(c - metaClassStart)
	if i >= 7 {
		return base.Background(s.metaFg[i-7]).Foreground(s.metaFillFg)
	}
	return base.Foreground(s.metaFg[i])
}
