package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/repos"
	"github.com/homeend/gigagit/internal/worktree"
)

type rerootRequest struct {
	Path string `json:"path"`
	// Repair: the client confirmed rebinding a worktree recorded under the
	// other environment's path notation (WSL vs Windows) before switching —
	// the second leg of the 409-confirm-retry handshake below.
	Repair bool `json:"repair"`
}

func (s *Server) reposStatePath() string {
	if s.reposPath != "" {
		return s.reposPath
	}
	return repos.DefaultStatePath()
}

// expandHome resolves a leading "~" against home ("" leaves the path
// untouched — no home dir, nothing to expand). Only the bare "~" and "~/…"
// forms expand; "~user" is not supported (the TUI's repoPathPopup rule).
func expandHome(path, home string) string {
	if home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(home, path[2:])
	}
	return path
}

// handleReroot points the running server at a different worktree of the
// current repo, a previously-opened repo from the MRU registry, or — the
// palette's "open repo (path)" — any filesystem path the user typed. The
// allowlist is tried first (it maps picker rows to exact known paths); a
// miss falls back to treating the value as a path, like the TUI's Open
// repo palette entry. That widens nothing meaningful: writeGuard +
// hostGuard already gate this to the local user, who can open any of
// their repos in the TUI, and the preflight below still runs BEFORE the
// swap, so a garbage path is a 409 and the old root keeps serving.
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
		// Custom path lane. A leading dash is refused defensively (the
		// path reaches git argv via -C); everything else is preflight's
		// call — git reports "not a repository" more clearly than any
		// check this layer could invent.
		p := strings.TrimSpace(req.Path)
		if p == "" || strings.HasPrefix(p, "-") {
			writeErr(w, http.StatusBadRequest, errors.New("invalid repo path"))
			return
		}
		home, _ := os.UserHomeDir()
		target = expandHome(p, home)
	}
	// Cross-environment guard (the TUI's guardedReRoot, wire-shaped): a
	// worktree recorded under the other environment's path notation (a WSL
	// path seen from Windows gg, or vice versa on a shared disk) would fail
	// the preflight with a raw chdir error. Reachable-as-recorded proceeds;
	// reachable only under the translated notation asks the client to
	// confirm a `git worktree repair` (409 + repairable:true — the repair
	// rebinds the link records, so the worktree stops working in the OTHER
	// environment until repaired back); confirmed (req.Repair) runs the
	// repair from the CURRENT service, then switches to the translated path.
	// A foreign BACKSLASH-notation admin record makes git report the
	// worktree path with a trailing \.git — with backslashes it cannot
	// strip the suffix it strips from forward-slash records. Trim either
	// form so the reachability check sees the worktree dir, not its link.
	target = strings.TrimSuffix(strings.TrimSuffix(target, `\.git`), "/.git")
	if verdict, translated := worktree.CheckSwitchTarget(func(p string) error { _, err := os.Stat(p); return err }, runtime.GOOS, target); verdict == worktree.SwitchRepairable {
		if !req.Repair {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":      "worktree is linked for another environment — repair it for this one?",
				"repairable": true,
				"translated": translated,
			})
			return
		}
		events := make(chan engine.Event, 16)
		drained := make(chan struct{})
		go func() {
			for range events {
			}
			close(drained)
		}()
		_, rerr := s.service().Execute(r.Context(), engine.RepairWorktree{Path: translated}, events, nil)
		close(events)
		<-drained
		if rerr != nil {
			writeErr(w, http.StatusConflict, fmt.Errorf("worktree repair: %w", rerr))
			return
		}
		target = translated
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
	// The new root's [versions] policy (the serve-boot re-apply point).
	applyVersionsPolicy(r.Context(), cand, s.activeRepoConfigPathOr(r.Context(), cand))
	// The new root becomes navigable-back-to forever (touchMRU on serve
	// covers the original root).
	touchMRU(r.Context(), cand, s.reposStatePath())
	writeRepoInfo(w, r, cand)
}
