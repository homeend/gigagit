package tui

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repos"
)

// commitsPagedMsg signals a commit page load completed; gen ties it to the
// feed generation that issued it so a reload mid-page is dropped.
type commitsPagedMsg struct{ gen int }

// loadMoreCmd loads the next commit page off the UI thread.
func (m Model) loadMoreCmd() tea.Cmd {
	feed := m.feed
	gen := feed.Gen()
	return func() tea.Msg {
		_, _, _ = feed.LoadMore(context.Background())
		return commitsPagedMsg{gen: gen}
	}
}

// dataLoadedMsg carries a full repo snapshot loaded off the UI thread. gen is
// the load generation it was issued for; a result whose gen no longer matches
// the model's loadGen is stale (superseded by a newer load) and dropped.
type dataLoadedMsg struct {
	gen             int
	status          model.WorkingTreeStatus
	branches        []model.Branch
	remoteBranches  []model.RemoteBranch
	commits         []model.Commit
	worktrees       []model.Worktree
	tags            []model.Tag
	reflog          []model.ReflogEntry
	currentWorktree string
	cfg             config.Config
	gitCommonDir    string
	headTimes       map[string]int64
	conflict        domain.ConflictState
	err             error

	commitsExhausted bool
	commitErr        error
}

// loadCmd loads the repo snapshot and initial commit feed page concurrently via
// the domain layer (gated, parallel, coalesced) and, on success, layers in the
// non-git config + MRU touch. It bakes in the current loadGen so a stale
// result can be dropped.
func (m Model) loadCmd() tea.Cmd {
	svc := m.svc
	feed := m.feed
	statePath := m.statePath
	gen := m.loadGen
	return func() tea.Msg {
		ctx := context.Background()
		// Resolve config BEFORE the feed's first walk so commit_initial_count
		// governs the first paint. config.Load needs the repo toplevel; fetch it
		// up front (cheap; the gated Snapshot reads its own toplevel too).
		cfg := config.Defaults()
		top, topErr := svc.TopLevel(ctx)
		if topErr == nil && top != "" {
			if c, cfgErr := config.Load(config.DefaultGlobalPath(), filepath.Join(top, ".gg.toml")); cfgErr == nil {
				cfg = c
			}
		}
		feed.SetPageSizes(cfg.UI.CommitInitialCount, cfg.UI.CommitBatchSize)
		// Apply the EOL-only visibility setting before the Snapshot below reads
		// status, so the Files panel and count badge honor it from first paint.
		svc.SetShowEOLOnlyChanges(cfg.UI.ShowEOLOnlyChanges)

		var (
			snap    domain.Snapshot
			snapErr error
			fs      domain.FeedState
			feedErr error
			wg      sync.WaitGroup
		)
		wg.Add(2)
		go func() { defer wg.Done(); snap, snapErr = svc.Snapshot(ctx) }()
		go func() { defer wg.Done(); fs, feedErr = feed.LoadInitial(ctx) }()
		wg.Wait()
		if snapErr != nil {
			return dataLoadedMsg{gen: gen, err: snapErr}
		}
		out := dataLoadedMsg{
			gen:              gen,
			status:           snap.Status,
			branches:         snap.Branches,
			remoteBranches:   snap.RemoteBranches,
			worktrees:        snap.Worktrees,
			tags:             snap.Tags,
			reflog:           snap.Reflog,
			currentWorktree:  snap.CurrentWorktree,
			gitCommonDir:     snap.GitCommonDir,
			headTimes:        snap.HeadTimes,
			conflict:         snap.Conflict,
			commits:          fs.Commits,
			commitsExhausted: fs.Exhausted,
			commitErr:        feedErr,
			cfg:              cfg,
		}
		// MRU touch + reflog re-read are not git-status reads; do them after the
		// gated snapshot, keyed off the toplevel it reported.
		if snap.CurrentWorktree != "" {
			_ = repos.Touch(statePath, snap.CurrentWorktree, time.Now())
			if n := cfg.UI.ReflogLimit; n > 0 {
				if rl, err := svc.Reflog(ctx, n); err == nil {
					out.reflog = rl
				}
			}
		}
		return out
	}
}
