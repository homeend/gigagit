package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/repos"
)

type rerootRequest struct {
	Path string `json:"path"`
}

func (s *Server) reposStatePath() string {
	if s.reposPath != "" {
		return s.reposPath
	}
	return repos.DefaultStatePath()
}

// handleReroot points the running server at a different worktree of the
// current repo, or a previously-opened repo from the MRU registry. The
// client string is an identifier resolved by allowlist — only server-owned
// values ever reach domain.Open.
func (s *Server) handleReroot(w http.ResponseWriter, r *http.Request) {
	var req rerootRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	// Refuse while an op is live BEFORE any domain read: a parked op holds
	// the repo-gate reservation, so a Worktrees() read here would block
	// until the op finishes instead of 409ing. (A tiny window remains where
	// an op starts after this check; the authoritative re-check at swap
	// time still runs under opMu.)
	s.opMu.Lock()
	if s.cur != nil {
		s.cur.mu.Lock()
		live := !s.cur.done
		s.cur.mu.Unlock()
		if live {
			s.opMu.Unlock()
			writeErr(w, http.StatusConflict, errOpBusy)
			return
		}
	}
	s.opMu.Unlock()
	target := ""
	if wts, err := s.service().Worktrees(r.Context()); err == nil {
		for _, wt := range wts {
			if wt.Path == req.Path {
				target = wt.Path
				break
			}
		}
	}
	if target == "" {
		for _, e := range repos.Load(s.reposStatePath()) {
			if e.Path == req.Path {
				target = e.Path
				break
			}
		}
	}
	if target == "" {
		writeErr(w, http.StatusNotFound, errors.New("unknown target"))
		return
	}
	// Preflight BEFORE swapping: a broken target must never take down a
	// working server (the startup preflight, reused — same friendly
	// cross-environment error).
	cand := domain.Open(target)
	if err := preflight(r.Context(), cand, target); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	// Swap under opMu in one critical section with the live-op check:
	// startOp holds opMu too, so no op can begin mid-swap. The finished op
	// record is dropped — a late SSE read of the previous repo's op 404s,
	// which is correct (that op belongs to the old root).
	s.opMu.Lock()
	if s.cur != nil {
		s.cur.mu.Lock()
		live := !s.cur.done
		s.cur.mu.Unlock()
		if live {
			s.opMu.Unlock()
			writeErr(w, http.StatusConflict, errOpBusy)
			return
		}
	}
	s.svc.Store(cand)
	s.cur = nil
	s.opMu.Unlock()
	s.mu.Lock()
	s.feed = nil
	s.mu.Unlock()
	writeRepoInfo(w, r, cand)
}
