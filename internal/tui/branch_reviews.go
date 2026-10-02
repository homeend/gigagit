package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/clock"
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

// branchReviewRowBody is a review sub-row without its indent: "└ Review:
// 09-28 18:12 Claude Code". The Branches tab sets it in under the branch
// name, in the session sub-rows' column.
func branchReviewRowBody(r domain.ReviewHead) string {
	parts := []string{i18n.T("Review:")}
	if !r.Created.IsZero() {
		parts = append(parts, reviewStampAt(r.Created, clock.Now()))
	}
	if a := strings.TrimSpace(r.Agent); a != "" {
		parts = append(parts, sanitizeLine(a))
	}
	return "└ " + strings.Join(parts, " ")
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

// commitReviewed reports whether commit hash holds a review — the Commits
// list's ✎: a stored AI review or a range review (notes written in a commit
// pair, which sit on the pair's newer commit), created on the branch being
// viewed. A preview's review never marks a commit: it is the preview's. It reads
// the note counts the list already holds, so a scroll costs no read.
func (m Model) commitReviewed(hash string) bool {
	if hash == "" {
		return false
	}
	if len(m.shownScopes(hash)) > 0 {
		return true
	}
	view := m.viewBranches()
	for _, r := range m.noteCounts.Reviews {
		if r.Commit == hash && domain.ReviewShownOn(r.Branch, view) {
			return true
		}
	}
	return false
}

// viewBranches are the branches the reader is ON: the ones the commit list is
// narrowed to (solo / a scope), else the checked-out branch. nil when neither
// is known (a detached HEAD): nothing can be told apart then.
func (m Model) viewBranches() []string {
	if len(m.commitScopeBranches) > 0 {
		return m.commitScopeBranches
	}
	if m.status.Branch != "" {
		return []string{m.status.Branch}
	}
	return nil
}

// shownScopes are the range reviews commit hash shows on the viewed branch:
// what was created on a branch is shown on that branch and no other — not on
// the target it was merged into (domain.ReviewShownOn) — and a preview's
// review never on a commit. Hidden, never deleted: on its own branch it is back.
func (m Model) shownScopes(hash string) []domain.NoteScopeCount {
	return m.noteCounts.ScopesShownOn(hash, m.viewBranches())
}
