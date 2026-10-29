package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/steer"
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

// steerNavigateSessionOverview opens session overview c.HintID the way gg
// session files focus does (a reload first: it may have been written a
// moment ago, or closed with x); a miss falls to the permanent overview
// of that id.
func (m Model) steerNavigateSessionOverview(c steer.Command) (Model, tea.Cmd) {
	f := c
	f.Cmd, f.FileID, f.File = "file_focus", c.HintID, ""
	// By id only: an open file NAMED like the id is not this overview.
	if d, _ := m.findOverview(c.HintID); d == nil && m.docs != nil {
		m.docs.Reload(domain.CheckoutKey(m.currentWorktree))
		m = m.syncOverviews()
	}
	if d, _ := m.findOverview(c.HintID); d == nil {
		// Not a session overview here: a permanent one by id (its gone
		// answer is onOverviewHint's).
		return m.steerNavigateOverview(c)
	}
	return m.steerFileFocus(f)
}
