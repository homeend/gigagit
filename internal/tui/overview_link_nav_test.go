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

// A session overview link names ITS worktree: steered at a gg showing
// another one (an agent in a console, a look) it asks to switch — it never
// opens the shown worktree's overview of the same id (tours share ids).
func TestSteerSessionOverviewForAnotherWorktreeAsks(t *testing.T) {
	t.Parallel()
	m := sessionOverviewNavModel(t) // the shown worktree holds ov-1 too
	m.steerDir = t.TempDir()
	c := steer.Command{ID: "n2", Cmd: "navigate", HintKind: model.OverviewHintKind, HintID: "ov-1", Worktree: t.TempDir()}
	m, cmd := m.applySteer(c)
	m = drainCmds(t, m, cmd)
	if m.steerAsk == nil {
		t.Fatal("no switch ask")
	}
	if d := topDoc(m); d != nil && d.id() == "ov-1" {
		t.Fatal("opened the shown worktree's overview")
	}
}

// An open working-tree file whose NAME is the overview's id is not that
// overview: a session overview written a moment ago is still read from the
// docs directory and opened.
func TestSteerSessionOverviewNotAFileNamedLikeIt(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	bgDoc(m, fileSource{kind: srcWorktree}, "ov-2", 3)
	writeSessionOverview(t, m, dir, "ov-2", "Fresh", "", "[a](a.txt:1)") // not yet in the open-files list
	m = navToSessionOverview(t, m, "ov-2")
	if d := topDoc(m); d == nil || d.id() != "ov-2" || d.ov == nil {
		t.Fatalf("top doc = %+v", d)
	}
}
