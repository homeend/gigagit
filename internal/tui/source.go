package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
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
	srcStatus:    {panelFiles, panelStaged},
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
	source sourceKey
	gen    int
	value  any
	manual bool
	err    error
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

// readSourceCmd reads one source off the UI thread via the gated domain layer
// and returns a dataAvailableMsg. gen is captured now so a result superseded by
// a newer read of the same source is dropped by the handler. Derived data that
// the old Snapshot computed alongside a read (conflict for status, head-times
// for worktrees) is computed here so a per-source refresh is self-contained.
func (m Model) readSourceCmd(s sourceKey, manual bool) tea.Cmd {
	svc := m.svc
	feed := m.feed
	reflogLimit := m.cfg.UI.ReflogLimit
	gen := m.srcGen[s]
	return func() tea.Msg {
		ctx := context.Background()
		out := dataAvailableMsg{source: s, gen: gen, manual: manual}
		switch s {
		case srcStatus:
			st, err := svc.Status(ctx)
			if err != nil {
				out.err = err
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
		return out
	}
}
