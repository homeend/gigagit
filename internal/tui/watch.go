package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/gitwatch"
)

// watchDebounce is the quiet-window before a file-change event is emitted to
// the TUI. 200 ms coalesces rapid burst writes (e.g. a fast `git fetch` that
// rewrites packed-refs + individual loose refs) into a single refresh trigger.
const watchDebounce = 200 * time.Millisecond

// watchReadyMsg carries a freshly-built watcher (or a nil watcher when watching
// is unsupported / no sources are enabled). gen drops a build superseded by a
// repo switch or a toggle.
type watchReadyMsg struct {
	gen       int
	watcher   *gitwatch.Watcher
	supported bool
}

// watchEventMsg is one debounced file-change for a source. gen ties it to the
// current watcher; a stale gen (after rebuild) is ignored.
type watchEventMsg struct {
	gen    int
	source sourceKey
}

// watchClosedMsg lands when a watcher's events channel closes (after Close).
type watchClosedMsg struct{ gen int }

// watchSourceKey maps a gitwatch.Source to the TUI's sourceKey.
func watchSourceKey(s gitwatch.Source) sourceKey {
	switch s {
	case gitwatch.Branches:
		return srcBranches
	case gitwatch.Remotes:
		return srcRemotes
	case gitwatch.Reflog:
		return srcReflog
	case gitwatch.Worktrees:
		return srcWorktrees
	}
	return srcStatus // unreachable; status is never watch-eligible
}

// watchAffectedSources expands a watch trigger's primary source into the full set
// of sources a change to it dirties — mirroring opAffectedSources for ops. A ref
// change (branches/remotes) also dirties the commit feed: the Commits panel's
// %D decorations and ■/▲ tip markers come from the feed walk, so without a feed
// reload a new branch shows in the Branches panel but not at its commit in the
// Commits panel. A worktree change also dirties the Branches panel (its
// worktree-path column). The list always leads with the primary source.
func watchAffectedSources(primary sourceKey) []sourceKey {
	switch primary {
	case srcBranches:
		// A head ref (or $W/HEAD) moved: branch list + the commit feed's %D
		// decorations and ■/▲ tip markers.
		return []sourceKey{srcBranches, srcFeed}
	case srcRemotes:
		// A remote-tracking ref moved (e.g. fetch): remote list + feed (remote refs
		// / ▲ markers) + branches (model.Branch carries Upstream/Ahead/Behind, which
		// a remote move changes).
		return []sourceKey{srcRemotes, srcFeed, srcBranches}
	case srcWorktrees:
		// A worktree was added/removed: worktree list + branches (a new branch from
		// `worktree add -b`, plus the worktree-path column) + feed (that new branch's
		// decoration, in case branch-watch is off and only this watcher fired).
		return []sourceKey{srcWorktrees, srcBranches, srcFeed}
	case srcReflog:
		// logs/HEAD appended. The reflog panel is the only direct consequence: any
		// HEAD/ref move that produced the entry is independently caught by the
		// branches watcher (which watches both refs/heads and $W/HEAD). Working-tree
		// status after a checkout/reset stays on polling (git status is too
		// expensive to run on every HEAD move in a huge repo).
		return []sourceKey{srcReflog}
	}
	return []sourceKey{primary}
}

// enabledWatchSources returns the gitwatch sources to watch: those that are both
// watch-eligible (implemented) and toggled on in config.
func enabledWatchSources(cfg config.RefreshConfig) []gitwatch.Source {
	var out []gitwatch.Source
	add := func(it refreshItem, s gitwatch.Source) {
		if watchEligible(it) && watchOn(cfg, it) {
			out = append(out, s)
		}
	}
	add(refreshItem{source: srcWorktrees}, gitwatch.Worktrees)
	add(refreshItem{source: srcReflog}, gitwatch.Reflog)
	add(refreshItem{source: srcBranches}, gitwatch.Branches)
	add(refreshItem{source: srcRemotes}, gitwatch.Remotes)
	return out
}

// startWatchCmd builds a watcher off the UI thread. It resolves the common and
// per-worktree git dirs, computes Supported, and — if supported and at least one
// source is enabled — constructs the watcher. Always returns a watchReadyMsg
// (watcher may be nil).
func (m Model) startWatchCmd(gen int) tea.Cmd {
	svc := m.svc
	cfg := m.cfg.Refresh
	return func() tea.Msg {
		ctx := context.Background()
		common, err := svc.GitCommonDir(ctx)
		if err != nil || common == "" {
			return watchReadyMsg{gen: gen, supported: false}
		}
		supported := gitwatch.Supported(common)
		if !supported {
			return watchReadyMsg{gen: gen, supported: false}
		}
		enabled := enabledWatchSources(cfg)
		if len(enabled) == 0 {
			return watchReadyMsg{gen: gen, supported: true}
		}
		worktreeDir, err := svc.GitDir(ctx)
		if err != nil || worktreeDir == "" {
			worktreeDir = common // fall back; reflog HEAD lives at common in the main worktree
		}
		w, werr := gitwatch.New(gitwatch.Plan(common, worktreeDir, enabled), watchDebounce)
		if werr != nil {
			return watchReadyMsg{gen: gen, supported: true} // nil watcher → polling fallback
		}
		return watchReadyMsg{gen: gen, watcher: w, supported: true}
	}
}

// watchListenCmd blocks until the watcher emits a source, then returns it. When
// the channel closes (watcher stopped), it returns watchClosedMsg so the loop
// ends cleanly.
func watchListenCmd(w *gitwatch.Watcher, gen int) tea.Cmd {
	return func() tea.Msg {
		s, ok := <-w.Events()
		if !ok {
			return watchClosedMsg{gen: gen}
		}
		return watchEventMsg{gen: gen, source: watchSourceKey(s)}
	}
}
