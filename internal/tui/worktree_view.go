package tui

import (
	"context"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gitwatch"
	"github.com/homeend/gigagit/internal/i18n"
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

// seedHome makes path gg's own worktree and the viewed one, with the live
// service as its slot — the first load (both startup paths) and the first
// load after a repo switch, which dropped the slots.
func (m Model) seedHome(path string) Model {
	if m.views == nil {
		m.views = map[string]*worktreeView{}
	}
	if m.home != "" && m.views[m.viewed] != nil {
		return m
	}
	m.home = filepath.Clean(path)
	m.viewed = m.home
	m.views[m.home] = &worktreeView{path: m.home, svc: m.svc}
	return m
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

// switchView makes path's slot the live one. Same repository only: a path
// that is not one of m.worktrees, one unreachable under this environment's
// notation, or a swap asked while an operation runs is refused with a
// status message. The leaving slot sleeps (its watchers close; a read in
// flight carries the old srcStatus generation and is dropped on arrival).
// The arriving slot is on screen at once — its remembered status, or the
// loading marker until its first read — and viewKick makes the Update tail
// launch its refresh, watcher and docs sync (viewKickCmd).
func (m Model) switchView(path string) (Model, bool) {
	key := filepath.Clean(path)
	if key == m.viewed {
		return m, true
	}
	if !m.isRepoWorktree(key) {
		m.statusMsg = i18n.T("%s is not a worktree of this repository", path)
		return m, false
	}
	if verdict, _ := checkSwitchTarget(guardStat, guardGOOS, key); verdict != switchOK {
		m.statusMsg = i18n.T("cannot switch: %s is not reachable from here", path)
		return m, false
	}
	if !m.opsIdle() {
		m.statusMsg = i18n.T("an operation is running — switch once it has finished")
		return m, false
	}
	m = m.saveView()
	// Put the leaving slot to sleep: close its watchers, keep its state.
	if m.watcher != nil {
		_ = m.watcher.Close()
	}
	closeDocWatch(m.docWatch.w)
	if v := m.views[m.viewed]; v != nil {
		v.watcher, v.watchSupported = nil, false
		v.docWatch = docWatchState{gen: v.docWatch.gen + 1}
	}
	m = m.loadView(m.ensureView(key))
	m.watcher, m.watchSupported = nil, false
	m.watchGen++
	m.docWatch = docWatchState{gen: m.docWatch.gen + 1}
	m.srcGen[srcStatus]++ // a status read launched for the old slot cannot land here
	m.srcInflight[srcStatus] = false
	m.srcLoading[srcStatus] = false
	m.workingReviewsGen++ // likewise a reviews read
	m.viewKick = true
	return m, true
}

// viewKickCmd is the live slot's wake-up: a manual status read (never
// cancelled by a background lane), its git watcher and its open-files
// sync. Launched by the Update tail after a switchView, which marks the
// read in flight on the live model first.
func (m Model) viewKickCmd() tea.Cmd {
	read := m.readSourceCmd(context.Background(), srcStatus, reloadOpts{manual: true})
	_, docs := m.syncAgentDocs()
	return tea.Batch(read, m.startWatchCmd(m.watchGen), docs)
}

// abandonGoneView is the error arm's check: a read through the viewed
// slot's service failed and its directory is no longer there (the
// worktree was removed under us) — the slot goes, home comes back, its
// list read will say the rest. false when the view is fine.
func (m Model) abandonGoneView() (Model, bool) {
	if m.viewed == "" || m.viewed == m.home || guardStat(m.viewed) == nil {
		return m, false
	}
	gone := m.viewed
	if v := m.views[gone]; v != nil {
		if v.watcher != nil {
			_ = v.watcher.Close()
		}
		closeDocWatch(v.docWatch.w)
		delete(m.views, gone)
	}
	home := m.views[m.home]
	if home == nil {
		return m, false
	}
	m.viewed = "" // the gone slot must not be saved back
	m = m.loadView(home)
	for s := sourceKey(0); s < srcCount; s++ {
		m.srcGen[s]++ // every read launched through the gone service is moot
	}
	m.srcInflight = map[sourceKey]bool{}
	m.srcLoading = map[sourceKey]bool{}
	m.viewKick = true
	m.statusMsg = i18n.T("%s is gone — showing %s", shortWorktreeName(gone), shortWorktreeName(m.home))
	return m, true
}

// pruneViews drops the slots of worktrees that left the list (removed,
// recycled, pruned). The viewed one going falls back to home: its
// service would point at a tree that is not there.
func (m Model) pruneViews() Model {
	for key, v := range m.views {
		if m.isRepoWorktree(key) || key == m.home {
			continue
		}
		if v.watcher != nil {
			_ = v.watcher.Close()
		}
		closeDocWatch(v.docWatch.w)
		delete(m.views, key)
		if key == m.viewed {
			gone := key
			if home := m.views[m.home]; home != nil {
				m.viewed = "" // the gone slot must not be saved back
				m = m.loadView(home)
				m.srcGen[srcStatus]++
				m.viewKick = true
			}
			m.statusMsg = i18n.T("%s is gone — showing %s", shortWorktreeName(gone), shortWorktreeName(m.home))
		}
	}
	return m
}

// adoptView moves gg's identity to the viewed slot — the user's own switch,
// as opposed to a console looking at another worktree. What reRoot does
// for the identity, without the teardown: the exit directory, the session
// registry's worktree, the session snapshot (disabled now, re-resolved off
// thread), the steering inbox and the pending-send watch (closed for the
// old worktree, re-homed by snapshotTargetMsg → initSteerInbox), the hosted
// web page. A shown console's return point moves too: a close stays where
// the user asked to be.
func (m Model) adoptView() (Model, tea.Cmd) {
	if m.viewed == "" || m.viewed == m.home {
		return m, nil
	}
	m.home = m.viewed
	m.switchTarget = m.viewed
	publishedWT.Store(m.viewed)
	removeSnapshotFile(m.snapshotPath)
	m.snapshotPath, m.snapshotCommonDir, m.snapshotWorktree, m.lastSnapshot = "", "", "", nil
	m = m.closeSteerInbox()
	m = m.closePendingWatch()
	m.steerGen++
	m.noticeGen++ // the pending-send watch of the old worktree
	if m.console != nil && m.console.ret != nil {
		m.console.ret.view = m.home
	}
	m.statusMsg = i18n.T("switched to %s", shortWorktreeName(m.home))
	return m, tea.Batch(snapshotTargetCmd(m.svc), m.pendingWatchCmd(m.noticeGen), m.pendingSendsReadCmd(m.noticeGen), m.webRerootCmd())
}

// homeWorktree is gg's own worktree: home once the slots are seeded, the
// current worktree before (a test literal, the first load).
func (m Model) homeWorktree() string {
	if m.home != "" {
		return m.home
	}
	return filepath.Clean(m.currentWorktree)
}
