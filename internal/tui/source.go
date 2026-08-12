package tui

import (
	"context"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repos"
)

// sourceKey identifies one independently-refreshable data source. Each maps to a
// single gated domain query and feeds one or more panels (see srcConsumers). It
// is the unit of the reactive refresh layer: a read of a source emits a
// dataAvailableMsg, and every consuming panel re-renders from the stored value.
type sourceKey int

const (
	srcStatus sourceKey = iota
	srcBranches
	srcRemotes
	srcTags
	srcReflog
	srcWorktrees
	srcFeed
	srcIdentity
	srcCount
)

// srcConsumers maps a source to the panels that render from it. Used to target
// the manual-refresh spinner and, in Phase B, to decide which sources a timer
// polls. srcIdentity feeds the Settings popup, not a left panel, so it is absent.
var srcConsumers = map[sourceKey][]panel{
	srcStatus:    {panelFiles, panelStaged, panelCommits},
	srcBranches:  {panelBranches, panelCommits},
	srcRemotes:   {panelRemotes},
	srcTags:      {panelTags},
	srcReflog:    {panelReflog},
	srcWorktrees: {panelWorktrees, panelBranches},
	srcFeed:      {panelCommits},
}

// dataAvailableMsg is the single event every source read produces. value is
// typed per source and asserted in the handler; gen ties the result to the read
// that issued it (stale gens are dropped); manual=true means a user-initiated
// read whose spinner must be cleared on arrival (false = silent, Phase B).
type dataAvailableMsg struct {
	source  sourceKey
	gen     int
	value   any
	manual  bool
	startup bool          // part of the app-start fan-out; its duration is NOT measured (parallel/contended)
	dur     time.Duration // wall-clock of the domain read (Phase C measurement)
	err     error
}

// statusPayload carries a working-tree status together with the conflict state
// derived from it, so a per-source status refresh is self-contained.
type statusPayload struct {
	status   model.WorkingTreeStatus
	conflict domain.ConflictState
}

// worktreesPayload carries the worktree list together with the per-head
// committer timestamps derived from it, so a per-source worktrees refresh is
// self-contained (mirroring what Snapshot's worktrees arm computed).
type worktreesPayload struct {
	worktrees []model.Worktree
	headTimes map[string]int64
}

// feedPayload carries the initial commit feed page produced by LoadInitial.
type feedPayload struct {
	commits   []model.Commit
	exhausted bool
}

// anySourceLoading reports whether any source is mid manual-refresh. It backs
// the derived m.loading flag (the legacy action-blocking gate) and the
// per-panel spinner targeting (Task 8).
func (m Model) anySourceLoading() bool {
	for _, v := range m.srcLoading {
		if v {
			return true
		}
	}
	return false
}

// maybeFeedUpstreamRewalk reports whether the one-time "re-walk the feed now
// that tracked upstreams are known" should fire. It is true only when upstreams
// exist, the applied scope is stale, AND no srcFeed read is in flight. The
// in-flight guard serializes the initial LoadInitial against the scoped re-walk
// so they never write the feed concurrently (Blocker 2). Both the srcBranches
// and srcFeed arrival handlers call it; whichever lands last with the guard
// clear fires the re-walk exactly once.
func (m Model) maybeFeedUpstreamRewalk() bool {
	return len(m.feedUpstreams()) > 0 &&
		m.feedScopeApplied != m.feedScopeSig() &&
		!m.srcInflight[srcFeed]
}

// sourceNames maps each source key to its English name — the identity/fallback
// behind sourceDisplayName and the reference for its passthrough test.
// Package-level to avoid reallocating the literal on every error.
var sourceNames = map[sourceKey]string{
	srcStatus: "status", srcBranches: "branches", srcRemotes: "remotes",
	srcTags: "tags", srcReflog: "reflog", srcWorktrees: "worktrees",
	srcFeed: "commits", srcIdentity: "identity",
}

// sourceErr formats a per-source error for display on the status line.
func sourceErr(s sourceKey, err error) string {
	return sourceDisplayName(s) + ": " + err.Error()
}

