// Package textdiff aligns two texts line-by-line for side-by-side display.
// It is pure presentation support — no git semantics — and deliberately has
// no git/TUI imports so the M3 conflict editor and MCP can reuse it.
package textdiff

import (
	"bytes"
	"strings"
)

// Kind classifies one aligned row.
type Kind int

const (
	Same    Kind = iota // line present and equal on both sides
	Changed             // paired old/new line, both present, different
	Del                 // left only; the right cell is a gap
	Add                 // right only; the left cell is a gap
)

// Row is one display row of the side-by-side view. Line numbers are 1-based;
// 0 means "no line on that side" (the gap cell of a Del/Add row).
type Row struct {
	Kind    Kind
	Left    string
	Right   string
	LeftNo  int
	RightNo int
}

// Result is a full alignment. Blocks holds the row index of the first row of
// each contiguous non-Same run — the jump targets for prev/next change.
// Truncated reports that a size guard replaced real alignment with one
// del-run/add-run replace block.
type Result struct {
	Rows      []Row
	Blocks    []int
	Truncated bool
}

// The guards run on the TRIMMED middle, so a small change in a huge file
// still aligns perfectly. maxLines caps the Myers input size; maxEditD caps
// the edit distance (Myers is O((N+M)·D) — two large fully-different files
// would otherwise spin for minutes).
const (
	maxLines = 50000
	maxEditD = 2000
)

// IsBinary reports whether content looks binary: a NUL byte within the
// first 8000 bytes (git's own heuristic).
func IsBinary(b []byte) bool {
	n := len(b)
	if n > 8000 {
		n = 8000
	}
	return bytes.IndexByte(b[:n], 0) >= 0
}

// Compare aligns old and new line-by-line. Compare(nil, x) is a new file
// (all Add); Compare(x, nil) a deleted one (all Del).
func Compare(old, new []byte) Result {
	a, aNL := splitLines(old)
	b, bNL := splitLines(new)
	if len(a) > 0 && len(b) > 0 && aNL != bNL {
		// Only the final newline differs (or differs among other changes):
		// keep one extra empty row on the side that HAS the newline, so a
		// newline-at-EOF-only change is visible instead of rendering two
		// identical panes for a file git reports as modified.
		if aNL {
			a = append(a, "")
		} else {
			b = append(b, "")
		}
	}

	pre := commonPrefix(a, b)
	suf := commonSuffix(a[pre:], b[pre:]) // operating past pre caps pre+suf ≤ min(len)
	midA := a[pre : len(a)-suf]
	midB := b[pre : len(b)-suf]

	rows := make([]Row, 0, len(a)+len(b))
	for i := 0; i < pre; i++ {
		rows = append(rows, Row{Kind: Same, Left: a[i], Right: a[i], LeftNo: i + 1, RightNo: i + 1})
	}

	truncated := false
	var mid []Row
	if len(midA) > maxLines || len(midB) > maxLines {
		truncated = true
	} else if script, ok := myers(midA, midB); ok {
		mid = alignRows(midA, midB, script, pre+1, pre+1)
	} else {
		truncated = true
	}
	if truncated {
		oldNo, newNo := pre+1, pre+1
		for _, l := range midA {
			mid = append(mid, Row{Kind: Del, Left: l, LeftNo: oldNo})
			oldNo++
		}
		for _, l := range midB {
			mid = append(mid, Row{Kind: Add, Right: l, RightNo: newNo})
			newNo++
		}
	}
	rows = append(rows, mid...)

	for i := 0; i < suf; i++ {
		l := a[len(a)-suf+i]
		rows = append(rows, Row{
			Kind: Same, Left: l, Right: l,
			LeftNo:  len(a) - suf + i + 1,
			RightNo: len(b) - suf + i + 1,
		})
	}

	var blocks []int
	for i := range rows {
		if rows[i].Kind != Same && (i == 0 || rows[i-1].Kind == Same) {
			blocks = append(blocks, i)
		}
	}
	return Result{Rows: rows, Blocks: blocks, Truncated: truncated}
}

