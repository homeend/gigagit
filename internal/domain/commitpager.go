package domain

import (
	"context"
	"sync"

	"github.com/gigagit/gg/internal/model"
)

// CommitPager fetches one page of commits for a feed generation. Implementations
// decide ordering and any acceleration (e.g. ensuring a commit-graph). The feed
// delegates page-fetching here so the loading strategy is swappable.
type CommitPager interface {
	Page(ctx context.Context, limit, skip, gen int, scope LogScope) ([]model.Commit, error)
	Name() string
}

// dateOrderPager is the legacy strategy: always `git log --date-order`. Slow on
// a repo without a commit-graph, by design — the A/B baseline (v1).
type dateOrderPager struct{ svc *Service }

func (p dateOrderPager) Page(ctx context.Context, limit, skip, gen int, scope LogScope) ([]model.Commit, error) {
	return p.svc.logPage(ctx, limit, skip, scope, gen, true)
}

func (p dateOrderPager) Name() string { return "date-order" }

// graphPager uses --date-order only when a commit-graph exists (cheap there),
// else git's plain order (instant). The order is captured ONCE per generation
// (keyed on gen): all pages of one generation share an order, because the feed
// pages with --skip and a mid-generation order flip with the same skip would
// silently drop commits. The only order transition is the next generation
// (after the background commit-graph write triggers a reload). v2.
type graphPager struct {
	svc       *Service
	mu        sync.Mutex
	gen       int  // generation whose order is cached (0 = none captured yet)
	dateOrder bool // cached order for gen
}

func (p *graphPager) Page(ctx context.Context, limit, skip, gen int, scope LogScope) ([]model.Commit, error) {
	p.mu.Lock()
	if gen != p.gen {
		p.gen = gen
		has, _ := p.svc.HasCommitGraph(ctx)
		p.dateOrder = has
	}
	do := p.dateOrder
	p.mu.Unlock()
	return p.svc.logPage(ctx, limit, skip, scope, gen, do)
}

func (p *graphPager) Name() string { return "graph" }
