package tui

import (
	"context"
	"path/filepath"
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
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

	workingReviews []domain.WorkingReview

	loaded bool // its first status landed (false: the panel shows loading)
}

// Generations the handlers key on (loadGen, srcGen, watchGen, docWatch.gen,
// workingReviewsGen) are NOT in the slot: they stay Model-global and only
// ever grow, so a slot can never restore an older one and let a result
// built for another slot land. Watchers are not in the slot either: a
// sleeping slot has none (switchView closes them), the live one rebuilds
// its own on the kick.

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
	applyServicePolicies(v.svc, m.cfg) // the live service's policies (load.go), or an op here would write version refs the config forbids
	m.views[key] = v                   // views is a map (a pointer): the value receiver writes through
	return v
}

// applyServicePolicies gives svc the config's policies — the ones the live
// service gets at load (load.go, source.go): the branch-version policy
// (whether an op writes version refs, their retention), diff colouring,
// EOL-only visibility, the notes cap and the PR cache. Every slot's
// service needs them too: an op launched while another worktree is on
// screen runs through THAT slot's service.
func applyServicePolicies(svc *domain.Service, cfg config.Config) {
	svc.SetShowEOLOnlyChanges(cfg.UI.ShowEOLOnlyChanges)
	svc.SetSyntaxHighlighting(cfg.UI.SyntaxOn())
	svc.SetVersionsPolicy(versionsPolicyFromConfig(cfg))
	svc.SetNotesPolicy(cfg.Notes.MaxAgeDays, cfg.Notes.MaxEntries)
	svc.SetPRCachePolicy(cfg.Forge.CacheMaxAge(), cfg.Forge.PrefetchCount())
}

// applyPoliciesToSlots re-applies m.cfg's policies to the live service and
// every slot's: a config reload or a Settings change reaches all of them,
// not only the worktree on screen.
func (m Model) applyPoliciesToSlots() Model {
	if m.svc != nil {
		applyServicePolicies(m.svc, m.cfg)
	}
	for _, v := range m.views {
		if v.svc != nil && v.svc != m.svc {
			applyServicePolicies(v.svc, m.cfg)
		}
	}
	return m
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
	v.workingReviews = m.workingReviews
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
	m.workingReviews = v.workingReviews
	return m.markHead(m.worktreeBranch(v.path))
}

// worktreeBranch is the branch checked out in the listed worktree at path
// ("" when detached, or not listed).
func (m Model) worktreeBranch(path string) string {
	key := filepath.Clean(path)
	for _, w := range m.worktrees {
		if filepath.Clean(w.Path) == key {
			return w.Branch
		}
	}
	return ""
}

// markHead re-marks the shared lists' per-worktree head flags for the
// viewed worktree: Branch.IsHead (the Branches panel's *) and the commit
// feed's local Ref.Head (the *name identity in Commits) came from reads
// rooted at ONE worktree, and the slots share both lists. From the
// worktree list, no git: the swap stays instant. The lists are cloned
// where they change (domain hands out cached slices).
func (m Model) markHead(branch string) Model {
	var bs []model.Branch
	for i, b := range m.branches {
		head := branch != "" && b.Name == branch
		if b.IsHead == head {
			continue
		}
		if bs == nil {
			bs = slices.Clone(m.branches)
		}
		bs[i].IsHead = head
	}
	if bs != nil {
		m.branches = bs
		m.bfMemo.invalidate()
	}
	var cs []model.Commit
	for i, c := range m.commits {
		for j, r := range c.Refs {
			head := branch != "" && r.Name == branch
			if r.Kind != model.RefLocal || r.Head == head {
				continue
			}
			if cs == nil {
				cs = slices.Clone(m.commits)
			}
			refs := slices.Clone(cs[i].Refs)
			refs[j].Head = head
			cs[i].Refs = refs
		}
	}
	if cs != nil {
		m.commits = cs
		m = m.rebuildCommitGraph()
	}
	return m
}

// homeWorktree is gg's own worktree: home once the slots are seeded, the
// current worktree before (a test literal, the first load).
func (m Model) homeWorktree() string {
	if m.home != "" {
		return m.home
	}
	return filepath.Clean(m.currentWorktree)
}

// homeSvc is the service of gg's own worktree — what the identity-bound
// pieces (the session snapshot target, the hosted web page) are rooted at,
// whichever slot a console has put on screen. The live service before the
// slots are seeded, and right after a repo switch dropped them.
func (m Model) homeSvc() *domain.Service {
	if v := m.views[m.home]; m.home != "" && v != nil && v.svc != nil {
		return v.svc
	}
	return m.svc
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
	m = m.loadView(m.ensureView(key))
	m.watcher, m.watchSupported = nil, false
	m.watchGen++
	m.docWatch = docWatchState{gen: m.docWatch.gen + 1}
	m.loadGen++           // a full load (loadCmd) launched on the old slot cannot land here
	m.srcGen[srcStatus]++ // nor a status read
	m.srcInflight[srcStatus] = false
	m.srcLoading[srcStatus] = false
	m.workingReviewsGen++ // likewise a reviews read
	m.viewKick = true
	return m, true
}

