package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// pairLinkFor builds the gg:// address of a CHANGE-SET: `@<a>..<b>`, what b
// changed relative to a. Both halves must be full shas — a producer always
// knows them, and a pair is a fixed pair of commits by definition (plan 1b
// ruling R2), so nothing positional or abbreviated may ride one.
func (m Model) pairLinkFor(a, b string) (string, bool) {
	if len(a) < 40 || len(b) < 40 {
		return "", false
	}
	repo, ok := m.linkRepoFor("")
	if !ok {
		return "", false
	}
	l := model.Link{
		Repo:   repo,
		Target: model.LinkTarget{State: model.StateCommitted, Pair: &model.LinkPair{A: a, B: b}},
		Side:   model.NoteSideNew,
	}
	return l.String(), true
}

// stashLinkMsg carries a stash row's resolved pair back to the UI thread.
type stashLinkMsg struct {
	ref         string // the positional ref that was asked about — for the notice only
	parent, sha string
	err         error
}

// stashLinkCmd resolves a stash row to the pair its link carries. This is the
// ONE copy row that cannot be built when the menu is: `stash@{N}` is
// positional (any push or drop renumbers every N), so the link must hold the
// stash's SHA, and a sha is a git call. The ref is an input to that resolve
// and never reaches the link (spec §3.4 rule 1).
func (m Model) stashLinkCmd(ref string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		parent, sha, err := svc.StashPair(context.Background(), ref)
		return stashLinkMsg{ref: ref, parent: parent, sha: sha, err: err}
	}
}

// stashLinkRow is the stash list's "Copy link": the PAIR — what the stash
// changes, which is what a stash is for — never the stash's own tree.
func (m Model) stashLinkRow(ref string) actionRow {
	return actionRow{
		id:    "copy-link",
		label: i18n.T("Copy link"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.stashLinkCmd(ref)
		},
	}
}

// resolvedStashLink copies the link once the pair is known. A stash dropped
// between opening the menu and running the row copies nothing.
func (m Model) resolvedStashLink(msg stashLinkMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = i18n.T("stash is gone: %s", msg.ref)
		return m, nil
	}
	text, ok := m.pairLinkFor(msg.parent, msg.sha)
	if !ok {
		m.statusMsg = i18n.T("▸ no gg link for this place")
		return m, nil
	}
	return m, m.copyToClipboardCmd(i18n.T("Copied link: %s", text), text)
}
