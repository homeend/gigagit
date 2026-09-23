package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/hunkpick"
	"github.com/homeend/gigagit/internal/textdiff"
)

type hunkBlock struct {
	Index int      `json:"index"`
	Del   []string `json:"del"`
	Add   []string `json:"add"`
}

// hunkRefusal is a hunk-eligibility failure with its HTTP mapping —
// loadHunkDoc writes it; the diff handler's inline-hunk tagging drops
// metadata silently instead.
type hunkRefusal struct {
	status int
	err    error
}

// buildHunkDoc reads a tracked file's index + worktree bytes and builds the
// hunk doc, mapping the TUI's guards to typed refusals. The hash is the
// freshness token a stage POST must echo (picks are positional — valid
// only against the exact bytes the client saw). Mixed EOL is refused
// (dominant-EOL rejoin would silently normalize the minority); consistent
// CRLF round-trips since hunkpick's EOL fix.
// hunkLane is which diff a row selection is made in, and so what "staging a
// row" means for it. Both lanes build the doc in the orientation the reader
// SEES (left = old, right = new), so a row's ordinal names the same row on the
// screen and in the doc.
type hunkLane string

const (
	// laneUnstaged is the index → working-tree diff: a selected row takes its
	// working-tree version into the index (stage).
	laneUnstaged hunkLane = "unstaged"
	// laneStaged is the HEAD → index diff: a selected row puts HEAD's version
	// back into the index (unstage).
	laneStaged hunkLane = "staged"
)

// hunkSides reads the two versions a lane compares: index → working tree for
// the unstaged diff, HEAD → index for the staged one.
func hunkSides(ctx context.Context, svc *domain.Service, path string, lane hunkLane) (old, nw []byte, ref *hunkRefusal) {
	switch lane {
	case laneStaged:
		index, ierr := svc.ShowFile(ctx, "", path)
		if ierr != nil {
			return nil, nil, &hunkRefusal{http.StatusNotFound, fmt.Errorf("no staged version: %w", ierr)}
		}
		head, herr := svc.ShowFile(ctx, "HEAD", path)
		if herr != nil {
			return nil, nil, &hunkRefusal{http.StatusUnprocessableEntity, errors.New("not in HEAD (a newly added file) — unstage the whole file instead")}
		}
		return head, index, nil
	default:
		work, werr := svc.WorktreeFile(ctx, path)
		if werr != nil {
			return nil, nil, &hunkRefusal{http.StatusNotFound, fmt.Errorf("unreadable path: %w", werr)}
		}
		index, ierr := svc.ShowFile(ctx, "", path)
		if ierr != nil {
			return nil, nil, &hunkRefusal{http.StatusUnprocessableEntity, errors.New("no index blob (untracked?) — stage the whole file instead")}
		}
		return index, work, nil
	}
}

func buildHunkDoc(ctx context.Context, svc *domain.Service, path string, lane hunkLane) (*hunkpick.Doc, [][]blockRow, string, *hunkRefusal) {
	if !isGitArgSafe(path) {
		return nil, nil, "", &hunkRefusal{http.StatusBadRequest, errors.New("invalid path")}
	}
	old, nw, ref := hunkSides(ctx, svc, path, lane)
	if ref != nil {
		return nil, nil, "", ref
	}
	if textdiff.IsBinary(old) || textdiff.IsBinary(nw) {
		return nil, nil, "", &hunkRefusal{http.StatusUnprocessableEntity, errors.New("binary file — stage the whole file instead")}
	}
	if mixedEOL(old) || mixedEOL(nw) {
		return nil, nil, "", &hunkRefusal{http.StatusUnprocessableEntity, errors.New("file mixes CRLF and LF line endings — stage the whole file instead")}
	}
	sum := sha256.New()
	sum.Write([]byte(lane))
	sum.Write([]byte{0})
	sum.Write(old)
	sum.Write([]byte{0})
	sum.Write(nw)
	// The rows are the SAME alignment FromDiff makes (textdiff.Compare with
	// zero options), so the doc's blocks and the row walk agree by construction.
	rows := hunkBlockRows(textdiff.Compare(old, nw, textdiff.Options{}).Rows)
	return hunkpick.FromDiff(old, nw), rows, hex.EncodeToString(sum.Sum(nil)), nil
}

