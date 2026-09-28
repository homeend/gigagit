package tui

import "github.com/homeend/gigagit/internal/model"

// versionLinkFor is the TUI's ONE producer of a version's preview link —
// gg://<repo>@<base>..<ours>?version=<id> — shared by the versions popup's L
// and the drift notice's copy action. false for a one-branch record (it
// records no preview), a record whose trailer does not hold two usable shas
// (the trailer is never sha-checked on write), or a checkout the grammar
// cannot spell. Never concatenated: built as a model.Link and rendered.
func (m Model) versionLinkFor(v model.BranchVersion) (string, bool) {
	if v.Base == "" || v.Ours == "" {
		return "", false
	}
	if _, err := model.CommitEndpoint(v.Base); err != nil {
		return "", false
	}
	if _, err := model.CommitEndpoint(v.Ours); err != nil {
		return "", false
	}
	repo, ok := m.linkRepoFor("")
	if !ok {
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
