package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/branchfilter"
	"github.com/homeend/gigagit/internal/commitgraph"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gitwatch"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// repoView is the REPOSITORY-scoped part of the Model, remembered per
// member of an open workspace so a switch between repositories is a copy,
// not a reRoot (spec docs/superpowers/specs/2026-10-22-workspaces-design.md
// §6). It sits above the per-worktree slots (worktreeView): the lists all
// worktrees of one repository share (branches, remotes, tags, commits, the
// worktree list), the feed and its scope, the branch filters, the notices
// and repository health, the forge state, this repository's config overlay,
// its service and its map of worktree slots. The Model's own fields stay the
// LIVE copy — no reader or renderer changed — and saveRepo/loadRepo copy out
// to the leaving member and in from the arriving one, the way saveView /
// loadView do for worktrees.
//
// NOT here: what is tied to gg's identity (home) rather than to a
// repository — the session snapshot, the steer inbox, the published
// worktree — which adoptView moves; the process-wide config (m.wsCfg, from
// the workspace's settings repository); the per-panel sort modes and text
// filters; the generations, which stay Model-global and only grow.
type repoView struct {
	key      model.CheckoutKey // of mainPath (model.KeyOf)
	mainPath string            // the main checkout path — the member identity (domain.RepoMainPath)
	name     string            // directory name shown as the group heading

	svc  *domain.Service
	cfg  config.Config      // config.MergeMember(m.wsCfg, this repository's overlay)
	feed *domain.CommitFeed // this repository's Commits feed

	views            map[model.CheckoutKey]*worktreeView
	viewed           model.CheckoutKey // the slot on screen while this member is live ("" = never shown)
	lastViewed       string            // the worktree path last on screen in this member ("" = never)
	branchFilterSlot map[panel]int

	branches       []model.Branch
	remoteBranches []model.RemoteBranch
	tags           []model.Tag
	commits        []model.Commit
	worktrees      []model.Worktree
	previews       []previewRow
	graphLayer     *commitgraph.Layer
	segLayer       *commitgraph.SegmentLayer

	commitScopeBranches []string
	commitFilter        commitFilterFields
	feedScopeApplied    string
	filterMemo          *commitFilterMemo
	commitCompareSet    map[string]bool
	previewCompareSet   map[string]bool
	mark                *markState

	branchFilters [branchfilter.MaxSlots]branchfilter.Compiled
	bfSlotsLoaded bool
	bfCfgApplied  bool
	bfMemo        *branchFilterMemos

	notices                []notice
	driftNotices           []driftNoticeSource
	noticesUnread          bool
	noticeSessionDismissed map[string]bool
	repoHealth             model.RepoHealth
	repoHealthKnown        bool
	toolStatuses           []domain.ToolTemplateStatus

	forgeShown       bool
	forgeProvider    string
	forgeProbeKicked bool
	prs              []model.PullRequest
	prsErr           string
	prsLoaded        bool
	prsAnswered      bool
	remoteTagNames   map[string]bool
	identity         model.Identity
	linkRepoName     string

	sel map[panel]int // the cursors of the per-repository panels (repoPanels)

	loaded   bool // its first load landed (false: the member's panels show the loading marker)
	missing  bool // its path did not exist when the workspace opened
	everLive bool // it has been on screen: its whole repository was read once (the first switch's full read)
	// dropAnswered: the drop prompt for this member was answered for the
	// drop in progress (askBeforeDrop re-enters its caller once).
	dropAnswered bool

	// Its own repository-level watcher while it sleeps (member_watch.go);
	// watchGen moves on whenever it closes, so a stale build or event is
	// dropped; polling = the fallback beat runs instead; watchStarting = a
	// build is in flight.
	watcher       *gitwatch.Watcher
	watchGen      int
	polling       bool
	watchStarting bool
}

// repoPanels are the panels whose cursor belongs to one repository and
// travels with its slot; Branches and Worktrees list every member and keep
// a live cursor, Files/Staged/Reflog are the worktree slot's.
var repoPanels = []panel{panelCommits, panelTags, panelRemotes, panelPreviews, panelPRs}

