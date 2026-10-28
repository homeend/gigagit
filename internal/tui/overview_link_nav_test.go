package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/linknav"
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

// gg open with no live gg launches the TUI with a hint-only --at: once the
// TUI is ready the landing opens the session overview from its docs.
func TestStartAtSessionOverviewLinkOpensIt(t *testing.T) {
	t.Parallel()
	m := sessionOverviewNavModel(t)
	link, err := domain.SessionOverviewLink(m.currentWorktree, "ov-1")
	if err != nil {
		t.Fatal(err)
	}
	l, err := model.ParseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	m.startAt, m.startAtPending = l, true
	m, cmd := m.consumeStartAt()
	m = drainCmds(t, m, cmd)
	if d := topDoc(m); d == nil || d.id() != "ov-1" || d.ov == nil {
		t.Fatalf("top doc = %+v (status %q)", d, m.statusMsg)
	}
}

// A session overview link to ANOTHER checkout in the # prompt asks to
// switch, and the switch arms the hint-only landing there.
func TestGotoSessionOverviewLinkInAnotherCheckoutAsksThenArmsIt(t *testing.T) {
	t.Parallel()
	m, _ := gotoLinkModel(t)
	other := otherCheckout(t, m)
	ctx := context.Background()
	osvc := domain.Open(other)
	top, err := osvc.TopLevel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cd, err := osvc.GitCommonDir(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data, err := agentdocs.EncodeOverviewFile(agentdocs.OverviewFile{Title: "There", Author: "agent", Text: "[a](a.txt:1)"})
	if err != nil {
		t.Fatal(err)
	}
	if err := agentdocs.WriteDoc(linknav.SessionDocsDir(cd, top), "ov-9", agentdocs.KindOverview, data, true); err != nil {
		t.Fatal(err)
	}
	link, err := domain.SessionOverviewLink(top, "ov-9")
	if err != nil {
		t.Fatal(err)
	}
	m, cmd := pasteLink(t, m, link)
	m, _ = send(m, cmd())
	p := layerOf[*gotoCommitPopup](m)
	if p == nil || p.pending == nil || p.err != "" {
		t.Fatalf("no switch confirm (prompt %+v)", p)
	}
	if !domain.SamePath(p.pending.checkout, top) || p.pending.bare {
		t.Fatalf("pending = %+v", p.pending)
	}
	m, _ = send(m, keyType(tea.KeyEnter))
	if !m.startAtPending || !domain.IsSessionOverviewLink(m.startAt) || m.startAt.Hint.ID != "ov-9" {
		t.Fatalf("startAt = %+v pending=%v", m.startAt, m.startAtPending)
	}
}
