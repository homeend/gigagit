package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// sessionOverviewNavModel holds the attached session overview ov-1.
func sessionOverviewNavModel(t *testing.T) Model {
	t.Helper()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "ov-1", "Tour", "", "[a](a.txt:1)")
	m, _ = m.onAgentDocsChanged()
	return m
}

func navToSessionOverview(t *testing.T, m Model, id string) Model {
	t.Helper()
	m.steerDir = t.TempDir()
	c := steer.Command{ID: "n1", Cmd: "navigate", HintKind: model.OverviewHintKind, HintID: id, Wait: true}
	m, cmd := m.steerNavigate(c)
	return drainCmds(t, m, cmd)
}

func TestSteerNavigateOpensSessionOverview(t *testing.T) {
	t.Parallel()
	m := navToSessionOverview(t, sessionOverviewNavModel(t), "ov-1")
	if d := topDoc(m); d == nil || d.id() != "ov-1" {
		t.Fatalf("top doc = %+v (top %T)", d, m.topLayer())
	}
	if r, ok := steer.AwaitReply(m.steerDir, "n1", 2*time.Second); !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

func TestSteerNavigateSessionOverviewClosedReopens(t *testing.T) {
	t.Parallel()
	m := sessionOverviewNavModel(t)
	d, _ := m.findOverview("ov-1")
	if d == nil {
		t.Fatal("ov-1 not listed")
	}
	m = m.closeDoc(d)
	m = navToSessionOverview(t, m, "ov-1")
	if d := topDoc(m); d == nil || d.id() != "ov-1" {
		t.Fatalf("top doc = %+v", d)
	}
}

func TestSteerNavigateSessionOverviewGone(t *testing.T) {
	t.Parallel()
	m := navToSessionOverview(t, sessionOverviewNavModel(t), "nope-1")
	r, ok := steer.AwaitReply(m.steerDir, "n1", 2*time.Second)
	if !ok || r.OK || !strings.Contains(r.Error+r.Detail, "overview nope-1 no longer exists") {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}