// readSourceCmd reads one source off the UI thread via the gated domain layer
// and returns a dataAvailableMsg. gen is captured now so a result superseded by
// a newer read of the same source is dropped by the handler. Derived data that
// the old Snapshot computed alongside a read (conflict for status, head-times
// for worktrees) is computed here so a per-source refresh is self-contained.
// ctx is provided by the caller: background reads pass m.bgCtx (cancellable by
// a starting user op); manual reads pass context.Background() (never cancelled).
func (m Model) readSourceCmd(ctx context.Context, s sourceKey, manual, startup bool) tea.Cmd {
	svc := m.svc
	feed := m.feed
	reflogLimit := m.cfg.UI.ReflogLimit
	gen := m.srcGen[s]
	return func() tea.Msg {
		start := time.Now()
		out := dataAvailableMsg{source: s, gen: gen, manual: manual, startup: startup}
		switch s {
		case srcStatus:
			st, err := svc.Status(ctx)
			if err != nil {
				out.err = err
				out.dur = time.Since(start)
				return out
			}
			out.value = statusPayload{status: st, conflict: svc.Conflict(ctx, st)}
		case srcBranches:
			bs, err := svc.Branches(ctx)
			out.value, out.err = bs, err
		case srcRemotes:
			rbs, err := svc.RemoteBranches(ctx)
			out.value, out.err = rbs, err
		case srcTags:
			tags, err := svc.Tags(ctx)
			out.value, out.err = tags, err
		case srcReflog:
			// svc.Reflog maps limit<=0 to defaultReflogLimit internally.
			rl, err := svc.Reflog(ctx, reflogLimit)
			out.value, out.err = rl, err
		case srcWorktrees:
			wts, err := svc.Worktrees(ctx)
			if err != nil {
				out.err = err
				out.dur = time.Since(start)
				return out
			}
			shas := make([]string, 0, len(wts))
			for _, w := range wts {
				if w.Head != "" {
					shas = append(shas, w.Head)
				}
			}
			times, _ := svc.CommitTimes(ctx, shas) // best-effort, as in loadSnapshot
			if times == nil {
				times = map[string]int64{}
			}
			out.value = worktreesPayload{worktrees: wts, headTimes: times}
		case srcFeed:
			fs, err := feed.LoadInitial(ctx)
			out.value = feedPayload{commits: fs.Commits, exhausted: fs.Exhausted}
			out.err = err
		case srcIdentity:
			id, err := svc.Identity(ctx)
			out.value, out.err = id, err
		}
		out.dur = time.Since(start)
		return out
	}
}

// anySourceInflight reports whether any source read is currently in flight.
// Used by the r handler to block duplicate fan-out reads.
func (m Model) anySourceInflight() bool {
	for _, v := range m.srcInflight {
		if v {
			return true
		}
	}
	return false
}

// reloadSourcesCmd bumps each source's generation, marks it in-flight (and, if
// manual, loading), and returns a batch that reads them all concurrently. The
// per-source gen bump means any older in-flight read of the same source is
// dropped when it lands.
func (m Model) reloadSourcesCmd(srcs []sourceKey, manual, startup bool) (Model, tea.Cmd) {
	// Defensive lazy-init: a Model built as a literal in a test (rather than via
	// the constructor patched in Task 1) would panic on the nil-map writes below.
	if m.srcGen == nil {
		m.srcGen = map[sourceKey]int{}
	}
	if m.srcInflight == nil {
		m.srcInflight = map[sourceKey]bool{}
	}
	if m.srcLoading == nil {
		m.srcLoading = map[sourceKey]bool{}
	}
	cmds := make([]tea.Cmd, 0, len(srcs))
	for _, s := range srcs {
		m.srcGen[s]++
		m.srcInflight[s] = true
		if manual {
			m.srcLoading[s] = true
		}
		cmds = append(cmds, m.readSourceCmd(context.Background(), s, manual, startup))
	}
	// Keep the legacy action-blocking flag in sync (see the handler note in Task 4).
	m.loading = m.anySourceLoading()
	return m, tea.Batch(cmds...)
}

// sourcesOrAll returns srcs, or every source when srcs is nil (the safe default
// for any op not explicitly mapped — correctness never regresses, only speed).
func sourcesOrAll(srcs []sourceKey) []sourceKey {
	if srcs != nil {
		return srcs
	}
	all := make([]sourceKey, 0, srcCount)
	for s := sourceKey(0); s < srcCount; s++ {
		all = append(all, s)
	}
	return all
}

// reloadAllCmd refreshes every source — the registry's "reload everything" (r,
// and the post-bootstrap startup fan-out).
func (m Model) reloadAllCmd(manual, startup bool) (Model, tea.Cmd) {
	all := make([]sourceKey, 0, srcCount)
	for s := sourceKey(0); s < srcCount; s++ {
		all = append(all, s)
	}
	return m.reloadSourcesCmd(all, manual, startup)
}

