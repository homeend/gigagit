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
// containing '@', ':', '#' or '?' (spec §1 — the producers refuse rather than emit
// something that reparses as a different place), a remoteless checkout whose
// own PATH holds '@', '#' or '?' (model.LinkAbsOK — same rule, other half of the
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
	return m.buildLinkFor(addr, side, line, hunk, model.LinkHint{}, "")
}

// hintedLinkFor is linkFor plus a landing hint: the link a bookmark or shelf
// ROW copies, so pasting it reveals that entry (`?bookmark=<id>` /
// `?shelf=<id>`). The hint never changes what the link ADDRESSES — compare
// ignores it (spec §3.3 rule 1) — with the two shelf exceptions
// domain.EndpointForLink documents, which exist precisely so a shelved
// working-tree file, whose bytes were never in git, is still comparable.
// A hint the grammar cannot hold is refused, like every other separator.
func (m Model) hintedLinkFor(addr model.FileAddress, hint model.LinkHint) (string, bool) {
	if !model.LinkHintKindOK(hint.Kind) || !model.LinkHintIDOK(hint.ID) {
		return "", false
	}
	return m.buildLinkFor(addr, model.NoteSideNew, 0, 0, hint, "")
}

// lineLinkFor is linkFor for a LINE whose text is known: the text becomes the
// link's fingerprint when the address is uncommitted.
func (m Model) lineLinkFor(addr model.FileAddress, side model.NoteSide, line int, text string) (string, bool) {
	return m.buildLinkFor(addr, side, line, 0, model.LinkHint{}, text)
}

