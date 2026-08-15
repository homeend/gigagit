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

// The shelf: frozen copies (a file's bytes, or a commit's changed files as a
// tar) that outlive the thing they came from. Same wire shape family as
// bookmarks; the store is domain's.
const maxShelfRows = 200

type shelfRow struct {
	ID      string `json:"id"`
	Bucket  string `json:"bucket,omitempty"`
	Kind    string `json:"kind"` // "file" | "commit"
	Display string `json:"display"`
	Label   string `json:"label,omitempty"`
	Path    string `json:"path,omitempty"`
	Commit  string `json:"commit,omitempty"`
	State   string `json:"state"`
	Size    int64  `json:"size"`
	Created string `json:"created,omitempty"`
}

func shelfKindName(k model.ShelfKind) string {
	if k == model.ShelfKindCommit {
		return "commit"
	}
	return "file"
}

func shelfRowFrom(e model.ShelfEntry) shelfRow {
	r := shelfRow{
		ID: e.ID, Bucket: e.Bucket, Kind: shelfKindName(e.Kind),
		Display: e.Origin.Display(), Label: e.Label, Path: e.Origin.Path,
		Commit: e.Origin.Commit, State: fileStateName(e.Origin.State), Size: e.Size,
	}
	if !e.Created.IsZero() {
		r.Created = e.Created.UTC().Format(time.RFC3339)
	}
	return r
}

func (s *Server) handleShelf(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	ctx := readCtx(r)
	es, err := svc.ShelfList(ctx, r.URL.Query().Get("bucket"), 0, maxShelfRows)
	if err != nil {
		// No state directory = nothing shelved, an empty section (the
		// bookmarks lane's rule).
		if errors.Is(err, domain.ErrShelfDisabled) {
			writeJSON(w, map[string]any{"entries": []shelfRow{}, "buckets": []string{}, "disabled": true})
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]shelfRow, 0, len(es))
	for _, e := range es {
		rows = append(rows, shelfRowFrom(e))
	}
	buckets := []string{}
	if bs, berr := svc.ShelfBuckets(ctx); berr == nil {
		for _, b := range bs {
			buckets = append(buckets, b.Name)
		}
	}
	writeJSON(w, map[string]any{"entries": rows, "buckets": buckets})
}

func (s *Server) handleShelfAdd(w http.ResponseWriter, r *http.Request) {
	var req entryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	svc := s.service()
	ctx := readCtx(r)
	var (
		out model.ShelfEntry
		err error
	)
	if req.Path == "" {
		// A COMMIT entry: the commit's changed files, frozen as one archive.
		if !isHexSha(req.Sha) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid commit"))
			return
		}
		out, err = svc.ShelfAddCommit(ctx, req.Sha, req.Label)
	} else {
		addr, aerr := s.address(r, req)
		if aerr != nil {
			writeErr(w, http.StatusBadRequest, aerr)
			return
		}
		out, err = svc.ShelfAdd(ctx, addr, req.Bucket)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"entry": shelfRowFrom(out)})
}

func (s *Server) handleShelfRemove(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, errors.New("id required"))
		return
	}
	if err := s.service().ShelfRemove(readCtx(r), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleShelfFiles lists the files a SHELVED COMMIT froze — what the entry
// actually holds, which is what makes it browsable rather than opaque.
func (s *Server) handleShelfFiles(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, errors.New("id required"))
		return
	}
	files, err := s.service().ShelfCommitFiles(readCtx(r), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	writeJSON(w, map[string]any{"files": paths})
}
