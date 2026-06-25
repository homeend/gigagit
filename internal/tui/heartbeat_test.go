package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
)

func TestFormatElapsed(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5s"},
		{59 * time.Second, "59s"},
		{90 * time.Second, "1m30s"},
		{9*time.Minute + 5*time.Second, "9m05s"},
		{2*time.Hour + 3*time.Minute, "2h03m"},
	}
	for _, c := range cases {
		if got := formatElapsed(c.d); got != c.want {
			t.Errorf("formatElapsed(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// TestHeartbeatIsPerpetual confirms a heartbeat always re-arms (one tick chain
// started in Init, for the app's life) — it never depends on startOp's returned
// command, which the many manual-drive tests rely on staying a single waitForOp.
func TestHeartbeatIsPerpetual(t *testing.T) {
	m := New(domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()}))

	// Idle: still re-arms (so the next op is covered without restarting it).
	if _, cmd := m.Update(heartbeatMsg{}); cmd == nil {
		t.Fatal("heartbeat should re-arm even when idle")
	}

	// startOp keeps its single-command contract (waitForOp), not a batch.
	m, cmd := m.startOp(blockingOp{})
	t.Cleanup(m.opCancel)
	if cmd == nil {
		t.Fatal("startOp should still return its op-wait command")
	}
	if _, hb := m.Update(heartbeatMsg{}); hb == nil {
		t.Fatal("heartbeat should re-arm while an op runs")
	}
}

// TestBusyLineShowsElapsed proves the elapsed readout reaches the rendered
// status line, so a long op visibly advances instead of looking frozen.
func TestBusyLineShowsElapsed(t *testing.T) {
	_, repo := newRepoDir(t)
	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.width, m.height = 120, 40

	m, _ = m.startOp(blockingOp{})
	t.Cleanup(m.opCancel)
	m.opStart = time.Now().Add(-95 * time.Second)
	if got := m.View(); !strings.Contains(got, "1m35s") {
		t.Fatalf("busy line missing elapsed readout; view:\n%s", got)
	}
}
