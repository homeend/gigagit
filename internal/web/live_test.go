package web

import (
	"slices"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/gitwatch"
)

func recvLive(t *testing.T, ch <-chan liveMsg, d time.Duration) (liveMsg, bool) {
	t.Helper()
	select {
	case m, ok := <-ch:
		return m, ok
	case <-time.After(d):
		return liveMsg{}, false
	}
}

func TestLiveHubEmitReachesEverySubscriber(t *testing.T) {
	t.Parallel()
	h := newLiveHub(config.RefreshConfig{Enabled: true}, true, nil)
	a, cancelA := h.subscribe()
	defer cancelA()
	b, cancelB := h.subscribe()
	defer cancelB()
	h.emit(liveMsg{Changed: []string{"branches"}, Reason: "watch"})
	for _, ch := range []<-chan liveMsg{a, b} {
		m, ok := recvLive(t, ch, time.Second)
		if !ok || !slices.Equal(m.Changed, []string{"branches"}) || m.Reason != "watch" {
			t.Fatalf("subscriber got %+v ok=%v", m, ok)
		}
	}
}

func TestLiveHubGateDropsEmit(t *testing.T) {
	t.Parallel()
	busy := true
	h := newLiveHub(config.RefreshConfig{Enabled: true}, true, func() bool { return busy })
	ch, cancel := h.subscribe()
	defer cancel()
	h.emit(liveMsg{Changed: []string{"status"}})
	if _, ok := recvLive(t, ch, 50*time.Millisecond); ok {
		t.Fatal("emit must be dropped while the gate is closed")
	}
	busy = false
	h.emit(liveMsg{Changed: []string{"status"}})
	if _, ok := recvLive(t, ch, time.Second); !ok {
		t.Fatal("emit must pass once the gate opens")
	}
}

func TestLiveHubFullBufferNeverBlocks(t *testing.T) {
	t.Parallel()
	h := newLiveHub(config.RefreshConfig{Enabled: true}, true, nil)
	_, cancel := h.subscribe() // never read
	defer cancel()
	done := make(chan struct{})
	go func() {
		for i := 0; i < liveSubBuffer*3; i++ {
			h.emit(liveMsg{Changed: []string{"feed"}})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("emit blocked on a subscriber that never reads")
	}
}

func TestLiveHubCloseClosesSubscribers(t *testing.T) {
	t.Parallel()
	h := newLiveHub(config.RefreshConfig{Enabled: true}, true, nil)
	ch, cancel := h.subscribe()
	defer cancel()
	h.close()
	h.close() // idempotent
	if _, ok := <-ch; ok {
		t.Fatal("subscriber channel must be closed by close()")
	}
	h.emit(liveMsg{Changed: []string{"feed"}}) // must not panic after close
}

func TestWatchFanOutMirrorsTUI(t *testing.T) {
	t.Parallel()
	cases := map[gitwatch.Source][]string{
		gitwatch.Branches:  {"branches", "feed"},
		gitwatch.Remotes:   {"remotes", "feed", "branches"},
		gitwatch.Worktrees: {"worktrees", "branches", "feed"},
		gitwatch.Reflog:    {"reflog"},
	}
	for src, want := range cases {
		if got := watchFanOut(src); !slices.Equal(got, want) {
			t.Errorf("%s → %v, want %v", src, got, want)
		}
	}
}

func TestLiveHubWatchActive(t *testing.T) {
	t.Parallel()
	cfg := config.RefreshConfig{Enabled: true, BranchesWatch: true, ReflogWatch: true}
	h := newLiveHub(cfg, true, nil)
	if !h.watchActive("branches") || !h.watchActive("reflog") {
		t.Fatal("toggled-on eligible sources must be watch-active")
	}
	if h.watchActive("remotes") || h.watchActive("status") || h.watchActive("feed") {
		t.Fatal("off or ineligible sources must not be watch-active")
	}
	unsupported := newLiveHub(cfg, false, nil)
	if unsupported.watchActive("branches") {
		t.Fatal("unsupported filesystem disables watch")
	}
}
