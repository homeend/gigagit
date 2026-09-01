package tui

import "github.com/homeend/gigagit/internal/model"

// segLayer folds development-line segments over the loaded feed incrementally —
// the coloring analog of commitgraph.Layer. Each commit gets a segment id: it
// inherits its child's segment along the first-parent edge, a merge's second+
// parent lines start fresh segments, and a boundary commit (another branch's
// territory — see Model.segBoundary) starts a fresh segment even when claimed.
// Segment ids feed lanePalette, so in a scoped (solo) view — where a linear
// history collapses to one lane and one color — the ● dots still show where one
// branch's commits end and the inherited history begins.
//
// Paging in older commits is a strict newest→oldest append, so the pending
// first-parent claims carry across calls; the claims map holds only unprocessed
// parents (pruned on processing), keeping it O(open lines), not O(history).
type segLayer struct {
	claims map[string]int // hash → smallest segment id claimed by a processed child
	next   int            // next unused segment id
}

func newSegLayer() *segLayer { return &segLayer{claims: map[string]int{}} }

// append assigns a segment id to each commit, continuing newest→oldest from the
// layer's current state. Non-topological input (a parent listed before its
// child) degrades deterministically: the not-yet-claimed row starts a fresh
// segment. When two children claim the same fork point, the smaller id wins —
// the mainline keeps its color through the fork, mirroring the lane engine's
// leftmost-lane-wins.
func (l *segLayer) append(commits []model.Commit, boundary func(model.Commit) bool) []int {
	out := make([]int, len(commits))
	for i, c := range commits {
		s, claimed := l.claims[c.Hash]
		if claimed {
			delete(l.claims, c.Hash) // prune: each claim is consumed exactly once
		}
		if !claimed || boundary(c) {
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

// rebuildSegments brings the commitSegs cache in line with m.commits. laid is
// the count already covered when the incremental state is intact (the caller's
// graphLaidReal, under the same append invariant as the lane fold); a nil layer
// or a length mismatch forces a from-scratch walk. O(new commits) on the append
// path — held-End paging must not re-walk the whole loaded history.
func (m Model) rebuildSegments(laid int) Model {
	b := m.segBoundary()
	if m.segLayer == nil || len(m.commitSegs) != laid || len(m.commits) < laid {
		m.segLayer = newSegLayer()
		m.commitSegs = m.segLayer.append(m.commits, b)
		return m
	}
	if len(m.commits) > laid {
		m.commitSegs = append(m.commitSegs, m.segLayer.append(m.commits[laid:], b)...)
	}
	return m
}

// segColorOn reports whether scoped segment coloring drives the ● dot color: a
// scoped feed (solo branch/commit/tag or a multi-branch view) whose walk is
// contiguous — a path/author/date filter lists non-adjacent commits with their
// true parents, which would shred first-parent chains into rainbow noise, so
// those fall back to plain lane color.
func (m Model) segColorOn() bool {
	return len(m.commitScopeBranches) > 0 && !m.commitFilter.filtered() &&
		len(m.commitSegs) == len(m.commits)
}

// segBoundary returns the predicate marking commits where another branch's
// territory begins in a scoped walk: a local branch tip outside the scope, or a
// remote-tracking tip that is not a scoped branch's own upstream. Tags and the
// detached-HEAD marker are never boundaries (a soloed tag's own decoration is
// deliberately covered by the tag case). The scoped refs themselves are
// excluded so the soloed branch's tip (and its upstream) never split its own
// segment.
func (m Model) segBoundary() func(model.Commit) bool {
	scoped := make(map[string]bool, len(m.commitScopeBranches))
	for _, s := range m.commitScopeBranches {
		scoped[s] = true
	}
	ownUpstream := map[string]bool{}
	for _, b := range m.branches {
		if scoped[b.Name] && b.Upstream != "" {
			ownUpstream[b.Upstream] = true
		}
	}
	return func(c model.Commit) bool {
		for _, r := range c.Refs {
			switch r.Kind {
			case model.RefLocal:
				if !scoped[r.Name] {
					return true
				}
			case model.RefRemote:
				if !ownUpstream[r.Name] {
					return true
				}
			}
		}
		return false
	}
}
