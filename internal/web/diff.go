package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/textdiff"
)

type diffRow struct {
	Kind       string      `json:"kind"`
	Left       string      `json:"left"`
	Right      string      `json:"right"`
	LeftNo     int         `json:"left_no"`
	RightNo    int         `json:"right_no"`
	LeftSpans  [][2]int    `json:"left_spans,omitempty"`
	RightSpans [][2]int    `json:"right_spans,omitempty"`
	LeftTok    []tokTriple `json:"left_tok,omitempty"`
	RightTok   []tokTriple `json:"right_tok,omitempty"`
	Hunk       *int        `json:"hunk,omitempty"`
	// HunkIndexLine / HunkWorkLine are this row's line WITHIN its hunk, on the
	// index and working-tree sides — what a per-line pick names.
	HunkIndexLine *int `json:"hl,omitempty"`
	HunkWorkLine  *int `json:"hw,omitempty"`
	// HunkRow is the row's ordinal within its hunk: what a row selection sends.
	HunkRow *int `json:"hr,omitempty"`
}

// tokTriple is one syntax run on the wire: [start, end, class-suffix].
type tokTriple [3]any

// unstyledOnWire are classes style.css has no .tk-* rule for (the rules are
// scoped to table.diff and #blame-body — a new coloured surface must join
// that selector list or its spans paint nothing): sending them
// would spend wire bytes (Name alone is roughly a third of a Go file's runs)
// and split renderCell's runs for a span that paints nothing. The TUI palette
// leaves the same classes blank.
var unstyledOnWire = map[syntax.Class]bool{syntax.Name: true}

