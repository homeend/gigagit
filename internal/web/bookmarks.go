package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// Bookmarks: durable, richly-addressed references to a file or a commit. The
// store is gg's own (machine-local state, not git), owned by domain — this
// file is only the wire.
//
// maxBookmarkRows bounds one page the way the tags and reflog sections do.
const maxBookmarkRows = 200

type bookmarkRow struct {
	ID       string `json:"id"`
	Display  string `json:"display"` // "path @ container", the store's own display string
	Label    string `json:"label,omitempty"`
	State    string `json:"state"`
	Path     string `json:"path,omitempty"`
	Commit   string `json:"commit,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	ShelfID  string `json:"shelf_id,omitempty"`
	IsCommit bool   `json:"is_commit"`
	Created  string `json:"created,omitempty"`
}

// fileStateName maps a FileState to the wire name. Identical to the MCP
// frontend's fileStateProto — the two surfaces describe the same store, and a
// second vocabulary for it would be a bug waiting to happen.
func fileStateName(st model.FileState) string {
	switch st {
	case model.StateCommitted:
		return "committed"
	case model.StateShelf:
		return "shelf"
	case model.StateStaged:
		return "staged"
	case model.StateUntracked:
		return "untracked"
	default:
		return "unstaged"
	}
}

// fileStateFrom is the inverse, and it is an ALLOWLIST: an unknown name is
// refused rather than defaulting to unstaged, so a typo cannot quietly file an
// entry under the wrong state.
func fileStateFrom(name string) (model.FileState, bool) {
	switch name {
	case "committed":
		return model.StateCommitted, true
	case "staged":
		return model.StateStaged, true
	case "untracked":
		return model.StateUntracked, true
	case "unstaged", "":
		return model.StateUnstaged, true
	}
	return 0, false
}

func bookmarkRowFrom(b model.Bookmark) bookmarkRow {
	r := bookmarkRow{
		ID: b.ID, Display: b.Address().Display(), Label: b.Label,
		State: fileStateName(b.State), Path: b.Path, Commit: b.Commit,
		Branch: b.Branch, Worktree: b.Worktree, ShelfID: b.ShelfID,
		IsCommit: b.IsCommit(),
	}
	if !b.Created.IsZero() {
		r.Created = b.Created.UTC().Format(time.RFC3339)
	}
	return r
}

func (s *Server) handleBookmarks(w http.ResponseWriter, r *http.Request) {
	bs, err := s.service().BookmarkList(readCtx(r), 0, maxBookmarkRows)
	if err != nil {
		// A machine with no state directory has no bookmarks — an empty
		// section, not an error bar the user cannot act on.
		if errors.Is(err, domain.ErrBookmarksDisabled) {
			writeJSON(w, map[string]any{"entries": []bookmarkRow{}, "disabled": true})
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]bookmarkRow, 0, len(bs))
	for _, b := range bs {
		rows = append(rows, bookmarkRowFrom(b))
	}
	writeJSON(w, map[string]any{"entries": rows})
}

// entryRequest is the shared shape for "remember this": a COMMIT (sha, no
// path) or a FILE at some state. The worktree and branch are never taken from
// the wire — they are read from the served repo, so one POST cannot file an
// entry against another checkout.
type entryRequest struct {
	Sha    string `json:"sha"`
	Path   string `json:"path"`
	State  string `json:"state"`
	Label  string `json:"label"`
	Bucket string `json:"bucket"`
}

// address resolves a request into the address it names, filling the server's
// own worktree/branch for working-tree states.
func (s *Server) address(r *http.Request, req entryRequest) (model.FileAddress, error) {
	st, ok := fileStateFrom(req.State)
	if !ok {
		return model.FileAddress{}, fmt.Errorf("unknown state %q", req.State)
	}
	if req.Path == "" {
		return model.FileAddress{}, errors.New("path required")
	}
	addr := model.FileAddress{Path: req.Path, State: st}
	svc := s.service()
	ctx := readCtx(r)
	if st == model.StateCommitted {
		if !isHexSha(req.Sha) {
			return model.FileAddress{}, errors.New("invalid commit")
		}
		addr.Commit = req.Sha
		return addr, nil
	}
	top, err := svc.TopLevel(ctx)
	if err != nil {
		return model.FileAddress{}, err
	}
	addr.Worktree = top
	if st, err := svc.Status(ctx); err == nil {
		addr.Branch = st.Branch
	}
	return addr, nil
}

func (s *Server) handleBookmarkAdd(w http.ResponseWriter, r *http.Request) {
	var req entryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	svc := s.service()
	var b model.Bookmark
	if req.Path == "" {
		// A COMMIT bookmark: path-less, and the label is what the switcher
		// shows (the TUI prefills it with the commit's subject).
		if !isHexSha(req.Sha) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid commit"))
			return
		}
		b = model.Bookmark{Commit: req.Sha, State: model.StateCommitted, Label: req.Label}
	} else {
		addr, err := s.address(r, req)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		b = model.Bookmark{
			Worktree: addr.Worktree, Branch: addr.Branch, Commit: addr.Commit,
			Path: addr.Path, State: addr.State, Label: req.Label,
		}
	}
	out, err := svc.BookmarkAdd(readCtx(r), b)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"entry": bookmarkRowFrom(out)})
}

func (s *Server) handleBookmarkRemove(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, errors.New("id required"))
		return
	}
	if err := s.service().BookmarkRemove(readCtx(r), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
