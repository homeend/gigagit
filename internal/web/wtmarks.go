package web

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
)

// The TUI's Worktrees . menu rows Reserve (no agents) / Unreserve and
// Release claim (worktree_marks.go), as two writes through the domain.
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/worktree/reserve", writeGuard(s.handleWorktreeReserve))
		mux.HandleFunc("POST /api/worktree/release-claim", writeGuard(s.handleWorktreeReleaseClaim))
	})
}

type worktreeMarkReq struct {
	Path string `json:"path"`
	On   bool   `json:"on"`
}

func decodeWorktreeMarkReq(w http.ResponseWriter, r *http.Request) (worktreeMarkReq, bool) {
	var req worktreeMarkReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil || req.Path == "" {
		writeErr(w, http.StatusBadRequest, errors.New("bad request: want {path}"))
		return req, false
	}
	return req, true
}

// markErr: a path that is not one of the repo's worktrees (a stale page) is
// the caller's mistake; anything else is ours.
func markErr(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrUnknownWorktree) {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeErr(w, http.StatusInternalServerError, err)
}

// handleWorktreeReserve writes [agents] reserved through the domain's
// main-anchored repo config (the TUI's setReserved).
func (s *Server) handleWorktreeReserve(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeWorktreeMarkReq(w, r)
	if !ok {
		return
	}
	svc := s.service()
	ac, cfgPath, err := svc.AgentsConfig(r.Context())
	if err == nil {
		err = svc.SetWorktreeReserved(r.Context(), cfgPath, ac.Reserved, req.Path, req.On)
	}
	if err != nil {
		markErr(w, err)
		return
	}
	s.worktreesChanged()
	writeJSON(w, map[string]bool{"ok": true})
}

// handleWorktreeReleaseClaim takes a worktree back from the agent that
// claimed it — forced, the user's release (the TUI's confirmReleaseClaim;
// the page asked first).
func (s *Server) handleWorktreeReleaseClaim(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeWorktreeMarkReq(w, r)
	if !ok {
		return
	}
	released, err := s.service().ReleaseWorktree(r.Context(), req.Path, "", true)
	if err != nil {
		markErr(w, err)
		return
	}
	s.worktreesChanged()
	writeJSON(w, map[string]bool{"ok": true, "released": released})
}

// worktreesChanged tells every tab to re-read its Worktrees rows.
func (s *Server) worktreesChanged() {
	if h := s.liveHubRef(); h != nil {
		h.fanOut(liveMsg{Changed: []string{"worktrees"}})
	}
}
