package tui

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repos"
)

// worktreeView is the worktree-scoped part of the Model, remembered per
// worktree of the repository on screen so a switch between them is a copy,
// not a reload. The Model's own fields are the LIVE copy — every reader and
// renderer is untouched — and switchView copies out to the leaving slot
// and in from the arriving one. Worktrees of one repository share
// branches, commits, stashes, tags and the reflog, so none of those is
// here.
type worktreeView struct {
	key  model.CheckoutKey // the map key: the path's identity (model.KeyOf)
	path string            // the worktree path as LISTED (`git worktree list`): what reaches disk, git and the screen
	svc  *domain.Service   // rooted at path: every read and op of that tree

	status         model.WorkingTreeStatus
	conflict       domain.ConflictState
	filesIdx       []int
	filesIdxReview []int
	stagedIdx      []int
	selFiles       int
	selStaged      int
	fileMarks      map[string]bool

	workingReviews []domain.WorkingReview

	resumePromptShown bool // the continue/abort prompt fired for THIS tree's paused op (model.go's flag, per slot: a round trip through another tree must not fire it again)

	windows windowState // this worktree's windows while it sleeps (window_state.go): the layer pile, the files/stash views, the steer leftovers, the window gens
	queued  []tea.Msg   // results addressed to this worktree that landed while it slept, oldest first (slot_replay.go); applied on return

	loaded bool // its first status landed (false: the panels are empty, not clean — viewLoading says so)
}

// Generations the handlers key on (loadGen, srcGen, watchGen, docWatch.gen,
// workingReviewsGen) are NOT in the slot: they stay Model-global and only
// ever grow, so a slot can never restore an older one and let a result
// built for another slot land. Watchers are not in the slot either: a
// sleeping slot has none (switchView closes them), the live one rebuilds
// its own on the kick.

// listedWorktree is the Worktrees list's own spelling of the worktree
// path names (by checkout key: a session, a link or the user's cwd may
// spell it in another case on a folding platform), and whether it is
// listed. The listed spelling is the one that reaches disk and git.
func (m Model) listedWorktree(path string) (string, bool) {
	key := model.KeyOf(path)
	for _, w := range m.worktrees {
		if model.KeyOf(w.Path) == key {
			return filepath.Clean(w.Path), true
		}
	}
	return "", false
}

// bareRepository reports whether path is the repository's bare entry of
// the worktree list (a bare main repo with linked worktrees): listed, but
// no working tree — never a slot.
func (m Model) bareRepository(path string) bool {
	key := model.KeyOf(path)
	for _, w := range m.worktrees {
		if w.Bare && model.KeyOf(w.Path) == key {
			return true
		}
	}
	return false
}

// isRepoWorktree reports whether path is a worktree of the repository on
// screen — the only paths a slot may be made for.
func (m Model) isRepoWorktree(path string) bool {
	_, ok := m.listedWorktree(path)
	return ok
}

// viewPath is the on-disk path behind a slot key: the slot's listed
// spelling, else the list's, else "" (not a worktree of this repo).
func (m Model) viewPath(k model.CheckoutKey) string {
	if v := m.views[k]; v != nil {
		return v.path
	}
	for _, w := range m.worktrees {
		if model.KeyOf(w.Path) == k {
			return filepath.Clean(w.Path)
		}
	}
	return ""
}

// ensureView is the slot for path, made on first use with a service rooted
// there. Home is seeded by the first load (dataLoadedMsg), so a caller
// asking for home gets the live slot back.
func (m Model) ensureView(path string) *worktreeView {
	key := model.KeyOf(path)
	if v, ok := m.views[key]; ok {
		return v
	}
	if listed, ok := m.listedWorktree(path); ok {
		path = listed // the service roots at the spelling git lists
	} else {
		path = filepath.Clean(path)
	}
	var svc *domain.Service
	if home := m.views[m.home]; home != nil && home.svc != nil {
		svc = domain.OpenTUISharing(path, home.svc) // the repository's caches are one set: a diff cached from home is a hit here
	} else {
		svc = domain.OpenTUI(path) // home is seeded by the first load before any slot is made; total anyway
	}
	v := &worktreeView{key: key, path: path, svc: svc}
	v.windows.layers = &layerStack{}   // a fresh slot: an empty pile, as the Model starts with
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
		m.views = map[model.CheckoutKey]*worktreeView{}
	}
	if m.home != "" && m.views[m.viewed] != nil {
		return m
	}
	m.home = model.KeyOf(path)
	m.viewed = m.home
	if listed, ok := m.listedWorktree(path); ok {
		path = listed
	} else {
		path = filepath.Clean(path)
	}
	m.views[m.home] = &worktreeView{key: m.home, path: path, svc: m.svc, loaded: true} // seeded by a load that landed
	return m
}

