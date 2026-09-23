package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// Opening, leaving and driving a stack. The stack is always built from the
// LIST the diff was opened from (m.diffNav), so the two are never out of step:
// the files-view tree, or one working-tree section.

// buildTreeStack is the files view's rows as stack files, in list order.
// Full-tree mode is excluded: it is every file of a commit, not a change set.
func (m Model) buildTreeStack() []stackFile {
	if m.filesView == nil || m.inFullTree() {
		return nil
	}
	var out []stackFile
	for _, l := range m.filesView.visible() {
		if l.path == "" { // a heading row or the placeholder
			continue
		}
		out = append(out, stackFile{path: l.path, oldPath: l.oldPath, status: l.status, line: l})
	}
	return out
}

// buildStatusStack is ONE working-tree section as stack files (design §9): the
// Files panel (unstaged) or the Staged panel, never both — they are two
// different comparisons of the same paths. A conflicted row joins as a
// header-only entry: a conflict has no plain diff, it has a resolver.
func (m Model) buildStatusStack(staged bool) []stackFile {
	p := panelFiles
	if staged {
		p = panelStaged
	}
	var out []stackFile
	for _, bi := range m.displayIndices(p) {
		f := m.status.Files[bi]
		letter := f.Unstaged
		if staged {
			letter = f.Staged
		}
		out = append(out, stackFile{
			path:     f.Path,
			oldPath:  f.OrigPath,
			status:   string(rune(letter)),
			fs:       f,
			conflict: f.Kind == model.KindUnmerged,
		})
	}
	return out
}

// stackSourceOf is the stack source for a nav kind, and whether it stacks.
func stackSourceOf(nav diffNavKind) (staged, ok bool) {
	switch nav {
	case diffNavTree:
		return false, true
	case diffNavStatus:
		return false, true
	case diffNavStaged:
		return true, true
	}
	return false, false
}

// openStack opens (or converts the open diff into) a stack over the list the
// diff comes from, scrolled to focusPath. seed, when non-nil and loaded, is
// the single-file view of focusPath: its rows are reused, so flipping with S
// costs no re-read and keeps the line you were on.
func (m Model) openStack(nav diffNavKind, focusPath string, seed *diffView) (tea.Model, tea.Cmd) {
	staged, ok := stackSourceOf(nav)
	if !ok {
		m.diffNotice = i18n.T("▸ nothing to stack here")
		return m, nil
	}
	var files []stackFile
	if nav == diffNavTree {
		files = m.buildTreeStack()
	} else {
		files = m.buildStatusStack(staged)
	}
	if len(files) == 0 {
		m.diffNotice = i18n.T("▸ nothing to stack here")
		return m, nil
	}
	focus := 0
	for i := range files {
		if files[i].path == focusPath {
			focus = i
			break
		}
	}
	// Past the threshold the stack opens folded (design R2) — except the file
	// the user actually asked for, which is what they came to read.
	if len(files) > stackCollapseOver {
		for i := range files {
			files[i].collapsed = i != focus
		}
	}
	m.stackSeq++
	stk := &diffStack{gen: m.stackSeq, src: nav, staged: staged, files: files}

	v := &diffView{stk: stk, partial: m.diffPartial, long: m.diffLong}
	v.width, _ = m.overlayDims()
	if nav == diffNavTree {
		_, _, ctx := m.treeFileLoad(files[focus].line)
		v.context, v.rev = ctx, m.filesHash
	} else {
		v.context = statusDiffContext(staged)
	}
	// Flipping from the single view: the file on screen is already read, so it
	// joins the stack loaded and the cursor keeps its line inside it.
	seeded := -1
	if seed != nil && seed.stk == nil && !seed.loading && seed.err == nil && len(seed.full) > 0 {
		// A COPY: seed is usually the live layer, and the stack is about to be
		// written into that same object — keeping the pointer would leave the
		// file holding the stack itself, with no rows at all.
		kept := *seed
		files[focus].d, files[focus].load = &kept, stackLoaded
		files[focus].add, files[focus].del = countRows(seed.full)
		files[focus].counted = true
		seeded = seed.curLine
	}
	v.rebuild()

	m.diffNav = nav
	m.diffTag = "" // any single-file load still in flight is no longer wanted
	m.diffNotice = ""
	if dv := m.diffLayer(); dv != nil {
		*dv = *v
	} else {
		m = m.pushLayer(v)
	}
	dv := m.diffLayer()
	body := m.diffBodyRows()
	dv.cur = focus
	if seeded >= 0 {
		dv.setCursorLine(dv.stk.files[focus].hdr+2+seeded, body) // past the header and its rule
		dv.alignCursor(alignCenter, body)
	} else {
		dv.goToStackFile(focus, body) // the file's header, with the usual lead above it
	}
	dv.syncStackTitle()
	mm, load := m.pumpStack()
	return mm, tea.Batch(load, mm.stackStatCmd(dv.stk))
}

