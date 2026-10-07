package domain

import (
	"context"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// PRPrefetch fetches and prepares, in the background, the most recently
// opened PRs whose forge head moved past the local ref (spec §2.5): each
// fetch runs through Execute (the repo gate), then PRPreview, so the next
// open is a cache hit. One at a time per Service — the TUI and the page it
// hosts may both ask after the same list refresh — and it steps aside while
// a user operation holds or waits for the gate. Returns how many PRs it
// warmed; 0 when [forge] prefetch is 0.
func (s *Service) PRPrefetch(ctx context.Context) int {
	v, _ := s.flight.Do("pr-prefetch", func() (any, error) { return s.prefetchPRs(ctx), nil })
	return v.(int)
}

// gateBusy: a user operation holds or waits for the repo gate.
func (s *Service) gateBusy(ctx context.Context) bool {
	return len(s.gateFor(ctx).Queue()) > 0
}

func (s *Service) prefetchPRs(ctx context.Context) int {
	s.forgeMu.Lock()
	_, limit := s.prPolicyLocked()
	s.forgeMu.Unlock()
	st := s.prCacheStore(ctx)
	if limit <= 0 || st == nil {
		return 0
	}
	listed := map[int]model.PullRequest{}
	if prs, ok := s.PullRequestsCached(); ok {
		for _, p := range prs {
			listed[p.Number] = p
		}
	}
	warmed := 0
	for _, e := range st.Entries() {
		if warmed >= limit || ctx.Err() != nil || s.gateBusy(ctx) {
			break
		}
		p, ok := listed[e.Number]
		if !ok || !p.IsOpen() || p.HeadSHA == "" {
			continue
		}
		if local, err := s.repo.ResolveCommit(ctx, git.PRRef(p.Number)); err == nil && local == p.HeadSHA {
			continue // unmoved: nothing to warm
		}
		op, err := s.PRFetchOp(ctx, p.Number)
		if err != nil {
			continue
		}
		if _, err := s.Execute(ctx, op, nil, nil); err != nil {
			continue
		}
		if _, err := s.PRPreview(ctx, p); err == nil {
			warmed++
		}
	}
	return warmed
}
