package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// The Files panel's Review row (spec 2026-10-04 working reviews §7): while a
// review of this worktree's uncommitted changes is current, a row at the top
// of the panel opens it, and each file it still matches carries ✎. The row
// is a pseudo row: its backing index is reviewRowIdx, which backingIndex,
// rowKeyAt and selectedKey refuse — every file action already guards that ok,
// and the walkers that index m.status.Files skip it.

const reviewRowIdx = -1

// workingReviewsMsg is a fresh read of this worktree's working reviews.
type workingReviewsMsg struct {
	reviews []domain.WorkingReview
	err     error
}

// loadWorkingReviewsCmd reads and matches the working reviews off-thread;
// nil when the counts say there are none (the read would find nothing).
func (m Model) loadWorkingReviewsCmd() tea.Cmd {
	svc := m.svc
	if svc == nil || len(m.noteCounts.WorkingReviews) == 0 {
		return nil
	}
	return func() tea.Msg {
		rs, err := svc.WorkingReviews(context.Background())
		return workingReviewsMsg{reviews: rs, err: err}
	}
}

// currentWorkingReview is the review the row opens: the newest current one.
func (m Model) currentWorkingReview() (domain.WorkingReview, bool) {
	for _, r := range m.workingReviews { // newest first
		if r.Current {
			return r, true
		}
	}
	return domain.WorkingReview{}, false
}

// reviewRowShown: the row is in the Files panel — a review is current and
// no / filter narrows the panel to files.
func (m Model) reviewRowShown() bool {
	if m.filterActive(panelFiles) {
		return false
	}
	_, ok := m.currentWorkingReview()
	return ok
}

// reviewedPaths are the files a current review still matches: their ✎.
func (m Model) reviewedPaths() map[string]bool {
	var out map[string]bool
	for _, r := range m.workingReviews {
		if !r.Current {
			continue
		}
		for p, st := range r.States {
			if st == domain.WorkingFileMatches {
				if out == nil {
					out = map[string]bool{}
				}
				out[p] = true
			}
		}
	}
	return out
}

// withWorkingReviews installs a fresh read, keeping the Files cursor on the
// file it was on when the row comes or goes.
func (m Model) withWorkingReviews(rs []domain.WorkingReview) Model {
	before := m.reviewRowShown()
	m.workingReviews = rs
	after := m.reviewRowShown()
	switch {
	case !before && after:
		m.sel[panelFiles]++
	case before && !after && m.sel[panelFiles] > 0:
		m.sel[panelFiles]--
	}
	return m.withReviewRowIdx()
}

// withReviewRowIdx refreshes the Files panel's prefixed fast path: filesIdx
// with the Review row's sentinel in front (nil while the row is not shown).
func (m Model) withReviewRowIdx() Model {
	m.filesIdxReview = nil
	if _, ok := m.currentWorkingReview(); ok && m.filesIdx != nil {
		m.filesIdxReview = append([]int{reviewRowIdx}, m.filesIdx...)
	}
	return m
}

// filesPanelFileCount is the Files tab's count: its rows minus the Review row.
func (m Model) filesPanelFileCount() int {
	n := m.panelLen(panelFiles)
	if m.reviewRowShown() {
		n--
	}
	return n
}

// workingReviewRowText is the row: "✎ Review: 10-05 14:12 Claude Code".
func workingReviewRowText(r domain.WorkingReview) string {
	parts := []string{"✎", i18n.T("Review:")}
	if !r.Created.IsZero() {
		parts = append(parts, reviewStampAt(r.Created, clock.Now()))
	}
	if a := strings.TrimSpace(r.Agent); a != "" {
		parts = append(parts, sanitizeLine(a))
	}
	return strings.Join(parts, " ")
}

// onReviewRow: the Files cursor is on the Review row.
func (m Model) onReviewRow() bool {
	if m.focus != panelFiles {
		return false
	}
	idx := m.displayIndices(panelFiles)
	s := m.sel[panelFiles]
	return s >= 0 && s < len(idx) && idx[s] == reviewRowIdx
}

// workingReviewRowMenu is the whole "." menu on the Review row: Open and
// Delete (user ruling 2026-10-05 for note rows).
func (m Model) workingReviewRowMenu() ([]actionRow, bool) {
	if !m.onReviewRow() {
		return nil, false
	}
	r, _ := m.currentWorkingReview()
	id, summary, quote := r.ID, r.Summary, reviewQuote(r.Agent, r.Summary)
	return []actionRow{
		{id: "open-review", label: i18n.T("Open review"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openReview(id, summary)
		}},
		{id: "delete-review", label: i18n.T("Delete review"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmStoredDelete(id, i18n.T("Delete this review?")+"\n"+quote, true)
		}},
	}, true
}