// setStackedPref flips the session's stacked preference and persists it. A
// store that refuses only costs the memory, never the flip.
func (m Model) setStackedPref(on bool) Model {
	m.diffStacked = on
	if m.promptStore != nil {
		if err := m.promptStore.SetStackedDiff(on); err != nil {
			m.statusMsg = i18n.T("could not remember the stacked-diff choice: %s", err.Error())
		}
	}
	return m
}

// toggleStacked is the S key: single ⇄ stacked, keeping the file you are on.
func (m Model) toggleStacked() (tea.Model, tea.Cmd) {
	v := m.diffLayer()
	if v == nil {
		return m, nil
	}
	if v.stk != nil {
		return m.unstack()
	}
	if _, ok := stackSourceOf(m.diffNav); !ok || m.inFullTree() {
		// A picker compare (or the full tree) has no change-set list behind
		// it: there is nothing to stack, and saying so beats a silent key.
		m.diffNotice = i18n.T("▸ nothing to stack here")
		return m, nil
	}
	m = m.setStackedPref(true)
	return m.openStack(m.diffNav, v.title, v)
}

// unstack leaves the stack for the single-file view of the file under the
// cursor, pointing the source list at it so esc lands there too.
func (m Model) unstack() (tea.Model, tea.Cmd) {
	v := m.diffLayer()
	f := v.stk.files[v.curFile()]
	m = m.setStackedPref(false)
	// The single view is loaded asynchronously and opens on its first change
	// block, so the LINE being read is parked and landed when it arrives —
	// otherwise S drops the reader at the top of the file they were in.
	land := v.cursorLineLanding()
	if v.stk.src == diffNavTree {
		if p := m.filesView; p != nil {
			for i, l := range p.visible() {
				if l.path == f.path {
					p.sel = i
					break
				}
			}
		}
		tm, cmd := m.openDiffForFileLine(f.line)
		return m.withLineLanding(land, tm, cmd)
	}
	p := panelFiles
	if v.stk.staged {
		p = panelStaged
	}
	for s, bi := range m.displayIndices(p) {
		if m.status.Files[bi].Path == f.path {
			m.sel[p] = s
			break
		}
	}
	if f.conflict {
		// A conflicted file has no plain diff to return to: close the view and
		// leave the panel selection on it, where enter opens the resolver.
		m = m.popLayer()
		m.diffTag = ""
		return m, nil
	}
	tm, cmd := m.openStatusDiff(f.fs, v.stk.staged)
	return m.withLineLanding(land, tm, cmd)
}

// foldFile folds or unfolds one file and keeps the cursor on its header, so a
// fold never scrolls the reader somewhere else.
func (m Model) foldFile(v *diffView, i int, body int) Model {
	if i < 0 || i >= len(v.stk.files) {
		return m
	}
	v.stk.files[i].collapsed = !v.stk.files[i].collapsed
	v.rebuild()
	v.setCursorLine(v.stk.files[i].hdr, body)
	v.syncStackTitle()
	return m
}

// allCollapsed reports whether every file of the stack is folded — what `_`
// dispatches on (all folded → unfold everything, else fold everything).
func (v *diffView) allCollapsed() bool {
	for i := range v.stk.files {
		if !v.stk.files[i].collapsed {
			return false
		}
	}
	return len(v.stk.files) > 0
}