// loadHunkDoc is buildHunkDoc plus the HTTP error layer, for the endpoints
// that answer a hunk request directly.
func loadHunkDoc(w http.ResponseWriter, r *http.Request, svc *domain.Service, path string, lane hunkLane) (*hunkpick.Doc, [][]blockRow, string, bool) {
	doc, rows, hash, ref := buildHunkDoc(r.Context(), svc, path, lane)
	if ref != nil {
		writeErr(w, ref.status, ref.err)
		return nil, nil, "", false
	}
	return doc, rows, hash, true
}

// mixedEOL reports whether b mixes CRLF and bare-LF line endings — the one
// case hunkpick's dominant-EOL rejoin would still silently normalize.
// Consistent CRLF round-trips byte-faithfully since the hunkpick EOL fix.
func mixedEOL(b []byte) bool {
	crlf := bytes.Count(b, []byte("\r\n"))
	return crlf > 0 && bytes.Count(b, []byte("\n")) > crlf
}

// handleHunks lists a file's unstaged change blocks plus the freshness
// hash a stage-hunks POST must echo back.
func (s *Server) handleHunks(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	doc, _, hash, ok := loadHunkDoc(w, r, svc, r.URL.Query().Get("path"), laneUnstaged)
	if !ok {
		return
	}
	blocks := doc.Blocks()
	rows := make([]hunkBlock, 0, len(blocks))
	for i, b := range blocks {
		rows = append(rows, hunkBlock{Index: i, Del: b.Current, Add: b.Incoming})
	}
	writeJSON(w, map[string]any{"count": len(rows), "hash": hash, "blocks": rows})
}

type stageHunksRequest struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
	// Lane is the diff the selection was made in: "unstaged" (the default —
	// stage the rows) or "staged" (unstage them).
	Lane hunkLane `json:"lane"`
	// Blocks is the selection: per block, the ROWS picked (their ordinal
	// within the block, as /api/diff tags them in "hr"), or Whole for the
	// hunk. The original wire — a bare list of block ordinals in "picks" —
	// still decodes, as whole blocks.
	Blocks []stageBlockPick `json:"-"`
}

// stageBlockPick is one block's selection: whole, or some of its rows.
type stageBlockPick struct {
	Block int   `json:"block"`
	Rows  []int `json:"rows"`
	Whole bool  `json:"whole"`
}

