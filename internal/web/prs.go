package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
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
		mux.HandleFunc("GET /api/pr/open", s.handlePROpen)
		mux.HandleFunc("POST /api/pr/revalidate", writeGuard(s.handlePRRevalidate))
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

// cachedPR finds PR n among the rows the page was shown: the polled list
// first, then the last search's results (prsearch.go) — a closed PR found by
// searching is in no list until it has been fetched.
func (s *Server) cachedPR(svc *domain.Service, n int) (model.PullRequest, bool) {
	_, rows, _, _ := s.prsSnapshot(svc)
	for _, p := range rows {
		if p.Number == n {
			return p, true
		}
	}
	if last, ok := svc.PRSearchLast(); ok {
		for _, p := range last.PRs {
			if p.Number == n {
				return p, true
			}
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

	budget := prLoadBudget
	if s.prBudget > 0 {
		budget = s.prBudget
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	state, errText := prsReady, ""
	var rows []model.PullRequest
	if st := svc.ForgeStatus(ctx); !st.Available() {
		state = prsOff
		// A probe that ran out of time is not a verdict — domain does not cache
		// it either. Stay unprobed so the next read (or ⟳) asks again, instead
		// of turning the feature off for the session over one slow start.
		if ctx.Err() != nil || errors.Is(st.Err, context.DeadlineExceeded) || errors.Is(st.Err, context.Canceled) {
			state, errText = prsUnprobed, "the forge did not answer in time"
		}
	} else if got, err := svc.PullRequests(ctx); err != nil {
		errText = err.Error()
	} else {
		rows = got
	}

	s.prs.mu.Lock()
	if s.prs.svc != svc {
		// Re-rooted while this listing ran: the cache belongs to another
		// repository now, and at() would hand it BACK to this one.
		s.prs.mu.Unlock()
		return
	}
	c = &s.prs
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

// dropCachedPR removes a no-longer-open row at once, so a forgotten PR leaves
// the list without waiting for the re-list. An open one stays: forgetting it
// only drops the local ref, the forge still lists it.
func (s *Server) dropCachedPR(svc *domain.Service, n int) {
	s.prs.mu.Lock()
	defer s.prs.mu.Unlock()
	c := s.prs.at(svc)
	c.prs = slices.DeleteFunc(slices.Clone(c.prs), func(p model.PullRequest) bool {
		return p.Number == n && !p.IsOpen()
	})
}

// prNumber parses the ?n= of a pull-request read. Anything but a positive
// integer is refused before it can reach a ref name.
func prNumber(r *http.Request) (int, bool) {
	n, err := strconv.Atoi(r.URL.Query().Get("n"))
	return n, err == nil && n > 0
}

// handlePROpen resolves PR n to the pair its diff opens on and answers the
// merge preview's open shape, so the page shows it on the same compare screen.
// "source"/"target" in the answer are DISPLAY names — the head may live in a
// fork — and are never read back as refs.
//
// state "unfetched" (no local refs/gg/pr/<n>) is a LIVE ref check, never the
// cached row's flag: the page asks right after a pr-fetch finishes, while the
// post-run re-list is still in flight.
func (s *Server) handlePROpen(w http.ResponseWriter, r *http.Request) {
	n, ok := prNumber(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, errPRNumber)
		return
	}
	svc := s.service()
	pr, ok := s.cachedPR(svc, n)
	if !ok {
		writeErr(w, http.StatusNotFound, fmt.Errorf("unknown pull request #%d", n))
		return
	}
	label := fmt.Sprintf("PR #%d · %s", n, pr.Title)
	ctx := readCtx(r)
	if !svc.PRFetched(ctx)[n] {
		writeJSON(w, map[string]any{"state": "unfetched", "label": label, "pr": n, "source": pr.Source, "target": pr.Target})
		return
	}
	pair := svc.PRPair(ctx, pr)
	eps, err := svc.PreviewOpen(ctx, pair.Head, pair.Base)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	body := previewOpenBody(eps, label, pr.Source, pr.Target)
	body["pr"] = n
	// The pair as a gg:// link names it. Sent OUT only (R1): the page pastes
	// them into a copied link and never hands them back as a wire value.
	body["link_source"], body["link_target"] = pair.Head, pair.Base
	writeJSON(w, body)
}

// prRevalidateBudget bounds the one forge call a cached open still makes.
const prRevalidateBudget = 30 * time.Second

// handlePRRevalidate is the background half of a cached open. The page shows
// the diff it already has (a local read), then posts here: the forge is asked
// for PR n, the listed row takes the answer (no re-list), and "moved" says
// whether a pr-fetch would change what is on screen. It spends a forge call,
// so it is a guarded POST.
func (s *Server) handlePRRevalidate(w http.ResponseWriter, r *http.Request) {
	n, ok := prNumber(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, errPRNumber)
		return
	}
	svc := s.service()
	if _, ok := s.cachedPR(svc, n); !ok {
		writeErr(w, http.StatusNotFound, fmt.Errorf("unknown pull request #%d", n))
		return
	}
	ctx, cancel := context.WithTimeout(readCtx(r), prRevalidateBudget)
	defer cancel()
	rv, err := svc.PRRevalidate(ctx, n)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	s.prs.mu.Lock()
	if s.prs.svc == svc {
		rows := slices.Clone(s.prs.prs)
		for i := range rows {
			if rows[i].Number == n {
				rows[i] = rv.PR
			}
		}
		s.prs.prs = rows
	}
	s.prs.mu.Unlock()
	writeJSON(w, map[string]any{"moved": rv.Moved, "state": rv.PR.State})
}
