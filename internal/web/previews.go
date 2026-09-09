package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// Merge previews: saved (source → target) pairs whose diff is
// merge-base(target, source)..source — the GitHub PR files-changed view.
// Names are resolved to hashes HERE; the client opens the existing compare
// page with the two hashes (revs=1), never with the names.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/preview", s.handlePreviews)
		mux.HandleFunc("POST /api/preview", writeGuard(s.handlePreviewAdd))
		mux.HandleFunc("POST /api/preview/rename", writeGuard(s.handlePreviewRename))
		mux.HandleFunc("DELETE /api/preview", writeGuard(s.handlePreviewRemove))
		mux.HandleFunc("GET /api/preview/open", s.handlePreviewOpen)
		mux.HandleFunc("GET /api/preview/diff", s.handlePreviewDiff)
	})
}

type previewRow struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Source     string `json:"source"`
	Target     string `json:"target"`
	State      string `json:"state"`
	Files      int    `json:"files"`
	Ahead      int    `json:"ahead"`
	SourceHash string `json:"source_hash,omitempty"`
	TargetHash string `json:"target_hash,omitempty"`
}

func previewRowFrom(p model.MergePreview, sum domain.PreviewSummary) previewRow {
	return previewRow{ID: p.ID, Label: p.Label, Source: p.Source, Target: p.Target,
		State: sum.State.String(), Files: sum.Files, Ahead: sum.Ahead,
		SourceHash: sum.SourceHash, TargetHash: sum.TargetHash}
}

func (s *Server) handlePreviews(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	ctx := readCtx(r)
	ps, err := svc.PreviewList(ctx)
	if err != nil {
		if errors.Is(err, domain.ErrPreviewsDisabled) {
			writeJSON(w, map[string]any{"entries": []previewRow{}, "disabled": true})
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]previewRow, 0, len(ps))
	for _, p := range ps {
		sum, err := svc.PreviewSummary(ctx, p.Source, p.Target)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		rows = append(rows, previewRowFrom(p, sum))
	}
	writeJSON(w, map[string]any{"entries": rows})
}

// knownRefName resolves a wire name against the live local + remote-tracking
// lists (the /api/compare allowlist posture: an unknown name must be a 404,
// not an empty preview). code is 0 when ok.
func (s *Server) knownRefName(r *http.Request, name string) (int, error) {
	if !isGitArgSafe(name) {
		return http.StatusBadRequest, errors.New("invalid branch")
	}
	svc := s.service()
	bs, err := svc.Branches(r.Context())
	if err != nil {
		return http.StatusInternalServerError, err
	}
	for _, b := range bs {
		if b.Name == name {
			return 0, nil
		}
	}
	rs, err := svc.RemoteBranches(r.Context())
	if err != nil {
		return http.StatusInternalServerError, err
	}
	for _, b := range rs {
		if b.Name == name {
			return 0, nil
		}
	}
	return http.StatusNotFound, fmt.Errorf("unknown branch %q", name)
}

func (s *Server) handlePreviewAdd(w http.ResponseWriter, r *http.Request) {
	var req struct{ Source, Target, Label string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	for _, n := range []string{req.Source, req.Target} {
		if code, err := s.knownRefName(r, n); code != 0 {
			writeErr(w, code, err)
			return
		}
	}
	svc := s.service()
	p, err := svc.PreviewAdd(readCtx(r), req.Source, req.Target, req.Label)
	if errors.Is(err, domain.ErrPreviewExists) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "already saved", "id": p.ID})
		return
	}
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	sum, _ := svc.PreviewSummary(readCtx(r), p.Source, p.Target)
	s.emitPreviews()
	writeJSON(w, map[string]any{"entry": previewRowFrom(p, sum)})
}

func (s *Server) handlePreviewRename(w http.ResponseWriter, r *http.Request) {
	var req struct{ ID, Label string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" || req.Label == "" {
		writeErr(w, http.StatusBadRequest, errors.New("id and label required"))
		return
	}
	if err := s.service().PreviewRename(readCtx(r), req.ID, req.Label); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	s.emitPreviews()
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handlePreviewRemove(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, errors.New("id required"))
		return
	}
	if err := s.service().PreviewRemove(readCtx(r), id); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	s.emitPreviews()
	writeJSON(w, map[string]any{"ok": true})
}

// writePreviewOpen resolves the pair and answers the open/diff shape.
func (s *Server) writePreviewOpen(w http.ResponseWriter, r *http.Request, label, source, target string) {
	eps, err := s.service().PreviewOpen(r.Context(), source, target)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{
		"state": eps.Summary.State.String(), "label": label,
		"source": source, "target": target,
		"left": eps.Left.Hash, "right": eps.Right.Hash,
		"source_hash": eps.Summary.SourceHash, "target_hash": eps.Summary.TargetHash,
	})
}

func (s *Server) handlePreviewOpen(w http.ResponseWriter, r *http.Request) {
	p, err := s.service().PreviewGet(r.Context(), r.URL.Query().Get("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	s.writePreviewOpen(w, r, p.Label, p.Source, p.Target)
}

func (s *Server) handlePreviewDiff(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	source, target := q.Get("source"), q.Get("target")
	for _, n := range []string{source, target} {
		if code, err := s.knownRefName(r, n); code != 0 {
			writeErr(w, code, err)
			return
		}
	}
	s.writePreviewOpen(w, r, source+" → "+target, source, target)
}

// emitPreviews tells every open page the previews changed ("previews" is
// not a ticker source; mutations are its only producer, like notes).
func (s *Server) emitPreviews() {
	if h := s.liveHubRef(); h != nil {
		h.emit(liveMsg{Changed: []string{"previews"}, Reason: "previews"})
	}
}
