package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func intPtr(v int) *int { return &v }

func forgeOK() domain.ForgeStatus { return domain.ForgeStatus{Provider: "github"} }

func testPRs() []model.PullRequest {
	return []model.PullRequest{
		{Number: 7, Title: "Add streaming parser", Author: "octocat", State: model.PRStateOpen,
			Source: "feat/x", Target: "main", ReviewState: "approved", URL: "https://github.com/o/r/pull/7",
			Updated: time.Unix(1_700_000_000, 0)},
		{Number: 12, Title: "Old work", Author: "hubot", State: model.PRStateMerged,
			Source: "old", Target: "main", URL: "https://github.com/o/r/pull/12",
			Updated: time.Unix(1_600_000_000, 0)},
	}
}

// Ruling 13: the pull-request poll has its own switch. The master gate that
// turns every other interval off must not reach it.
func TestPRsDueIgnoresMasterSwitch(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_000_000, 0)
	cfg := config.RefreshConfig{Enabled: false, Status: 10}
	if d := dueItems(t0, map[refreshItem]time.Time{}, cfg, false, false); len(d) != 0 {
		t.Fatalf("master off: the ordinary items stay off, got %v", d)
	}
	if !prsDue(t0, map[refreshItem]time.Time{}, cfg) {
		t.Fatal("the PR poll must be due with the master switch off (default 300s, never run)")
	}
	last := map[refreshItem]time.Time{prsItem: t0}
	if prsDue(t0.Add(299*time.Second), last, cfg) {
		t.Fatal("not due before 300s")
	}
	if !prsDue(t0.Add(300*time.Second), last, cfg) {
		t.Fatal("due at 300s")
	}
}

func TestPRsDueOffAtZeroAndFlooredByMinSeconds(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_000_000, 0)
	off := config.RefreshConfig{PRs: intPtr(0)}
	if prsDue(t0.Add(time.Hour), map[refreshItem]time.Time{}, off) {
		t.Fatal("prs = 0 turns the poll off")
	}
	fast := config.RefreshConfig{PRs: intPtr(1), MinSeconds: 60}
	last := map[refreshItem]time.Time{prsItem: t0}
	if prsDue(t0.Add(30*time.Second), last, fast) {
		t.Fatal("prs = 1 is floored by min_seconds = 60")
	}
	if !prsDue(t0.Add(60*time.Second), last, fast) {
		t.Fatal("due at the floor")
	}
}

// dueItems never yields the PR item: it is gated on a usable forge, which only
// the Model knows.
func TestDueItemsNeverYieldsPRs(t *testing.T) {
	t.Parallel()
	cfg := config.RefreshConfig{Enabled: true, PRs: intPtr(10)}
	for _, it := range dueItems(time.Unix(5_000_000, 0), map[refreshItem]time.Time{}, cfg, false, false) {
		if it.isPRs {
			t.Fatal("dueItems returned the PR item")
		}
	}
}

func TestRefreshTickSkipsPRsWithoutAForge(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.loading = false
	m.cfg.Refresh = config.RefreshConfig{}
	m, cmd := m.refreshTick(time.Unix(3_000_000, 0))
	if cmd != nil || m.bgBusy || m.prsInflight {
		t.Fatalf("no forge: the tick must not read PRs (cmd=%v busy=%v inflight=%v)", cmd != nil, m.bgBusy, m.prsInflight)
	}
}

func TestRefreshTickReadsPRsWhenTheForgeIsShown(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.loading = false
	m.forgeShown = true
	m.cfg.Refresh = config.RefreshConfig{} // master OFF
	m, cmd := m.refreshTick(time.Unix(3_000_000, 0))
	if cmd == nil || !m.bgBusy || !m.bgActiveItem.isPRs || !m.prsInflight {
		t.Fatalf("want a background PR read (cmd=%v busy=%v item=%+v inflight=%v)", cmd != nil, m.bgBusy, m.bgActiveItem, m.prsInflight)
	}
	// A second tick while it is in flight starts nothing.
	if _, cmd2 := m.refreshTick(time.Unix(3_000_400, 0)); cmd2 != nil {
		t.Fatal("a PR read is in flight: no second read")
	}
}

