package cli

import (
	"context"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// versionLinkText is the ONE CLI producer of a version's preview link:
// gg://<repo>@<base>..<ours>?version=<id>. false for a one-branch record
// (records no preview), a record whose trailer does not hold two usable
// shas, or a checkout the grammar cannot spell (LinkRepo error) — the
// caller prints nothing rather than something ParseLink would refuse.
func versionLinkText(ctx context.Context, svc *domain.Service, v model.BranchVersion) (string, bool) {
	if v.Base == "" || v.Ours == "" {
		return "", false
	}
	if _, err := model.CommitEndpoint(v.Base); err != nil {
		return "", false
	}
	if _, err := model.CommitEndpoint(v.Ours); err != nil {
		return "", false
	}
	repo, err := svc.LinkRepo(ctx)
	if err != nil {
		return "", false
	}
	l := model.Link{
		Repo:   repo,
		Target: model.LinkTarget{State: model.StateCommitted, Pair: &model.LinkPair{A: v.Base, B: v.Ours}},
		Side:   model.NoteSideNew,
		Hint:   model.LinkHint{Kind: "version", ID: v.ID()},
	}
	s := l.String()
	if _, err := model.ParseLink(s); err != nil {
		return "", false
	}
	return s, true
}
