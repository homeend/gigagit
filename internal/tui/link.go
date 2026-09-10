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
// hunk carries a hunk reference through to model.Link.Hunk; no TUI call site
// passes a non-zero value today (the TUI addresses a cursor LINE, never a
// hunk ordinal) — it exists so this one builder can also serve a future
// TUI hunk-picker producer without a signature change, matching the CLI/web
// producers' shape (spec §7).
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

// contextLinkText is the link for whatever the user is looking at, mirroring
// contextCopyRows' precedence exactly (controller ruling P23): a stack
// surface (history/blame) on top of a diff out-ranks the diff underneath it
// — m.diffLayer()/diffNoteAddress() search the WHOLE layer stack, not just
// the top, so they must not be consulted while a surface owns the keyboard.
// Order:
//  1. history/blame on top → that surface's file link (full sha; no line —
//     neither surface exposes a cursor line that maps onto a diff row).
//  2. else the diff view itself on top → the cursor LINE (the point of the
//     feature); a diff with no note address (a two-sided compare, or any
//     other view the loader never stamped) refuses outright rather than
//     falling through to a lower-precedence surface underneath it.
//  3. else the focused panel's row: focusedBookmark (files-view row,
//     Files/Staged panel row) first, then the Commits panel's commit —
//     gated on !inContentWindow() so a content window with nothing above it
//     that focusedBookmark also declines (e.g. a stash file tree) does not
//     fall through to the Commits panel focus underneath it.
func (m Model) contextLinkText() (string, bool) {
	switch m.topLayer().(type) {
	case *historyView, *blameView:
		if b, ok := m.focusedBookmark(); ok {
			return m.linkFor(b.Address(), model.NoteSideNew, 0, 0)
		}
		return "", false
	}
	if _, ok := m.topLayer().(*diffView); ok {
		if addr, ok := m.diffNoteAddress(); ok {
			side, line, _, has := m.noteAnchorAtCursor()
			if !has {
				side, line = model.NoteSideNew, 0
			}
			return m.linkFor(addr, side, line, 0)
		}
		return "", false
	}
	if b, ok := m.focusedBookmark(); ok {
		return m.linkFor(b.Address(), model.NoteSideNew, 0, 0)
	}
	if !m.inContentWindow() && m.focus == panelCommits {
		if bi, ok := m.backingIndex(panelCommits); ok && bi < len(m.commits) {
			return m.linkFor(model.FileAddress{State: model.StateCommitted, Commit: m.commits[bi].Hash}, "", 0, 0)
		}
	}
	return "", false
}

// contextLinkRow is the `.` menu's "Copy link". It is a separate row rather
// than a member of contextCopyRows so the existing copy-row tests keep their
// exact expectations; both of that function's call sites splice it in beside
// the other copy rows (see insertCopyLinkRow).
func (m Model) contextLinkRow() (actionRow, bool) {
	text, ok := m.contextLinkText()
	if !ok {
		return actionRow{}, false
	}
	return m.copyRow("copy-link", i18n.T("Copy link"), i18n.T("Copied link: %s", text), text), true
}