func TestPRsLoadedRevealsTheTabOnce(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	if got := len(m.leftTabs()); got != 4 {
		t.Fatalf("hidden: leftTabs = %d, want 4", got)
	}
	m.prsGen, m.prsInflight = 1, true
	nm, _ := m.Update(prsLoadedMsg{gen: 1, status: domain.ForgeStatus{Provider: "github"}, prs: testPRs()})
	m = nm.(Model)
	if !m.forgeShown || len(m.prs) != 2 || m.prsInflight || m.prsErr != "" {
		t.Fatalf("shown=%v prs=%d inflight=%v err=%q", m.forgeShown, len(m.prs), m.prsInflight, m.prsErr)
	}
	if tabs := m.leftTabs(); len(tabs) != 5 || tabs[4] != panelPRs {
		t.Fatalf("leftTabs = %v, want PRs last", tabs)
	}
	// A later failure keeps the tab AND the old list; the error is a row.
	m.prsGen, m.prsInflight = 2, true
	nm, _ = m.Update(prsLoadedMsg{gen: 2, status: domain.ForgeStatus{Provider: "github"}, err: errors.New("HTTP 502\nmore")})
	m = nm.(Model)
	if !m.forgeShown || len(m.prs) != 2 || m.prsErr != "HTTP 502" {
		t.Fatalf("after failure: shown=%v prs=%d err=%q", m.forgeShown, len(m.prs), m.prsErr)
	}
	if m.statusMsg != "" {
		t.Fatalf("a background failure must stay off the status line, got %q", m.statusMsg)
	}
}

func TestPRsLoadedUnavailableStaysHidden(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.prsGen, m.prsInflight = 1, true
	nm, _ := m.Update(prsLoadedMsg{gen: 1, status: domain.ForgeStatus{Err: errors.New("gh: not logged in")}})
	m = nm.(Model)
	if m.forgeShown || m.statusMsg != "" || m.prsInflight || len(m.leftTabs()) != 4 {
		t.Fatalf("shown=%v status=%q inflight=%v", m.forgeShown, m.statusMsg, m.prsInflight)
	}
}

func TestPRsLoadedStaleAndCancelledAreDropped(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.forgeShown, m.prs = true, testPRs()
	m.prsGen, m.prsInflight = 3, true
	// The stale arrival is the BACKGROUND lane's read (a repo switch bumped the
	// generation under it): its data is dropped, but it must still free the
	// lane — nothing else will, and every background poll would stop.
	m.bgBusy, m.bgActiveItem = true, prsItem
	nm, _ := m.Update(prsLoadedMsg{gen: 2, bg: true, status: domain.ForgeStatus{Provider: "github"}})
	m = nm.(Model)
	if len(m.prs) != 2 || !m.prsInflight {
		t.Fatalf("a stale read must change nothing (prs=%d inflight=%v)", len(m.prs), m.prsInflight)
	}
	if m.bgBusy {
		t.Fatal("a stale background read must still free the lane")
	}
	nm, _ = m.Update(prsLoadedMsg{gen: 3, status: domain.ForgeStatus{Provider: "github"}, err: context.Canceled})
	m = nm.(Model)
	if len(m.prs) != 2 || m.prsErr != "" || m.prsInflight {
		t.Fatalf("a pre-empted read is not a failure (prs=%d err=%q inflight=%v)", len(m.prs), m.prsErr, m.prsInflight)
	}
}

func TestPRsLoadedFreesTheBackgroundLane(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.forgeShown = true
	m.prsGen, m.prsInflight, m.bgBusy, m.bgActiveItem = 1, true, true, prsItem
	nm, _ := m.Update(prsLoadedMsg{gen: 1, bg: true, dur: time.Second, status: domain.ForgeStatus{Provider: "github"}})
	m = nm.(Model)
	if m.bgBusy {
		t.Fatal("the lane must be freed")
	}
	if len(m.refreshDur[prsItem]) != 1 {
		t.Fatal("a background read feeds the rates table")
	}
}

func TestPRsRefreshKeysAndSettingsRow(t *testing.T) {
	t.Parallel()
	if got := refreshTomlKey(prsItem); got != "prs" {
		t.Fatalf("toml key = %q", got)
	}
	cfg := config.RefreshConfig{}
	if got := refreshIntervalFor(cfg, prsItem); got != 300 {
		t.Fatalf("interval = %d, want 300", got)
	}
	setRefreshIntervalField(&cfg, prsItem, 0)
	if got := refreshIntervalFor(cfg, prsItem); got != 0 {
		t.Fatalf("after set 0 = %d", got)
	}
	found := false
	for _, it := range scheduledItems {
		found = found || it == prsItem
	}
	if !found {
		t.Fatal("the PR item must be a Settings refresh-rates row")
	}
}
