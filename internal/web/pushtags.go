package web

import (
	"context"
	"net/http"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// Pushing a branch whose TIP carries tags the remote does not have: the TUI's
// P asks first ("Push branch + tags" / "Push branch only" / "Cancel") and then
// chains one tag push after the branch push. This is that flow's server half —
// the check the client asks before starting, and the verification of what it
// sends back.
//
// pushTagCheckBudget is the TUI's: a remote that does not answer must never
// turn a push into a hang. On timeout the check reports nothing and the push
// goes ahead as it always did.
const pushTagCheckBudget = 5 * time.Second

// handlePushTagCheck reports the current branch tip's tags that the default
// remote does not have. Empty on any failure — this is an offer, not a gate.
func (s *Server) handlePushTagCheck(w http.ResponseWriter, r *http.Request) {
	names := []string{}
	if tags, err := s.unpushedTipTags(readCtx(r)); err == nil {
		names = tags
	}
	writeJSON(w, map[string]any{"tags": names})
}

// unpushedTipTags is the shared computation: tags pointing at the current
// branch's tip, minus the ones a fresh (bounded) remote listing already has.
// The TUI's tagsAtCommit + remoteSet difference, same order of operations.
func (s *Server) unpushedTipTags(ctx context.Context) ([]string, error) {
	svc := s.service()
	tip, err := s.currentTipHash(ctx, svc)
	if err != nil || tip == "" {
		return nil, err
	}
	tags, err := svc.Tags(ctx)
	if err != nil {
		return nil, err
	}
	var atTip []model.Tag
	for _, t := range tags {
		if t.Target == tip {
			atTip = append(atTip, t)
		}
	}
	if len(atTip) == 0 {
		return nil, nil // nothing to offer, and no network call made
	}
	bctx, cancel := context.WithTimeout(ctx, pushTagCheckBudget)
	defer cancel()
	remote, err := svc.RemoteTagsFresh(bctx)
	if err != nil || remote == nil {
		return nil, err // timed out or unreachable: offer nothing, push normally
	}
	var out []string
	for _, t := range atTip {
		if !remote[t.Name] {
			out = append(out, t.Name)
		}
	}
	return out, nil
}

// currentTipHash resolves the checked-out branch's tip.
func (s *Server) currentTipHash(ctx context.Context, svc *domain.Service) (string, error) {
	bs, err := svc.Branches(ctx)
	if err != nil {
		return "", err
	}
	for _, b := range bs {
		if b.IsHead {
			return b.Hash, nil
		}
	}
	return "", nil // detached HEAD: no branch tip to carry tags
}

// verifiedTipTags keeps only the requested names that really are tags at the
// current tip. The client sends back what the check offered, but a wire value
// is never a push target on trust: a stale or invented name is dropped rather
// than handed to git.
func (s *Server) verifiedTipTags(ctx context.Context, want []string) []string {
	if len(want) == 0 {
		return nil
	}
	svc := s.service()
	tip, err := s.currentTipHash(ctx, svc)
	if err != nil || tip == "" {
		return nil
	}
	tags, err := svc.Tags(ctx)
	if err != nil {
		return nil
	}
	atTip := make(map[string]bool, len(tags))
	for _, t := range tags {
		if t.Target == tip {
			atTip[t.Name] = true
		}
	}
	var out []string
	for _, n := range want {
		if atTip[n] && isGitArgSafe(n) {
			out = append(out, n)
		}
	}
	return out
}

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/push-tag-check", s.handlePushTagCheck)
	})
}
