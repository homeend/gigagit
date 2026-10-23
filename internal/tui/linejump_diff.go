package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// newerSide is the version the diff's NEW side shows, when the file viewer
// can open it: the working tree or a commit (spec 2026-10-23-go-to-line). It
// reads the identity the diff already carries, most specific first — a
// compare stamp's right endpoint, then the note address's state, then a bare
// rev — and answers false for anything else (the index, a shelf, a bookmark,
// a two-sided compare), where a hidden line is unfolded in place instead.
// Stacked, the cursor's file answers.
func (v *diffView) newerSide() (fileSource, bool) {
	if v.stk != nil {
		fi := v.curFile()
		if fi < 0 || fi >= len(v.stk.files) || v.stk.files[fi].d == nil {
			return fileSource{}, false
		}
		return v.stk.files[fi].d.newerSide()
	}
	if v.cmp != nil {
		switch r := v.cmp.right; r.Kind() {
		case model.EndpointWorkTree:
			return fileSource{kind: srcWorktree}, true
		case model.EndpointCommit:
			return fileSource{kind: srcCommit, rev: r.Hash()}, true
		}
		return fileSource{}, false
	}
	if v.noteAddr.Path != "" {
		switch v.noteAddr.State {
		case model.StateUnstaged, model.StateUntracked:
			return fileSource{kind: srcWorktree}, true
		case model.StateCommitted:
			if v.noteAddr.Commit != "" {
				return fileSource{kind: srcCommit, rev: v.noteAddr.Commit}, true
			}
		}
		return fileSource{}, false
	}
	if v.rev != "" && !v.compare {
		return fileSource{kind: srcCommit, rev: v.rev}, true
	}
	return fileSource{}, false
}

// jumpPath is the path of the file a jump lands in: the stack's file fi, the
// compare stamp's, else the title.
func (v *diffView) jumpPath(fi int) string {
	switch {
	case v.stk != nil && fi >= 0 && fi < len(v.stk.files):
		return v.stk.files[fi].path
	case v.cmp != nil:
		return v.cmp.path
	case v.noteAddr.Path != "":
		return v.noteAddr.Path
	}
	return v.title
}

// diffJumpKey gives the go-to-line prompt first refusal in the diff view
// (single and stacked). : opens it only over drawable text rows.
func (m Model) diffJumpKey(v *diffView, msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if v.jump.typing {
		if ev, n := v.jump.key(msg); ev == jumpGo {
			nm, cmd := m.jumpDiff(v, n)
			return nm, cmd, true
		}
		return m, nil, true
	}
	if !jumpOpenKey(msg) || v.loading || v.err != nil || v.binary || v.tooLarge || v.hasImages() ||
		len(v.lines) == 0 || v.cs.on || v.search.typing {
		return m, nil, false
	}
	v.jump.open()
	return m, nil, true
}

// jumpDiff lands line n of the diff's file (stacked: the cursor's file) —
// landSteer's recipe with the spec's one rule: a line the diff shows is
// landed in place; one it hides opens the newer version in the viewer when
// newerSide can name it, else the folds open. Line numbers are the new
// side's, the old side's for a file with no new side (a deletion).
func (m Model) jumpDiff(v *diffView, n int) (Model, tea.Cmd) {
	body := m.diffBodyRows()
	fi := -1
	if v.stk != nil {
		fi = v.curFile()
		if fi < 0 || fi >= len(v.stk.files) {
			return m, nil
		}
		f := &v.stk.files[fi]
		if f.collapsed {
			f.collapsed = false
			v.rebuild()
		}
		if f.load != stackLoaded && f.load != stackStale {
			// Its rows are not here yet: park the landing and fetch them, the
			// cursor on the file's header until they arrive (landSteer's park).
			v.setCursorLine(f.hdr, body)
			v.syncStackTitle()
			v.stk.land = &stackLanding{file: fi, side: model.NoteSideNew, no: n}
			return m.pumpStack()
		}
	}
	bounds := func() (int, int) {
		if fi >= 0 {
			return v.fileLineRange(fi)
		}
		return 0, len(v.lines) - 1
	}
	lo, hi := bounds()
	old := v.lastLineNoIn(lo, hi, false) == 0
	find := func() (int, bool) {
		lo, hi := bounds()
		return v.lineAnchorIn(lo, hi, n, old)
	}
	li, shown := find()
	if !shown {
		if src, ok := v.newerSide(); ok && !old {
			path := v.jumpPath(fi)
			if src.kind == srcWorktree {
				return m.openFileViewer(path, n)
			}
			return m.openFileAtCommitLine(src.rev, path, n, 0)
		}
		if li >= 0 { // the fold hiding it opens
			var ok bool
			if m, li, ok = m.expandFoldFor(v, li, find); !ok {
				li = -1
			}
		}
		if li < 0 { // past the end: clamp to the file's last line, folds open
			if v.partial {
				m = m.expandAllFolds(v)
			}
			lo, hi := bounds()
			last := v.lastLineNoIn(lo, hi, old)
			if last == 0 || n <= last {
				return m, nil
			}
			m.diffNotice = i18n.T("line %d is past the end of %s (%d lines)", n, v.jumpPath(fi), last)
			n = last
			if li, _ = find(); li < 0 {
				return m, nil
			}
		}
	}
	v.setCursorLine(li, body)
	v.alignCursor(alignCenter, body)
	v.landOnSide(old)
	if v.stk != nil {
		v.syncStackTitle()
	}
	return m, nil
}
