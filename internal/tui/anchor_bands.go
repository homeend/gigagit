package tui

import "sort"

// Overview anchor bands (spec 2026-10-07-overview-anchor-bands): a file an
// overview's anchor opened draws every line / range anchor that overview
// has in it — the current one apart from the rest — and n / p walk them.

// anchorBand is one band: lines start..end (1-based) of the file, standing
// for overview anchor i (the first in document order with that range).
type anchorBand struct{ start, end, i int }

// anchorBands is the bands anchors make in path, a file of nLines lines: its
// line and range anchors (a file anchor has no line; a note anchor is the
// note's own mark), sorted by start then end, one per range, a band past the
// last line dropped and one running past it cut there.
func anchorBands(anchors []anchor, path string, nLines int) []anchorBand {
	var out []anchorBand
	seen := map[[2]int]bool{}
	for i, a := range anchors {
		t := a.target
		if t.Note != "" || t.Path != path || t.Start <= 0 || t.Start > nLines {
			continue
		}
		end := min(max(t.End, t.Start), nLines)
		k := [2]int{t.Start, end}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, anchorBand{t.Start, end, i})
	}
	sort.SliceStable(out, func(x, y int) bool {
		if out[x].start != out[y].start {
			return out[x].start < out[y].start
		}
		return out[x].end < out[y].end
	})
	return out
}

// bandOf is the index in bands of the band anchor dest makes in a file of
// nLines lines (its range clamped as anchorBands clamps it), or -1.
func bandOf(bands []anchorBand, anchors []anchor, dest string, nLines int) int {
	if dest == "" {
		return -1
	}
	for _, a := range anchors {
		t := a.target
		if a.dest != dest || t.Note != "" || t.Start <= 0 {
			continue
		}
		end := min(max(t.End, t.Start), nLines)
		for k, b := range bands {
			if b.start == t.Start && b.end == end {
				return k
			}
		}
	}
	return -1
}

// stepBand is n (dir 1) / p (-1): from band cur when the cursor line is in
// it, else from the cursor — the next band starting below it, the last
// starting above it — wrapping at the ends (wrapped says so; one band never
// "wraps"). -1 when there are none.
func stepBand(bands []anchorBand, cur, line, dir int) (int, bool) {
	n := len(bands)
	if n == 0 {
		return -1, false
	}
	if cur >= 0 && cur < n && bands[cur].start <= line && line <= bands[cur].end {
		next := cur + dir
		return ((next % n) + n) % n, (next < 0 || next >= n) && n > 1
	}
	if dir > 0 {
		for k, b := range bands {
			if b.start > line {
				return k, false
			}
		}
		return 0, n > 1
	}
	for k := n - 1; k >= 0; k-- {
		if bands[k].start < line {
			return k, false
		}
	}
	return n - 1, n > 1
}
