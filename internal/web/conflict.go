package web

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/hunkpick"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

// conflictPayload is the paused-op object /api/status carries whenever a
// sequencer op (merge/rebase/cherry-pick/revert) is in progress — including
// paused with every conflict resolved (conflicted == 0), which is what lets
// the client's Continue light up. Absent entirely when nothing is paused.
type conflictPayload struct {
	Op         string `json:"op"`
	Source     string `json:"source,omitempty"`
	Target     string `json:"target,omitempty"`
	Desc       string `json:"desc,omitempty"` // domain's human phrase ("merging feature into main")
	Conflicted int    `json:"conflicted"`
}

// conflictItem is one run of the conflicted file in order: passthrough text
// (kind "text") or a decidable block (kind "block"). index has NO omitempty —
// block 0 must reach the client.
type conflictItem struct {
	Kind   string   `json:"kind"`
	Lines  []string `json:"lines,omitempty"`
	Index  int      `json:"index"`
	Ours   []string `json:"ours,omitempty"`
	Theirs []string `json:"theirs,omitempty"`
}

// loadConflictDoc resolves path's eligibility against a FRESH status (the
// discard precedent: unknown → 404, known-but-not-conflicted → 422), reads
// the working-tree bytes, and parses the conflict markers. The hash is the
// freshness token resolve-hunks must echo — picks are positional, valid only
// against the exact bytes the client saw.
func loadConflictDoc(w http.ResponseWriter, r *http.Request, svc *domain.Service, path string) (*hunkpick.Doc, string, bool) {
	ctx := r.Context()
	if !isGitArgSafe(path) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
		return nil, "", false
	}
	st, err := svc.Status(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return nil, "", false
	}
	known, unmerged := false, false
	for _, f := range st.Files {
		if f.Path == path {
			known, unmerged = true, f.Kind == model.KindUnmerged
			break
		}
	}
	if !known {
		writeErr(w, http.StatusNotFound, errors.New("unknown path"))
		return nil, "", false
	}
	if !unmerged {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("not conflicted"))
		return nil, "", false
	}
	work, err := svc.WorktreeFile(ctx, path)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return nil, "", false
	}
	if textdiff.IsBinary(work) {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("binary file — resolve in your editor, then mark resolved"))
		return nil, "", false
	}
	doc, perr := hunkpick.ParseConflict(work)
	if perr != nil || len(doc.Blocks()) == 0 {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("no usable conflict markers — resolve in your editor, then mark resolved"))
		return nil, "", false
	}
	sum := sha256.Sum256(work)
	return doc, hex.EncodeToString(sum[:]), true
}

// handleConflictHunks lists a conflicted file's pickable blocks with the
// passthrough text between them, plus the freshness hash a resolve POST must
// echo back.
func (s *Server) handleConflictHunks(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	doc, hash, ok := loadConflictDoc(w, r, svc, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	items := make([]conflictItem, 0, len(doc.Items))
	idx := 0
	for _, it := range doc.Items {
		if it.Block == nil {
			items = append(items, conflictItem{Kind: "text", Lines: it.Literal})
			continue
		}
		items = append(items, conflictItem{Kind: "block", Index: idx, Ours: it.Block.Current, Theirs: it.Block.Incoming})
		idx++
	}
	writeJSON(w, map[string]any{"count": idx, "hash": hash, "items": items})
}
