package web

import (
	"net/http"
	"time"

	"github.com/homeend/gigagit/internal/engine"
)

// Stranded git lockfiles, and the way out of them.
//
// A lockfile only outlives its git process when that process died without
// running its cleanup handler. Until it is removed every operation touching
// the file it shadows fails with "Another git process seems to be running in
// this repository" — and the browser used to show that error with no way out,
// while the TUI has had a whole recovery surface for it.
//
// Presence is NOT proof of staleness: a git running right now legitimately
// holds one, and gg cannot see processes it did not start. So this surface
// reports the locks and their age and lets the human decide, exactly as
// domain.StaleLocks and engine.RemoveGitLocks are designed for.
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/locks", s.handleLocks)
	})
	RegisterOp("remove-locks", buildRemoveLocks)
}

// lockRow is one lockfile on the wire. AgeSeconds is computed here rather
// than from ModTime in the browser: both clocks are the same machine's, but
// only this side knows the file's mtime in the server's own frame, and the
// age is the whole staleness hint.
type lockRow struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	ModTime    string `json:"mtime"`
	AgeSeconds int64  `json:"age_seconds"`
}

// handleLocks answers the lockfiles present in this worktree's git dir and
// the repository's common dir. Stat-level only — no git invocation — so the
// client may call it on every refresh without cost.
func (s *Server) handleLocks(w http.ResponseWriter, r *http.Request) {
	locks, err := s.service().StaleLocks(readCtx(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	now := time.Now()
	rows := make([]lockRow, 0, len(locks))
	for _, l := range locks {
		age := int64(now.Sub(l.ModTime) / time.Second)
		if age < 0 {
			age = 0 // a clock that moved backwards must not read as "in the future"
		}
		rows = append(rows, lockRow{
			Path:       l.Path,
			Name:       l.Name,
			ModTime:    l.ModTime.Format(time.RFC3339),
			AgeSeconds: age,
		})
	}
	writeJSON(w, map[string]any{"locks": rows})
}

// buildRemoveLocks carries out the human's decision. It deliberately does NOT
// re-implement the path guard: engine.RemoveGitLocks validates every path
// against the repository's git dirs before removing anything, and duplicating
// that check here would give the rule two homes that can drift apart. A path
// the wire aims elsewhere fails the operation with that refusal.
func buildRemoveLocks(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
	return engine.RemoveGitLocks{Paths: req.Paths}, nil, 0, nil
}
