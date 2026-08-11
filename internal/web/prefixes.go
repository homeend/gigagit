package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

type prefixRow struct {
	ID    string `json:"id"`
	Value string `json:"value"`
	Scope string `json:"scope"` // "global" | "repo"
	// UserLabels are the value's <user:…> labels, in order — the client
	// prompts for these before asking for a resolve.
	UserLabels []string `json:"user_labels"`
}

// handlePrefixes lists the branch-name prefixes, global rows then repo rows.
func (s *Server) handlePrefixes(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	ps, err := svc.Prefixes(readCtx(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]prefixRow, 0, len(ps))
	for _, p := range ps {
		rows = append(rows, prefixRow{ID: p.ID, Value: p.Value, Scope: p.Scope.String(), UserLabels: domain.PrefixUserLabels(p.Value)})
	}
	writeJSON(w, map[string]any{"prefixes": rows})
}

// handlePrefixAdd stores a new prefix. The value is validated up front
// (domain.ValidatePrefixValue — empty, <branch>, malformed tokens) so a
// refusal is a clean 400 rather than a store error.
func (s *Server) handlePrefixAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Value string `json:"value"`
		Scope string `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	scope, ok := parseProfileScope(req.Scope)
	if !ok {
		writeErr(w, http.StatusBadRequest, errors.New("scope must be global or repo"))
		return
	}
	if err := domain.ValidatePrefixValue(req.Value); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	added, err := s.service().AddPrefix(r.Context(), model.Prefix{Value: strings.TrimSpace(req.Value), Scope: scope})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]string{"id": added.ID})
}

// handlePrefixRemove deletes one prefix; the (scope,id) pair resolves against
// a fresh list read (404 unknown), the web identifier convention.
func (s *Server) handlePrefixRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope string `json:"scope"`
		ID    string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	scope, ok := parseProfileScope(req.Scope)
	if !ok {
		writeErr(w, http.StatusBadRequest, errors.New("scope must be global or repo"))
		return
	}
	svc := s.service()
	if _, ok := s.prefixByID(r, req.ID, scope); !ok {
		writeErr(w, http.StatusNotFound, errors.New("unknown prefix"))
		return
	}
	if err := svc.RemovePrefix(r.Context(), scope, req.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handlePrefixResolve previews a stored prefix: tokens resolve against the
// live repo, seq counters are PEEKED (a canceled prefill must not consume a
// number), and the wire never carries a raw template value — only the id of
// a prefix that exists in a fresh read.
func (s *Server) handlePrefixResolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope  string            `json:"scope"`
		ID     string            `json:"id"`
		Inputs map[string]string `json:"inputs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	scope, ok := parseProfileScope(req.Scope)
	if !ok {
		writeErr(w, http.StatusBadRequest, errors.New("scope must be global or repo"))
		return
	}
	p, ok := s.prefixByID(r, req.ID, scope)
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("unknown prefix"))
		return
	}
	resolved, seqNames, err := s.service().ResolvePrefixValue(r.Context(), p.Value, req.Inputs)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(w, map[string]any{"resolved": resolved, "seq_names": seqNames})
}

// prefixByID resolves (id,scope) against a fresh domain read.
func (s *Server) prefixByID(r *http.Request, id string, scope model.ProfileScope) (model.Prefix, bool) {
	if id == "" {
		return model.Prefix{}, false
	}
	ps, err := s.service().Prefixes(r.Context())
	if err != nil {
		return model.Prefix{}, false
	}
	for _, p := range ps {
		if p.ID == id && p.Scope == scope {
			return p, true
		}
	}
	return model.Prefix{}, false
}
