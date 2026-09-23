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
	"sort"

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
func buildHunkDoc(ctx context.Context, svc *domain.Service, path string) (*hunkpick.Doc, string, *hunkRefusal) {
	if !isGitArgSafe(path) {
		return nil, "", &hunkRefusal{http.StatusBadRequest, errors.New("invalid path")}
	}
	work, werr := svc.WorktreeFile(ctx, path)
	if werr != nil {
		return nil, "", &hunkRefusal{http.StatusNotFound, fmt.Errorf("unreadable path: %w", werr)}
	}
	index, ierr := svc.ShowFile(ctx, "", path)
	if ierr != nil {
		return nil, "", &hunkRefusal{http.StatusUnprocessableEntity, errors.New("no index blob (untracked?) — stage the whole file instead")}
	}
	if textdiff.IsBinary(index) || textdiff.IsBinary(work) {
		return nil, "", &hunkRefusal{http.StatusUnprocessableEntity, errors.New("binary file — stage the whole file instead")}
	}
	if mixedEOL(work) || mixedEOL(index) {
		return nil, "", &hunkRefusal{http.StatusUnprocessableEntity, errors.New("file mixes CRLF and LF line endings — stage the whole file instead")}
	}
	sum := sha256.New()
	sum.Write(index)
	sum.Write([]byte{0})
	sum.Write(work)
	return hunkpick.FromDiff(index, work), hex.EncodeToString(sum.Sum(nil)), nil
}

// loadHunkDoc is buildHunkDoc plus the HTTP error layer, for the endpoints
// that answer a hunk request directly.
func loadHunkDoc(w http.ResponseWriter, r *http.Request, svc *domain.Service, path string) (*hunkpick.Doc, string, bool) {
	doc, hash, ref := buildHunkDoc(r.Context(), svc, path)
	if ref != nil {
		writeErr(w, ref.status, ref.err)
		return nil, "", false
	}
	return doc, hash, true
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
	doc, hash, ok := loadHunkDoc(w, r, svc, r.URL.Query().Get("path"))
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
	// Blocks is what the picks decoded to. The wire shape is either the
	// original list of block ordinals — "take these blocks' working side",
	// which is what a whole-block pick has always meant — or the per-line
	// shape the TUI's picker has always had: {"block":1,"work":[0,2],
	// "index":[1]}. Both land here.
	Blocks []stageBlockPick `json:"-"`
}

// stageBlockPick is one block's contribution to the staged content: the lines
// taken from each side, or WholeWork for the old ordinal shape.
type stageBlockPick struct {
	Block     int   `json:"block"`
	Index     []int `json:"index"`
	Work      []int `json:"work"`
	WholeWork bool  `json:"-"`
}

// decodeStagePicks parses a /api/stage-hunks body, accepting both pick shapes.
// It is its own function so the two shapes are tested without a server.
func decodeStagePicks(req *stageHunksRequest, body []byte) error {
	var raw struct {
		Path  string            `json:"path"`
		Hash  string            `json:"hash"`
		Picks []json.RawMessage `json:"picks"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("bad request body: %w", err)
	}
	req.Path, req.Hash = raw.Path, raw.Hash
	for _, p := range raw.Picks {
		var n int
		if err := json.Unmarshal(p, &n); err == nil {
			req.Blocks = append(req.Blocks, stageBlockPick{Block: n, WholeWork: true})
			continue
		}
		var b stageBlockPick
		if err := json.Unmarshal(p, &b); err != nil {
			return fmt.Errorf("bad pick %s: %w", p, err)
		}
		req.Blocks = append(req.Blocks, b)
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
	doc, hash, ok := loadHunkDoc(w, r, svc, req.Path)
	if !ok {
		return
	}
	if req.Hash != hash {
		writeErr(w, http.StatusConflict, errors.New("file changed; refresh"))
		return
	}
	doc.SetAll(hunkpick.TakeCurrent) // default: nothing staged
	blocks := doc.Blocks()
	for _, p := range req.Blocks {
		if p.Block < 0 || p.Block >= len(blocks) {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("pick %d out of range (0..%d)", p.Block, len(blocks)-1))
			return
		}
		b := blocks[p.Block]
		if p.WholeWork {
			b.Mode = hunkpick.TakeIncoming
			continue
		}
		// Line by line, the picker's own representation. The pick ORDER decides
		// the staged line order, so it is rebuilt in READING order — by line
		// number, the index side first where both sides name the same one,
		// which is how a side-by-side row shows them (old above/left of new).
		b.Mode, b.Picks = hunkpick.LineByLine, nil
		type lp struct {
			line int
			side hunkpick.Side
		}
		var want []lp
		for _, ln := range p.Index {
			if ln < 0 || ln >= len(b.Current) {
				writeErr(w, http.StatusBadRequest, fmt.Errorf("block %d: index line %d out of range", p.Block, ln))
				return
			}
			want = append(want, lp{ln, hunkpick.Current})
		}
		for _, ln := range p.Work {
			if ln < 0 || ln >= len(b.Incoming) {
				writeErr(w, http.StatusBadRequest, fmt.Errorf("block %d: working line %d out of range", p.Block, ln))
				return
			}
			want = append(want, lp{ln, hunkpick.Incoming})
		}
		sort.SliceStable(want, func(i, j int) bool {
			if want[i].line != want[j].line {
				return want[i].line < want[j].line
			}
			return want[i].side == hunkpick.Current
		})
		for _, w := range want {
			b.Picks = append(b.Picks, hunkpick.Pick{Side: w.side, Line: w.line})
		}
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
