package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/gitwatch"
)

func TestWatchSourceKeyMapping(t *testing.T) {
	t.Parallel()
	cases := map[gitwatch.Source]sourceKey{
		gitwatch.Worktrees: srcWorktrees,
		gitwatch.Reflog:    srcReflog,
		gitwatch.Branches:  srcBranches,
		gitwatch.Remotes:   srcRemotes,
	}
	for in, want := range cases {
		if got := watchSourceKey(in); got != want {
			t.Errorf("watchSourceKey(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestEnabledWatchSourcesD2(t *testing.T) {
	t.Parallel()
	cfg := config.RefreshConfig{WorktreesWatch: true, ReflogWatch: true, BranchesWatch: true, RemotesWatch: true}
	got := enabledWatchSources(cfg)
	if len(got) != 4 {
		t.Fatalf("D2 should enable all four, got %v", got)
	}
}

func TestWatchAffectedSources(t *testing.T) {
	t.Parallel()
	has := func(ss []sourceKey, want sourceKey) bool {
		for _, s := range ss {
			if s == want {
				return true
			}
		}
		return false
	}
	// A branch ref change must also dirty the commit feed (the Commits panel's
	// %D decorations / tip markers come from the feed walk).
	if got := watchAffectedSources(srcBranches); !has(got, srcBranches) || !has(got, srcFeed) {
		t.Errorf("branches should affect {branches, feed}, got %v", got)
	}
	// A remote ref change (fetch) also dirties the feed (remote refs / ▲ markers)
	// AND the branch list (model.Branch carries Upstream/Ahead/Behind).
	if got := watchAffectedSources(srcRemotes); !has(got, srcRemotes) || !has(got, srcFeed) || !has(got, srcBranches) {
		t.Errorf("remotes should affect {remotes, feed, branches}, got %v", got)
	}
	// A worktree change dirties the branch list (new branch + worktree-path column)
	// and the feed (the new branch's decoration when only this watcher fired).
	if got := watchAffectedSources(srcWorktrees); !has(got, srcWorktrees) || !has(got, srcBranches) || !has(got, srcFeed) {
		t.Errorf("worktrees should affect {worktrees, branches, feed}, got %v", got)
	}
	// Reflog is self-contained: HEAD/ref consequences are caught by the branches
	// watcher (refs/heads + $W/HEAD), so reflog only refreshes the reflog panel.
	if got := watchAffectedSources(srcReflog); len(got) != 1 || got[0] != srcReflog {
		t.Errorf("reflog should affect only {reflog}, got %v", got)
	}
}

func TestWatchBranchEventAlsoRefreshesFeed(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.cfg.Refresh = config.RefreshConfig{Enabled: true, BranchesWatch: true}
	m.watchSupported = true
	m.watchGen = 1
	m.watcher = &gitwatch.Watcher{}
	m2, _ := m.Update(watchEventMsg{gen: 1, source: srcBranches})
	mm := m2.(Model)
	var sawBranches, sawFeed bool
	for _, it := range mm.bgQueue {
		switch it.source {
		case srcBranches:
			sawBranches = true
		case srcFeed:
			sawFeed = true
		}
	}
	if !sawBranches || !sawFeed {
		t.Errorf("a branch watch event must enqueue both branches and feed; queue=%v", mm.bgQueue)
	}
}

func TestWatchEventEnqueuesWhenActive(t *testing.T) {
	t.Parallel()
	m := newTestModel(t) // existing helper wired to a real temp repo
	m.cfg.Refresh = config.RefreshConfig{Enabled: true, WorktreesWatch: true}
	m.watchSupported = true
	m.watchGen = 1
	// Simulate a watcher present so the handler doesn't early-return.
	// (watcher pointer non-nil is required; a zero-value Watcher is fine for this
	// branch because watchListenCmd is the returned cmd, not invoked here.)
	m.watcher = &gitwatch.Watcher{}
	m2, _ := m.Update(watchEventMsg{gen: 1, source: srcWorktrees})
	mm := m2.(Model)
	found := false
	for _, it := range mm.bgQueue {
		if it.source == srcWorktrees {
			found = true
		}
	}
	if !found {
		t.Error("watchEventMsg should enqueue an active source into bgQueue")
	}
}

func TestWatchEventIgnoresStaleGen(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.cfg.Refresh = config.RefreshConfig{Enabled: true, WorktreesWatch: true}
	m.watchSupported = true
	m.watchGen = 2
	m.watcher = &gitwatch.Watcher{}
	m2, _ := m.Update(watchEventMsg{gen: 1, source: srcWorktrees}) // stale
	if len(m2.(Model).bgQueue) != 0 {
		t.Error("stale-gen watch event must be ignored")
	}
}
