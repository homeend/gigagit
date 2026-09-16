package tui

// emphLevel is one display rune's emphasis in a paint mask. It replaces the
// single bool the word-diff used to carry, because the in-view search needs two
// more states than "emphasized": an ordinary hit and THE current one (spec
// §4.3). Precedence is the enum order — the current hit outranks a hit, a hit
// outranks word emphasis, and any of them outranks the syntax colour, which is
// what keeps a search hit legible on a changed line.
type emphLevel uint8

const (
	emphNone emphLevel = iota // plain: the syntax class (if any) paints
	emphWord                  // inside a word-diff span (what sanitizeCell marks)
	emphHit                   // inside a search hit
	emphCur                   // inside the CURRENT search hit
)

// hitSpan is a search hit reduced to what a painter needs: a half-open range of
// DISPLAY runes in the line the painter is about to draw, and whether it is the
// current hit. Painters never see searchHit — hitsOn converts.
type hitSpan struct {
	start, end int
	cur        bool
}

// overlayHits paints hit spans onto a display-rune emphasis mask. emph is the
// mask of the n runes starting at display-rune offset off (nil when the caller
// has no mask yet — an unlexed blame line); the spans are offsets in the SAME
// index space as off, i.e. the whole sanitized line.
//
// When nothing intersects the window the input is returned untouched, so a view
// with no search allocates nothing and renders byte-identically. When something
// does, a COPY is painted: every mask a caller hands in may be a shared cache
// value (the picker gives the same sanLine to the grid and to the output pane,
// and textdiff rows are shared across views), so writing through is a bug.
func overlayHits(emph []emphLevel, off, n int, hits []hitSpan) []emphLevel {
	if n <= 0 || len(hits) == 0 {
		return emph
	}
	var out []emphLevel
	for _, h := range hits {
		lo, hi := h.start-off, h.end-off
		if lo < 0 {
			lo = 0
		}
		if hi > n {
			hi = n
		}
		if lo >= hi {
			continue
		}
		if out == nil {
			out = make([]emphLevel, n)
			copy(out, emph)
		}
		lvl := emphHit
		if h.cur {
			lvl = emphCur
		}
		for i := lo; i < hi; i++ {
			out[i] = lvl
		}
	}
	if out == nil {
		return emph
	}
	return out
}
