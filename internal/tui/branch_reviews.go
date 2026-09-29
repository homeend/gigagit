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

// reviewStampAt is a review's local time for a Branches sub-row: "09-28 23:37"
// for a review made in now's year, "2025-12-31 08:05" for an older one — the
// short form lets "└ Review: <date> <agent>" fit the tab's default width.
func reviewStampAt(t, now time.Time) string {
	t = t.Local()
	if t.Year() == now.Local().Year() {
		return t.Format("01-02 15:04")
	}
	return t.Format("2006-01-02 15:04")
}

// branchReviewRowBody is "  └ Review: 09-28 18:12 Claude Code": the
// Branches tab puts it at its gutter, and the two leading spaces set it in
// under the branch name, below any session sub-rows' └.
func branchReviewRowBody(r domain.ReviewHead) string {
	parts := []string{i18n.T("Review:")}
	if !r.Created.IsZero() {
		parts = append(parts, reviewStampAt(r.Created, time.Now()))
	}
	if a := strings.TrimSpace(r.Agent); a != "" {
		parts = append(parts, sanitizeLine(a))
	}
	return "  └ " + strings.Join(parts, " ")
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
