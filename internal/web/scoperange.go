package web

import (
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
)

// A commit's Range review rows. Notes written over a commit pair sit on the
// pair's newer commit, mostly on files it does not change (a merge preview's
// review is the preview's and is never a commit's row);
// /api/notes/counts names the ranges per commit (scopes_by_commit) and this
// route turns one into the two commits it opens as, frozen at that commit
// (domain.ScopeAtCommit) — the page then opens them as a pair landing
// (/api/compare-links?a=&b=), the TUI's Range review row.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/scope-range", s.handleScopeRange)
	})
}

// wireScope is one range a commit's notes were written in.
type wireScope struct {
	Scope string `json:"scope"`
	Label string `json:"label"`
	N     int    `json:"n"`
	// Branch is the branch a range review was written on ("" = none known):
	// the page shows it on that branch only. Preview marks a merge preview's
	// review, which a commit never shows.
	Branch  string `json:"branch,omitempty"`
	Preview bool   `json:"preview,omitempty"`
}

// wireScopes keeps the field an OBJECT on the wire (orEmptyCounts' reason).
func wireScopes(m map[string][]domain.NoteScopeCount) map[string][]wireScope {
	out := make(map[string][]wireScope, len(m))
	for sha, scs := range m {
		for _, sc := range scs {
			out[sha] = append(out[sha], wireScope{Scope: sc.Scope, Label: domain.NoteScopeLabel(sc.Scope), N: sc.N,
				Branch: sc.Branch, Preview: domain.IsPreviewScope(sc.Scope)})
		}
	}
	return out
}

func (s *Server) handleScopeRange(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	commit, scope := q.Get("commit"), q.Get("scope")
	if !isFullSha(commit) {
		writeErr(w, http.StatusBadRequest, errors.New("commit must be a full commit id"))
		return
	}
	if scope == "" {
		writeErr(w, http.StatusBadRequest, errors.New("scope is required"))
		return
	}
	ctx := readCtx(r)
	svc := s.service()
	// The scope names revisions and reaches git: resolve only one the commit's
	// own notes name (the allowlist every wire value headed for git gets).
	counts, err := svc.NoteCounts(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotesDisabled) {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	known := false
	for _, sc := range counts.ScopesByCommit[commit] {
		known = known || domain.SameNoteScope(sc.Scope, scope)
	}
	if !known {
		writeErr(w, http.StatusNotFound, errors.New("no notes of that range on this commit"))
		return
	}
	a, b, err := svc.ScopeAtCommit(ctx, scope, commit)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(w, map[string]any{"a": a, "b": b, "label": domain.NoteScopeLabel(scope)})
}
