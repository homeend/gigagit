package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// pathSeg is one segment of a filesystem path plus the separator run written
// before it ("" for the first segment of a relative path, "/" or "\" — or a
// root run like "//" — otherwise).
type pathSeg struct{ sep, text string }

// splitPathSegs tokenizes a path (no trailing separator) into segments,
// preserving each segment's preceding separator run verbatim.
func splitPathSegs(s string) []pathSeg {
	var segs []pathSeg
	var cur pathSeg
	for _, r := range s {
		if r == '/' || r == '\\' {
			if cur.text != "" {
				segs = append(segs, cur)
				cur = pathSeg{}
			}
			cur.sep += string(r)
		} else {
			cur.text += string(r)
		}
	}
	return append(segs, cur)
}

// elidePath shortens a filesystem path to at most n display columns by
// dropping WHOLE segments from the middle, marked by a single "…". Segments
// survive by priority: the final segment (the file/repo name) first, then the
// directory just before it, then the path's FIRST segment (a glimpse of where
// the path starts), then alternating right/left working inward — the
// outermost parts first, the middle last. The dropped run is always
// contiguous, so the result reads head + "…" + tail. Separators are kept as
// written (/ or \), as is a trailing separator (directory headings).
//
// When not even "…/<name>" fits, the name itself is cut in the middle via
// elideNameMiddle (its beginning and extension/ending survive).
func elidePath(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	trimmed := strings.TrimRight(s, `/\`)
	trail := s[len(trimmed):]
	if trimmed == "" { // a pure separator run: nothing structured to keep
		return truncate(s, n)
	}
	segs := splitPathSegs(trimmed)
	last := len(segs) - 1
	if last == 0 {
		return elideNameMiddle(segs[0].text, n-lipgloss.Width(trail)) + trail
	}
	// build renders the kept prefix run segs[:keepL], a "…" for the dropped
	// middle [keepL,keepR), and the kept suffix run segs[keepR:]. The "…"
	// borrows the first dropped segment's separator so it slots into the path
	// ("/mnt/…/name"); with no prefix kept it leads bare ("…/name").
	build := func(keepL, keepR int) string {
		var b strings.Builder
		for i := 0; i < keepL; i++ {
			b.WriteString(segs[i].sep)
			b.WriteString(segs[i].text)
		}
		if keepL < keepR {
			if keepL > 0 {
				b.WriteString(segs[keepL].sep)
			}
			b.WriteString("…")
		}
		for i := keepR; i < len(segs); i++ {
			b.WriteString(segs[i].sep)
			b.WriteString(segs[i].text)
		}
		b.WriteString(trail)
		return b.String()
	}
	if lipgloss.Width(build(0, last)) > n {
		// Not even "…/<name>" fits: cut inside the name itself.
		return elideNameMiddle(segs[last].text, n-lipgloss.Width(trail)) + trail
	}
	// Greedily grow both kept runs inward, right first, in strict priority
	// order. A side closes permanently once its next segment no longer fits
	// (widths only grow); skipping it for a deeper segment would leave a
	// second gap.
	keepL, keepR := 0, last
	leftNext, rightNext := 0, last-1
	leftOpen, rightOpen := true, true
	for leftOpen || rightOpen {
		if rightOpen {
			if rightNext >= keepL && lipgloss.Width(build(keepL, rightNext)) <= n {
				keepR = rightNext
				rightNext--
			} else {
				rightOpen = false
			}
		}
		if leftOpen {
			if leftNext < keepR && lipgloss.Width(build(leftNext+1, keepR)) <= n {
				keepL = leftNext + 1
				leftNext++
			} else {
				leftOpen = false
			}
		}
	}
	return build(keepL, keepR)
}

// elideNameMiddle cuts a bare name (no separators) to at most n display
// columns by dropping its MIDDLE: the beginning of the name plus its
// extension — or, with no extension, its ending — survive around a "…".
func elideNameMiddle(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	tail := ""
	if dot := strings.LastIndex(s, "."); dot > 0 { // >0: a dotfile is a name, not an extension
		tail = s[dot:]
	}
	if tail == "" || lipgloss.Width(tail)+2 > n {
		// No extension, or one too wide to leave room for the beginning: the
		// ending's share is a third of the budget.
		tail = tailWidth(s, (n-1)/3)
	}
	return takeWidth(s, n-1-lipgloss.Width(tail)) + "…" + tail
}

// tailWidth returns the longest trailing run of s whose display width is <= w.
func tailWidth(s string, w int) string {
	r := []rune(s)
	for lo := 0; lo < len(r); lo++ {
		if lipgloss.Width(string(r[lo:])) <= w {
			return string(r[lo:])
		}
	}
	return ""
}