// workspaceState is the open workspace: its members in walk order and the
// one on screen. nil on the Model = no workspace (the single-repository gg).
type workspaceState struct {
	name     string
	members  []*repoView
	live     int    // index of the member on screen
	settings string // the settings repository's main path ("" = the member gg was opened in)
}

// newRepoView is a fresh member slot rooted at mainPath with svc.
func newRepoView(mainPath string, svc *domain.Service) *repoView {
	mainPath = filepath.Clean(mainPath)
	var feed *domain.CommitFeed
	if svc != nil {
		feed = svc.CommitFeed()
	}
	return &repoView{
		key:                    model.KeyOf(mainPath),
		mainPath:               mainPath,
		name:                   filepath.Base(mainPath),
		svc:                    svc,
		feed:                   feed,
		views:                  map[model.CheckoutKey]*worktreeView{},
		branchFilterSlot:       map[panel]int{},
		filterMemo:             &commitFilterMemo{},
		bfMemo:                 &branchFilterMemos{},
		noticeSessionDismissed: map[string]bool{},
		sel:                    map[panel]int{},
	}
}

// inWorkspace reports an open workspace.
func (m Model) inWorkspace() bool { return m.ws != nil }

// homeRepo is the repository slot gg was opened in: the home member of
// the workspace, else the single repository's own slot (seeded by the first
// load; nil before it).
func (m Model) homeRepo() *repoView {
	if m.ws != nil && m.home != "" {
		for _, r := range m.ws.members {
			if r.views[m.home] != nil {
				return r
			}
		}
	}
	return m.homeRepoView
}

// homeView is the slot of gg's own worktree, in whichever member it lives.
func (m Model) homeView() *worktreeView {
	if m.home == "" {
		return nil
	}
	if v := m.views[m.home]; v != nil {
		return v
	}
	if r := m.homeRepo(); r != nil {
		return r.views[m.home]
	}
	return nil
}

// memberAt is the member a path belongs to: a worktree its list names, a
// slot it has, its main path, or (before its list landed) a path under its
// main checkout. (nil, false) when no workspace is open or nobody owns it.
func (m Model) memberAt(path string) (*repoView, bool) {
	if m.ws == nil {
		return nil, false
	}
	key := model.KeyOf(path)
	clean := filepath.Clean(path)
	for _, r := range m.ws.members {
		if r.key == key || r.views[key] != nil {
			return r, true
		}
		for _, w := range r.worktrees {
			if model.KeyOf(w.Path) == key {
				return r, true
			}
		}
	}
	for _, r := range m.ws.members {
		if len(r.worktrees) == 0 && strings.HasPrefix(clean, r.mainPath+string(filepath.Separator)) {
			return r, true
		}
	}
	return nil, false
}

// isMemberWorktree reports a path some member of the open workspace owns.
func (m Model) isMemberWorktree(path string) bool {
	_, ok := m.memberAt(path)
	return ok
}

// defaultWorktree is where a switch into r lands when the caller names no
// worktree: the one last on screen there, else its main checkout (the
// first non-bare entry of its list when the main repository is bare).
func (r *repoView) defaultWorktree() string {
	if r.lastViewed != "" {
		return r.lastViewed
	}
	for _, w := range r.worktrees {
		if !w.Bare {
			return filepath.Clean(w.Path)
		}
	}
	return r.mainPath
}

