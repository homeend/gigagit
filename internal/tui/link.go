package tui

import (
	"path/filepath"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// linkRepoFor builds the link's repository half: the remote NAME when this
// checkout has one, else the local absolute-path form. It refuses a checkout
// path the grammar cannot hold (the first '@' is the target separator and the
// first '#' the hunk one), exactly as the CLI and web producers do.
func (m Model) linkRepoFor(worktree string) (model.LinkRepo, bool) {
	if m.linkRepoName != "" {
		return model.LinkRepo{Name: m.linkRepoName}, true
	}
	wt := worktree
	if wt == "" {
		wt = m.currentWorktree
	}
	if wt == "" {
		return model.LinkRepo{}, false
	}
	abs := filepath.ToSlash(filepath.Clean(wt))
	if !model.LinkAbsOK(abs) {
		return model.LinkRepo{}, false
	}
	return model.LinkRepo{Abs: abs}, true
}

// linkFor builds the gg:// address for one place in this repository, or
// refuses. It refuses when the grammar cannot hold the place: a path
// containing '@', ':' or '#' (spec §1 — the producers refuse rather than emit
// something that reparses as a different place), a remoteless checkout whose
// own PATH holds '@' or '#' (model.LinkAbsOK — same rule, other half of the
// link), a shelf entry (there is no shelf form), or a commit address with no
// full sha (a producer always knows the full sha; anything shorter did not
// come from a real commit read).
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
	repo, ok := m.linkRepoFor(addr.Worktree)
	if !ok {
		return "", false
	}
	var l model.Link
	l.Repo = repo
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

// previewLinkFor builds the gg:// address of a place in a MERGE PREVIEW:
// the pair itself (path ""), one of its files, or a new-side line in one.
// The preview's old side is the merge base, which no address names, so there
// is no old-side form and no side parameter — every preview link is new-side.
//
// It refuses, rather than emitting something that reparses as a different
// place, when a branch NAME or the path is not expressible in the grammar.
func (m Model) previewLinkFor(source, target, path string, line int) (string, bool) {
	if !model.LinkRefOK(source) || !model.LinkRefOK(target) {
		return "", false
	}
	if path != "" && !model.LinkPathOK(path) {
		return "", false
	}
	if path == "" && line > 0 {
		return "", false
	}
	repo, ok := m.linkRepoFor("")
	if !ok {
		return "", false
	}
	l := model.Link{
		Repo: repo,
		Path: path,
		Target: model.LinkTarget{
			State:   model.StateCommitted,
			Preview: &model.LinkPreview{Source: source, Target: target},
		},
		Side: model.NoteSideNew,
		Line: line,
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
//     2a. a preview's diff → the PREVIEW form with the cursor line: the diff's
//     note address is a commit on the tip, but the place the user is looking
//     at is the preview, and that is the address that travels.
//  3. else the focused panel's row: focusedBookmark (files-view row,
//     Files/Staged panel row) first, then the Commits panel's commit —
//     gated on !inContentWindow() so a content window with nothing above it
//     that focusedBookmark also declines (e.g. a stash file tree) does not
//     fall through to the Commits panel focus underneath it.
//     3a. a preview's FILE TREE (no diff on top) → the preview's file form. This
//     must be tested BEFORE focusedBookmark, which would otherwise hand back
//     the tip commit's file link for the same row. When the tree declines
//     (unfocused, a heading row, or a deleted file) the row still belongs to
//     the open preview, so this returns the PAIR's own link rather than
//     falling through to a lower-precedence surface.
//     3b. the Previews panel row → the pair's own link.
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
			// A preview diff's rows are note-addressable at the tip, but the
			// PLACE is the preview. noteAnchorAtCursor already refuses the old
			// side inside a preview, so `has == false` on a deletion row is
			// exactly the "line 0, new side" the spec asks for.
			if set := m.previewNoteSet(); set != nil {
				return m.previewLinkFor(set.Source, set.Target, addr.Path, line)
			}
			return m.linkFor(addr, side, line, 0)
		}
		return "", false
	}
	// A preview's file list: the row is a file IN THE PREVIEW, not a file of
	// the tip commit — checked before focusedBookmark, which answers with the
	// tip's commit address for the very same row. When the preview is open but
	// the row itself declines (tree unfocused, a directory heading, a deleted
	// file), the pair's own link is returned rather than falling through to
	// the branches below: every row here belongs to the open preview.
	if set := m.filesPreviewSet; set != nil && m.filesView != nil {
		if b, ok := m.focusedBookmark(); ok {
			return m.previewLinkFor(set.Source, set.Target, b.Path, 0)
		}
		return m.previewLinkFor(set.Source, set.Target, "", 0)
	}
	if b, ok := m.focusedBookmark(); ok {
		return m.linkFor(b.Address(), model.NoteSideNew, 0, 0)
	}
	if !m.inContentWindow() && m.focus == panelPreviews {
		if r, ok := m.selectedPreview(); ok {
			return m.previewLinkFor(r.rec.Source, r.rec.Target, "", 0)
		}
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