// splitLines splits content into lines, reporting whether it ended with a
// newline (the phantom empty last line is dropped here; Compare re-adds one
// row when only one side has the final newline).
func splitLines(b []byte) ([]string, bool) {
	if len(b) == 0 {
		return nil, false
	}
	s := string(b)
	nl := strings.HasSuffix(s, "\n")
	if nl {
		s = s[:len(s)-1]
	}
	return strings.Split(s, "\n"), nl
}

func commonPrefix(a, b []string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

func commonSuffix(a, b []string) int {
	n := 0
	for n < len(a) && n < len(b) && a[len(a)-1-n] == b[len(b)-1-n] {
		n++
	}
	return n
}

// editOp is one step of the edit script: equal, delete (from old), add (to new).
type editOp byte

const (
	opEq  editOp = 'e'
	opDel editOp = 'd'
	opAdd editOp = 'a'
)

// myers computes the forward Myers O(ND) edit script from a to b, giving up
// (ok=false) once the edit distance would exceed maxEditD. Window snapshots
// (2d+1 ints per round) keep traceback memory bounded by the budget.
func myers(a, b []string) (script []editOp, ok bool) {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil, true
	}
	budget := n + m
	if budget > maxEditD {
		budget = maxEditD
	}
	// Ensure the v array is large enough for the direction-choice reads
	// (v[offset±1]) even when budget is tiny (e.g. n+m == 1).
	if budget < 1 {
		budget = 1
	}
	offset := budget
	v := make([]int, 2*budget+1)
	var trace [][]int
	dFound := -1
	for d := 0; d <= budget && dFound < 0; d++ {
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1] // step down: one line added
			} else {
				x = v[offset+k-1] + 1 // step right: one line deleted
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[offset+k] = x
			if x >= n && y >= m {
				dFound = d
				break
			}
		}
		win := make([]int, 2*d+1)
		copy(win, v[offset-d:offset+d+1])
		trace = append(trace, win)
	}
	if dFound < 0 {
		return nil, false
	}

	// Traceback from (n, m); ops come out reversed.
	var rev []editOp
	x, y := n, m
	for d := dFound; d > 0; d-- {
		win := trace[d-1] // window of round d-1, indices -(d-1)..(d-1)
		woff := d - 1
		k := x - y
		var prevK int
		if k == -d || (k != d && win[woff+k-1] < win[woff+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := win[woff+prevK]
		prevY := prevX - prevK
		for x > prevX && y > prevY { // the snake
			rev = append(rev, opEq)
			x--
			y--
		}
		if x == prevX {
			rev = append(rev, opAdd)
			y--
		} else {
			rev = append(rev, opDel)
			x--
		}
		x, y = prevX, prevY
	}
	for x > 0 { // round 0 is a pure equal prefix
		rev = append(rev, opEq)
		x--
		y--
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev, true
}

// alignRows turns an edit script into aligned rows: each maximal non-equal
// run is zipped (its deletions paired with its additions as Changed rows,
// the longer side's tail as Del/Add rows). oldNo/newNo are the 1-based line
// numbers of the first middle lines.
func alignRows(a, b []string, script []editOp, oldNo, newNo int) []Row {
	var rows []Row
	i, j := 0, 0
	for p := 0; p < len(script); {
		if script[p] == opEq {
			rows = append(rows, Row{Kind: Same, Left: a[i], Right: b[j], LeftNo: oldNo, RightNo: newNo})
			i++
			j++
			oldNo++
			newNo++
			p++
			continue
		}
		q := p
		var dels, adds int
		for q < len(script) && script[q] != opEq {
			if script[q] == opDel {
				dels++
			} else {
				adds++
			}
			q++
		}
		zip := dels
		if adds < zip {
			zip = adds
		}
		for z := 0; z < zip; z++ {
			rows = append(rows, Row{Kind: Changed, Left: a[i], Right: b[j], LeftNo: oldNo, RightNo: newNo})
			i++
			j++
			oldNo++
			newNo++
		}
		for z := zip; z < dels; z++ {
			rows = append(rows, Row{Kind: Del, Left: a[i], LeftNo: oldNo})
			i++
			oldNo++
		}
		for z := zip; z < adds; z++ {
			rows = append(rows, Row{Kind: Add, Right: b[j], RightNo: newNo})
			j++
			newNo++
		}
		p = q
	}
	return rows
}
