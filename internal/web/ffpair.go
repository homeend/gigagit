package web

import (
	"errors"
	"net/http"
)

// GET /api/ff-pair?a=<branch>&b=<branch> — which of two branches, if either,
// can fast-forward to the other. The drag-drop pair menu asks before it
// renders, so the fast-forward row appears only when it applies (and names
// the direction). Names resolve against the server's own branch list (the
// compare allowlist precedent); a probe on unknown names would fail loudly
// anyway, but 404 keeps the contract explicit.
func (s *Server) handleFFPair(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	q := r.URL.Query()
	a, b := q.Get("a"), q.Get("b")
	if a == "" || b == "" || !isGitArgSafe(a) || !isGitArgSafe(b) {
		writeErr(w, http.StatusBadRequest, errors.New("a and b are required"))
		return
	}
	branches, err := svc.Branches(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if branchTip(branches, a) == "" || branchTip(branches, b) == "" {
		writeErr(w, http.StatusNotFound, errors.New("unknown branch"))
		return
	}
	p, err := svc.FastForwardPair(r.Context(), a, b)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"ok": p.OK, "behind": p.Behind, "ahead": p.Ahead})
}