// stackJumpMenu is the J key: the stack's files as a type-to-filter menu, each
// row scrolling to (and unfolding) its file.
func (m Model) stackJumpMenu(v *diffView) Model {
	rows := make([]actionRow, 0, len(v.stk.files))
	for i := range v.stk.files {
		f := v.stk.files[i]
		idx := i
		label := f.status + "  " + f.path
		rows = append(rows, actionRow{
			id:    "stack-file",
			label: label,
			run: func(m Model) (tea.Model, tea.Cmd) {
				dv := m.diffLayer()
				if dv == nil || dv.stk == nil || idx >= len(dv.stk.files) {
					return m, nil
				}
				body := m.diffBodyRows()
				dv.goToStackFile(idx, body)
				dv.syncStackTitle()
				return m.pumpStack()
			},
		})
	}
	m.actionMenu = &actionMenu{rows: rows}
	return m
}

// stackKey gives the stacked view first refusal on a key: the keys that only
// exist in a stack, and the ones whose single-file meaning does not apply
// there. handled == true means the caller must return immediately.
func (m Model) stackKey(v *diffView, msg tea.KeyMsg, body int) (tea.Model, tea.Cmd, bool) {
	if v.stk == nil {
		if msg.String() == "S" {
			nm, cmd := m.toggleStacked()
			return nm, cmd, true
		}
		return m, nil, false
	}
	switch msg.String() {
	case "S":
		nm, cmd := m.toggleStacked()
		return nm, cmd, true
	case "-":
		m = m.foldFile(v, v.curFile(), body)
		return m, nil, true
	case "_":
		fold := !v.allCollapsed()
		cur := v.curFile()
		for i := range v.stk.files {
			v.stk.files[i].collapsed = fold
		}
		v.rebuild()
		v.setCursorLine(v.stk.files[cur].hdr, body)
		v.syncStackTitle()
		nm, cmd := m.pumpStack()
		return nm, cmd, true
	case "n", "p":
		// The change walk spans the stack; a change in a folded or never-read
		// file is reached by opening that file and parking the step
		// (diff_stack_nav.go). Everything else is the single-file stepping.
		dir := 1
		if msg.String() == "p" {
			dir = -1
		}
		if nm, cmd, ok := m.stackChangeStep(v, dir, body); ok {
			return nm, cmd, true
		}
		return m, nil, false
	case "J":
		return m.stackJumpMenu(v), nil, true
	case "enter":
		// A conflicted file's rows are not a diff: enter hands it to the
		// region picker, the same pipeline the Files panel's enter runs.
		f := v.stk.files[v.curFile()]
		if !f.conflict {
			return m, nil, true
		}
		if reason := conflictPickable(f.fs); reason != "" {
			m.statusMsg = reason
			return m, nil, true
		}
		return m, m.loadConflictFileCmd(f.path), true
	case "home":
		// The whole stack's top / bottom. No file-step arming: every file of
		// the list is already here, so there is nowhere to step to.
		v.setCursorLine(0, body)
		v.scrollBy(-len(v.disp), body)
		v.syncStackTitle()
		nm, cmd := m.pumpStack()
		return nm, cmd, true
	case "end":
		v.setCursorLine(len(v.lines)-1, body)
		v.scrollBy(len(v.disp), body)
		v.syncStackTitle()
		nm, cmd := m.pumpStack()
		return nm, cmd, true
	case "N", "P":
		// The file step: n/p walk changes across the whole stack, so stepping a
		// WHOLE file lives on N/P — which is what they already mean in the
		// single-file view ("go to the next file of the list"). One press here,
		// though: there is no last-change boundary to arm against.
		dir := 1
		if msg.String() == "P" {
			dir = -1
		}
		cur := v.curFile()
		next := cur + dir
		if next < 0 || next >= len(v.stk.files) {
			if dir > 0 {
				m.diffNotice = i18n.T("▸ no next file")
			} else {
				m.diffNotice = i18n.T("▸ no previous file")
			}
			return m, nil, true
		}
		v.goToStackFile(next, body)
		v.syncStackTitle()
		nm, cmd := m.pumpStack()
		return nm, cmd, true
	case "ctrl+down", "ctrl+up":
		// n/p step FILES in a stack (v.blocks are the headers), so the walk
		// from change to change inside ONE file lives on the ctrl-arrows —
		// their single-file meaning, one scope narrower. It stops at the
		// file's ends: stepping on would be n's job.
		dir := 1
		if msg.String() == "ctrl+up" {
			dir = -1
		}
		if li, ok := v.changeInFile(v.curLine, dir); ok {
			v.setCursorLine(li, body)
			v.syncStackTitle()
		} else {
			m.diffNotice = i18n.T("▸ no more changes in this file — n/p step files")
		}
		nm, cmd := m.pumpStack()
		return nm, cmd, true
	}
	return m, nil, false
}

