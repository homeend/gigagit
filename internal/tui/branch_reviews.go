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
// sub-rows under it, always shown, after its session sub-rows: they are
// brEntry rows (worktree_sessions.go), so sorting, filtering and
// backingIndex treat them exactly like a session row.

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

// branchReviewRowBody is "└ ◆ 2h · Claude Code · Review: feature"; the
// Branches tab indents it to its gutter, like a session sub-row.
func branchReviewRowBody(r domain.ReviewHead, now time.Time) string {
	var parts []string
	if !r.Created.IsZero() {
		parts = append(parts, coarseAgo(now.Sub(r.Created)))
	}
	if a := strings.TrimSpace(r.Agent); a != "" {
		parts = append(parts, sanitizeLine(a))
	}
	parts = append(parts, sanitizeLine(r.Summary))
	return "└ ◆ " + strings.Join(parts, " · ")
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
	e, ok := m.selectedBranchEntry()
	if !ok || e.review == "" {
		return domain.ReviewHead{}, false
	}
	return m.reviewHead(e.review)
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
			return m.openReview(h.ID, h.Summary)
		},
	}, true
}