// switchRepo makes member r the live repository and lands on worktree
// (its default when ""): a LOOK — gg's home stays where it is; the caller
// that adopts runs adoptView after. Refused, with the message every switch
// shares, while an operation runs or a surface owns the keyboard
// (switchRefusalBy). The leaving member's worktree slot is parked and its
// repository-wide state saved; the arriving member's is loaded and the
// worktree slot swap lands as a same-repository switch does, so the Update
// tail's kick refreshes it. Within the live member it is switchViewBy.
func (m Model) switchRepo(r *repoView, worktree string, byUser bool) (Model, bool) {
	if r == nil {
		return m, false
	}
	if worktree == "" {
		worktree = r.defaultWorktree()
	}
	if r == m.liveRepo() {
		return m.switchViewBy(worktree, byUser)
	}
	if m.home == "" {
		m.statusMsg = i18n.T("the repository is still loading — switch once it is shown")
		return m, false
	}
	if r.missing || r.svc == nil {
		// A member whose path was not there when the workspace opened: still
		// gone → refused; back (remounted) → revived, and the first switch's
		// full read fills it.
		if st, err := os.Stat(r.mainPath); err != nil || !st.IsDir() {
			m.statusMsg = i18n.T("%s (missing)", r.name)
			return m, false
		}
		r.svc = domain.OpenTUISharing(r.mainPath, m.homeSvc())
		r.feed = r.svc.CommitFeed()
		r.missing, r.loaded = false, false
		if worktree == r.mainPath || worktree == "" {
			worktree = r.mainPath
		}
	}
	if verdict, _ := checkSwitchTarget(guardStat, guardGOOS, worktree); verdict != switchOK {
		m.statusMsg = i18n.T("cannot switch: %s is not reachable from here", worktree)
		return m, false
	}
	if why := m.switchRefusalBy(byUser); why != "" {
		m.statusMsg = why
		return m, false
	}
	m = m.parkView()
	m = m.sleepView()
	m = m.dropLiveReads() // reads launched for the leaving member must not land in the arriving one
	m = m.saveRepo()
	m = m.loadRepo(r)
	r.closeWatch() // the live member is the ordinary watcher's (the kick below starts it)
	m = m.loadView(m.ensureMemberView(r, worktree))
	m.viewKick = true
	m.memberWatchKick = true // the leaving member sleeps: its own watcher starts
	if !r.everLive {
		r.everLive = true
		m.memberFirstLive = true // the Update tail reads its whole repository (reloadAllCmd + health)
	}
	return m, true
}

// dropLiveReads moves every repository-wide generation on and cancels the
// background lane, so a read, probe or check launched for the member that
// is leaving the screen cannot land in the one arriving: dataAvailableMsg
// carries no service, only a generation. sleepView covers the per-worktree
// sources (status, notes, feed, reflog); this covers the rest.
func (m Model) dropLiveReads() Model {
	for _, s := range []sourceKey{srcBranches, srcRemotes, srcTags, srcWorktrees, srcIdentity, srcPreviews} {
		m.srcGen[s]++
		m.srcInflight[s] = false
		m.srcLoading[s] = false
	}
	m.loading = m.anySourceLoading()
	m.loadGen++
	m.noticeGen++
	m.prsGen++
	m.remoteTagsGen++
	m.pushCheckGen++
	if m.bgCancel != nil {
		m.bgCancel()
		m.bgCancel = nil
	}
	m.bgBusy = false
	return m
}

// ensureMemberView is ensureView for a member just made live: its main
// checkout's slot is served by the member's own service (the one its lists
// are read through — one service per repository root, as home's slot is),
// any other worktree by a sharing service rooted there.
func (m Model) ensureMemberView(r *repoView, worktree string) *worktreeView {
	key := model.KeyOf(worktree)
	if v, ok := r.views[key]; ok {
		return v
	}
	if key == r.key && r.svc != nil {
		v := &worktreeView{key: key, path: r.mainPath, svc: r.svc}
		v.windows.layers = &layerStack{}
		applyServicePolicies(v.svc, m.cfg)
		r.views[key] = v
		return v
	}
	return m.ensureView(worktree)
}

// liveRepo is the repository slot on screen: the live member, else the
// single repository's slot.
func (m Model) liveRepo() *repoView {
	if m.ws != nil && m.ws.live >= 0 && m.ws.live < len(m.ws.members) {
		return m.ws.members[m.ws.live]
	}
	return m.homeRepoView
}

// seedHomeRepo gives the single repository its slot on the first load: the
// live service, the live worktree slot map, a main path taken from the
// worktree list when it is known (its main entry), else the home worktree
// (refined when the list lands: refreshHomeRepoMain).
func (m Model) seedHomeRepo(homePath string) Model {
	if m.homeRepoView != nil {
		return m
	}
	main := m.mainWorktreePath(homePath)
	r := newRepoView(main, m.rootServiceFor(main, homePath))
	r.feed = m.feed
	r.views = m.views
	r.viewed = m.viewed
	r.cfg = m.cfg
	r.loaded, r.everLive = true, true
	m.homeRepoView = r
	return m
}

