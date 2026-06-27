package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

func TestDueItemsRespectsIntervalAndMaster(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	cfg := config.RefreshConfig{Enabled: true, Status: 30, Branches: 0}
	last := map[refreshItem]time.Time{{source: srcStatus}: t0}

	// 29s later: not due.
	if d := dueItems(t0.Add(29*time.Second), last, nil, cfg, false); len(d) != 0 {
		t.Fatalf("status should not be due at 29s, got %v", d)
	}
	// 31s later: due.
	d := dueItems(t0.Add(31*time.Second), last, nil, cfg, false)
	if len(d) != 1 || d[0].source != srcStatus {
		t.Fatalf("status should be due at 31s, got %v", d)
	}
	// Branches interval 0 → never due even at 31s.
	for _, it := range d {
		if it.source == srcBranches {
			t.Fatal("branches (interval 0) must never be due")
		}
	}
	// Master off → nothing due.
	if d := dueItems(t0.Add(31*time.Second), last, nil, cfg, false); len(d) == 1 {
		cfgOff := cfg
		cfgOff.Enabled = false
		if d2 := dueItems(t0.Add(31*time.Second), last, nil, cfgOff, false); len(d2) != 0 {
			t.Fatalf("master off must yield nothing, got %v", d2)
		}
	}
	// Suppressed → nothing due.
	if d := dueItems(t0.Add(31*time.Second), last, nil, cfg, true); len(d) != 0 {
		t.Fatalf("suppressed must yield nothing, got %v", d)
	}
}

func TestDueItemsFirstRunWhenUnseen(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	cfg := config.RefreshConfig{Enabled: true, Status: 30}
	// No lastRun entry → treat as due immediately (first poll after enable).
	d := dueItems(t0, map[refreshItem]time.Time{}, nil, cfg, false)
	if len(d) != 1 || d[0].source != srcStatus {
		t.Fatalf("unseen item with interval>0 should be due, got %v", d)
	}
}

func TestRefreshTickFiresSilentReadAndIsSuppressed(t *testing.T) {
	m := newTestModel(t)
	m.cfg.Refresh = config.RefreshConfig{Enabled: true, Status: 30}
	m.loading = false // simulate post-init: New() starts with loading=true; clear it here
	t0 := time.Unix(2_000_000, 0)

	// Not suppressed, status unseen → due → fires a silent read via the single-lane
	// queue (gen bumps, NO srcLoading, lane marked busy).
	genBefore := m.srcGen[srcStatus]
	m2, cmd := m.refreshTick(t0)
	if cmd == nil {
		t.Fatal("expected a background read command")
	}
	if m2.srcGen[srcStatus] != genBefore+1 {
		t.Fatal("status gen should bump for a fired background read")
	}
	if m2.srcLoading[srcStatus] {
		t.Fatal("background (auto) read must NOT set srcLoading (silent)")
	}
	if !m2.bgBusy {
		t.Fatal("lane must be busy after firing a background read")
	}
	if m2.bgActiveItem.source != srcStatus {
		t.Fatalf("bgActiveItem must be srcStatus, got %v", m2.bgActiveItem)
	}

	// Suppressed (op running) → no fire.
	mb := newTestModel(t)
	mb.cfg.Refresh = config.RefreshConfig{Enabled: true, Status: 30}
	mb.loading = false // same post-init baseline
	mb.running = true
	_, cmd2 := mb.refreshTick(t0)
	if cmd2 != nil {
		t.Fatal("must not fire while an op is running")
	}
}

// newTestModelWithRemote builds a Model backed by a real cloned repo that has a
// reachable local bare remote (origin). This is the minimal setup for bgFetchCmd
// to succeed in unit tests without a network call.
func newTestModelWithRemote(t *testing.T) Model {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(root, "init", "--bare", "-b", "main", origin)
	run(root, "clone", origin, seed)
	if err := os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// No checkout needed: cloning a bare repo initialized with -b main already
	// puts the clone on main (or on the unborn main branch if no commits yet).
	// Using -b main here would error on git versions that see main as already existing.
	run(seed, "add", ".")
	run(seed, "commit", "-m", "v1")
	run(seed, "push", "-u", "origin", "main")
	run(root, "clone", origin, clone)
	run(clone, "checkout", "main")

	runner := gitexec.NewExecRunner("git", clone, observ.NewRing(50))
	return New(domain.New(&git.Repo{Runner: runner}))
}

// BLOCKING-bug guard: a silent (auto) read that fails — e.g. context.Canceled
// because a user op preempted it — must NEVER write the status line. Otherwise
// the user sees "branches: context canceled" from a refresh they never asked
// for. (Phase A's handler currently surfaces every err to statusMsg.)
func TestSilentReadErrorDoesNotTouchStatus(t *testing.T) {
	m := newTestModel(t)
	m.statusMsg = "keep me"
	nm, _ := m.Update(dataAvailableMsg{
		source: srcBranches, gen: m.srcGen[srcBranches],
		manual: false, err: context.Canceled,
	})
	if got := nm.(Model).statusMsg; got != "keep me" {
		t.Fatalf("silent read error must not change statusMsg, got %q", got)
	}
}

func TestBgFetchEnqueuesRemotesOnSuccess(t *testing.T) {
	m := newTestModel(t)
	m.bgBusy = true
	m.bgActiveItem = fetchItem
	nm, _ := m.Update(bgFetchDoneMsg{dur: time.Second})
	mm := nm.(Model)
	if mm.bgBusy {
		t.Fatal("fetch done must free the lane")
	}
	found := false
	for _, it := range mm.bgQueue {
		if it == (refreshItem{source: srcRemotes}) {
			found = true
		}
	}
	if !found {
		t.Fatal("successful fetch must enqueue a remotes refresh")
	}
}

func TestRefreshTickSkipsSourceAlreadyInflight(t *testing.T) {
	m := newTestModel(t)
	m.cfg.Refresh = config.RefreshConfig{Enabled: true, Remotes: 1}
	m.loading = false
	m.srcInflight[srcRemotes] = true // a manual remotes read is already out
	m.bgQueue = []refreshItem{{source: srcRemotes}}
	_, cmd := m.refreshTick(time.Unix(3_000_000, 0))
	if cmd != nil {
		t.Fatal("must not fire a bg read for a source already in flight")
	}
}