// viewKickCmd is the live slot's wake-up: a SILENT status read (a
// background context would be cancelled by a starting op, so it runs on
// context.Background; silent, so it never raises "⏳ reloading…" nor trips
// the action gates — the slot's remembered status stays on screen until
// the fresh one lands, and the next alt+w is never refused), its git
// watcher and its open-files sync. Launched by the Update tail after a
// switchView, which marks the read in flight on the live model first.
func (m Model) viewKickCmd() tea.Cmd {
	read := m.readSourceCmd(context.Background(), srcStatus, reloadOpts{})
	_, docs := m.syncAgentDocs()
	return tea.Batch(read, m.startWatchCmd(m.watchGen), docs)
}

// takeQueuedReturn performs the return a console close queued while an op
// ran (closeConsole → pendingReturnView): called by the op's end once it
// has dispatched nothing further in that worktree. The refresh that
// follows reads the returned-to slot; the op's own slot refreshes on its
// next kick.
func (m Model) takeQueuedReturn() Model {
	if p := m.pendingReturnView; p != "" {
		m.pendingReturnView = ""
		m, _ = m.switchView(p)
	}
	return m
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
	delete(m.views, gone)
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
	for key := range m.views {
		if m.isRepoWorktree(key) || key == m.home {
			continue
		}
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
// thread), the steering inbox (closed for the old worktree, re-homed by
// snapshotTargetMsg → initSteerInbox), the hosted web page. A shown console's return point moves too: a close stays where
// the user asked to be.
func (m Model) adoptView() (Model, tea.Cmd) {
	if m.viewed == "" || m.viewed == m.home {
		return m, nil
	}
	m.home = m.viewed
	m.switchTarget = m.viewed
	m.pendingReturnView = "" // a return queued to the OLD home is moot: the user asked to be here
	publishedWT.Store(m.viewed)
	removeSnapshotFile(m.snapshotPath)
	m.snapshotPath, m.snapshotCommonDir, m.snapshotWorktree, m.lastSnapshot = "", "", "", nil
	m = m.closeSteerInbox()
	m.steerGen++
	if m.console != nil && m.console.ret != nil {
		m.console.ret.view = m.home
	}
	m.statusMsg = i18n.T("switched to %s", shortWorktreeName(m.home))
	return m, tea.Batch(snapshotTargetCmd(m.svc), m.webRerootCmd())
}

// cycleWorktrees is alt+w. A press without the keyboard on the Branches
// panel — a console shown (bound or not), another panel focused — is a
// FIRST HIT: a shown console hides (the session keeps running; the panels
// stay on the worktree they show), Branches takes focus (its border says
// so) with its cursor on the viewed worktree's branch, and that is all.
// With Branches focused the panels show the next worktree of the Worktrees
// tab's order (its sort), past the last one the first — the same fast
// switch alt+a makes for a console's worktree, no session needed — and the
// Branches cursor moves to that worktree's branch. A look, never an
// adoption: gg's own worktree stays home.
func (m Model) cycleWorktrees() (Model, tea.Cmd) {
	order := m.worktreeOrder()
	n := len(order)
	at := m.worktreeIndex(m.viewed)
	if m.console != nil || !m.panelFocused(panelBranches) || m.activeLeftTab != panelBranches {
		if m.console != nil {
			if m.console.ret != nil {
				m.console.ret.view = m.viewed // stay: hiding is not leaving
				m.console.ret.focus = panelBranches
			}
			m = m.closeConsole()
		}
		m = m.activateTab(panelBranches)
		m = m.selectWorktreeBranch(m.viewed)
		if at < n {
			m.statusMsg = i18n.T("%s — %d of %d worktrees", shortWorktreeName(m.viewed), at+1, n)
		}
		return m, nil
	}
	if n < 2 {
		m.statusMsg = i18n.T("this repository has one worktree — alt+w cycles them once there are more")
		return m, nil
	}
	next := 0
	if at < n {
		next = (at + 1) % n
	}
	wt := m.worktrees[order[next]]
	nm, ok := m.switchView(wt.Path)
	if !ok {
		return nm, nil
	}
	nm.pendingReturnView = ""
	nm = nm.selectWorktreeBranch(nm.viewed)
	nm.statusMsg = i18n.T("%s — %d of %d worktrees", shortWorktreeName(wt.Path), next+1, n)
	return nm, nil
}

// selectWorktreeBranch puts the Branches cursor on the branch a worktree
// has checked out — the row alt+w lands on; a detached or unlisted one
// leaves the cursor alone, as does a filter hiding the row.
func (m Model) selectWorktreeBranch(path string) Model {
	name := m.worktreeBranch(path)
	if name == "" {
		return m
	}
	bi := -1
	for i, b := range m.branches {
		if b.Name == name {
			bi = i
			break
		}
	}
	if bi < 0 {
		return m
	}
	ents := m.branchEntries()
	for di, u := range m.displayIndices(panelBranches) {
		if u < len(ents) && ents[u].br == bi && !ents[u].sub() {
			if m.sel == nil {
				m.sel = map[panel]int{}
			}
			m.sel[panelBranches] = di
			break
		}
	}
	return m
}
