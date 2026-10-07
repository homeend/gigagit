package tui

import (
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// The Previews list shows a preview's (or commit pair's) AI reviews as
// sub-rows under it (spec R3), like the Branches tab's review sub-rows:
// pvEntry rows, so sorting, the filter and backingIndex treat them as the
// Branches tab treats a branch's.

type pvEntry struct {
	pv     int    // index into m.previews
	review string // a review sub-row: its note id ("" = the saved row itself)
}

// sub reports a review sub-row, not a saved row.
func (e pvEntry) sub() bool { return e.review != "" }

// previewEntries is the Previews list in backing order: each row followed by
// its reviews (readPreviews classified them: current, then older tips).
func (m Model) previewEntries() []pvEntry {
	out := make([]pvEntry, 0, len(m.previews))
	for i, r := range m.previews {
		out = append(out, pvEntry{pv: i})
		for _, h := range r.reviews {
			out = append(out, pvEntry{pv: i, review: h.ID})
		}
	}
	return out
}

// previewReviewRowBody is a sub-row without its indent: the Branches
// sub-row's text, plus " · older tip" for a review of a tip the preview has
// moved past (R2: it still opens exactly what was reviewed).
func previewReviewRowBody(h domain.ReviewHead) string {
	body := branchReviewRowBody(h)
	if h.Older {
		body += " · " + i18n.T("older tip")
	}
	return body
}

func (m Model) previewReviewHead(pv int, id string) (domain.ReviewHead, bool) {
	if pv < 0 || pv >= len(m.previews) {
		return domain.ReviewHead{}, false
	}
	for _, h := range m.previews[pv].reviews {
		if h.ID == id {
			return h, true
		}
	}
	return domain.ReviewHead{}, false
}

func (m Model) selectedPreviewEntry() (pvEntry, bool) {
	idx := m.displayIndices(panelPreviews)
	sel := m.sel[panelPreviews]
	if sel < 0 || sel >= len(idx) {
		return pvEntry{}, false
	}
	ents := m.previewEntries()
	if idx[sel] >= len(ents) {
		return pvEntry{}, false
	}
	return ents[idx[sel]], true
}

// selectedPreviewReview resolves a review sub-row under the Previews cursor.
func (m Model) selectedPreviewReview() (domain.ReviewHead, bool) {
	if m.focus != panelPreviews {
		return domain.ReviewHead{}, false
	}
	e, ok := m.selectedPreviewEntry()
	if !ok || !e.sub() {
		return domain.ReviewHead{}, false
	}
	return m.previewReviewHead(e.pv, e.review)
}

// previewDisplayIndex is the display row of saved row bi — a steer landing's
// target, never one of its sub-rows.
func (m Model) previewDisplayIndex(bi int) (int, bool) {
	ents := m.previewEntries()
	for di, u := range m.displayIndices(panelPreviews) {
		if u < len(ents) && !ents[u].sub() && ents[u].pv == bi {
			return di, true
		}
	}
	return 0, false
}
