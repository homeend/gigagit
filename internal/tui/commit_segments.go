package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/commitgraph"
	"github.com/homeend/gigagit/internal/model"
)

// segAppend runs the shared segment walker (commitgraph.SegmentLayer) over
// commits, translating the per-commit boundary predicate to the layer's
// per-index one. Segment ids feed lanePalette so scoped-view ● dots show where
// one branch's commits end and inherited history begins.
func segAppend(l *commitgraph.SegmentLayer, commits []model.Commit, boundary func(model.Commit) bool) []int {
	cs := make([]commitgraph.Commit, len(commits))
	for i, c := range commits {
		cs[i] = commitgraph.Commit{Hash: c.Hash, Parents: c.Parents}
	}
	return l.Append(cs, func(i int) bool { return boundary(commits[i]) })
}

// rebuildSegments brings the commitSegs cache in line with m.commits. laid is
// the count already covered when the incremental state is intact (the caller's
// graphLaidReal, under the same append invariant as the lane fold); a nil layer
// or a length mismatch forces a from-scratch walk. O(new commits) on the append
// path — held-End paging must not re-walk the whole loaded history.
func (m Model) rebuildSegments(laid int) Model {
	b := m.segBoundary()
	if m.segLayer == nil || len(m.commitSegs) != laid || len(m.commits) < laid {
		m.segLayer = &commitgraph.SegmentLayer{}
		m.commitSegs = segAppend(m.segLayer, m.commits, b)
		return m
	}
	if len(m.commits) > laid {
		m.commitSegs = append(m.commitSegs, segAppend(m.segLayer, m.commits[laid:], b)...)
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
	boundaries := m.segBoundaryHashes
	return func(c model.Commit) bool {
		// Merge-base fork points (from ScopeBoundaries): the only marker that
		// survives the base branch moving on past the fork — its tip then sits
		// outside the scoped walk and leaves no decoration below.
		if boundaries[c.Hash] {
			return true
		}
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

// scopeBoundariesMsg carries the merge-base fork points for one scope
// signature; a msg whose sig no longer matches the live scope is dropped.
type scopeBoundariesMsg struct {
	sig    string
	hashes map[string]bool
}

// loadScopeBoundariesCmd queries the fork commits between the scoped entries
// and every OTHER local branch (domain.ScopeBoundaries, off the UI thread).
// nil when the feed is unscoped or there is no other branch to fork from. The
// result recolors the segments via scopeBoundariesMsg; decorations alone miss
// a base branch whose tip moved past the fork.
func (m Model) loadScopeBoundariesCmd() tea.Cmd {
	if len(m.commitScopeBranches) == 0 {
		return nil
	}
	scope := append([]string(nil), m.commitScopeBranches...)
	scoped := make(map[string]bool, len(scope))
	for _, s := range scope {
		scoped[s] = true
	}
	var others []string
	for _, b := range m.branches {
		if !scoped[b.Name] {
			others = append(others, b.Name)
		}
	}
	if len(others) == 0 {
		return nil
	}
	svc := m.svc
	sig := m.feedScopeSig()
	return func() tea.Msg {
		hs, err := svc.ScopeBoundaries(context.Background(), scope, others)
		set := make(map[string]bool, len(hs))
		if err == nil {
			for _, h := range hs {
				set[h] = true
			}
		}
		return scopeBoundariesMsg{sig: sig, hashes: set}
	}
}
