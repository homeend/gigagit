package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
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

// fileArmDir records that a file-step was primed: the next same-direction press
// performs it. Two gestures share this arm — End/Home at the file's bottom/top,
// and N/P on the last/first change — since both resolve to the same next/prev
// file step. Cleared by any other key (reset at the top of updateDiffViewKey),
// exactly like the n/p change-wrap arm (wrapArm). The cue shows bottom-left.
type fileArmDir int

const (
	fileArmNone fileArmDir = iota
	fileArmNext            // primed: next End/N → next file
	fileArmPrev            // primed: next Home/P → previous file
)

// fileArmCue is the bottom-left prompt shown while a file-step is primed.
func fileArmCue(d fileArmDir) string {
	switch d {
	case fileArmNext:
		return "▸ N/end again → next file"
	case fileArmPrev:
		return "▸ P/home again → previous file"
	}
	return ""
}

// onLastBlock / onFirstBlock report whether the focused change is the file's
// last / first — the boundary where N / P step to the next / previous file. A
// diff with no change blocks (no content difference) counts as both, mirroring
// how End/Home treat an empty diff as simultaneously at the top and bottom.
func (v *diffView) onLastBlock() bool {
	return len(v.dispBlocks) == 0 || v.cur == len(v.dispBlocks)-1
}

func (v *diffView) onFirstBlock() bool {
	return len(v.dispBlocks) == 0 || v.cur == 0
}

// boundaryCue is the proactive bottom-left hint shown while the focused change
// sits on a boundary: it advertises the wrap (n/p) and file-step (N/P) gestures
// available there, so the user is prompted simply by being on the last/first
// change — no priming press needed. "" when off a boundary (mid-file) or when
// no gesture applies. The wrap segment shows only with >1 change (a wrap target
// exists); the file segment only when a neighbour file exists (peekDiffFile),
// so a picker compare (diffNavNone) never advertises a file step.
func (m Model) boundaryCue() string {
	v := m.diffLayer()
	if v == nil {
		return ""
	}
	multi := len(v.dispBlocks) > 1
	var segs []string
	if v.onFirstBlock() { // previous-direction gestures
		if multi {
			segs = append(segs, "pp → bottom")
		}
		if m.peekDiffFile(-1) {
			segs = append(segs, "PP → prev file")
		}
	}
	if v.onLastBlock() { // next-direction gestures
		if multi {
			segs = append(segs, "nn → top")
		}
		if m.peekDiffFile(1) {
			segs = append(segs, "NN → next file")
		}
	}
	if len(segs) == 0 {
		return ""
	}
	return "▸ " + strings.Join(segs, " · ")
}

// peekDiffFile reports whether a previous (dir<0) / next (dir>0) diffable file
// exists in the open diff's source list — used to decide whether to prime a
// file-step (only when a neighbor exists). False when there is no source list.
func (m Model) peekDiffFile(dir int) bool {
	switch m.diffNav {
	case diffNavTree:
		if m.filesView == nil {
			return false
		}
		return nextFileRow(m.filesView.visible(), m.filesView.sel, dir) >= 0
	case diffNavStatus:
		_, _, ok := m.nextStatusFile(dir, false)
		return ok
	case diffNavStaged:
		_, _, ok := m.nextStatusFile(dir, true)
		return ok
	}
	return false
}

// stepDiffFile opens the previous (dir<0) or next (dir>0) file's diff in the
// open diff's source list, leaving the diff view open and posting a bottom-left
// arrival notice naming the file. A boundary (no such file) is a no-op; so is a
// diff with no source list.
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
	nm, cmd := m.openDiffForFileLine(vis[next])
	return withDiffArrival(nm, vis[next].path), cmd
}

// nextStatusFile finds the previous (dir<0) / next (dir>0) diffable file in the
// Status (staged=false) or Staged (staged=true) panel: its display-row index,
// the file, and ok. Conflicted (unmerged) rows have no plain diff — enter
// refuses them via canShowFileDiff — so they are skipped.
func (m Model) nextStatusFile(dir int, staged bool) (int, model.FileStatus, bool) {
	p := panelFiles
	if staged {
		p = panelStaged
	}
	idx := m.displayIndices(p)
	for s := m.sel[p] + dir; s >= 0 && s < len(idx); s += dir {
		f := m.status.Files[idx[s]]
		if f.Kind != model.KindUnmerged {
			return s, f, true
		}
	}
	return 0, model.FileStatus{}, false
}

// stepDiffFileStatus steps the Status/Staged panel selection to the next/previous
// diffable file and reopens its diff.
func (m Model) stepDiffFileStatus(dir int, staged bool) (tea.Model, tea.Cmd) {
	s, f, ok := m.nextStatusFile(dir, staged)
	if !ok {
		return m, nil // boundary (or only conflicted rows remain): clamp
	}
	p := panelFiles
	if staged {
		p = panelStaged
	}
	m.sel[p] = s
	nm, cmd := m.openStatusDiff(f, staged)
	return withDiffArrival(nm, f.Path), cmd
}

// withDiffArrival posts the bottom-left arrival notice on the model an open path
// returned (the open path cleared diffNotice, dropping any stale value first).
func withDiffArrival(tm tea.Model, path string) tea.Model {
	m := tm.(Model)
	m.diffNotice = "▸ now: " + path
	return m
}
