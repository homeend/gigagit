package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// A pull request's review threads and its details. Like every PR read the
// page names the PR by NUMBER (R1): the pair a PR's diff opens on holds
// refs/gg/pr/<n> and sometimes a bare sha, neither of which the preview
// endpoints' branch allowlist could ever pass.
//
// R2 shapes the three routes: the notes GET reads the domain's comment cache
// and never the forge; the two routes that DO spend a forge call — the comment
// re-poll and the details overlay's load — are write-guarded POSTs the page
// makes on purpose.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/pr/notes", s.handlePRNotes)
		mux.HandleFunc("POST /api/pr/comments/refresh", writeGuard(s.handlePRCommentsRefresh))
		mux.HandleFunc("GET /api/pr/details", s.handlePRDetailsCached)
		mux.HandleFunc("POST /api/pr/details", writeGuard(s.handlePRDetails))
	})
}

// knownPR resolves ?n= to a listed pull request, answering the refusal itself.
func (s *Server) knownPR(w http.ResponseWriter, r *http.Request) (*domain.Service, model.PullRequest, bool) {
	n, ok := prNumber(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, errPRNumber)
		return nil, model.PullRequest{}, false
	}
	svc := s.service()
	pr, ok := s.cachedPR(svc, n)
	if !ok {
		writeErr(w, http.StatusNotFound, fmt.Errorf("unknown pull request #%d", n))
		return nil, model.PullRequest{}, false
	}
	return svc, pr, true
}

// handlePRNotes is /api/preview/notes for a pull request: the same answer
// shape over the pair PRPair resolves. The HEAD is the set's source — the
// domain recognises a PR by that ref and appends its cached threads.
func (s *Server) handlePRNotes(w http.ResponseWriter, r *http.Request) {
	svc, pr, ok := s.knownPR(w, r)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if path != "" && !isGitArgSafe(path) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
		return
	}
	ctx := readCtx(r)
	out := map[string]any{"notes": []wireNote{}, "tip": "", "counts": map[string]int{}, "total": 0}
	if !svc.PRFetched(ctx)[pr.Number] {
		writeJSON(w, out) // no local head yet: nothing to hang a note on
		return
	}
	pair := svc.PRPair(ctx, pr)
	set, err := svc.PreviewNotes(ctx, pair.Head, pair.Base)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out["tip"] = set.Tip
	if !set.OK() {
		writeJSON(w, out)
		return
	}
	notes := []wireNote{}
	if path != "" { // no path = the counts-only form, as on /api/preview/notes
		res, err := svc.PreviewNotesAt(ctx, set, path)
		if err != nil && !errors.Is(err, domain.ErrNotesDisabled) {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		for _, n := range res {
			notes = append(notes, domain.ToWireNotePreview(n, true))
		}
	}
	counts, total, cerr := svc.PreviewNoteCounts(ctx, set)
	if cerr != nil && !errors.Is(cerr, domain.ErrNotesDisabled) {
		writeErr(w, http.StatusInternalServerError, cerr)
		return
	}
	out["notes"], out["counts"], out["total"] = notes, orEmptyCounts(counts), total
	writeJSON(w, out)
}

// handlePRCommentsRefresh re-reads PR n's comments from the forge. The page
// posts it right after a PR diff opens and on every `prs` live event, and
// re-reads the notes only when "changed" says there is something new.
func (s *Server) handlePRCommentsRefresh(w http.ResponseWriter, r *http.Request) {
	svc, pr, ok := s.knownPR(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(readCtx(r), prRevalidateBudget)
	defer cancel()
	changed, err := svc.PRCommentsRefresh(ctx, pr.Number)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, map[string]any{"changed": changed})
}

// handlePRDetails is the details overlay's one load: the PR with its
// description (the listing carries none) and the comments that have no place
// in the diff — the conversation, the review verdicts and the outdated
// threads. It refreshes the comment cache on the way, so the diff's threads
// are as fresh as the overlay.
func (s *Server) handlePRDetails(w http.ResponseWriter, r *http.Request) {
	svc, pr, ok := s.knownPR(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(readCtx(r), prRevalidateBudget)
	defer cancel()
	full, err := svc.PullRequest(ctx, pr.Number)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	if _, err := svc.PRCommentsRefresh(ctx, pr.Number); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	c, _ := svc.PRCommentsCached(pr.Number)
	writeJSON(w, prDetailsBody(full, c))
}

// handlePRDetailsCached is the overlay's FIRST read: what the last forge read
// left in the domain's caches, so a PR looked at before shows at once while
// the POST above re-reads it. It never calls the forge (R2); "cached": false
// means there is nothing to show yet and the page waits for the POST.
func (s *Server) handlePRDetailsCached(w http.ResponseWriter, r *http.Request) {
	svc, pr, ok := s.knownPR(w, r)
	if !ok {
		return
	}
	full, c, ok := svc.PRDetailsCached(pr.Number)
	if !ok {
		writeJSON(w, map[string]any{"cached": false})
		return
	}
	body := prDetailsBody(full, c)
	body["cached"] = true
	writeJSON(w, body)
}

func prDetailsBody(pr model.PullRequest, c domain.PRComments) map[string]any {
	return map[string]any{
		"pr":        pr,
		"hub":       orEmptyComments(c.Hub),
		"outdated":  orEmptyComments(c.Outdated),
		"truncated": c.Truncated,
	}
}

func orEmptyComments(c []model.ForgeComment) []model.ForgeComment {
	if c == nil {
		return []model.ForgeComment{}
	}
	return c
}
