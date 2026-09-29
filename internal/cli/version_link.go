package cli

import "github.com/homeend/gigagit/internal/model"

// versionLinkText is the ONE CLI producer of a version's preview link:
// gg://<repo>@<base>..<ours>?version=<id>. repo/repoErr are the caller's
// ONE svc.LinkRepo answer (it asks git for the remote; a list must not ask
// once per row). false for a one-branch record (records no preview), a
// record whose trailer does not hold two usable shas, or a checkout the
// grammar cannot spell (repoErr) — the caller prints nothing rather than
// something ParseLink would refuse.
func versionLinkText(repo model.LinkRepo, repoErr error, v model.BranchVersion) (string, bool) {
	if repoErr != nil || v.Base == "" || v.Ours == "" {
		return "", false
	}
	if _, err := model.CommitEndpoint(v.Base); err != nil {
		return "", false
	}
	if _, err := model.CommitEndpoint(v.Ours); err != nil {
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
