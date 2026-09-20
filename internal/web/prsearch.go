package web

// Pull-request search: the way to a closed or merged PR gg never fetched.
//
//   - POST /api/pr/search spends the forge call (R2: a GET never does), so it
//     is write-guarded like the details read.
//   - GET /api/pr/search answers domain's session-only last result — what a
//     page that was reloaded shows again, at no forge cost.
//   - A result row is opened by its NUMBER through /api/pr/open (R1); cachedPR
//     knows the searched rows, so a PR the polled list never had resolves.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/pr/search", s.handlePRSearchLast)
		mux.HandleFunc("POST /api/pr/search", writeGuard(s.handlePRSearch))
	})
}

// prSearchBudget bounds the one forge call a search makes.
const prSearchBudget = 45 * time.Second

type prSearchQuery struct {
	Text  string `json:"text"`
	State string `json:"state"`
}

func (s *Server) writePRSearch(w http.ResponseWriter, r *http.Request, svc *domain.Service, res domain.PRSearchResult, has bool) {
	out := make([]prRow, 0, len(res.PRs))
	if len(res.PRs) > 0 {
		fetched := svc.PRFetched(readCtx(r))
		for _, p := range res.PRs {
			out = append(out, prRowFrom(p, fetched[p.Number]))
		}
	}
	var q *prSearchQuery
	if has {
		q = &prSearchQuery{Text: res.Query.Text, State: res.Query.State}
	}
	writeJSON(w, map[string]any{"query": q, "prs": out, "more": res.More})
}

// handlePRSearchLast never reaches the forge: it is the session's last answer.
func (s *Server) handlePRSearchLast(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	res, ok := svc.PRSearchLast()
	s.writePRSearch(w, r, svc, res, ok)
}

func (s *Server) handlePRSearch(w http.ResponseWriter, r *http.Request) {
	var req prSearchQuery
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// The page's limit is fixed; the text is opaque and goes to the forge's
	// own search as one argv value.
	q, err := domain.NormalizePRQuery(domain.PRQuery{State: req.State, Text: req.Text, Limit: domain.PRSearchDefaultLimit})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	svc := s.service()
	ctx, cancel := context.WithTimeout(readCtx(r), prSearchBudget)
	defer cancel()
	res, err := svc.PRSearch(ctx, q)
	switch {
	case errors.Is(err, domain.ErrForgeUnavailable):
		writeErr(w, http.StatusNotFound, err)
		return
	case err != nil:
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	s.writePRSearch(w, r, svc, res, true)
}