// saveView copies the live worktree-scoped fields into the viewed slot.
func (m Model) saveView() Model {
	if m.viewed == "" {
		return m
	}
	v := m.views[m.viewed]
	if v == nil {
		return m
	}
	v.svc = m.svc
	v.status, v.conflict = m.status, m.conflict
	v.filesIdx, v.filesIdxReview, v.stagedIdx = m.filesIdx, m.filesIdxReview, m.stagedIdx
	v.selFiles, v.selStaged = m.sel[panelFiles], m.sel[panelStaged]
	v.fileMarks = m.fileMarks
	v.workingReviews = m.workingReviews
	v.resumePromptShown = m.resumePromptShown
	v.windows = m.windowState // the worktree owns its windows: one assignment, nothing filtered
	v.windows.workingAttention = takeWorkingAttention(m.attention)
	return m
}

// takeWorkingAttention moves the `gg session highlight` bands on WORKING
// files (commit == "") out of attention and returns them: they are the
// leaving tree's (the same path exists in the arriving one); a commit's
// bands are the repository's and stay.
func takeWorkingAttention(attention map[attentionKey][]steerMark) map[attentionKey][]steerMark {
	var working map[attentionKey][]steerMark
	for k, marks := range attention {
		if k.commit != "" {
			continue
		}
		if working == nil {
			working = map[attentionKey][]steerMark{}
		}
		working[k] = marks
		delete(attention, k)
	}
	return working
}

// loadView makes slot v the live one. The open-files registry is already
// keyed per worktree (openFilesReg.byWT) and stays shared.
func (m Model) loadView(v *worktreeView) Model {
	m.viewed = v.key
	m.svc = v.svc
	m.currentWorktree = v.path
	publishedView.Store(v.path) // the session registry says what this TUI shows (another gg's guard)
	if m.feed != nil {
		m.feed.SetService(v.svc) // the shared Commits list walks from the VIEWED tree's HEAD (a detached one's commits); the kick reconciles
	}
	m.workingReviews = v.workingReviews // before the rows: withStatus derives the Review row from the reviews
	m = m.withStatus(v.status)          // recomputes the index slices and the status stack
	m.conflict = v.conflict
	m.resumePromptShown = v.resumePromptShown
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	m.sel[panelFiles], m.sel[panelStaged] = v.selFiles, v.selStaged
	m.fileMarks = v.fileMarks
	m.windowState = v.windows // its windows, exactly as it left them (a fresh slot: none)
	if m.layers == nil {
		m.layers = &layerStack{}
	}
	for k, marks := range v.windows.workingAttention {
		if m.attention == nil {
			m.attention = map[attentionKey][]steerMark{}
		}
		m.attention[k] = marks
	}
	m.workingAttention = nil                 // merged back; the live copy is m.attention
	m.replay = append(m.replay, v.queued...) // what landed for it while it slept: the Update tail applies it
	v.queued = nil
	if m.console == nil {
		m = m.restoreConsoleParked() // the console that displaced them is gone (a queued return): they are live again
	}
	// Always rebuilt: the WIP rows are derived from the status inside it,
	// whether or not a head mark moved (both trees detached, tips off page).
	return m.markHead(m.worktreeBranch(v.path)).rebuildCommitGraph()
}

