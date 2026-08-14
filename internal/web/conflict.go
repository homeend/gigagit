package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
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
	// Standalone: unmerged paths with NO paused sequencer op (a conflicted
	// stash apply). The client swaps continue/abort/AI for the
	// discard-conflicted-changes action (abort-apply).
	Standalone bool `json:"standalone,omitempty"`
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
// discard precedent: unknown → 404, known-but-not-conflicted → 422), then
// regenerates the conflict text from the index stages (svc.ConflictPickerFile)
// and parses it at the marker size the regeneration chose — nested-marker-safe,
// unlike a raw worktree read whose markers can be ambiguous. The hash is the
// freshness token resolve-hunks must echo, computed over the regenerated
// bytes — picks are positional, valid only against the exact content the
// client saw.
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
	content, markerSize, err := svc.ConflictPickerFile(ctx, path)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return nil, "", false
	}
	if textdiff.IsBinary(content) {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("binary file — resolve in your editor, then mark resolved"))
		return nil, "", false
	}
	doc, perr := hunkpick.ParseConflictSized(content, markerSize)
	if perr != nil || len(doc.Blocks()) == 0 {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("no usable conflict markers — resolve in your editor, then mark resolved"))
		return nil, "", false
	}
	sum := sha256.Sum256(content)
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

// resolvePick is one block's decision, positional against Doc.Blocks(): a
// whole-side take ("ours"/"theirs") or an ordered line-by-line pick list
// ("lines") — ties to hunkpick's LineByLine mode, where result order is
// array order and sides may interleave. An empty Lines list under "lines" is
// decided-empty (both sides dropped for that block), not "undecided".
type resolvePick struct {
	Mode  string `json:"mode"` // "ours" | "theirs" | "lines"
	Lines []struct {
		Side string `json:"side"` // "ours" | "theirs"
		Line int    `json:"line"`
	} `json:"lines"`
}

type resolveHunksRequest struct {
	Path  string        `json:"path"`
	Picks []resolvePick `json:"picks"` // positional: picks[i] resolves block i
	Hash  string        `json:"hash"`
}

// handleResolveHunks resolves a conflicted file from a full set of per-block
// picks: recompute the doc fresh, verify the freshness hash (409 on drift),
// require EVERY block picked (a partial resolve would stage a file still
// containing markers), assemble Doc.Resolved(), and write+stage through
// engine.ResolveConflictHunks. Success response = fresh status (the
// stage-hunks convention).
func (s *Server) handleResolveHunks(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	var req resolveHunksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	doc, hash, ok := loadConflictDoc(w, r, svc, req.Path)
	if !ok {
		return
	}
	if req.Hash != hash {
		writeErr(w, http.StatusConflict, errors.New("file changed; refresh"))
		return
	}
	blocks := doc.Blocks()
	if len(req.Picks) != len(blocks) {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("picks: got %d, want %d (every block must be picked)", len(req.Picks), len(blocks)))
		return
	}
	for i, p := range req.Picks {
		switch p.Mode {
		case "ours":
			blocks[i].Mode = hunkpick.TakeCurrent
		case "theirs":
			blocks[i].Mode = hunkpick.TakeIncoming
		case "lines":
			// Ordered line-pick model: result order = array order, sides may
			// interleave, an empty list is decided-empty (drop both sides).
			seen := map[[2]int]bool{}
			picks := make([]hunkpick.Pick, 0, len(p.Lines))
			for j, ln := range p.Lines {
				var side hunkpick.Side
				var max int
				switch ln.Side {
				case "ours":
					side, max = hunkpick.Current, len(blocks[i].Current)
				case "theirs":
					side, max = hunkpick.Incoming, len(blocks[i].Incoming)
				default:
					writeErr(w, http.StatusBadRequest, fmt.Errorf("pick %d line %d: side %q (want ours|theirs)", i, j, ln.Side))
					return
				}
				if ln.Line < 0 || ln.Line >= max {
					writeErr(w, http.StatusBadRequest, fmt.Errorf("pick %d line %d: %s line %d out of range (0..%d)", i, j, ln.Side, ln.Line, max-1))
					return
				}
				key := [2]int{int(side), ln.Line}
				if seen[key] {
					writeErr(w, http.StatusBadRequest, fmt.Errorf("pick %d: duplicate %s line %d", i, ln.Side, ln.Line))
					return
				}
				seen[key] = true
				picks = append(picks, hunkpick.Pick{Side: side, Line: ln.Line})
			}
			blocks[i].Mode = hunkpick.LineByLine
			blocks[i].Picks = picks
		default:
			writeErr(w, http.StatusBadRequest, fmt.Errorf("pick %d: mode %q (want ours|theirs|lines)", i, p.Mode))
			return
		}
	}
	content, resolved := doc.Resolved()
	if !resolved {
		writeErr(w, http.StatusInternalServerError, errors.New("unresolved blocks"))
		return
	}
	if _, err := runOp(r.Context(), svc, engine.ResolveConflictHunks{Path: req.Path, Content: content}); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.writeStatus(w, r)
}