// buildLinkFor is the one builder behind linkFor, lineLinkFor and
// hintedLinkFor. text is the RAW text of the line the link names ("" = not
// known): an uncommitted line link carries its fingerprint.
func (m Model) buildLinkFor(addr model.FileAddress, side model.NoteSide, line, hunk int, hint model.LinkHint, text string) (string, bool) {
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
	l.Hint = hint
	if l.Side == "" {
		l.Side = model.NoteSideNew
	}
	if l.Path == "" && (line > 0 || hunk > 0) {
		return "", false
	}
	// An uncommitted line carries its fingerprint, so the link can be re-found
	// (or reported changed) once the file moves on. A commit is fixed already.
	if line > 0 && l.Target.State != model.StateCommitted {
		l.Fingerprint = model.LineFingerprint(text)
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

// withPreviewHint decorates a SAVED Previews row's link with its landing hint,
// ?preview=<id>: the same address, plus "reveal this row". Only the row's own
// copy wears it — a file or line copied from inside an open preview lands on
// the line, and the store keeps the bare text. An id the grammar cannot carry
// degrades to the bare link rather than refusing the copy.
func withPreviewHint(id string) func(string, bool) (string, bool) {
	return func(text string, ok bool) (string, bool) {
		if !ok || !model.LinkHintIDOK(id) {
			return text, ok
		}
		l, err := model.ParseLink(text)
		if err != nil {
			return text, ok
		}
		l.Hint = model.LinkHint{Kind: "preview", ID: id}
		return l.String(), true
	}
}

// refLinkFor builds the gg:// address of a branch or tag TIP: `@ref:<name>`.
// The NAME rides the link and the consumer re-resolves it (plan 1b ruling R2)
// — a ref link follows its branch, which is the whole point of copying one
// instead of the commit underneath it. A point, so UNBOUNDED: comparing two
// of them compares the two tips. It refuses a name the grammar cannot hold.
func (m Model) refLinkFor(name string) (string, bool) {
	if !model.LinkRefOK(name) {
		return "", false
	}
	repo, ok := m.linkRepoFor("")
	if !ok {
		return "", false
	}
	l := model.Link{
		Repo:   repo,
		Target: model.LinkTarget{State: model.StateCommitted, Ref: name},
		Side:   model.NoteSideNew,
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
//     feature). A two-sided compare has no note address, and is addressed by
//     its loader's stamp instead (compareLinkText): two commits are the pair
//     `@<a>..<b>`, never the newer side's own commit link, which describes
//     parent→b, not a→b. A view no loader stamped refuses outright rather
//     than falling through to a lower-precedence surface underneath it. The
//     web twin is internal/web/static/links.js, linkFor.
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
//     3a'. a two-commit compare's FILE TREE → the pair's file form, for the
//     same reason: the row is a file in the comparison.
//     3b. the Previews panel row → the pair's own link.
//     3c. a Branches / Remotes / Tags row → the ref's own link (`@ref:<name>`),
//     gated on !inContentWindow() exactly like the Commits arm below: a files
//     view opened over one of those panels is not "the branch".
func (m Model) contextLinkText() (string, bool) {
	switch m.topLayer().(type) {
	case *historyView, *blameView:
		if b, ok := m.focusedBookmark(); ok {
			return m.linkFor(b.Address(), model.NoteSideNew, 0, 0)
		}
		return "", false
	}
	if _, ok := m.topLayer().(*diffView); ok {
		// A live selection: the link names its lines, on the cursor's side.
		// The address is read where the SELECTION is — a frozen one stays in
		// its file while the cursor walks a stack — so the cursor is lent to
		// its first line for the length of this call.
		sel, selOn := m.diffLinkSelection()
		if selOn {
			if sel.refusal != "" {
				return "", false
			}
			v := m.diffLayer()
			defer func(cur int) { v.curLine = cur }(v.curLine)
			v.curLine = sel.row
		}
		side, line, text, has := m.linkAnchorAtCursor()
		if !has {
			side, line, text = model.NoteSideNew, 0, ""
		}
		if selOn {
			side, line, text = sel.side, sel.first, sel.block[0]
		}
		ranged := func(link string, ok bool) (string, bool) {
			if !ok || !selOn {
				return link, ok
			}
			return rangeLink(link, sel.last, sel.block)
		}
		if addr, ok := m.diffNoteAddress(); ok {
			// A scope's diff rows are note-addressable at the tip, but the
			// PLACE is the preview or the pair. linkAnchorAtCursor already
			// refuses the old side inside a merge preview, so `has == false`
			// on a deletion row there is exactly the "line 0, new side" the
			// spec asks for; a commit pair's old side is commit a and travels.
			if set := m.previewNoteSet(); set != nil {
				return ranged(m.scopeLinkFor(set, addr.Path, side, line))
			}
			return ranged(m.lineLinkFor(addr, side, line, text))
		}
		return ranged(m.compareLinkText(side, line, text))
	}
	// A preview's file list: the row is a file IN THE PREVIEW, not a file of
	// the tip commit — checked before focusedBookmark, which answers with the
	// tip's commit address for the very same row. When the preview is open but
	// the row itself declines (tree unfocused, a directory heading, a deleted
	// file), the pair's own link is returned rather than falling through to
	// the branches below: every row here belongs to the open preview.
	if set := m.filesPreviewSet; set != nil && m.filesView != nil {
		if b, ok := m.focusedBookmark(); ok {
			return m.scopeLinkFor(set, b.Path, model.NoteSideNew, 0)
		}
		return m.scopeLinkFor(set, "", model.NoteSideNew, 0)
	}
	// A two-commit compare's file list: the row is a file IN the comparison,
	// so it is the pair's file form — focusedBookmark would answer with the
	// newer commit's own link, which describes parent→b, not a→b.
	if v := m.filesView; v != nil && m.filesTreeFocused && m.inCompareMode() {
		if vis := v.visible(); v.sel >= 0 && v.sel < len(vis) && vis[v.sel].path != "" {
			left, right := m.compareSides(vis[v.sel])
			if left.Kind() == model.EndpointCommit && right.Kind() == model.EndpointCommit {
				return m.pairFileLinkFor(left.Hash(), right.Hash(), vis[v.sel].path, model.NoteSideNew, 0)
			}
		}
	}
	if b, ok := m.focusedBookmark(); ok {
		return m.linkFor(b.Address(), model.NoteSideNew, 0, 0)
	}
	if !m.inContentWindow() && m.focus == panelPreviews {
		if r, ok := m.selectedPreview(); ok {
			// A comparison is TWO links; "the link for this place" has no
			// answer (previewRowLink refuses). Its halves are offered by name
			// (comparisonLinkRows).
			return withPreviewHint(r.id())(m.previewRowLink(r))
		}
	}
	if !m.inContentWindow() {
		switch m.focus {
		case panelBranches:
			if bi, ok := m.backingIndex(panelBranches); ok && bi < len(m.branches) {
				return m.refLinkFor(m.branches[bi].Name)
			}
		case panelRemotes:
			if bi, ok := m.backingIndex(panelRemotes); ok && bi < len(m.remoteBranches) {
				return m.refLinkFor(m.remoteBranches[bi].Name)
			}
		case panelTags:
			if bi, ok := m.backingIndex(panelTags); ok && bi >= 0 && bi < len(m.tags) {
				return m.refLinkFor(m.tags[bi].Name)
			}
		}
	}
	if !m.inContentWindow() && m.focus == panelCommits {
		if bi, ok := m.backingIndex(panelCommits); ok && bi < len(m.commits) {
			return m.linkFor(model.FileAddress{State: model.StateCommitted, Commit: m.commits[bi].Hash}, "", 0, 0)
		}
	}
	return "", false
}

// linkAnchorAtCursor is the side and line a link to the cursor row carries:
// the cursor side's line, or the other side's when the cursor side is a gap —
// noteAnchorsAtCursor's order — with that line's raw text, which an
// uncommitted link fingerprints. It differs from the note anchor in one place: a
// COMMIT PAIR's old side is commit a, which a link can name (`:old:<line>`)
// although no note can hang off it. A merge preview's old side is the merge
// base, which nothing names.
func (m Model) linkAnchorAtCursor() (model.NoteSide, int, string, bool) {
	v := m.diffLayer()
	if v == nil {
		return "", 0, "", false
	}
	r, ok := v.cursorRow()
	if !ok {
		return "", 0, "", false
	}
	set := m.previewNoteSet()
	oldOK := r.LeftNo > 0 && (set == nil || set.IsPair())
	switch {
	case v.onOld && oldOK:
		return model.NoteSideOld, r.LeftNo, r.Left, true
	case r.RightNo > 0:
		return model.NoteSideNew, r.RightNo, r.Right, true
	case oldOK:
		return model.NoteSideOld, r.LeftNo, r.Left, true
	}
	return "", 0, "", false
}

// compareLinkText is the link for the cursor line of a two-sided compare, read
// off the loader's stamp (diffView.cmp). Two commits are the pair `@<a>..<b>`
// with the line on either side. Any other compare has no pair form, so the
// line is addressed as the VERSION it sits in: the working tree, the index, or
// a commit (that commit's own text, hence the new side of its link). A side no
// link names — a shelf entry, a link member with its own source — refuses.
func (m Model) compareLinkText(side model.NoteSide, line int, text string) (string, bool) {
	v := m.diffLayer().curNoteView()
	if v == nil || v.cmp == nil {
		return "", false
	}
	c := v.cmp
	if c.left.Kind() == model.EndpointCommit && c.right.Kind() == model.EndpointCommit {
		return m.pairFileLinkFor(c.left.Hash(), c.right.Hash(), c.path, side, line)
	}
	ep, path := c.right, c.path
	if side == model.NoteSideOld {
		ep = c.left
		if c.oldPath != "" {
			path = c.oldPath
		}
	}
	addr := model.FileAddress{Worktree: m.currentWorktree, Path: path}
	switch ep.Kind() {
	case model.EndpointWorkTree:
		addr.State = model.StateUnstaged
	case model.EndpointIndex:
		addr.State = model.StateStaged
	case model.EndpointCommit:
		addr.State, addr.Commit = model.StateCommitted, ep.Hash()
	default:
		return "", false
	}
	return m.lineLinkFor(addr, model.NoteSideNew, line, text)
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
	label := i18n.T("Copy link")
	if n := m.linkSelectionLines(); n > 1 {
		label = i18n.T("Copy link to selected lines (%d)", n)
	}
	return m.copyRow("copy-link", label, i18n.T("Copied link: %s", text), text), true
}

// linkSel is a diff selection as a link names it: lines first..last on one
// side of ONE file, with their raw text (the block an uncommitted range link
// fingerprints). refusal is the translated reason there is no such link.
type linkSel struct {
	side        model.NoteSide
	first, last int
	row         int // the view line carrying `first`: where the link's address is read
	block       []string
	refusal     string
	crossFile   bool // the refusal: the marks reach into another file of a stack
}

// diffLinkSelection reads the live selection of the diff on top (on == false:
// none). The side is the cursor's; the range runs from the first to the last
// selected row that HAS a number on that side — gap rows at the edges are
// trimmed, and the text comes from the file's full aligned rows, so lines a
// fold hides inside the range are part of the block. A stack's selection that
// reaches into another file names no one file and is refused.
func (m Model) diffLinkSelection() (sel linkSel, on bool) {
	v := m.diffLayer()
	if v == nil || !v.lsel.on {
		return linkSel{}, false
	}
	lo, hi, _ := v.lsel.bounds(v.curLine)
	lo, hi = max(lo, 0), min(hi, len(v.lines)-1)
	old := v.onOld
	sel.side = model.NoteSideNew
	if old {
		sel.side = model.NoteSideOld
		if set := m.previewNoteSet(); set != nil && !set.IsPair() {
			// A merge preview's old side is the merge base, which nothing names.
			sel.refusal = i18n.T("▸ nothing to link on this side")
			return sel, true
		}
	}
	file := v.lines[lo].file
	for i := lo; i <= hi; i++ {
		ln := v.lines[i]
		if v.stk != nil && ln.file != file {
			sel.refusal, sel.crossFile = i18n.T("▸ a link marks lines of one file"), true
			return sel, true
		}
		if !ln.isBody() || !sidePresent(ln.Row, old) {
			continue
		}
		no := ln.Row.RightNo
		if old {
			no = ln.Row.LeftNo
		}
		if no <= 0 {
			continue
		}
		if sel.first == 0 {
			sel.first, sel.row = no, i
		}
		sel.last = no
	}
	fv := v
	if v.stk != nil {
		fv = v.stk.files[file].d
	}
	if sel.first == 0 || fv == nil {
		sel.refusal = i18n.T("▸ nothing to link on this side")
		return sel, true
	}
	for _, r := range fv.full {
		if !sidePresent(r, old) {
			continue
		}
		no, text := r.RightNo, r.Right
		if old {
			no, text = r.LeftNo, r.Left
		}
		if no >= sel.first && no <= sel.last {
			sel.block = append(sel.block, text)
		}
	}
	if len(sel.block) != sel.last-sel.first+1 {
		sel.refusal = i18n.T("▸ nothing to link on this side")
	}
	return sel, true
}

// linkSelectionLines is how many lines the link under L / Copy link names
// when a selection is live (0 = no selection, or one with no link).
func (m Model) linkSelectionLines() int {
	if sel, on := m.diffLinkSelection(); on && sel.refusal == "" {
		return sel.last - sel.first + 1
	}
	return 0
}

// rangeLink turns the link to a range's FIRST line into the link to the whole
// range first..last. block is the raw text of those lines: an uncommitted
// range carries its fingerprint (model.BlockFingerprint), which the resolver
// checks — a block that changed makes the link stale. A one-line range stays
// the single-line link it already is.
func rangeLink(link string, last int, block []string) (string, bool) {
	l, err := model.ParseLink(link)
	if err != nil || l.Line <= 0 {
		return "", false
	}
	if last <= l.Line {
		return link, true
	}
	l.End, l.Fingerprint = last, ""
	if l.Target.State != model.StateCommitted {
		l.Fingerprint = model.BlockFingerprint(block)
	}
	return l.String(), true
}
