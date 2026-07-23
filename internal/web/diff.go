package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
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
	if wt := q.Get("wt"); wt != "" {
		s.handleWorktreeDiff(w, r, wt)
		return
	}
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
	writeDiffJSON(w, d)
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

// handleWorktreeDiff serves the wt=unstaged|staged forms: the working
// tree's or the index's pending change for one file. Never cached (the
// working tree mutates without a key change): Key "" disables caching in
// the Differ.
func (s *Server) handleWorktreeDiff(w http.ResponseWriter, r *http.Request, wt string) {
	q := r.URL.Query()
	if q.Get("sha") != "" {
		writeErr(w, http.StatusBadRequest, errors.New("wt and sha are mutually exclusive"))
		return
	}
	path := q.Get("path")
	oldPath := q.Get("old")
	if oldPath == "" {
		oldPath = path
	}
	if path == "" || !isGitArgSafe(path) || !isGitArgSafe(oldPath) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
		return
	}
	var oldSrc, newSrc domain.ByteSource
	switch wt {
	case "unstaged":
		oldSrc = lenient(func(ctx context.Context) ([]byte, error) {
			return s.svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceStaged, Path: oldPath})
		})
		newSrc = lenient(func(ctx context.Context) ([]byte, error) {
			return s.svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceUnstaged, Path: path})
		})
	case "staged":
		oldSrc = lenient(func(ctx context.Context) ([]byte, error) {
			return s.svc.ShowFile(ctx, "HEAD", oldPath)
		})
		newSrc = lenient(func(ctx context.Context) ([]byte, error) {
			return s.svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceStaged, Path: path})
		})
	default:
		writeErr(w, http.StatusBadRequest, errors.New("wt must be unstaged or staged"))
		return
	}
	d, err := s.svc.Differ().Diff(r.Context(), domain.Request{Key: "", Old: oldSrc, New: newSrc})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeDiffJSON(w, d)
}

// lenient maps a byte-source error to an empty side. A missing side is
// routine here — an untracked file has no index version, a deleted file no
// working-tree bytes, a newly added path no HEAD version — and git's
// "not in index / bad revision" errors are not worth distinguishing from
// genuinely empty for a probe-quality diff (the sha form stays strict).
func lenient(src domain.ByteSource) domain.ByteSource {
	return func(ctx context.Context) ([]byte, error) {
		b, err := src(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, nil
		}
		return b, nil
	}
}

// writeDiffJSON converts an aligned diff into the /api/diff JSON contract.
// Shared by the commit (sha=) and working-tree (wt=) branches.
func writeDiffJSON(w http.ResponseWriter, d domain.Diff) {
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
