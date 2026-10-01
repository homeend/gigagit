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
	go func() { PublishSessions(ctx, dir, func() string { return "/tui/wt" }); close(done) }()
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
	self := fmt.Sprintf("%d-1", os.Getpid())
	lv := liveView{
		running: map[string]bool{"live-1/s1": true, "live-1/s2": false},
		procs:   map[string]bool{"live-1": true},
	}
	cases := map[string]bool{
		"live-1/s1":  false, // listed running
		"live-1/s2":  true,  // its registry is live and says exited
		"live-1/s9":  true,  // its registry is live and does not list it
		self + "/s1": false, // no live registry, but the process is alive (stalled TUI)
		"0-1/s1":     true,  // no registry, no process
		"garbage":    true,
	}
	for id, want := range cases {
		if got := sessionDead(id, lv); got != want {
			t.Errorf("sessionDead(%q) = %v, want %v", id, got, want)
		}
	}
}
