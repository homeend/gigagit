package tui

import (
	"context"
	"errors"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// overviewLinkRow is "Copy gg link" for overview d (spec
// 2026-10-28-overview-links-design): another agent reads the link with gg
// overview show. A session overview's link names its worktree, a permanent
// one its commit, a review's stored overview the review itself.
func (m Model) overviewLinkRow(d *openFile) actionRow {
	svc := m.svc
	var build func(context.Context) (string, error)
	switch d.src.kind {
	case srcCommitOverview:
		id := d.src.rev
		build = func(ctx context.Context) (string, error) { return svc.OverviewLink(ctx, id) }
	case srcReviewOverview:
		id := d.src.rev
		build = func(ctx context.Context) (string, error) { return svc.ReviewLink(ctx, id) }
	default:
		wt, id := "", d.id()
		if d.docs != nil {
			wt = d.docs.AttachedWorktree(d.root)
		}
		build = func(context.Context) (string, error) {
			if wt == "" {
				return "", errors.New(i18n.T("this overview is not saved to a worktree — no link"))
			}
			return domain.SessionOverviewLink(wt, id)
		}
	}
	return m.asyncCopyLinkRow("overview-copy-link", i18n.T("Copy gg link"), build)
}

// permanentOverviewLinkRow is "Copy gg link" on a permanent overview's
// entry (a commit's or a PR view's Overviews row, a Branches └ Overview row):
// no document is open there, only its id.
func (m Model) permanentOverviewLinkRow(id string) actionRow {
	return m.overviewLinkRow(&openFile{src: fileSource{kind: srcCommitOverview, rev: id}})
}
