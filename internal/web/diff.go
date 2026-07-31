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
	Hunk       *int     `json:"hunk,omitempty"`
}

// diffHunksMeta tags an unstaged working-tree diff's rows with hunk
// ordinals so the client can stage hunks INLINE in the diff view (full
// context preserved) instead of a separate block list. Valid only because
// /api/diff's rows and hunkpick.FromDiff's blocks derive from the same
// textdiff alignment of the same index↔worktree pair — the count equality
// checked at the build site is the safety latch.
type diffHunksMeta struct {
	count   int
	hash    string
	rowTags []int // per aligned row; -1 = context
}

// diffHunkTags numbers each aligned row's hunk: contiguous non-Same runs
// in order, -1 for context rows — the same segmentation hunkpick's
// Doc.Blocks() yields from the same alignment.
func diffHunkTags(rows []textdiff.Row) (tags []int, count int) {
	tags = make([]int, len(rows))
	in := false
	for i, r := range rows {
		if r.Kind == textdiff.Same {
			tags[i] = -1
			in = false
			continue
		}
		if !in {
			count++
			in = true
		}
		tags[i] = count - 1
	}
	return tags, count
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	q := r.URL.Query()
	if wt := q.Get("wt"); wt != "" {
		s.handleWorktreeDiff(w, r, wt)
		return
	}
	if q.Get("left") != "" || q.Get("right") != "" {
		s.handleRevDiff(w, r)
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
		oldSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, sha+"^", oldPath) }
	}
	if status != "D" {
		newSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, sha, path) }
	}
	d, err := svc.Differ().Diff(r.Context(), domain.Request{
		Key: sha + "^.." + sha + ":" + path,
		Old: oldSrc,
		New: newSrc,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeDiffJSON(w, d, nil)
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

// handleRevDiff serves the two-revision form: one file as it stands at
// `left` against the same file at `right`. It backs the branch comparison,
// whose two sides are unrelated revisions rather than a commit and its
// parent. Both revs are commit HASHES resolved by /api/compare, never branch
// names, so the pair is immutable and the diff cache key (left..right:path)
// stays honest.
func (s *Server) handleRevDiff(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	q := r.URL.Query()
	left, right, path := q.Get("left"), q.Get("right"), q.Get("path")
	oldPath := q.Get("old")
	if oldPath == "" {
		oldPath = path
	}
	// left/right are hex-only, enforcing the doc comment above: a branch
	// name here would poison the session-lived commit↔commit diff cache.
	if !isHexSha(left) || !isHexSha(right) || !isGitArgSafe(path) || !isGitArgSafe(oldPath) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid left/right/path"))
		return
	}
	// Status has the same meaning as in the commit form, relative to the
	// right side: A = absent on the left, D = absent on the right.
	status := q.Get("status")
	var oldSrc, newSrc domain.ByteSource
	if status != "A" {
		oldSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, left, oldPath) }
	}
	if status != "D" {
		newSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, right, path) }
	}
	d, err := svc.Differ().Diff(r.Context(), domain.Request{
		Key: left + ".." + right + ":" + path,
		Old: oldSrc,
		New: newSrc,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeDiffJSON(w, d, nil)
}

// handleWorktreeDiff serves the wt=unstaged|staged forms: the working
// tree's or the index's pending change for one file. Never cached (the
// working tree mutates without a key change): Key "" disables caching in
// the Differ.
func (s *Server) handleWorktreeDiff(w http.ResponseWriter, r *http.Request, wt string) {
	svc := s.service()
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
			return svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceStaged, Path: oldPath})
		})
		newSrc = lenient(func(ctx context.Context) ([]byte, error) {
			return svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceUnstaged, Path: path})
		})
	case "staged":
		oldSrc = lenient(func(ctx context.Context) ([]byte, error) {
			return svc.ShowFile(ctx, "HEAD", oldPath)
		})
		newSrc = lenient(func(ctx context.Context) ([]byte, error) {
			return svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceStaged, Path: path})
		})
	default:
		writeErr(w, http.StatusBadRequest, errors.New("wt must be unstaged or staged"))
		return
	}
	d, err := svc.Differ().Diff(r.Context(), domain.Request{Key: "", Old: oldSrc, New: newSrc})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Inline-hunk tagging: only the unstaged form of an eligible file (no
	// rename — the hunk doc is single-path) gets hunk ordinals + the
	// freshness hash. Any refusal or a run/block count mismatch (e.g. a
	// truncated alignment) just yields an untagged diff — the client then
	// simply offers no hunk staging for it.
	var hunks *diffHunksMeta
	if wt == "unstaged" && oldPath == path && !d.Binary && !d.TooLarge && !d.Result.Truncated {
		if doc, hash, ref := buildHunkDoc(r.Context(), svc, path); ref == nil {
			tags, n := diffHunkTags(d.Result.Rows)
			if n > 0 && n == len(doc.Blocks()) {
				hunks = &diffHunksMeta{count: n, hash: hash, rowTags: tags}
			}
		}
	}
	writeDiffJSON(w, d, hunks)
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
// Shared by the commit (sha=) and working-tree (wt=) branches; a non-nil
// hunks adds inline hunk ordinals + the staging freshness hash.
func writeDiffJSON(w http.ResponseWriter, d domain.Diff, hunks *diffHunksMeta) {
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
		if hunks != nil && hunks.rowTags[i] >= 0 {
			tag := hunks.rowTags[i]
			rows[i].Hunk = &tag
		}
	}
	payload := map[string]any{
		"rows":      rows,
		"binary":    d.Binary,
		"too_large": d.TooLarge,
		"truncated": d.Result.Truncated,
	}
	if hunks != nil {
		payload["hunks"] = map[string]any{"count": hunks.count, "hash": hunks.hash}
	}
	writeJSON(w, payload)
}
