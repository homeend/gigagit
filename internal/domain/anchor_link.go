package domain

import (
	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/model"
)

// AnchorLink is an overview anchor's gg:// link in repo: the working tree
// when commit is "", else that commit — how `gg overview show` hands another
// agent the exact lines (gg link text <link>). A note anchor has no file
// address; the caller prints the note instead.
func AnchorLink(repo model.LinkRepo, commit string, a agentdocs.Anchor) (string, error) {
	l := model.Link{Path: a.Path, Line: a.Start, Target: model.LinkTarget{State: model.StateUnstaged}}
	if commit != "" {
		l.Target = model.LinkTarget{State: model.StateCommitted, Commit: commit}
	}
	if a.End > a.Start {
		l.End = a.End
	}
	return linkTextIn(repo, l)
}
