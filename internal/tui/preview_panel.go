package tui

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// previewRow is one saved merge preview with its live summary. A row whose
// summary read failed keeps err (rendered as the state text).
type previewRow struct {
	rec    model.MergePreview
	sum    domain.PreviewSummary
	notes  int            // root notes gathered along the branch, hidden ones included
	byPath map[string]int // the same counts per path; feeds the open preview's file list
	err    error
}

// previewsPayload is srcPreviews' dataAvailableMsg value.
type previewsPayload struct{ rows []previewRow }

// readPreviews lists the records, summarises each and counts its notes. An
// unchanged pair costs two rev-parse calls for the summary (name → hash is how
// movement is detected) and no diff work; only a moved pair runs the three
// summary calls. A previewable pair then costs roughly two more, because
// PreviewNotes resolves the pair again to gather the commit range — parked for
// the final wave rather than threaded through here.
func readPreviews(ctx context.Context, svc *domain.Service) (previewsPayload, error) {
	ps, err := svc.PreviewList(ctx)
	if err != nil {
		return previewsPayload{}, err
	}
	rows := make([]previewRow, 0, len(ps))
	for _, p := range ps {
		sum, err := svc.PreviewSummary(ctx, p.Source, p.Target)
		row := previewRow{rec: p, sum: sum, err: err}
		// Ruling 6: a pair that is not previewable has no note scope at all,
		// so it gets no badge and costs no store read.
		if err == nil && sum.State == domain.PreviewOK {
			if set, serr := svc.PreviewNotes(ctx, p.Source, p.Target); serr == nil {
				if byPath, total, cerr := svc.PreviewNoteCounts(ctx, set); cerr == nil {
					row.notes, row.byPath = total, byPath
				}
			}
		}
		rows = append(rows, row)
	}
	return previewsPayload{rows: rows}, nil
}

// previewList is the panelList behind the Previews tab: Key is the record id
// (stable across renames and refreshes), Name the label, Date the creation.
type previewList struct {
	rows []previewRow
	text []string
}

func (l previewList) Len() int         { return len(l.rows) }
func (l previewList) Row(i int) string { return l.text[i] }

// Haystack is the filter-match text: the row WITHOUT its ◆N note badge (the
// statusList contract), so typing a digit never matches a pair because of how
// many notes it carries.
func (l previewList) Haystack(i int) string {
	return strings.TrimSuffix(l.text[i], noteBadge(l.rows[i].notes))
}

func (l previewList) Name(i int) string { return l.rows[i].rec.Label }
func (l previewList) Date(i int) int64  { return l.rows[i].rec.Created.Unix() }
func (l previewList) Key(i int) string  { return l.rows[i].rec.ID }

// previewStateText is the right-hand cell: counts when ok, else the state.
func previewStateText(r previewRow) string {
	if r.err != nil {
		return i18n.T("error: %s", r.err.Error())
	}
	switch r.sum.State {
	case domain.PreviewMerged:
		return i18n.T("merged")
	case domain.PreviewMissingSource:
		return i18n.T("missing: %s", r.rec.Source)
	case domain.PreviewMissingTarget:
		return i18n.T("missing: %s", r.rec.Target)
	case domain.PreviewNoBase:
		return i18n.T("no common base")
	}
	if r.sum.Files == 1 {
		return i18n.T("1 file  ↑%d", r.sum.Ahead)
	}
	return i18n.T("%d files  ↑%d", r.sum.Files, r.sum.Ahead)
}

// previewRows renders "<label>  <source → target>  <state>" with the label
// column padded to the widest label (display width, CJK-safe).
func (m Model) previewRows() []string {
	labels := make([]string, len(m.previews))
	for i, r := range m.previews {
		labels[i] = r.rec.Label
	}
	w := maxLabelWidth(8, labels...)
	out := make([]string, 0, len(m.previews))
	for _, r := range m.previews {
		pair := r.rec.Source + " → " + r.rec.Target
		// The SAME ◆N badge every other note-bearing row wears (it brings its
		// own leading gap), never a glyph of this panel's own.
		out = append(out, padCell(r.rec.Label, w)+"  "+pair+"  "+previewStateText(r)+noteBadge(r.notes))
	}
	return out
}

// selectedPreview is the focused row when the Previews tab has one.
func (m Model) selectedPreview() (previewRow, bool) {
	i, ok := m.backingIndex(panelPreviews)
	if !ok || i >= len(m.previews) {
		return previewRow{}, false
	}
	return m.previews[i], true
}
