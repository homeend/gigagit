package domain

import (
	"context"

	"github.com/gigagit/gg/internal/model"
)

// CommitPager fetches one page of commits for a feed generation. Implementations
// decide ordering and any acceleration (e.g. ensuring a commit-graph). The feed
// delegates page-fetching here so the loading strategy is swappable.
type CommitPager interface {
	Page(ctx context.Context, limit, skip, gen int, scope LogScope) ([]model.Commit, error)
	Name() string
}

// dateOrderPager is the legacy strategy: a plain `git log --date-order` walk via
// logPage. It is behavior-identical to the pre-refactor feed.
type dateOrderPager struct{ svc *Service }

func (p dateOrderPager) Page(ctx context.Context, limit, skip, gen int, scope LogScope) ([]model.Commit, error) {
	return p.svc.logPage(ctx, limit, skip, scope, gen)
}

func (p dateOrderPager) Name() string { return "date-order" }
