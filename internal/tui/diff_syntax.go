// Syntax-colouring helpers of the diff view: the class→colour palette, the
// per-line token lookup, and the rune-mask machinery that carries token
// classes (and word-diff coverage) through tab/control-char expansion. Split
// out of diff_render.go, which keeps the row/cell rendering itself.

package tui

import (
	"bytes"
	"strings"

	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/textdiff"
)

// tokAt returns the syntax runs of source line no (1-based) or nil for a gap
// (no == 0) or a line the lexer did not cover. The returned slice aliases the
// cached domain.Diff — READ-ONLY.
func tokAt(side [][]syntax.Tok, no int) []syntax.Tok {
	if no <= 0 || no > len(side) {
		return nil
	}
	return side[no-1]
}

// sanitizeCell expands text exactly as sanitizeLine and returns the display
// runes with two parallel masks: emph marks runes whose source raw rune is
// covered by a word-diff span (emphWord; search hits are overlaid later), cls
// carries each rune's syntax class. Raw indices are counted over the
// \r-trimmed text (matching sanitizeLine); span and token ends are clamped to
// that length.
func sanitizeCell(s string, spans []textdiff.Span, toks []syntax.Tok) (disp []rune, emph []emphLevel, cls []syntax.Class) {
	s = strings.TrimSuffix(s, "\r")
	runes := []rune(s)
	cover := coverMask(len(runes), spans)
	classes := classMask(len(runes), toks)
	col := 0
	for raw, r := range runes {
		// sanitizeCell only ever marks emphWord: search hits are overlaid on
		// top of its mask afterwards (overlayHits), never mixed in here.
		on, c := emphNone, classes[raw]
		if cover[raw] {
			on = emphWord
		}
		switch {
		case r == '\t':
			n := 4 - col%4
			for k := 0; k < n; k++ {
				disp = append(disp, ' ')
				emph = append(emph, on)
				cls = append(cls, c)
			}
			col += n
		case r < 0x20 || r == 0x7f:
			disp = append(disp, '·')
			emph = append(emph, on)
			cls = append(cls, c)
			col++
		default:
			disp = append(disp, r)
			emph = append(emph, on)
			cls = append(cls, c)
			col++
		}
	}
	return disp, emph, cls
}

// hasBareCR reports whether data holds a \r that is NOT part of a CRLF. The
// surfaces that lex their own content (blame, the file preview) treat such a
// file as unhighlightable: they turn a lone \r into a line break while
// syntax.Lex keeps it inside its line, so every line after it would receive
// the previous line's runs. A CRLF file is safe — both sides count one line.
func hasBareCR(data []byte) bool {
	return bytes.Contains(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), []byte("\r"))
}

// classMask marks raw rune indices [0,n) with their token class (ends clamped;
// later runs win on overlap, which never happens for coalesced lexer output).
func classMask(n int, toks []syntax.Tok) []syntax.Class {
	mask := make([]syntax.Class, n)
	for _, tk := range toks {
		lo, hi := tk.Start, tk.End
		if lo < 0 {
			lo = 0
		}
		if hi > n {
			hi = n
		}
		for i := lo; i < hi; i++ {
			mask[i] = tk.Class
		}
	}
	return mask
}

// coverMask marks raw rune indices [0,n) covered by any span (ends clamped).
func coverMask(n int, spans []textdiff.Span) []bool {
	mask := make([]bool, n)
	for _, sp := range spans {
		lo, hi := sp.Start, sp.End
		if lo < 0 {
			lo = 0
		}
		if hi > n {
			hi = n
		}
		for i := lo; i < hi; i++ {
			mask[i] = true
		}
	}
	return mask
}
