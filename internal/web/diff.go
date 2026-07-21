package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/textdiff"
)

type diffRow struct {
	Kind       string   `json:"kind"`
	Left       string   `json:"left"`
	Right      string   `json:"right"`
	LeftNo     int      `json:"left_no"`
	RightNo    int      `json:"right_no"`
	LeftSpans  [][2]int `json:"left_spans,omitempty"`
	RightSpans [][2]int `json:"right_spans,omitempty"`
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sha, path := q.Get("sha"), q.Get("path")
	if sha == "" || path == "" {
		writeErr(w, http.StatusBadRequest, errors.New("sha and path are required"))
		return
	}
	status := q.Get("status")
	oldPath := q.Get("old")
	if oldPath == "" {
		oldPath = path
	}
	// Untrusted params flow into git argv (sha as sha^ / sha, oldPath+path
	// after rev:); reject anything git would read as an option before any
	// verb sees it. isGitArgSafe lives in server.go (Task 1/3).
	if !isGitArgSafe(sha) || !isGitArgSafe(path) || !isGitArgSafe(oldPath) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid sha/path"))
		return
	}
	var oldSrc, newSrc domain.ByteSource
	if status != "A" {
		oldSrc = func(ctx context.Context) ([]byte, error) { return s.svc.ShowFile(ctx, sha+"^", oldPath) }
	}
	if status != "D" {
		newSrc = func(ctx context.Context) ([]byte, error) { return s.svc.ShowFile(ctx, sha, path) }
	}
	d, err := s.svc.Differ().Diff(r.Context(), domain.Request{
		Key: sha + "^.." + sha + ":" + path,
		Old: oldSrc,
		New: newSrc,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]diffRow, len(d.Result.Rows))
	for i, row := range d.Result.Rows {
		rows[i] = diffRow{
			Kind:       diffKindString(row.Kind),
			Left:       row.Left,
			Right:      row.Right,
			LeftNo:     row.LeftNo,
			RightNo:    row.RightNo,
			LeftSpans:  spanPairs(row.LeftSpans),
			RightSpans: spanPairs(row.RightSpans),
		}
	}
	writeJSON(w, map[string]any{
		"rows":      rows,
		"binary":    d.Binary,
		"too_large": d.TooLarge,
		"truncated": d.Result.Truncated,
	})
}

func diffKindString(k textdiff.Kind) string {
	switch k {
	case textdiff.Changed:
		return "change"
	case textdiff.Del:
		return "del"
	case textdiff.Add:
		return "add"
	}
	return "same"
}

func spanPairs(spans []textdiff.Span) [][2]int {
	if len(spans) == 0 {
		return nil
	}
	out := make([][2]int, len(spans))
	for i, sp := range spans {
		out[i] = [2]int{sp.Start, sp.End}
	}
	return out
}