// decodeStagePicks parses a /api/stage-hunks body. It is its own function so
// the shapes are tested without a server.
func decodeStagePicks(req *stageHunksRequest, body []byte) error {
	var raw struct {
		Path   string            `json:"path"`
		Hash   string            `json:"hash"`
		Lane   hunkLane          `json:"lane"`
		Picks  []json.RawMessage `json:"picks"`
		Blocks []stageBlockPick  `json:"blocks"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("bad request body: %w", err)
	}
	req.Path, req.Hash, req.Lane = raw.Path, raw.Hash, raw.Lane
	if req.Lane == "" {
		req.Lane = laneUnstaged
	}
	if req.Lane != laneUnstaged && req.Lane != laneStaged {
		return fmt.Errorf("lane must be unstaged or staged, not %q", req.Lane)
	}
	for _, p := range raw.Picks {
		var n int
		if err := json.Unmarshal(p, &n); err != nil {
			return fmt.Errorf("bad pick %s: want a block ordinal", p)
		}
		req.Blocks = append(req.Blocks, stageBlockPick{Block: n, Whole: true})
	}
	req.Blocks = append(req.Blocks, raw.Blocks...)
	return nil
}

// blockRow is one row of a change block, in the order the reader sees it:
// its kind and which line of the block it is on each side (-1 = none). It is
// the unit the web stages — a modified row is ONE change (its old line
// replaced by its new one), a removed-only row its old line, an added-only
// row its new line.
type blockRow struct {
	cur, inc int // line within the block's Current (left) / Incoming (right) side
}

// hunkBlockRows groups aligned rows into their blocks, numbering each row's
// line per side exactly as hunkpick.FromDiff fills Current (the LEFT of
// Changed/Del rows) and Incoming (the RIGHT of Changed/Add rows).
func hunkBlockRows(rows []textdiff.Row) [][]blockRow {
	var out [][]blockRow
	in := false
	cur, inc := 0, 0
	for _, r := range rows {
		if r.Kind == textdiff.Same {
			in = false
			continue
		}
		if !in {
			out = append(out, nil)
			in = true
			cur, inc = 0, 0
		}
		br := blockRow{cur: -1, inc: -1}
		switch r.Kind {
		case textdiff.Changed:
			br.cur, br.inc = cur, inc
			cur++
			inc++
		case textdiff.Del:
			br.cur = cur
			cur++
		case textdiff.Add:
			br.inc = inc
			inc++
		}
		out[len(out)-1] = append(out[len(out)-1], br)
	}
	return out
}

// applyRowSelection decides every block of doc from a row selection, the way
// git add -p stages lines: a SELECTED row contributes its target version, an
// unselected row keeps its current one. Staging (the unstaged lane) targets
// the right side — the working tree — and keeps the left — the index;
// unstaging (the staged lane, HEAD → index) targets the left — HEAD — and
// keeps the right — the index. Blocks the selection does not name keep their
// current version whole. The resolved doc is the NEW INDEX content.
func applyRowSelection(doc *hunkpick.Doc, brows [][]blockRow, lane hunkLane, sel []stageBlockPick) error {
	keep, take := hunkpick.TakeCurrent, hunkpick.TakeIncoming
	if lane == laneStaged {
		keep, take = hunkpick.TakeIncoming, hunkpick.TakeCurrent
	}
	doc.SetAll(keep)
	blocks := doc.Blocks()
	if len(blocks) != len(brows) {
		return fmt.Errorf("the file's blocks moved (%d vs %d); refresh", len(blocks), len(brows))
	}
	for _, p := range sel {
		if p.Block < 0 || p.Block >= len(blocks) {
			return fmt.Errorf("block %d out of range (0..%d)", p.Block, len(blocks)-1)
		}
		b := blocks[p.Block]
		if p.Whole {
			b.Mode = take
			continue
		}
		picked := make(map[int]bool, len(p.Rows))
		for _, r := range p.Rows {
			if r < 0 || r >= len(brows[p.Block]) {
				return fmt.Errorf("block %d: row %d out of range", p.Block, r)
			}
			picked[r] = true
		}
		// Row by row, in reading order: the picks' ORDER is the staged order.
		b.Mode, b.Picks = hunkpick.LineByLine, nil
		for i, row := range brows[p.Block] {
			useLeft := !picked[i] // the staged lane inverts below
			if lane == laneStaged {
				useLeft = picked[i]
			}
			if useLeft {
				if row.cur >= 0 {
					b.Picks = append(b.Picks, hunkpick.Pick{Side: hunkpick.Current, Line: row.cur})
				}
			} else if row.inc >= 0 {
				b.Picks = append(b.Picks, hunkpick.Pick{Side: hunkpick.Incoming, Line: row.inc})
			}
		}
	}
	return nil
}

// handleStageHunks stages a selection of a file's change blocks through
// the TUI's own machinery: recompute the doc fresh, verify the freshness
// hash (409 on drift), flip the picked blocks to the working-tree side,
// and stage the resolved content via engine.StageHunks.
func (s *Server) handleStageHunks(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	var req stageHunksRequest
	if err := decodeStagePicks(&req, body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Blocks) == 0 {
		writeErr(w, http.StatusBadRequest, errors.New("picks required"))
		return
	}
	doc, brows, hash, ok := loadHunkDoc(w, r, svc, req.Path, req.Lane)
	if !ok {
		return
	}
	if req.Hash != hash {
		writeErr(w, http.StatusConflict, errors.New("file changed; refresh"))
		return
	}
	if err := applyRowSelection(doc, brows, req.Lane, req.Blocks); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	content, resolved := doc.Resolved()
	if !resolved {
		writeErr(w, http.StatusInternalServerError, errors.New("unresolved hunks"))
		return
	}
	if _, err := runOp(r.Context(), svc, engine.StageHunks{Path: req.Path, Content: content}); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.writeStatus(w, r) // success response = fresh status (the /api/stage convention)
}
