package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
)

// Solo mode narrows the commit list to one branch's history, the TUI's solo.
//
// It is SERVER state, not per-tab: there is a single CommitFeed behind
// /api/commits, so a second tab necessarily sees the same scope. Rather than
// pretend otherwise, every response that depends on the scope reports it, and
// the client renders an exit affordance from that — a mode you can enter and
// not see is a trap.
//
// The scope is stored as a branch name and turned into a LogScope only when a
// feed is built (feedFor). That is what makes it survive the feed being
// dropped after a state-changing op: resetFeed nils the feed, the next request
// rebuilds it, and the rebuild re-reads this field. Nothing has to remember to
// re-apply anything.

// soloBranch reports the branch the commit list is currently narrowed to
// ("" = the whole repo).
func (s *Server) soloBranch() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.solo
}

// setSolo records the scope and drops the feed so the next /api/commits
// rebuilds under it.
func (s *Server) setSolo(branch string) {
	s.mu.Lock()
	s.solo = branch
	s.feed = nil
	s.mu.Unlock()
}

// soloScope maps the stored branch to the feed's refspec. Branches alone is
// ref SELECTION, not a content filter, so the commit graph still renders.
func soloScope(branch string) domain.LogScope {
	if branch == "" {
		return domain.LogScope{}
	}
	return domain.LogScope{Branches: []string{branch}}
}

type soloRequest struct {
	Branch string `json:"branch"` // "" = show every branch again
}

// handleSolo sets or clears the commit-list scope.
//
// The ref is resolved against the server's own branch AND tag lists rather
// than merely sanitized. That is not about argv safety (isGitArgSafe already
// covers that) but about never entering a scope that cannot render: a
// nonexistent ref would make every subsequent /api/commits fail, and the
// exit affordance the client draws from that response would go with it.
// Tags qualify because a tag is just a ref to git log — the same scope
// machinery the TUI's solo-this-tag uses.
func (s *Server) handleSolo(w http.ResponseWriter, r *http.Request) {
	var req soloRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	if req.Branch != "" {
		if !isGitArgSafe(req.Branch) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid ref"))
			return
		}
		found := false
		branches, err := s.service().Branches(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		for _, b := range branches {
			if b.Name == req.Branch {
				found = true
				break
			}
		}
		if !found {
			tags, err := s.service().Tags(r.Context())
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			for _, tg := range tags {
				if tg.Name == req.Branch {
					found = true
					break
				}
			}
		}
		if !found {
			writeErr(w, http.StatusNotFound, errors.New("unknown branch or tag"))
			return
		}
	}
	s.setSolo(req.Branch)
	writeJSON(w, map[string]any{"solo": req.Branch})
}
