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
	Notes   int    `json:"notes,omitempty"` // notes gg left on the entry itself
}

func shelfKindName(k model.ShelfKind) string {
	switch k {
	case model.ShelfKindCommit:
		return "commit"
	case model.ShelfKindFiles:
		return "files"
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
	// ?id= answers ONE entry wherever it lives — ShelfFind scans every bucket,
	// the list below reads one — so a bucket beside it is a contradiction.
	if id := r.URL.Query().Get("id"); id != "" {
		if r.URL.Query().Get("bucket") != "" {
			writeErr(w, http.StatusBadRequest, errors.New("give id or bucket, not both"))
			return
		}
		e, err := svc.ShelfFind(ctx, id)
		if err != nil {
			writeErr(w, entryByIDStatus(err), err)
			return
		}
		row := shelfRowFrom(e)
		if counts, cerr := svc.NoteCounts(ctx); cerr == nil {
			row.Notes = counts.ByShelf[e.ID]
		}
		writeJSON(w, map[string]any{"entries": []shelfRow{row}})
		return
	}
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
	// Best effort: a notes store that cannot be read just leaves no badges.
	counts, _ := svc.NoteCounts(ctx)
	rows := make([]shelfRow, 0, len(es))
	for _, e := range es {
		row := shelfRowFrom(e)
		row.Notes = counts.ByShelf[e.ID]
		rows = append(rows, row)
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

// shelfNoteWire is one note on a whole shelf entry, read-only on the wire.
type shelfNoteWire struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	Author    string `json:"author,omitempty"`
	Summary   string `json:"summary"`
	Rationale string `json:"rationale,omitempty"`
	Created   string `json:"created"`
}

// handleShelfNotes answers GET /api/shelf/notes?id=: the notes gg left on the
// entry itself. There is no write route — users read these, they do not
// author them.
func (s *Server) handleShelfNotes(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, errors.New("id required"))
		return
	}
	ns, err := s.service().ShelfNotes(readCtx(r), id)
	if err != nil && !errors.Is(err, domain.ErrNotesDisabled) {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]shelfNoteWire, 0, len(ns))
	for _, n := range ns {
		out = append(out, shelfNoteWire{
			ID: n.Note.ID, Source: string(n.Note.Source), Author: n.Note.Author,
			Summary: n.Note.Summary, Rationale: n.Note.Rationale,
			Created: n.Note.Created.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, map[string]any{"notes": out})
}
