package domain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/sessionreg"
)

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in 5s")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestPublishSessionsWritesAndRemoves(t *testing.T) {
	// Not parallel: swaps the process-global session manager.
	if runtime.GOOS == "windows" {
		t.Skip("sleep-based session")
	}
	mgr := agentsession.NewManager()
	defer UseSessionManager(mgr)()
	dir := t.TempDir()
	wt := t.TempDir()
	s, err := mgr.Start(agentsession.StartSpec{Label: "t", AgentID: "claude", Dir: wt, Argv: []string{"sleep", "30"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.KillAll(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		PublishSessions(ctx, dir, func() string { return "/tui/wt" }, func() string { return "/tui/wt" }, "")
		close(done)
	}()
	id := agentsession.ProcTag() + "/" + string(s.Info().ID)
	waitFor(t, func() bool { return len(sessionreg.Live(dir)) == 1 })
	lv := readLive(dir)
	if !lv.running[id] || lv.agents[id] != "claude" {
		t.Fatalf("live = %+v", lv)
	}
	if len(lv.tuis) != 1 || lv.tuis[0] != "/tui/wt" || len(lv.byDir[filepath.Clean(wt)]) != 1 {
		t.Fatalf("live = %+v", lv)
	}
	cancel()
	<-done
	if regs := sessionreg.Live(dir); len(regs) != 0 {
		t.Fatalf("registry not removed: %+v", regs)
	}
}

func TestSessionDead(t *testing.T) {
	t.Parallel()
	self := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano()) // a real tag: this process started before now
	lv := liveView{
		running: map[string]bool{"live-1/s1": true, "live-1/s2": false},
		procs:   map[string]bool{"live-1": true},
	}
	cases := map[string]bool{
		"live-1/s1":                         false, // listed running
		"live-1/s2":                         true,  // its registry is live and says exited
		"live-1/s9":                         true,  // its registry is live and does not list it
		self + "/s1":                        false, // no live registry, but the process is alive (stalled TUI)
		"0-1/s1":                            true,  // no registry, no process
		fmt.Sprintf("%d-1/s1", os.Getpid()): true,  // alive pid, but it started after the tag: a reused pid
		"garbage":                           true,
	}
	for id, want := range cases {
		if got := sessionDead(id, lv); got != want {
			t.Errorf("sessionDead(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestRegistryCarriesTheChannelURL(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		PublishSessions(ctx, dir, func() string { return "/wt" }, func() string { return "/wt" }, "http://127.0.0.1:9/mcp")
		close(done)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		hosts := liveAgentHostsIn(dir)
		if len(hosts) == 1 && hosts[0].MCP == "http://127.0.0.1:9/mcp" && hosts[0].Worktree == "/wt" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("hosts = %+v", hosts)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
}

// A TUI looking at another worktree (the fast switch) publishes it beside
// its own: both are "a gg TUI is open here" to another process's guard.
func TestLiveViewListsTheViewedWorktreeToo(t *testing.T) {
	dir := t.TempDir()
	if err := sessionreg.Write(dir, "p1", sessionreg.Registry{PID: os.Getpid(), Worktree: "/tui/home", Viewed: "/tui/other"}); err != nil {
		t.Fatal(err)
	}
	lv := readLive(dir)
	has := func(w string) bool {
		for _, x := range lv.tuis {
			if x == w {
				return true
			}
		}
		return false
	}
	if !has("/tui/home") || !has("/tui/other") {
		t.Fatalf("tuis = %v, want home and viewed", lv.tuis)
	}
}
