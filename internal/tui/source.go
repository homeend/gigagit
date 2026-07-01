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

// sourceNames maps each source key to its human-readable name, used for error
// messages. Package-level to avoid reallocating the literal on every error.
var sourceNames = map[sourceKey]string{
	srcStatus: "status", srcBranches: "branches", srcRemotes: "remotes",
	srcTags: "tags", srcReflog: "reflog", srcWorktrees: "worktrees",
	srcFeed: "commits", srcIdentity: "identity",
}

// sourceErr formats a per-source error for display on the status line.
func sourceErr(s sourceKey, err error) string {
	return sourceNames[s] + ": " + err.Error()
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
	case engine.CreateWorktree, engine.CreateWorktreeForBranch:
		return []sourceKey{srcBranches, srcWorktrees}
	case engine.RemoveWorktree:
		return []sourceKey{srcBranches, srcWorktrees}
	case engine.SetIdentity:
		return []sourceKey{srcIdentity}
	case engine.SmartMerge, engine.SmartRebase:
		return []sourceKey{srcStatus, srcFeed, srcBranches}
	case engine.DeleteBranch, engine.RenameBranch:
		// Branch-only ref change: refresh the Branches panel and the feed (its
		// %D ref decorations and tip markers move). NOT tags — leaving these
		// unmapped fell through to "all sources", and the tags reload
		// auto-triggered a background ls-remote (the ▲ pushed-state lookup) on
		// every branch delete/rename, a needless network round-trip.
		return []sourceKey{srcBranches, srcFeed}
	case engine.ExportFile, engine.ExportToDir:
		return []sourceKey{} // writes outside the working tree; refresh nothing
	}
	return nil // unmapped → all sources (safe)
}

// configReadyMsg carries the loaded config from bootstrapCmd; its handler
// applies it and fans out the first all-source read.
type configReadyMsg struct {
	cfg      config.Config
	repoTOML string // <repo-top>/.gg.toml, "" if not in a repo
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
			repoTOML = filepath.Join(top, ".gg.toml")
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
		return configReadyMsg{cfg: cfg, repoTOML: repoTOML, top: root}
	}
}
