package domain

import (
	"context"

	"github.com/homeend/gigagit/internal/model"
)

// CommitPager fetches one page of commits for a feed generation. Implementations
// decide log ordering. The feed delegates page-fetching here so the loading
// strategy is swappable (selected by GG_COMMIT_PAGER).
type CommitPager interface {
	Page(ctx context.Context, limit, skip, gen int, scope LogScope) ([]model.Commit, error)
	Name() string
}

// plainPager is the default strategy: git's plain newest-first order, which is
// lazy — it parses only the page being shown — so it stays instant on a huge
// repo (~44ms vs ~2.2s for --date-order with a commit-graph; ~21s without). The
// order is topologically consistent for all practical viewing; only very deep,
// merge-heavy multi-branch history can produce a rare cosmetic lane stub.
type plainPager struct{ svc *Service }

func (p plainPager) Page(ctx context.Context, limit, skip, gen int, scope LogScope) ([]model.Commit, error) {
	return p.svc.logPage(ctx, limit, skip, scope, gen, false)
}

func (p plainPager) Name() string { return "plain" }

// dateOrderPager uses `git log --date-order`: a global topological sort that
// guarantees a parent never precedes its child (perfect graph lanes), at the
// cost of loading the whole history's ordering — slow on a large repo. Opt-in
// via GG_COMMIT_PAGER=date-order for anyone who wants guaranteed lanes.
type dateOrderPager struct{ svc *Service }

func (p dateOrderPager) Page(ctx context.Context, limit, skip, gen int, scope LogScope) ([]model.Commit, error) {
	return p.svc.logPage(ctx, limit, skip, scope, gen, true)
}

func (p dateOrderPager) Name() string { return "date-order" }
