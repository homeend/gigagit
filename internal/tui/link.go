package tui

import (
	"path/filepath"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// linkFor builds the gg:// address for one place in this repository, or
// refuses. It refuses when the grammar cannot hold the place: a path
// containing '@', ':' or '#' (spec §1 — the producers refuse rather than emit
// something that reparses as a different place), a shelf entry (there is no
// shelf form), or a commit address with no full sha (a producer always knows
// the full sha; anything shorter did not come from a real commit read).
//
// An UNTRACKED file uses the plain working-tree form: the grammar has no
// "untracked" target, and index→file is what the resolver reads for it anyway.
func (m Model) linkFor(addr model.FileAddress, side model.NoteSide, line, hunk int) (string, bool) {
	if addr.Path != "" && !model.LinkPathOK(addr.Path) {
		return "", false
	}
	var l model.Link
	if m.linkRepoName != "" {
		l.Repo = model.LinkRepo{Name: m.linkRepoName}
	} else {
		wt := addr.Worktree
		if wt == "" {
			wt = m.currentWorktree
		}
		if wt == "" {
			return "", false
		}
		l.Repo = model.LinkRepo{Abs: filepath.ToSlash(filepath.Clean(wt))}
	}
	l.Path = addr.Path
	switch addr.State {
	case model.StateShelf:
		return "", false
	case model.StateStaged:
		l.Target = model.LinkTarget{State: model.StateStaged}
	case model.StateCommitted:
		// A link always writes the FULL sha (sha256 repos are 64 hex chars;
		// never a hard == 40) — a short/abbreviated sha did not come from a
		// real commit read and must not be copied as if it did.
		if len(addr.Commit) < 40 {
			return "", false
		}
		l.Target = model.LinkTarget{State: model.StateCommitted, Commit: addr.Commit}
	default: // StateUnstaged, StateUntracked
		l.Target = model.LinkTarget{State: model.StateUnstaged}
	}
	l.Side, l.Line, l.Hunk = side, line, hunk
	if l.Side == "" {
		l.Side = model.NoteSideNew
	}
	if l.Path == "" && (line > 0 || hunk > 0) {
		return "", false
	}
	return l.String(), true
}

// contextLinkText is the link for whatever the user is looking at, in the same
// precedence the copy rows use: the diff view's cursor LINE first (the point
// of the feature), then whatever focusedBookmark resolves (history/blame file,
// files-view row, Files/Staged panel row), then the Commits panel's commit.
func (m Model) contextLinkText() (string, bool) {
	if addr, ok := m.diffNoteAddress(); ok {
		side, line, _, has := m.noteAnchorAtCursor()
		if !has {
			side, line = model.NoteSideNew, 0
		}
		return m.linkFor(addr, side, line, 0)
	}
	if b, ok := m.focusedBookmark(); ok {
		return m.linkFor(b.Address(), model.NoteSideNew, 0, 0)
	}
	if m.focus == panelCommits {
		if bi, ok := m.backingIndex(panelCommits); ok && bi < len(m.commits) {
			return m.linkFor(model.FileAddress{State: model.StateCommitted, Commit: m.commits[bi].Hash}, "", 0, 0)
		}
	}
	return "", false
}

// contextLinkRow is the `.` menu's "Copy link". It is a separate row rather
// than a member of contextCopyRows so the existing copy-row tests keep their
// exact expectations; both of that function's call sites append it.
func (m Model) contextLinkRow() (actionRow, bool) {
	text, ok := m.contextLinkText()
	if !ok {
		return actionRow{}, false
	}
	return m.copyRow("copy-link", i18n.T("Copy link"), i18n.T("Copied link: %s", text), text), true
}