// opAffectedSources returns the sources an operation dirties (nil = all, the
// safe default). Centralizes the post-op refresh mapping so the hottest ops
// (commit, push) refresh only what they changed instead of every source.
func opAffectedSources(op engine.Operation) []sourceKey {
	switch op.(type) {
	case engine.Commit:
		return []sourceKey{srcStatus, srcFeed, srcBranches}
	case engine.Push:
		// A push moves the remote-tracking ref, so the feed's %D decorations
		// and ↓/↑ local/remote tip markers change — refresh it too. NOT tags:
		// pushing a branch doesn't push tags, and a tags reload would auto-fire
		// a background ls-remote (the ▲ pushed-state lookup) for nothing.
		return []sourceKey{srcBranches, srcRemotes, srcFeed}
	case engine.Fetch:
		return []sourceKey{srcRemotes}
	case engine.AddFetchMappings:
		// New remote-tracking refs appear (Remotes panel, the feed's %D
		// decorations/↓↑ markers) and tracked branches gain ahead/behind
		// (Branches). Mapped explicitly so it doesn't fall through to "all
		// sources" and auto-fire the srcTags remote-tags network probe.
		return []sourceKey{srcBranches, srcRemotes, srcFeed}
	case engine.CreateWorktree, engine.CreateWorktreeForBranch:
		return []sourceKey{srcBranches, srcWorktrees}
	case engine.RemoveWorktree:
		return []sourceKey{srcBranches, srcWorktrees}
	case engine.MoveWorktree:
		// The worktree list changed; Branches shows per-branch worktree markers.
		// (A current-worktree move chains a full reRoot before this is consulted.)
		return []sourceKey{srcBranches, srcWorktrees}
	case engine.RemoveGitLocks:
		// Removing a lockfile changes no git state of its own — git's lock
		// protocol means the killed write was never applied. But the process
		// that died may have completed work gg never observed, and the panels
		// have been showing whatever was cached since, so re-read status.
		// Explicit rather than falling through to "all sources", which would
		// auto-fire the srcTags remote-tags network probe.
		return []sourceKey{srcStatus}
	case engine.RepairWorktree:
		// Only the worktree admin metadata changed. (The success path chains
		// a full reRoot before this mapping is consulted; this covers the
		// failure path without a full-reload + remote-tags probe.)
		return []sourceKey{srcWorktrees}
	case engine.SetIdentity:
		return []sourceKey{srcIdentity}
	case engine.AbortApply:
		// Only the working tree changes (conflicted files reset to HEAD; no
		// ref moves). Mapped so it doesn't fall through to "all sources" and
		// auto-fire the srcTags remote-tags network probe.
		return []sourceKey{srcStatus}
	case engine.SmartMerge, engine.SmartRebase:
		return []sourceKey{srcStatus, srcFeed, srcBranches}
	case engine.CherryPick:
		// Moves the branch tip and may leave conflicts, same shape as
		// SmartMerge/SmartRebase. Mapping it avoids falling through to "all
		// sources", which would auto-fire the srcTags-arrival remote-tags
		// probe (a needless network round-trip) after every cherry-pick.
		return []sourceKey{srcStatus, srcFeed, srcBranches}
	case engine.ApplyPatch:
		// Commits mode moves the branch tip and adds commits; working-tree
		// mode changes status (possibly to conflicted). One op covers both,
		// so refresh the union.
		return []sourceKey{srcStatus, srcFeed, srcBranches}
	case engine.DeleteBranch:
		// Branch-only ref change: refresh the Branches panel and the feed (its
		// %D ref decorations and tip markers move). NOT tags — leaving these
		// unmapped fell through to "all sources", and the tags reload
		// auto-triggered a background ls-remote (the ▲ pushed-state lookup) on
		// every branch delete/rename, a needless network round-trip. NOT
		// worktrees either: git refuses to delete a branch checked out in any
		// worktree, so a delete can never change the worktree list.
		return []sourceKey{srcBranches, srcFeed}
	case engine.RenameBranch:
		// Same Branches+feed+no-tags rationale as DeleteBranch, but a rename
		// reaches further: `git branch -m` follows the branch into any worktree
		// that has it checked out, and both the Branches panel's worktree
		// annotation and the Worktrees panel render from the cached worktree
		// list — without srcWorktrees the marker vanished until a manual
		// refresh. Renaming the CURRENT branch also changes the header's
		// "branch <name>" segment, which renders from srcStatus. One op covers
		// both, so refresh the union.
		return []sourceKey{srcStatus, srcBranches, srcFeed, srcWorktrees}
	case engine.DeleteRemoteBranch:
		// The remote-tracking ref vanishes (Remotes panel + the feed's %D
		// decorations/tip markers), and a local branch tracking it loses its
		// upstream/ahead-behind (Branches). Same no-tags rationale as
		// DeleteBranch: unmapped, this fell through to "all sources" and
		// auto-fired the remote-tags ls-remote probe after every delete.
		return []sourceKey{srcBranches, srcRemotes, srcFeed}
	case engine.RestoreBranchVersion:
		// Moves a branch tip (current branch: a hard reset that also touches
		// the working tree; another branch: update-ref) and may recreate a
		// deleted branch — refresh status, the branch list, the feed (%D
		// decorations/tip markers), and worktrees (a recreated branch could
		// be one a worktree tracks).
		return []sourceKey{srcStatus, srcBranches, srcFeed, srcWorktrees}
	case engine.DeleteBranchVersion:
		// Removes a refs/gg/versions/... ref only — no panel shows these,
		// so nothing needs a reload.
		return []sourceKey{}
	case engine.ExportFile, engine.ExportToDir:
		return []sourceKey{} // writes outside the working tree; refresh nothing
	case engine.WriteCommitGraph, engine.SetGitConfig:
		// A commit-graph write / config set changes no panel-visible data —
		// reloading all sources would also fire the srcTags-arrival remote-tags
		// probe: a needless network call right after fixing a huge repo.
		// Note: the git-config explorer's user.name/user.email writes DO feed
		// srcIdentity, but the explorer bypasses startOp entirely (it runs
		// through gitConfigWriteCmd, a stageCmd-style synchronous op-execute,
		// not this opAffectedSources table) and the identity view re-reads on
		// open anyway — so this mapping stays refresh-nothing. Revisit if a
		// config write is ever routed through startOp.
		return []sourceKey{}
	}
	return nil // unmapped → all sources (safe)
}

