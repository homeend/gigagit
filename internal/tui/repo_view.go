package tui

import (
	"path/filepath"

	"github.com/homeend/gigagit/internal/branchfilter"
	"github.com/homeend/gigagit/internal/commitgraph"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
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
	viewed, home     model.CheckoutKey // the slot on screen / gg's own, while this member is live
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

	loaded  bool // its first load landed (false: the member's panels show the loading marker)
	missing bool // its path did not exist when the workspace opened
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
	if m.ws != nil {
		for _, r := range m.ws.members {
			if r.views != nil && (r.views[m.home] != nil || r == m.homeRepoView) {
				return r
			}
		}
	}
	return m.homeRepoView
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
	r := newRepoView(m.mainWorktreePath(homePath), m.svc)
	r.feed = m.feed
	r.views = m.views
	r.viewed, r.home = m.viewed, m.home
	r.cfg = m.cfg
	r.loaded = true
	m.homeRepoView = r
	return m
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
	if main != r.mainPath {
		r.mainPath, r.key, r.name = main, model.KeyOf(main), filepath.Base(main)
	}
	return m
}

// saveRepo copies the live repository-wide fields into the live member's
// slot. A no-op before the first load seeded a slot.
func (m Model) saveRepo() Model {
	r := m.liveRepo()
	if r == nil {
		return m
	}
	r.svc, r.feed, r.cfg = m.svc, m.feed, m.cfg
	r.views, r.viewed, r.home = m.views, m.viewed, m.home
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
	m.views, m.viewed, m.home = r.views, r.viewed, r.home
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
	m.ws = nil
	return m
}
