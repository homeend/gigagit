package web

import (
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// handleReviewDiff serves /api/diff?review=<id>&path=<p>: one file of a
// review as the change the review covered (spec
// 2026-10-11-overview-split-diff §8) — /api/diff's rows plus whether the
// review changed the file, its status and old path, the sides' labels and
// the review's remarks on both sides (read-only).
func (s *Server) handleReviewDiff(w http.ResponseWriter, r *http.Request, id string) {
	path := r.URL.Query().Get("path")
	if !isGitArgSafe(path) {
		writeErr(w, http.StatusBadRequest, errors.New("a valid path is required"))
		return
	}
	rd, err := s.service().ReviewFileDiff(r.Context(), id, path, tokModeOf(r))
	if errors.Is(err, domain.ErrReviewNotFound) {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if tokModeOf(r) == domain.TokOnly { // the colours a defer_tok answer left out
		writeJSON(w, tokPayload(rd.Diff))
		return
	}
	p := diffPayload(rd.Diff, nil)
	p["changed"] = rd.Changed
	p["status"] = rd.Status
	p["old_path"] = rd.OldPath
	p["left_label"] = rd.Left
	p["right_label"] = rd.Right
	p["remarks"] = remarkRows(rd.Notes)
	writeJSON(w, p)
}

// overviewRemarkRows are a permanent overview's remarks on the wire: the
// review rows, owned by the overview (the page titles the box so).
func overviewRemarkRows(ns []domain.ResolvedNote) []noteRow {
	out := remarkRows(ns)
	for i := range out {
		out[i].Kind = "overview"
	}
	return out
}

// remarkRows are a review's remarks on the wire: read-only boxes, an
// old-side one marked so the page lays it under the deleted line.
func remarkRows(ns []domain.ResolvedNote) []noteRow {
	out := make([]noteRow, 0, len(ns))
	for _, n := range ns {
		row := noteRow{ID: n.Note.ID, Start: n.Range[0], End: max(n.Range[1], n.Range[0]), Summary: n.Note.Summary,
			Rationale: n.Note.Rationale, Author: n.Note.Author, ReadOnly: true, Kind: "review",
			Display: rowDisplay(domain.NoteMeta(n.Note, nil))}
		if n.Note.Side == model.NoteSideOld {
			row.Side = "old"
		}
		out = append(out, row)
	}
	return out
}
