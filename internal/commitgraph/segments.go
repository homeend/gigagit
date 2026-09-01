package commitgraph

// SegmentLayer folds development-line segments over a commit list
// incrementally — the coloring analog of Layer. Each commit gets a segment id:
// it inherits its child's segment along the first-parent edge, a merge's
// second+ parent lines start fresh segments, and a boundary commit (another
// branch's territory, decided by the caller) starts a fresh segment even when
// claimed. Segment ids index a recycled palette, so in a scoped (solo) view —
// where a linear history collapses to one lane and one color — the node dots
// still show where one branch's commits end and inherited history begins.
//
// Paging in older commits is a strict newest→oldest append, so the pending
// first-parent claims carry across calls; the claims map holds only
// unprocessed parents (pruned on processing), keeping it O(open lines), not
// O(history). The zero value is a ready empty layer.
type SegmentLayer struct {
	claims map[string]int // hash → smallest segment id claimed by a processed child
	next   int            // next unused segment id
}

// Append assigns a segment id to each commit, continuing newest→oldest from
// the layer's current state. boundary is asked per row index of THIS call.
// Non-topological input (a parent listed before its child) degrades
// deterministically: the not-yet-claimed row starts a fresh segment. When two
// children claim the same fork point, the smaller id wins — the mainline keeps
// its color through the fork, mirroring the lane engine's leftmost-lane-wins.
func (l *SegmentLayer) Append(commits []Commit, boundary func(i int) bool) []int {
	if l.claims == nil {
		l.claims = map[string]int{}
	}
	out := make([]int, len(commits))
	for i, c := range commits {
		s, claimed := l.claims[c.Hash]
		if claimed {
			delete(l.claims, c.Hash) // prune: each claim is consumed exactly once
		}
		if !claimed || boundary(i) {
			s = l.next
			l.next++
		}
		out[i] = s
		if len(c.Parents) > 0 {
			p := c.Parents[0]
			if prev, ok := l.claims[p]; !ok || s < prev {
				l.claims[p] = s
			}
		}
	}
	return out
}