// configReadyMsg carries the loaded config from bootstrapCmd; its handler
// applies it and fans out the first all-source read.
type configReadyMsg struct {
	cfg      config.Config
	repoTOML string // active per-repo write target: private user-dir file if present, else <repo-top>/.gg.toml; "" if not in a repo
	top      string // git working-tree root (== Snapshot.CurrentWorktree); "" if not in a repo
}

// bootstrapCmd loads config and applies the settings the first reads depend on
// (feed page sizes, EOL-only visibility) plus the MRU touch, then emits
// configReadyMsg. This preserves the ordering loadCmd had: config BEFORE the
// feed walk and status read.
func (m Model) bootstrapCmd() tea.Cmd {
	svc := m.svc
	feed := m.feed
	statePath := m.statePath
	return func() tea.Msg {
		ctx := context.Background()
		cfg := config.Defaults()
		repoTOML, root := "", ""
		if top, err := svc.TopLevel(ctx); err == nil && top != "" {
			root = top
			committed := filepath.Join(top, ".gg.toml")
			privatePath := ""
			if wts, werr := svc.Worktrees(ctx); werr == nil && len(wts) > 0 && wts[0].Path != "" {
				privatePath = config.PrivateRepoPath(wts[0].Path)
			}
			// One active per-repo file: private if it exists, else committed. The
			// read path and the write target are the SAME path — no layering (a
			// committed inverted-polarity key must not shadow a private "off").
			repoTOML = config.ActiveRepoConfigPath(committed, privatePath)
			if c, cerr := config.Load(config.DefaultGlobalPath(), repoTOML); cerr == nil {
				cfg = c
			}
			if statePath != "" {
				_ = repos.Touch(statePath, top, time.Now())
			}
		}
		feed.SetPageSizes(cfg.UI.CommitInitialCount, cfg.UI.CommitBatchSize)
		feed.SetSortMode(cfg.UI.CommitSort)
		svc.SetShowEOLOnlyChanges(cfg.UI.ShowEOLOnlyChanges)
		svc.SetVersionsPolicy(versionsPolicyFromConfig(cfg))
		return configReadyMsg{cfg: cfg, repoTOML: repoTOML, top: root}
	}
}
