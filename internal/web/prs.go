package web

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// Pull requests (read-only). Three rules shape this file:
//
//   - The page names a pull request by its NUMBER and nothing else. A PR's
//     head is refs/gg/pr/<n> and a merged PR's base is a raw sha; neither is a
//     branch, so neither can pass the preview endpoints' branch allowlist —
//     and neither is accepted from the page. The pair is resolved HERE.
//   - A GET never calls the forge. Reads serialise under the repo gate and a
//     gh call takes seconds; the list lane (loadPRs) is the one place that
//     spends one, and every GET answers from prCache.
//   - No usable forge means no UI at all: the page keeps its section hidden
//     until a list says available.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/pr", s.handlePRs)
		mux.HandleFunc("POST /api/pr/refresh", writeGuard(s.handlePRRefresh))
	})
}

// prLoadBudget bounds one list-lane run: the forge probe plus the listing.
const prLoadBudget = 60 * time.Second

const (
	prsUnprobed = ""
	prsOff      = "off"   // no usable forge — final for this service
	prsReady    = "ready" // probed; rows may still be empty
)

// prCache is the last pull-request listing taken for the CURRENT service. It
// is keyed to the service pointer (the remoteTagCache posture), so a re-root
// starts unknown again rather than showing the previous repo's rows.
type prCache struct {
	mu      sync.Mutex
	svc     *domain.Service
	state   string
	prs     []model.PullRequest
	err     string // the last listing's failure; the rows then stand as they were
	loading bool
}

// at returns the cache positioned on svc, reset when the service changed.
// Callers hold c.mu.
func (c *prCache) at(svc *domain.Service) *prCache {
	if c.svc != svc {
		c.svc, c.state, c.prs, c.err, c.loading = svc, prsUnprobed, nil, "", false
	}
	return c
}

type prRow struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	State       string `json:"state"`
	Draft       bool   `json:"draft"`
	ReviewState string `json:"review_state"`
	Source      string `json:"source"`
	Target      string `json:"target"`
	URL         string `json:"url"`
	Updated     string `json:"updated"`
	HeadSHA     string `json:"head_sha"`
	// Fetched: a local refs/gg/pr/<n> exists, so the diff opens without a fetch.
	Fetched bool `json:"fetched"`
}

func prRowFrom(p model.PullRequest, fetched bool) prRow {
	r := prRow{
		Number: p.Number, Title: p.Title, Author: p.Author, State: p.State, Draft: p.Draft,
		ReviewState: p.ReviewState, Source: p.Source, Target: p.Target, URL: p.URL,
		HeadSHA: p.HeadSHA, Fetched: fetched,
	}
	if !p.Updated.IsZero() {
		r.Updated = p.Updated.UTC().Format(time.RFC3339)
	}
	return r
}

// prsSnapshot is the cache's answer for svc.
func (s *Server) prsSnapshot(svc *domain.Service) (state string, rows []model.PullRequest, errText string, loading bool) {
	s.prs.mu.Lock()
	defer s.prs.mu.Unlock()
	c := s.prs.at(svc)
	return c.state, c.prs, c.err, c.loading
}

// cachedPR finds PR n among the rows the page was shown.
func (s *Server) cachedPR(svc *domain.Service, n int) (model.PullRequest, bool) {
	_, rows, _, _ := s.prsSnapshot(svc)
	for _, p := range rows {
		if p.Number == n {
			return p, true
		}
	}
	return model.PullRequest{}, false
}

// loadPRs is the list lane: the one place a listing talks to the forge. It
// probes (once per service, domain caches the verdict), lists, stores and
// tells every open page. A failed listing keeps the rows that were standing.
func (s *Server) loadPRs(svc *domain.Service) {
	s.prs.mu.Lock()
	c := s.prs.at(svc)
	if c.state == prsOff || c.loading {
		s.prs.mu.Unlock()
		return
	}
	c.loading = true
	s.prs.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), prLoadBudget)
	defer cancel()
	state, errText := prsReady, ""
	var rows []model.PullRequest
	if !svc.ForgeStatus(ctx).Available() {
		state = prsOff
	} else if got, err := svc.PullRequests(ctx); err != nil {
		errText = err.Error()
	} else {
		rows = got
	}

	s.prs.mu.Lock()
	c = s.prs.at(svc)
	c.loading = false
	c.state = state
	c.err = errText
	if errText == "" {
		c.prs = rows
	}
	s.prs.mu.Unlock()
	if s.service() != svc {
		return // re-rooted meanwhile: that repo's pages are gone
	}
	if h := s.liveHubRef(); h != nil {
		h.emit(liveMsg{Changed: []string{"prs"}, Reason: "prs"})
	}
}

// kickPRs starts a background listing. Without force it runs only for a
// service that was never probed (GET /api/pr's first call); with force it
// re-lists a ready one (the interval lane, a finished pr-fetch/pr-forget).
func (s *Server) kickPRs(svc *domain.Service, force bool) {
	s.prs.mu.Lock()
	c := s.prs.at(svc)
	skip := c.loading || c.state == prsOff || (!force && c.state != prsUnprobed)
	s.prs.mu.Unlock()
	if !skip {
		go s.loadPRs(svc)
	}
}

// writePRs answers the list shape from the cache. "loaded" is false only
// until the first listing lands; the page keeps its section hidden until
// "available".
func (s *Server) writePRs(w http.ResponseWriter, r *http.Request, svc *domain.Service) {
	state, rows, errText, _ := s.prsSnapshot(svc)
	out := make([]prRow, 0, len(rows))
	if len(rows) > 0 {
		fetched := svc.PRFetched(readCtx(r))
		for _, p := range rows {
			out = append(out, prRowFrom(p, fetched[p.Number]))
		}
	}
	writeJSON(w, map[string]any{
		"available": state == prsReady,
		"loaded":    state != prsUnprobed,
		"error":     errText,
		"prs":       out,
	})
}

func (s *Server) handlePRs(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	s.kickPRs(svc, false)
	s.writePRs(w, r, svc)
}

// handlePRRefresh is the section's manual refresh: it spends a forge call,
// so it is a guarded POST, and it answers with the fresh list.
func (s *Server) handlePRRefresh(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	s.loadPRs(svc)
	s.writePRs(w, r, svc)
}
