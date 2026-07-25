package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

// loadHunkDoc reads a tracked file's index + worktree bytes and builds the
// hunk doc, mapping the TUI's guards to HTTP statuses. The hash is the
// freshness token a stage POST must echo (picks are positional — valid
// only against the exact bytes the client saw). Mixed EOL is refused
// (dominant-EOL rejoin would silently normalize the minority); consistent
// CRLF round-trips since hunkpick's EOL fix.
func loadHunkDoc(w http.ResponseWriter, r *http.Request, svc *domain.Service, path string) (*hunkpick.Doc, string, bool) {
	if !isGitArgSafe(path) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
		return nil, "", false
	}
	work, werr := svc.WorktreeFile(r.Context(), path)
	if werr != nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("unreadable path: %w", werr))
		return nil, "", false
	}
	index, ierr := svc.ShowFile(r.Context(), "", path)
	if ierr != nil {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("no index blob (untracked?) — stage the whole file instead"))
		return nil, "", false
	}
	if textdiff.IsBinary(index) || textdiff.IsBinary(work) {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("binary file — stage the whole file instead"))
		return nil, "", false
	}
	if mixedEOL(work) || mixedEOL(index) {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("file mixes CRLF and LF line endings — stage the whole file instead"))
		return nil, "", false
	}
	sum := sha256.New()
	sum.Write(index)
	sum.Write([]byte{0})
	sum.Write(work)
	return hunkpick.FromDiff(index, work), hex.EncodeToString(sum.Sum(nil)), true
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
	Path  string `json:"path"`
	Picks []int  `json:"picks"`
	Hash  string `json:"hash"`
}

// handleStageHunks stages a selection of a file's change blocks through
// the TUI's own machinery: recompute the doc fresh, verify the freshness
// hash (409 on drift), flip the picked blocks to the working-tree side,
// and stage the resolved content via engine.StageHunks.
func (s *Server) handleStageHunks(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	var req stageHunksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	if len(req.Picks) == 0 {
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
	for _, p := range req.Picks {
		if p < 0 || p >= len(blocks) {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("pick %d out of range (0..%d)", p, len(blocks)-1))
			return
		}
		blocks[p].Mode = hunkpick.TakeIncoming
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
