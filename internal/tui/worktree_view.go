package tui

import (
	"path/filepath"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gitwatch"
	"github.com/homeend/gigagit/internal/model"
)

// worktreeView is the worktree-scoped part of the Model, remembered per
// worktree of the repository on screen so a switch between them is a copy,
// not a reload. The Model's own fields are the LIVE copy — every reader and
// renderer is untouched — and switchView copies out to the leaving slot
// and in from the arriving one. Worktrees of one repository share
// branches, commits, stashes, tags and the reflog, so none of those is
// here.
type worktreeView struct {
	path string          // cleaned worktree path (the map key)
	svc  *domain.Service // rooted at path: every read and op of that tree

	status         model.WorkingTreeStatus
	conflict       domain.ConflictState
	filesIdx       []int
	filesIdxReview []int
	stagedIdx      []int
	selFiles       int
	selStaged      int
	fileMarks      map[string]bool

	workingReviews    []domain.WorkingReview
	workingReviewsGen int

	docWatch docWatchState

	watcher        *gitwatch.Watcher
	watchGen       int
	watchSupported bool

	loaded bool // its first status landed (false: the panel shows loading)
}

// isRepoWorktree reports whether path is a worktree of the repository on
// screen — the only paths a slot may be made for.
func (m Model) isRepoWorktree(path string) bool {
	dir := filepath.Clean(path)
	for _, w := range m.worktrees {
		if filepath.Clean(w.Path) == dir {
			return true
		}
	}
	return false
}

// ensureView is the slot for path, made on first use with a service rooted
// there. Home is seeded by the first load (dataLoadedMsg), so a caller
// asking for home gets the live slot back.
func (m Model) ensureView(path string) *worktreeView {
	key := filepath.Clean(path)
	if v, ok := m.views[key]; ok {
		return v
	}
	v := &worktreeView{path: key, svc: domain.OpenTUI(key)}
	m.views[key] = v // views is a map (a pointer): the value receiver writes through
	return v
}

// saveView copies the live worktree-scoped fields into the viewed slot.
func (m Model) saveView() Model {
	if m.viewed == "" {
		return m
	}
	v := m.ensureView(m.viewed)
	v.svc = m.svc
	v.status, v.conflict = m.status, m.conflict
	v.filesIdx, v.filesIdxReview, v.stagedIdx = m.filesIdx, m.filesIdxReview, m.stagedIdx
	v.selFiles, v.selStaged = m.sel[panelFiles], m.sel[panelStaged]
	v.fileMarks = m.fileMarks
	v.workingReviews, v.workingReviewsGen = m.workingReviews, m.workingReviewsGen
	v.docWatch = m.docWatch
	v.watcher, v.watchGen, v.watchSupported = m.watcher, m.watchGen, m.watchSupported
	v.loaded = m.loadedOK
	return m
}

// loadView makes slot v the live one. The open-files registry is already
// keyed per worktree (openFilesReg.byWT) and stays shared.
func (m Model) loadView(v *worktreeView) Model {
	m.viewed = v.path
	m.svc = v.svc
	m.currentWorktree = v.path
	m = m.withStatus(v.status) // recomputes the index slices and the status stack
	m.conflict = v.conflict
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	m.sel[panelFiles], m.sel[panelStaged] = v.selFiles, v.selStaged
	m.fileMarks = v.fileMarks
	m.workingReviews, m.workingReviewsGen = v.workingReviews, v.workingReviewsGen
	m.docWatch = v.docWatch
	m.watcher, m.watchGen, m.watchSupported = v.watcher, v.watchGen, v.watchSupported
	return m
}
