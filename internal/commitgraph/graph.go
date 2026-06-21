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

// Lay folds commits (newest-first) into per-row graph cells. Deterministic.
func Lay(commits []Commit) ([]Row, int) {
	rows := make([]Row, 0, len(commits))
	var lanes []string // lanes[i] = hash lane i is waiting for ("" = free)
	maxLanes := 0

	for _, c := range commits {
		// 1. node lane = leftmost lane targeting this commit; extras = merging.
		node := -1
		var merging []int
		for i, t := range lanes {
			if t == c.Hash {
				if node < 0 {
					node = i
				} else {
					merging = append(merging, i)
				}
			}
		}
		if node < 0 {
			node = firstFree(lanes, nil)
			if node == len(lanes) {
				lanes = append(lanes, "")
			}
		}
		mergeSet := toSet(merging)

		// 2. outgoing targets: first parent keeps the node lane; extras fork into
		//    fresh free lanes (never reusing a merging slot on this row, to keep
		//    glyphs unambiguous). A root frees its lane.
		var forks []int
		if len(c.Parents) == 0 {
			lanes[node] = ""
		} else {
			lanes[node] = c.Parents[0]
			for _, p := range c.Parents[1:] {
				f := firstFree(lanes, mergeSet)
				if f == len(lanes) {
					lanes = append(lanes, "")
				}
				lanes[f] = p
				forks = append(forks, f)
			}
		}
		for _, mi := range merging { // free the merged-in children's lanes
			lanes[mi] = ""
		}

		// 3. render over the current lane count, then compact trailing frees.
		n := len(lanes)
		if n > maxLanes {
			maxLanes = n
		}
		rows = append(rows, Row{Cells: renderRow(n, node, merging, forks, lanes), Lane: node})
		for len(lanes) > 0 && lanes[len(lanes)-1] == "" {
			lanes = lanes[:len(lanes)-1]
		}
	}

	if maxLanes > MaxLanes {
		maxLanes = MaxLanes
	}
	width := maxLanes * 2
	for i := range rows {
		rows[i].Width = width
		rows[i].Cells = fit(rows[i].Cells, width)
	}
	return rows, width
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