// rootServiceFor is the repository's root service: the live one when gg
// runs in the main checkout, else a sharing service rooted at main (a
// linked-worktree launch), so a member slot made for the main checkout
// never reads the launch worktree.
func (m Model) rootServiceFor(main, worktree string) *domain.Service {
	if model.KeyOf(main) == model.KeyOf(worktree) || m.svc == nil {
		return m.svc
	}
	return domain.OpenTUISharingRooted(main, m.svc)
}

// mainWorktreePath is the repository's main checkout as the worktree list
// shows it (the first non-bare entry, or the bare entry); fallback when the
// list has not landed.
func (m Model) mainWorktreePath(fallback string) string {
	for _, w := range m.worktrees {
		if w.Bare {
			return filepath.Clean(w.Path)
		}
	}
	if len(m.worktrees) > 0 {
		return filepath.Clean(m.worktrees[0].Path)
	}
	return filepath.Clean(fallback)
}

// refreshHomeRepoMain re-derives the single repository's identity once the
// worktree list is in (the seed may have run before it).
func (m Model) refreshHomeRepoMain() Model {
	r := m.homeRepoView
	if r == nil || m.ws != nil || len(m.worktrees) == 0 {
		return m
	}
	main := m.mainWorktreePath(r.mainPath)
	return m.setHomeRepoMain(main)
}

// setHomeRepoMain re-roots the single repository's slot identity at main
// (the worktree list's main entry, or the health probe's common dir): the
// key, the heading name and — when the launch worktree is not main — a
// root service for the main checkout. A no-op inside a workspace or when
// nothing changed.
func (m Model) setHomeRepoMain(main string) Model {
	r := m.homeRepoView
	if r == nil || m.ws != nil || main == "" {
		return m
	}
	main = filepath.Clean(main)
	if main == r.mainPath {
		return m
	}
	r.mainPath, r.key, r.name = main, model.KeyOf(main), filepath.Base(main)
	r.svc = m.rootServiceFor(main, m.homeWorktree())
	return m
}

// mainPathOfCommonDir is the member identity behind a common git dir: its
// parent for a ".git" directory, the directory itself for a bare one
// (domain.RepoMainPath's rule, without a git call).
func mainPathOfCommonDir(common string) string {
	common = filepath.Clean(common)
	if strings.EqualFold(filepath.Base(common), ".git") {
		return filepath.Dir(common)
	}
	return common
}

// saveRepo copies the live repository-wide fields into the live member's
// slot. A no-op before the first load seeded a slot.
func (m Model) saveRepo() Model {
	r := m.liveRepo()
	if r == nil {
		return m
	}
	// NOT r.svc / r.feed: the member's service is its ROOT service (m.svc
	// is the viewed worktree slot's, loadView set it) and the feed is the
	// repository's one feed object.
	r.cfg = m.cfg
	r.views, r.viewed = m.views, m.viewed
	if p := m.viewPath(m.viewed); p != "" {
		r.lastViewed = p
	}
	r.branches, r.remoteBranches, r.tags, r.commits, r.worktrees = m.branches, m.remoteBranches, m.tags, m.commits, m.worktrees
	r.previews = m.previews
	r.graphLayer, r.segLayer = m.graphLayer, m.segLayer
	r.commitScopeBranches, r.commitFilter, r.feedScopeApplied, r.filterMemo = m.commitScopeBranches, m.commitFilter, m.feedScopeApplied, m.filterMemo
	r.commitCompareSet, r.previewCompareSet, r.mark = m.commitCompareSet, m.previewCompareSet, m.mark
	r.branchFilters, r.branchFilterSlot, r.bfSlotsLoaded, r.bfCfgApplied, r.bfMemo = m.branchFilters, m.branchFilterSlot, m.bfSlotsLoaded, m.bfCfgApplied, m.bfMemo
	r.notices, r.driftNotices, r.noticesUnread, r.noticeSessionDismissed = m.notices, m.driftNotices, m.noticesUnread, m.noticeSessionDismissed
	r.repoHealth, r.repoHealthKnown, r.toolStatuses = m.repoHealth, m.repoHealthKnown, m.toolStatuses
	r.forgeShown, r.forgeProvider, r.forgeProbeKicked = m.forgeShown, m.forgeProvider, m.forgeProbeKicked
	r.prs, r.prsErr, r.prsLoaded, r.prsAnswered = m.prs, m.prsErr, m.prsLoaded, m.prsAnswered
	r.remoteTagNames, r.identity, r.linkRepoName = m.remoteTagNames, m.identity, m.linkRepoName
	if r.sel == nil {
		r.sel = map[panel]int{}
	}
	for _, p := range repoPanels {
		r.sel[p] = m.sel[p]
	}
	return m
}

