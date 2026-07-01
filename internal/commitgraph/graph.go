// Package commitgraph lays out a single-line-per-commit Unicode commit graph:
// ordered commits (hash + parents, newest-first) → per-row lane glyph cells.
// Pure; no git/TUI/lipgloss imports.
package commitgraph

// Commit is the engine's minimal input.
type Commit struct {
	Hash    string
	Parents []string
}

// Row is the laid-out graph for one commit. Cells is padded to the shared Width.
type Row struct {
	Cells string
	Lane  int
	Width int
}

// MaxLanes is the absolute ceiling on rendered/cached plane width, in lanes.
// Lane *assignment* is never capped (it is bounded by the data); only the
// emitted cell string is clamped, to bound memory on pathological histories.
// A higher value is never reachable — config can only lower the cap.
const MaxLanes = 320

// Lay folds commits (newest-first) into per-row graph cells, fit to a uniform
// width. Deterministic. Equivalent to a single Layer.Append over all commits
// followed by fitting every row to the final width.
func Lay(commits []Commit) ([]Row, int) {
	var l Layer
	rows := l.Append(commits)
	width := l.Width()
	for i := range rows {
		rows[i].Width = width
		rows[i].Cells = fit(rows[i].Cells, width)
	}
	return rows, width
}

// Layer lays commits incrementally, preserving the open-lane state across calls
// so paging in older commits (a strict newest→oldest append) is O(new commits)
// instead of re-laying the whole history. Rows returned by Append carry their
// natural (unpadded) Cells and node Lane; the caller pads them to Width() — the
// running plane width, which grows only when a page introduces more concurrent
// lanes. The zero value is a ready empty layer.
type Layer struct {
	lanes    []string // lanes[i] = hash lane i is waiting for ("" = free)
	maxLanes int      // widest lane count seen so far (uncapped)
}

// Append lays commits (continuing newest→oldest from the current state) and
// returns one Row per commit. Cells is the natural glyph string (2 columns per
// live lane at that row, before trailing-free compaction), NOT padded to a
// uniform width; pad with the caller's fit against Width(). Deterministic.
func (l *Layer) Append(commits []Commit) []Row {
	rows := make([]Row, 0, len(commits))
	for _, c := range commits {
		// 1. node lane = leftmost lane targeting this commit; extras = merging.
		node := -1
		var merging []int
		for i, t := range l.lanes {
			if t == c.Hash {
				if node < 0 {
					node = i
				} else {
					merging = append(merging, i)
				}
			}
		}
		if node < 0 {
			node = firstFree(l.lanes, nil)
			if node == len(l.lanes) {
				l.lanes = append(l.lanes, "")
			}
		}
		mergeSet := toSet(merging)

		// 2. outgoing targets: first parent keeps the node lane; extras fork into
		//    fresh free lanes (never reusing a merging slot on this row, to keep
		//    glyphs unambiguous). A root frees its lane.
		var forks []int
		if len(c.Parents) == 0 {
			l.lanes[node] = ""
		} else {
			l.lanes[node] = c.Parents[0]
			for _, p := range c.Parents[1:] {
				f := firstFree(l.lanes, mergeSet)
				if f == len(l.lanes) {
					l.lanes = append(l.lanes, "")
				}
				l.lanes[f] = p
				forks = append(forks, f)
			}
		}
		for _, mi := range merging { // free the merged-in children's lanes
			l.lanes[mi] = ""
		}

		// 3. render over the current lane count, then compact trailing frees.
		n := len(l.lanes)
		if n > l.maxLanes {
			l.maxLanes = n
		}
		rows = append(rows, Row{Cells: renderRow(n, node, merging, forks, l.lanes), Lane: node})
		for len(l.lanes) > 0 && l.lanes[len(l.lanes)-1] == "" {
			l.lanes = l.lanes[:len(l.lanes)-1]
		}
	}
	return rows
}

// Width is the current plane width in display columns (2 per lane), clamped to
// MaxLanes. It only ever grows across Append calls.
func (l *Layer) Width() int {
	m := l.maxLanes
	if m > MaxLanes {
		m = MaxLanes
	}
	return m * 2
}

// renderRow draws one commit row across n lanes. Two display columns per lane:
// the lane glyph + a horizontal connector ('─' inside the node↔fork/merge span,
// else space). minC..maxC is that span.
func renderRow(n, node int, merging, forks []int, lanes []string) string {
	mergeSet, forkSet := toSet(merging), toSet(forks)
	minC, maxC := node, node
	for _, c := range append(append([]int{}, merging...), forks...) {
		if c < minC {
			minC = c
		}
		if c > maxC {
			maxC = c
		}
	}
	out := make([]rune, 0, n*2)
	for c := 0; c < n; c++ {
		var g rune
		switch {
		case c == node:
			g = '●'
		case mergeSet[c]: // child terminating from above, turning toward node
			switch {
			case c > minC && c < maxC:
				g = '┴'
			case c < node:
				g = '╰'
			default:
				g = '╯'
			}
		case forkSet[c]: // extra parent opening below, from node
			switch {
			case c > minC && c < maxC:
				g = '┬'
			case c < node:
				g = '╭'
			default:
				g = '╮'
			}
		case lanes[c] != "" && c > minC && c < maxC:
			g = '┼' // pass-through lane crossed by the horizontal span
		case lanes[c] != "":
			g = '│'
		case c > minC && c < maxC:
			g = '─' // empty column under the horizontal span
		default:
			g = ' '
		}
		out = append(out, g)
		if c >= minC && c < maxC {
			out = append(out, '─')
		} else {
			out = append(out, ' ')
		}
	}
	return string(out)
}

func firstFree(lanes []string, exclude map[int]bool) int {
	for i, t := range lanes {
		if t == "" && !exclude[i] {
			return i
		}
	}
	return len(lanes)
}

func toSet(xs []int) map[int]bool {
	if len(xs) == 0 {
		return nil
	}
	s := make(map[int]bool, len(xs))
	for _, x := range xs {
		s[x] = true
	}
	return s
}

// fit pads s with spaces up to w runes, or truncates it to w runes.
func fit(s string, w int) string {
	r := []rune(s)
	if len(r) > w {
		return string(r[:w])
	}
	for len(r) < w {
		r = append(r, ' ')
	}
	return string(r)
}
