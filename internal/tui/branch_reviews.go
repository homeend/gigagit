package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// The Branches tab lists a branch's AI reviews (of its CURRENT tip) as
// sub-rows under it, always shown — the Worktrees session sub-row pattern:
// the list is entry-based, a sub-row's Name and Date are its branch's so the
// stable sort keeps it underneath, and backingIndex refuses a sub-row so no
// branch action ever lands on the wrong branch.

// brEntry is one Branches row: a branch, or one review under it.
type brEntry struct {
	b      int    // index into m.branches
	review string // a review sub-row: its note id; "" = the branch itself
}

func (e brEntry) sub() bool { return e.review != "" }

// branchReviewHeads is b's reviews on its current tip, newest first.
func (m Model) branchReviewHeads(b model.Branch) []domain.ReviewHead {
	var out []domain.ReviewHead
	for _, r := range m.noteCounts.Reviews {
		if r.Branch == b.Name && sameCommit(r.Commit, b.Hash) {
			out = append(out, r)
		}
	}
	return out
}

// sameCommit matches a review's full sha against the branch list's hash,
// which git hands out short (%(objectname:short)).
func sameCommit(full, h string) bool {
	return h != "" && len(h) >= 7 && strings.HasPrefix(full, h)
}

// branchEntries is the Branches list in backing order: each branch followed
// by its reviews.
func (m Model) branchEntries() []brEntry {
	out := make([]brEntry, 0, len(m.branches))
	for i, b := range m.branches {
		out = append(out, brEntry{b: i})
		for _, r := range m.branchReviewHeads(b) {
			out = append(out, brEntry{b: i, review: r.ID})
		}
	}
	return out
}

// branchReviewRowText is "  └ ◆ 2h · Claude Code · Review: feature".
func branchReviewRowText(r domain.ReviewHead, now time.Time) string {
	var parts []string
	if !r.Created.IsZero() {
		parts = append(parts, coarseAgo(now.Sub(r.Created)))
	}
	if a := strings.TrimSpace(r.Agent); a != "" {
		parts = append(parts, sanitizeLine(a))
	}
	parts = append(parts, sanitizeLine(r.Summary))
	return "  └ ◆ " + strings.Join(parts, " · ")
}

func (m Model) reviewHead(id string) (domain.ReviewHead, bool) {
	for _, r := range m.noteCounts.Reviews {
		if r.ID == id {
			return r, true
		}
	}
	return domain.ReviewHead{}, false
}

// selectedBranchReview resolves a review sub-row under the Branches cursor.
func (m Model) selectedBranchReview() (domain.ReviewHead, bool) {
	idx := m.displayIndices(panelBranches)
	sel := m.sel[panelBranches]
	if sel < 0 || sel >= len(idx) {
		return domain.ReviewHead{}, false
	}
	ents := m.branchEntries()
	if idx[sel] >= len(ents) || !ents[idx[sel]].sub() {
		return domain.ReviewHead{}, false
	}
	return m.reviewHead(ents[idx[sel]].review)
}

// showBranchReviewRow is the branch .-menu's "Show review": the newest review
// of the selected branch's current tip.
func (m Model) showBranchReviewRow() (actionRow, bool) {
	if m.focus != panelBranches {
		return actionRow{}, false
	}
	b, ok := m.selectedBranch()
	if !ok {
		return actionRow{}, false
	}
	heads := m.branchReviewHeads(b)
	if len(heads) == 0 {
		return actionRow{}, false
	}
	h := heads[0]
	return actionRow{
		id:    "branch-show-review",
		label: i18n.T("Show review"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openReviewNote(h.ID, h.Summary)
		},
	}, true
}