// tokTriples returns the wire form of side's syntax runs for source line no
// (1-based), or nil when there is no line, the line has no styled runs, or
// highlighting was not computed for this diff.
func tokTriples(side [][]syntax.Tok, no int) []tokTriple {
	if no <= 0 || no > len(side) || len(side[no-1]) == 0 {
		return nil
	}
	line := side[no-1]
	out := make([]tokTriple, 0, len(line))
	for _, tk := range line {
		if unstyledOnWire[tk.Class] {
			continue
		}
		out = append(out, tokTriple{tk.Start, tk.End, tk.Class.String()})
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
	lane    hunkLane
	rowTags hunkRowTags
}

// sameBlockShape checks that the displayed rows split into the same blocks of
// the same row counts as the doc's own alignment. The displayed diff comes
// from the Differ, the doc from hunkpick.FromDiff; if they ever disagree, no
// row can be staged safely, so the diff simply goes untagged.
func sameBlockShape(tags hunkRowTags, brows [][]blockRow) bool {
	counts := make([]int, len(brows))
	for i, h := range tags.hunk {
		if h < 0 {
			continue
		}
		if h >= len(brows) {
			return false
		}
		counts[h]++
		if tags.row[i] != counts[h]-1 {
			return false
		}
	}
	for b := range brows {
		if counts[b] != len(brows[b]) {
			return false
		}
	}
	return true
}

// hunkRowTags is, per aligned row, which hunk it belongs to and which line of
// that hunk it is on each side. -1 means "none": a context row has no hunk, a
// deletion has no working-side line, an addition no index-side line. The line
// indexes are what let the client pick a block LINE BY LINE, the way the TUI's
// picker does — without them a row can only say "I belong to block 3".
type hunkRowTags struct {
	hunk  []int
	row   []int // the row's ordinal within its block — the unit a selection names
	index []int // line within the block's Current (left) side
	work  []int // line within the block's Incoming (right) side
}

// diffHunkTags numbers each aligned row's hunk: contiguous non-Same runs
// in order, -1 for context rows — the same segmentation hunkpick's
// Doc.Blocks() yields from the same alignment — and numbers each row's line
// within its block per side, mirroring hunkpick.FromDiff exactly (Current
// takes the LEFT of Changed/Del rows in order, Incoming the RIGHT of
// Changed/Add rows). The two walks live in ONE function so they cannot drift.
func diffHunkTags(rows []textdiff.Row) (tags hunkRowTags, count int) {
	tags = hunkRowTags{
		hunk:  make([]int, len(rows)),
		row:   make([]int, len(rows)),
		index: make([]int, len(rows)),
		work:  make([]int, len(rows)),
	}
	in := false
	cur, inc, nrow := 0, 0, 0 // lines used so far on each side, rows so far, in the CURRENT block
	for i, r := range rows {
		tags.index[i], tags.work[i], tags.row[i] = -1, -1, -1
		if r.Kind == textdiff.Same {
			tags.hunk[i] = -1
			in = false
			continue
		}
		if !in {
			count++
			in = true
			cur, inc, nrow = 0, 0, 0
		}
		tags.hunk[i] = count - 1
		tags.row[i] = nrow
		nrow++
		switch r.Kind {
		case textdiff.Changed:
			tags.index[i], tags.work[i] = cur, inc
			cur++
			inc++
		case textdiff.Del:
			tags.index[i] = cur
			cur++
		case textdiff.Add:
			tags.work[i] = inc
			inc++
		}
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
		Key:     sha + "^.." + sha + ":" + path,
		Path:    path,
		OldPath: oldPathFor(oldPath, path),
		Old:     oldSrc,
		New:     newSrc,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeDiffJSON(w, d, nil)
}

// oldPathFor is the OLD side's path for a domain.Request: empty unless it
// actually differs from path. The handlers default a missing `old` query
// param to path, while Request.OldPath's contract is "empty = same as Path".
func oldPathFor(oldPath, path string) string {
	if oldPath == path {
		return ""
	}
	return oldPath
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
		Key:     left + ".." + right + ":" + path,
		Path:    path,
		OldPath: oldPathFor(oldPath, path),
		Old:     oldSrc,
		New:     newSrc,
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
	d, err := svc.Differ().Diff(r.Context(), domain.Request{Key: "", Path: path, OldPath: oldPathFor(oldPath, path), Old: oldSrc, New: newSrc})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Inline-hunk tagging: only the unstaged form of an eligible file (no
	// rename — the hunk doc is single-path) gets hunk ordinals + the
	// freshness hash. Any refusal or a run/block count mismatch (e.g. a
	// truncated alignment) just yields an untagged diff — the client then
	// simply offers no hunk staging for it.
	// Both lanes are tagged now: the unstaged diff stages rows, the staged diff
	// (HEAD → index) unstages them.
	var hunks *diffHunksMeta
	if oldPath == path && !d.Binary && !d.TooLarge && !d.Result.Truncated {
		lane := hunkLane(wt)
		if doc, brows, hash, ref := buildHunkDoc(r.Context(), svc, path, lane); ref == nil {
			tags, n := diffHunkTags(d.Result.Rows)
			// The latch: the rows the reader sees must be the rows the doc was
			// built from, block for block and row for row — a row ordinal is
			// only meaningful if both sides number the same rows.
			if n > 0 && n == len(doc.Blocks()) && sameBlockShape(tags, brows) {
				hunks = &diffHunksMeta{count: n, hash: hash, lane: lane, rowTags: tags}
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
			LeftTok:    tokTriples(d.OldTok, row.LeftNo),
			RightTok:   tokTriples(d.NewTok, row.RightNo),
		}
		if hunks != nil && hunks.rowTags.hunk[i] >= 0 {
			tag := hunks.rowTags.hunk[i]
			rows[i].Hunk = &tag
			if li := hunks.rowTags.index[i]; li >= 0 {
				l := li
				rows[i].HunkIndexLine = &l
			}
			if wi := hunks.rowTags.work[i]; wi >= 0 {
				wl := wi
				rows[i].HunkWorkLine = &wl
			}
			hr := hunks.rowTags.row[i]
			rows[i].HunkRow = &hr
		}
	}
	payload := map[string]any{
		"rows":      rows,
		"binary":    d.Binary,
		"too_large": d.TooLarge,
		"truncated": d.Result.Truncated,
	}
	if hunks != nil {
		payload["hunks"] = map[string]any{"count": hunks.count, "hash": hunks.hash, "lane": hunks.lane}
	}
	writeJSON(w, payload)
}
