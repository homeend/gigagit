package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

// diffNavKind records which list a diff was opened from, so Home/End can step to
// the previous/next file in that list while the diff view stays open. diffNavNone
// (the zero value) means there is no source list — a bookmark/shelf compare —
// where file-stepping is inert.
type diffNavKind int

const (
	diffNavNone   diffNavKind = iota
	diffNavTree               // the files-view tree (m.filesView)
	diffNavStatus             // the Status panel (unstaged changes)
	diffNavStaged             // the Staged panel (staged changes)
)

// stepDiffFile opens the previous (dir<0) or next (dir>0) file's diff in the
// list the open diff was sourced from, leaving the diff view open. A boundary
// (no such file) is a no-op; so is a diff with no source list.
func (m Model) stepDiffFile(dir int) (tea.Model, tea.Cmd) {
	switch m.diffNav {
	case diffNavTree:
		return m.stepDiffFileTree(dir)
	case diffNavStatus:
		return m.stepDiffFileStatus(dir, false)
	case diffNavStaged:
		return m.stepDiffFileStatus(dir, true)
	}
	return m, nil
}

// nextFileRow returns the index of the first real file row (path != "") reached
// from index `from` moving in direction dir (excluding `from` itself), or -1 if
// none remains. It skips heading rows and the placeholder.
func nextFileRow(vis []contentLine, from, dir int) int {
	for i := from + dir; i >= 0 && i < len(vis); i += dir {
		if vis[i].path != "" {
			return i
		}
	}
	return -1
}

// stepDiffFileTree steps the files-view tree selection to the next/previous file
// row and reopens its diff. The tree (m.filesView) survives beneath the diff, so
// its selection is the source of truth — moving it here means esc lands on the
// last-viewed file too.
func (m Model) stepDiffFileTree(dir int) (tea.Model, tea.Cmd) {
	p := m.filesView
	if p == nil {
		return m, nil
	}
	vis := p.visible()
	next := nextFileRow(vis, p.sel, dir)
	if next < 0 {
		return m, nil // boundary: clamp
	}
	p.sel = next
	return m.openDiffForFileLine(vis[next])
}

// stepDiffFileStatus steps the Status (staged=false) or Staged (staged=true)
// panel selection to the next/previous diffable file and reopens its diff.
// Conflicted (unmerged) rows have no plain diff — enter refuses them via
// canShowFileDiff — so the step skips them too.
func (m Model) stepDiffFileStatus(dir int, staged bool) (tea.Model, tea.Cmd) {
	p := panelFiles
	if staged {
		p = panelStaged
	}
	idx := m.displayIndices(p)
	for s := m.sel[p] + dir; s >= 0 && s < len(idx); s += dir {
		f := m.status.Files[idx[s]]
		if f.Kind == model.KindUnmerged {
			continue
		}
		m.sel[p] = s
		return m.openStatusDiff(f, staged)
	}
	return m, nil // boundary (or only conflicted rows remain): clamp
}