// loadRepo makes r's repository-wide fields the live ones. The worktree
// slot swap (loadView) that follows lands on one of r's worktrees; the
// per-panel cursors of the repository panels come back as left.
func (m Model) loadRepo(r *repoView) Model {
	m.svc, m.feed, m.cfg = r.svc, r.feed, r.cfg
	if r.views == nil {
		r.views = map[model.CheckoutKey]*worktreeView{}
	}
	m.views, m.viewed = r.views, r.viewed
	m.branches, m.remoteBranches, m.tags, m.commits, m.worktrees = r.branches, r.remoteBranches, r.tags, r.commits, r.worktrees
	m.previews = r.previews
	m.graphLayer, m.segLayer = r.graphLayer, r.segLayer
	m.commitScopeBranches, m.commitFilter, m.feedScopeApplied = r.commitScopeBranches, r.commitFilter, r.feedScopeApplied
	m.filterMemo = r.filterMemo
	if m.filterMemo == nil {
		m.filterMemo = &commitFilterMemo{}
	}
	m.commitCompareSet, m.previewCompareSet, m.mark = r.commitCompareSet, r.previewCompareSet, r.mark
	m.branchFilters, m.bfSlotsLoaded, m.bfCfgApplied = r.branchFilters, r.bfSlotsLoaded, r.bfCfgApplied
	m.branchFilterSlot = r.branchFilterSlot
	if m.branchFilterSlot == nil {
		m.branchFilterSlot = map[panel]int{}
	}
	m.bfMemo = r.bfMemo
	if m.bfMemo == nil {
		m.bfMemo = &branchFilterMemos{}
	}
	m.notices, m.driftNotices, m.noticesUnread = r.notices, r.driftNotices, r.noticesUnread
	m.noticeSessionDismissed = r.noticeSessionDismissed
	if m.noticeSessionDismissed == nil {
		m.noticeSessionDismissed = map[string]bool{}
	}
	m.repoHealth, m.repoHealthKnown, m.toolStatuses = r.repoHealth, r.repoHealthKnown, r.toolStatuses
	m.forgeShown, m.forgeProvider, m.forgeProbeKicked = r.forgeShown, r.forgeProvider, r.forgeProbeKicked
	m.prs, m.prsErr, m.prsLoaded, m.prsAnswered = r.prs, r.prsErr, r.prsLoaded, r.prsAnswered
	m.remoteTagNames, m.identity, m.linkRepoName = r.remoteTagNames, r.identity, r.linkRepoName
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	for _, p := range repoPanels {
		m.sel[p] = r.sel[p]
	}
	if m.ws != nil {
		for i, mem := range m.ws.members {
			if mem == r {
				m.ws.live = i
			}
		}
	}
	return m
}

// closeMembers drops every member slot of the open workspace (a reRoot,
// a leave): the Model is no longer in a workspace. Member watchers are
// closed by the caller that owns them (member_watch.go).
func (m Model) closeMembers() Model {
	if m.ws != nil {
		for _, r := range m.ws.members {
			r.closeWatch()
		}
	}
	m.ws = nil
	return m
}