// reconcileStatusStack keeps a working-tree stack in step with the status it
// shows (design §9). It runs on every status write, so it must be cheap and
// must not move the reader:
//
//   - files gone from the section drop out, new ones are inserted in list order;
//   - a file already read is marked STALE rather than emptied — its old diff
//     stays on screen, and the lazy queue re-reads it when the reader is near
//     it, so a background refresh never blanks or flickers the view;
//   - the cursor keeps its file (by PATH — indexes shift) and its line in it;
//   - an emptied section closes the view, back to the list it came from.
//
// Loads in flight are dropped (the new generation ignores their answers) and
// their files simply become idle again, so nothing is left half-arrived. The
// re-reads themselves start at the next pump — every key press runs one.
func (m Model) reconcileStatusStack() Model {
	v := m.diffLayer()
	if v == nil || v.stk == nil || v.stk.src == diffNavTree {
		return m
	}
	fresh := m.buildStatusStack(v.stk.staged)
	if len(fresh) == 0 {
		m = m.popLayer()
		m.diffTag = ""
		return m
	}
	prev := make(map[string]stackFile, len(v.stk.files))
	for _, f := range v.stk.files {
		prev[f.path] = f
	}
	for i := range fresh {
		o, ok := prev[fresh[i].path]
		if !ok {
			continue
		}
		fresh[i].collapsed = o.collapsed
		fresh[i].d = o.d
		fresh[i].add, fresh[i].del, fresh[i].counted, fresh[i].bin = o.add, o.del, o.counted, o.bin
		if o.d != nil {
			fresh[i].load = stackStale // re-read it when the reader comes near
			// Its counts describe the OLD content; the re-read's rows refill
			// them (a working-tree numstat is not re-asked per refresh).
			fresh[i].counted = false
		}
	}
	// Where the cursor was, by path — the file's index may have moved.
	curPath := v.stk.files[v.curFile()].path
	cur := v.anchorAt(v.curLine)
	m.stackSeq++
	v.stk.gen, v.stk.files, v.stk.inflight = m.stackSeq, fresh, 0
	v.rebuild()
	cur.file = 0
	for i := range fresh {
		if fresh[i].path == curPath {
			cur.file = i
			break
		}
	}
	body := m.diffBodyRows()
	v.setCursorLine(v.lineAt(cur), body)
	v.syncStackTitle()
	return m
}

// stackMenuRows are the stacked view's `.` menu rows: the toggle wherever a
// diff with a list behind it is open, and the fold / jump rows inside a stack.
// They replay their KEY through Update, like every other menu row, so the menu
// and the key can never do different things.
func (m Model) stackMenuRows() []actionRow {
	v := m.diffLayer()
	if v == nil {
		return nil
	}
	if v.stk == nil {
		if _, ok := stackSourceOf(m.diffNav); !ok || m.inFullTree() {
			return nil
		}
		return []actionRow{{id: "stack-toggle", key: "S", label: i18n.T("Stacked diff: show every file in one scroll")}}
	}
	return []actionRow{
		{id: "stack-toggle", key: "S", label: i18n.T("Stacked diff: back to one file at a time")},
		{id: "stack-fold", key: "-", label: i18n.T("Fold / unfold this file")},
		{id: "stack-fold-all", key: "_", label: i18n.T("Fold / unfold every file")},
		{id: "stack-jump", key: "J", label: i18n.T("Jump to a file of the stack…")},
	}
}
