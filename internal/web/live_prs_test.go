package web

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

// The prs lane polls on [refresh] prs whatever the master switch says
// (ruling 13). These tests move the package clock, so they are serial.

func prsHub(cfg config.RefreshConfig, gate func() bool) (*liveHub, *atomic.Int32) {
	var calls atomic.Int32
	h := newLiveHub(cfg, false, gate)
	h.onPRs = func(*domain.Service) { calls.Add(1) }
	return h, &calls
}

func TestLivePRsTicksWithRefreshDisabled(t *testing.T) {
	clock := useFakeClock(t)
	h, calls := prsHub(config.RefreshConfig{Enabled: false, Status: 10}, nil) // prs unset → 300
	ch, cancel := h.subscribe()
	defer cancel()

	clock.advance(299 * time.Second)
	h.tickOnce(nil)
	if calls.Load() != 0 {
		t.Fatal("not due before 300 s")
	}
	clock.advance(2 * time.Second)
	h.tickOnce(nil)
	h.tickOnce(nil)
	if calls.Load() != 1 {
		t.Fatalf("prs lane ran %d times, want exactly 1", calls.Load())
	}
	if m, ok := recvLive(t, ch, 50*time.Millisecond); ok {
		t.Fatalf("refresh is disabled — nothing else may emit, got %+v", m)
	}
}

func TestLivePRsZeroIsOff(t *testing.T) {
	clock := useFakeClock(t)
	zero := 0
	h, calls := prsHub(config.RefreshConfig{Enabled: true, PRs: &zero}, nil)
	clock.advance(time.Hour)
	h.tickOnce(nil)
	if calls.Load() != 0 {
		t.Fatal("prs = 0 must turn the lane off")
	}
}

func TestLivePRsFlooredByMinSeconds(t *testing.T) {
	clock := useFakeClock(t)
	three := 3
	h, calls := prsHub(config.RefreshConfig{PRs: &three}, nil) // min_seconds unset → 10
	clock.advance(5 * time.Second)
	h.tickOnce(nil)
	if calls.Load() != 0 {
		t.Fatal("3 s is under the floor: not due at 5 s")
	}
	clock.advance(5 * time.Second)
	h.tickOnce(nil)
	if calls.Load() != 1 {
		t.Fatalf("due at the 10 s floor, ran %d times", calls.Load())
	}
}

func TestLivePRsSkippedWhileOpInFlight(t *testing.T) {
	clock := useFakeClock(t)
	busy := true
	h, calls := prsHub(config.RefreshConfig{}, func() bool { return busy })
	clock.advance(301 * time.Second)
	h.tickOnce(nil)
	if calls.Load() != 0 {
		t.Fatal("an op is in flight: the lane must wait")
	}
	busy = false
	h.tickOnce(nil) // lastRun was not stamped, so it is still due
	if calls.Load() != 1 {
		t.Fatalf("ran %d times after the op finished, want 1", calls.Load())
	}
}

func TestStartLiveRunsTickLoopForPRsAlone(t *testing.T) {
	clock := useFakeClock(t)
	isolateGlobal(t)
	dir := newRepoDir(t, 1)
	writeRepoRefresh(t, dir, "enabled = false\nprs = 20\n")
	srv := New(domain.Open(dir))
	srv.startLive(context.Background())
	t.Cleanup(srv.Close)
	var calls atomic.Int32
	h := srv.liveHubRef()
	h.mu.Lock()
	h.onPRs = func(*domain.Service) { calls.Add(1) }
	h.mu.Unlock()

	clock.advance(21 * time.Second)
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() == 0 {
		t.Fatal("with refresh disabled the tick loop must still run for prs")
	}
}

// The wired hook polls only a list that is already showing: an unprobed or
// forge-less service never earns an interval gh call.
func TestStartLiveWiresPRsToAReadyCacheOnly(t *testing.T) {
	isolateGlobal(t)
	f := &fakeForge{}
	ts, srv := prServe(t, newRepoDir(t, 1), f)
	srv.startLive(context.Background())
	h := srv.liveHubRef()
	h.onPRs(srv.service())
	time.Sleep(50 * time.Millisecond)
	if f.listCount() != 0 {
		t.Fatal("an unprobed cache must not be polled")
	}
	waitPRsLoaded(t, ts) // the page asked: now it is ready
	h.onPRs(srv.service())
	deadline := time.Now().Add(5 * time.Second)
	for f.listCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if f.listCount() != 2 {
		t.Fatalf("a ready cache is re-listed by the lane, lists = %d", f.listCount())
	}
}
