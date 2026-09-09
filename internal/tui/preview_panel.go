package tui

import (
	"context"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// previewRow is one saved merge preview with its live summary. A row whose
// summary read failed keeps err (rendered as the state text).
type previewRow struct {
	rec model.MergePreview
	sum domain.PreviewSummary
	err error
}

// previewsPayload is srcPreviews' dataAvailableMsg value.
type previewsPayload struct{ rows []previewRow }

// readPreviews lists the records and summarises each. An unchanged pair
// costs two rev-parse calls (name → hash is how movement is detected) and
// no diff work; only a moved pair runs the three summary calls.
func readPreviews(ctx context.Context, svc *domain.Service) (previewsPayload, error) {
	ps, err := svc.PreviewList(ctx)
	if err != nil {
		return previewsPayload{}, err
	}
	rows := make([]previewRow, 0, len(ps))
	for _, p := range ps {
		sum, err := svc.PreviewSummary(ctx, p.Source, p.Target)
		rows = append(rows, previewRow{rec: p, sum: sum, err: err})
	}
	return previewsPayload{rows: rows}, nil
}

// previewList is the panelList behind the Previews tab: Key is the record id
// (stable across renames and refreshes), Name the label, Date the creation.
type previewList struct {
	rows []previewRow
	text []string
}

func (l previewList) Len() int          { return len(l.rows) }
func (l previewList) Row(i int) string  { return l.text[i] }
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
		out = append(out, padCell(r.rec.Label, w)+"  "+pair+"  "+previewStateText(r))
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