// worktreeBranch is the branch checked out in the listed worktree at path
// ("" when detached, or not listed).
func (m Model) worktreeBranch(path string) string {
	key := model.KeyOf(path)
	for _, w := range m.worktrees {
		if model.KeyOf(w.Path) == key {
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
// where they change (domain hands out cached slices); the caller rebuilds
// the graph.
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
		m.commits = cs // loadView rebuilds the graph
	}
	return m
}

// homeWorktree is gg's own worktree: home once the slots are seeded, the
// current worktree before (a test literal, the first load).
func (m Model) homeWorktree() string {
	if v := m.views[m.home]; m.home != "" && v != nil {
		return v.path
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
	return m.switchViewBy(path, false)
}

// userSwitchView is the user's OWN swap (alt+w, alt+a/alt+t, enter on a
// worktree row): a parkable popup on top (parkableLayer) is parked with
// the worktree's windows instead of refusing.
func (m Model) userSwitchView(path string) (Model, bool) {
	return m.switchViewBy(path, true)
}

// switchViewBy is switchView with the caller's intent: byUser lets a
// parkable popup on top be parked; an agent's swap (a console show, a
// steer ask, a console's return, a gone slot) refuses under any popup.
func (m Model) switchViewBy(path string, byUser bool) (Model, bool) {
	key := model.KeyOf(path)
	if key == m.viewed {
		return m, true
	}
	if m.home == "" {
		// Before the first load seeds the slots (a repo switch in flight)
		// there is nothing to swap from.
		m.statusMsg = i18n.T("the repository is still loading — switch once it is shown")
		return m, false
	}
	listed, ok := m.listedWorktree(path)
	if !ok {
		m.statusMsg = i18n.T("%s is not a worktree of this repository", path)
		return m, false
	}
	path = listed // the spelling git lists is what is on disk
	if m.bareRepository(path) {
		m.statusMsg = i18n.T("%s is the bare repository — it has no working tree to show", path)
		return m, false
	}
	if verdict, _ := checkSwitchTarget(guardStat, guardGOOS, path); verdict != switchOK {
		m.statusMsg = i18n.T("cannot switch: %s is not reachable from here", path)
		return m, false
	}
	if why := m.switchRefusalBy(byUser); why != "" {
		m.statusMsg = why
		return m, false
	}
	m = m.saveView()
	m = m.sleepView()
	m = m.loadView(m.ensureView(path))
	m.viewKick = true
	return m, true
}

// switchRefusal is why the panels cannot swap right now, "" when they can:
// an operation running (the op's slot must stay on screen until it ends),
// or a surface that owns the keyboard — a decision, a popup being filled,
// a process, text being typed (steerRefusal's list). A popup opened over
// worktree A submits through m.svc: swapping under it would run its op in
// B. The swaps that are not the user's own key (a console's return, a
// gone slot) queue through pendingReturnView and drain when the surface
// clears (the Update tail).
func (m Model) switchRefusal() string {
	return m.switchRefusalBy(false)
}

// switchRefusalBy is switchRefusal with the caller's intent: for the
// user's own swap, a parkable popup on top is no refusal (it is parked);
// steerRefusal's other answers (typing, a decision, a process) stand.
func (m Model) switchRefusalBy(byUser bool) string {
	switch {
	case !m.opsIdle():
		return i18n.T("an operation is running — switch once it has finished")
	}
	why := m.steerRefusal()
	if why == "" || (byUser && why == refusalWindowOwnsKeyboard && parkableLayer(m.topLayer())) {
		return ""
	}
	return i18n.T("cannot switch while a window is open")
}

// viewLoading reports a viewed slot whose first status read has not
// landed: its panels are empty, not clean.
func (m Model) viewLoading() bool {
	v := m.views[m.viewed]
	return v != nil && !v.loaded
}

// markViewLoaded records the live slot's first status arrival.
func (m Model) markViewLoaded() Model {
	if v := m.views[m.viewed]; v != nil {
		v.loaded = true
	}
	return m
}

// sleepView puts the live slot's machinery to rest before another slot is
// loaded: its watchers close, and every generation a result could carry
// moves on, so a full load, status read, reviews read, watcher or docs
// sync launched for the leaving slot cannot land on the arriving one. The
// slot's STATE stays (saveView stored it); switchView and both drop paths
// (abandonGoneView, pruneViews) share this.
func (m Model) sleepView() Model {
	if m.watcher != nil {
		_ = m.watcher.Close()
	}
	closeDocWatch(m.docWatch.w)
	m.watcher, m.watchSupported = nil, false
	m.watchGen++
	m.docWatch = docWatchState{gen: m.docWatch.gen + 1}
	m.loadGen++           // a full load (loadCmd) launched on the old slot cannot land here
	m.srcGen[srcStatus]++ // nor a status read
	m.srcInflight[srcStatus] = false
	m.srcLoading[srcStatus] = false
	m.srcGen[srcNotes]++ // nor the note badges (per checkout)
	m.srcInflight[srcNotes] = false
	m.srcLoading[srcNotes] = false
	m.srcGen[srcFeed]++ // nor a commit walk from the old root (the feed is re-rooted on load)
	m.srcInflight[srcFeed] = false
	m.srcLoading[srcFeed] = false
	m.workingReviewsGen++ // likewise a reviews read
	// The windows, the parked tour, the parked navigate and the working-file
	// bands are NOT touched: saveView put them in the slot, loadView brings
	// the arriving slot's in. Nothing crosses, nothing is filtered.
	return m
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
	notes := m.readSourceCmd(context.Background(), srcNotes, reloadOpts{}) // the ✎ badges are the checkout's
	feed := m.readSourceCmd(context.Background(), srcFeed, reloadOpts{})   // reconcile: the walk now starts at this tree's HEAD
	_, docs := m.syncAgentDocs()
	return tea.Batch(read, notes, feed, m.startWatchCmd(m.watchGen), docs)
}

// takeQueuedReturn performs the swap a console close, a console SHOW (its
// own worktree) or a gone slot queued while the panels could not swap
// (pendingReturnView). The Update
// tail calls it once switchRefusal clears — so after an op's end only
// when the end dispatched nothing further in that worktree (a chained op
// is running again, a prompt is a surface), and after a staging round or
// a dismissed prompt too. A refusal for another reason drops the queue.
// The op's own slot refreshes on its next kick.
func (m Model) takeQueuedReturn() Model {
	p := m.pendingReturnView
	if p == "" {
		return m
	}
	if m.switchRefusal() != "" {
		return m // still held: the Update tail tries again once the surface or the op clears
	}
	m.pendingReturnView = ""
	path := m.viewPath(p)
	if path == "" || !m.isRepoWorktree(path) {
		// The slot it named is gone (pruned, removed): home is the fallback.
		if m.viewed == m.home {
			return m
		}
		m.statusMsg = i18n.T("%s is gone — showing %s", shortWorktreeName(string(p)), shortWorktreeName(m.homeWorktree()))
		path = m.homeWorktree()
	}
	m, _ = m.switchView(path)
	return m
}

// abandonGoneView is the error arm's check: a read through the viewed
// slot's service failed and its directory is no longer there (the
// worktree was removed under us) — the slot goes, home comes back, its
// list read will say the rest. false when the view is fine.
func (m Model) abandonGoneView() (Model, bool) {
	if m.viewed == "" || m.viewed == m.home || guardStat(m.viewPath(m.viewed)) == nil {
		return m, false
	}
	if m.switchRefusal() != "" {
		// A popup over the gone tree is being filled: its submit fails
		// against the missing directory, which is honest; a swap would run
		// it in home. The return home queues for the surface clearing.
		m.pendingReturnView = m.home
		return m, false
	}
	gone := m.viewPath(m.viewed)
	delete(m.views, m.viewed)
	home := m.views[m.home]
	if home == nil {
		return m, false
	}
	m = m.sleepView()
	m.viewed = "" // the gone slot must not be saved back: its windows and working-file bands die with it
	takeWorkingAttention(m.attention)
	m = m.loadView(home)
	for s := sourceKey(0); s < srcCount; s++ {
		m.srcGen[s]++ // every read launched through the gone service is moot
	}
	m.srcInflight = map[sourceKey]bool{}
	m.srcLoading = map[sourceKey]bool{}
	m.viewKick = true
	m.statusMsg = i18n.T("%s is gone — showing %s", shortWorktreeName(gone), shortWorktreeName(m.homeWorktree()))
	return m, true
}

// pruneViews drops the slots of worktrees that left the list (removed,
// recycled, pruned). The viewed one going falls back to home: its
// service would point at a tree that is not there.
func (m Model) pruneViews() Model {
	for key, v := range m.views {
		if m.isRepoWorktree(v.path) || key == m.home {
			continue
		}
		if key == m.viewed && m.switchRefusal() != "" {
			// A surface over the gone tree owns the keyboard: the slot stays
			// (its reads fail honestly), the return home queues for the
			// surface clearing — the next list read prunes it then.
			m.pendingReturnView = m.home
			continue
		}
		delete(m.views, key)
		if m.pendingReturnView == key {
			m.pendingReturnView = m.home // a return queued to the gone slot goes home instead
		}
		if key == m.viewed {
			gone := v.path
			if home := m.views[m.home]; home != nil {
				m = m.sleepView()
				m.viewed = "" // the gone slot must not be saved back: its windows and working-file bands die with it
				takeWorkingAttention(m.attention)
				m = m.loadView(home)
				m.viewKick = true
			}
			m.statusMsg = i18n.T("%s is gone — showing %s", shortWorktreeName(gone), shortWorktreeName(m.homeWorktree()))
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
	home := m.homeWorktree()
	m.switchTarget = home
	m.pendingReturnView = "" // a return queued to the OLD home is moot: the user asked to be here
	publishedWT.Store(home)
	removeSnapshotFile(m.snapshotPath)
	m.snapshotPath, m.snapshotCommonDir, m.snapshotWorktree, m.lastSnapshot = "", "", "", nil
	m = m.closeSteerInbox()
	m.steerGen++
	if m.console != nil && m.console.ret != nil {
		m.console.ret.view = m.home
	}
	m.statusMsg = i18n.T("switched to %s", shortWorktreeName(home))
	return m, tea.Batch(snapshotTargetCmd(m.svc), m.webRerootCmd(), touchRepoMRUCmd(home, m.linkRepoName))
}

// touchRepoMRUCmd records the adopted worktree in the repo switcher's MRU
// off-thread (a reload's loader does this at boot; an in-repo adopt has no
// loader).
func touchRepoMRUCmd(path, remote string) tea.Cmd {
	return func() tea.Msg {
		if sp := repos.DefaultStatePath(); sp != "" {
			_ = repos.Touch(sp, path, remote, clock.Now())
		}
		return nil
	}
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
	viewed := m.viewPath(m.viewed)
	at := m.worktreeIndex(viewed)
	if m.console != nil || !m.panelFocused(panelBranches) || m.activeLeftTab != panelBranches {
		if m.console != nil {
			if m.console.ret != nil {
				m.console.ret.view = m.viewed // stay: hiding is not leaving
				m.console.ret.focus = panelBranches
			}
			m = m.closeConsole()
		}
		m = m.activateTab(panelBranches)
		m = m.selectWorktreeBranch(viewed)
		if at < n {
			m.statusMsg = i18n.T("%s — %d of %d worktrees", shortWorktreeName(viewed), at+1, n)
		}
		return m, nil
	}
	if n < 2 {
		m.statusMsg = i18n.T("this repository has one worktree — alt+w cycles them once there are more")
		return m, nil
	}
	next, tries := 0, n // viewed but unlisted: every stop is a candidate
	if at < n {
		next, tries = (at+1)%n, n-1
	}
	// A worktree that cannot be reached from here (another environment's
	// notation, a directory gone) is skipped, named, and the ring moves on
	// — refusing it would leave the ring stuck behind it.
	var skipped []string
	for range tries {
		if verdict, _ := checkSwitchTarget(guardStat, guardGOOS, m.worktrees[order[next]].Path); verdict == switchOK {
			break
		}
		skipped = append(skipped, shortWorktreeName(m.worktrees[order[next]].Path))
		next = (next + 1) % n
	}
	if len(skipped) == tries {
		m.statusMsg = i18n.T("no other worktree is reachable from here")
		return m, nil
	}
	wt := m.worktrees[order[next]]
	nm, ok := m.userSwitchView(wt.Path)
	if !ok {
		return nm, nil
	}
	nm.pendingReturnView = ""
	nm = nm.selectWorktreeBranch(wt.Path)
	if len(skipped) > 0 {
		nm.statusMsg = i18n.T("%s — %d of %d worktrees (skipped %s: not reachable from here)", shortWorktreeName(wt.Path), next+1, n, strings.Join(skipped, ", "))
	} else {
		nm.statusMsg = i18n.T("%s — %d of %d worktrees", shortWorktreeName(wt.Path), next+1, n)
	}
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
