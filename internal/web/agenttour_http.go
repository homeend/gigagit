package web

// Agent tours (stage 4): POST /api/agent-tour files a session's brief or
// latest report as an overview in its worktree (again, when the user closed
// it) and says where it lives; the page switches there and opens it.

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/agent-tour", writeGuard(s.handleAgentTour))
	})
}

type agentTourReq struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // brief | report
}

func (s *Server) handleAgentTour(w http.ResponseWriter, r *http.Request) {
	var q agentTourReq
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if q.Kind != "brief" && q.Kind != "report" {
		writeErr(w, http.StatusBadRequest, errors.New(`kind must be "brief" or "report"`))
		return
	}
	if _, ok := sessionByID(w, q.ID); !ok {
		return
	}
	d, err := domain.AgentTour(domain.FullSessionID(domain.SessionID(q.ID)), q.Kind)
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	// A tour of the served worktree joins its open-file list in this same
	// turn (steerOverviewAdd's way): the page focuses it right after this
	// answer, before any follow pass could list it. One of another worktree
	// is listed by the follow pass the page's switch brings.
	root, _ := s.docsDirs(r.Context())
	wt := s.service().Root()
	var o agentdocs.Overview
	s.listDocs(wt, func() string {
		if o, _, err = s.docs.FileTour(d.Key, d.Root, d.Dir, d.Title, d.Text); err != nil {
			return ""
		}
		s.docs.CheckAnchors(o.ID)
		if d.Root != root {
			return ""
		}
		_, ev, _ := s.ofs.ensureOpenID(wt, overviewKey(o), o.ID, o.Title)
		return ev
	})
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	// here: the tour lives in the served worktree — the page opens it at
	// once; else it switches to worktree. Decided here, by the store keys,
	// never by the page comparing path spellings (Windows notation differs).
	writeJSON(w, map[string]any{"overview": o.ID, "worktree": d.Dir, "here": d.Root == root})
}
