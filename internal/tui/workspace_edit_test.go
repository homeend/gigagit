package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/workspace"
)

// savedWorkspace is a two-member workspace also on disk, under a scratch state.
func savedWorkspace(t *testing.T, blocked bool) (Model, *repoView, *fakeWebHost) {
	t.Helper()
	m, b, f := servedWorkspace(t, blocked)
	m.statePath = t.TempDir() + "/repos.toml"
	e := workspace.Entry{Name: "shop", Repos: []string{m.ws.members[0].mainPath, b.mainPath}}
	if err := workspace.Save(m.workspacesPath(), []workspace.Entry{e}); err != nil {
		t.Fatal(err)
	}
	return m, b, f
}

func TestPopupAndActorRenameUpdateOpenName(t *testing.T) {
	m, _, _ := savedWorkspace(t, true)
	nm, _, _, err := m.workspaceEditApply(domain.WorkspaceRequest{Kind: "edit", Edit: "rename", Name: "shop", Args: []string{"store"}})
	if err != nil || nm.ws.name != "store" {
		t.Fatalf("actor: err %v, open name %q", err, nm.ws.name)
	}
	if _, ok := workspace.Find(workspace.Load(nm.workspacesPath()), "store"); !ok {
		t.Fatal("actor: the file was not renamed")
	}
	p := &workspacePopup{ws: workspace.Load(nm.workspacesPath()), exists: map[string]bool{}}
	nm, _, _ = p.applyName(nm, "store", "shop")
	if nm.ws.name != "shop" {
		t.Fatalf("popup path: open name %q", nm.ws.name)
	}
}

func TestDeleteOpenWorkspaceLeavesBothPaths(t *testing.T) {
	m, _, _ := savedWorkspace(t, true)
	nm, _, _, err := m.workspaceEditApply(domain.WorkspaceRequest{Kind: "edit", Edit: "delete", Name: "shop"})
	if err != nil || nm.ws != nil {
		t.Fatalf("err %v, ws %+v", err, nm.ws)
	}
	if len(workspace.Load(nm.workspacesPath())) != 0 {
		t.Fatal("the entry stayed in the file")
	}
}

// liveSession puts a running session in b's main checkout for the test's length.
func liveSession(t *testing.T, b *repoView) domain.SessionID {
	t.Helper()
	s, err := domain.Sessions().Start(sessionSpecForTest("in-b", b.mainPath))
	if err != nil {
		t.Fatal(err)
	}
	id := s.Info().ID
	t.Cleanup(func() { _ = domain.Sessions().KillAndRemove(id) })
	return id
}

func TestActorDropAsksWithoutPopup(t *testing.T) {
	m, b, _ := savedWorkspace(t, true)
	id := liveSession(t, b)
	nm, _, reply, err := m.workspaceEditApply(domain.WorkspaceRequest{Kind: "leave"})
	if err != nil || len(reply.DropSessions) != 1 || reply.DropSessions[0].ID != id {
		t.Fatalf("err %v, reply %+v", err, reply)
	}
	if nm.topLayer() != nil {
		t.Fatal("no popup behind the block")
	}
	if nm.ws == nil {
		t.Fatal("nothing happens until the page answers")
	}
}

func TestActorDropCancelLeavesFileUntouched(t *testing.T) {
	m, b, _ := savedWorkspace(t, true)
	liveSession(t, b)
	before, _ := os.ReadFile(m.workspacesPath())
	if _, _, reply, _ := m.workspaceEditApply(domain.WorkspaceRequest{Kind: "edit", Edit: "delete", Name: "shop"}); len(reply.DropSessions) == 0 {
		t.Fatalf("reply %+v", reply)
	}
	after, _ := os.ReadFile(m.workspacesPath())
	if string(before) != string(after) {
		t.Fatalf("the file moved:\n%s\n%s", before, after)
	}
}

func TestActorDropStopStopsSessions(t *testing.T) {
	m, b, _ := savedWorkspace(t, true)
	id := liveSession(t, b)
	nm, _, _, err := m.workspaceEditApply(domain.WorkspaceRequest{Kind: "leave", Drop: "stop"})
	if err != nil || nm.ws != nil {
		t.Fatalf("err %v, ws %+v", err, nm.ws)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		s, ok := domain.Sessions().Get(id)
		if !ok || s.Info().State != domain.SessionRunning {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the session was not stopped")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestActorRefusedSwitchKeepsFile(t *testing.T) {
	m, _, _ := savedWorkspace(t, true)
	live := m.liveRepo().mainPath
	m.running = true
	before, _ := os.ReadFile(m.workspacesPath())
	_, _, _, err := m.workspaceEditApply(domain.WorkspaceRequest{Kind: "edit", Edit: "remove", Name: "shop", Args: []string{live}})
	if err == nil {
		t.Fatal("removing the member on screen during an operation must refuse")
	}
	after, _ := os.ReadFile(m.workspacesPath())
	if string(before) != string(after) {
		t.Fatal("a refused switch must leave the file as it was")
	}
}

func TestLaunchQuestionHeldWhileWebActive(t *testing.T) {
	f := installFakeHost(t) // the flip needs a page to flip to
	m := loadedModel(t)
	m.web = newWebHostState()
	m.web.host = f
	m.statePath = t.TempDir() + "/repos.toml"
	home := m.homeRepo()
	home.mainKnown = true
	if err := workspace.Save(m.workspacesPath(), []workspace.Entry{{Name: "shop", Repos: []string{home.mainPath, secondRepo(t)}}}); err != nil {
		t.Fatal(err)
	}
	m.webActive, m.workspaceAsked = true, false
	m, _ = m.maybeAskWorkspace()
	if m.topLayer() != nil {
		t.Fatal("no popup behind the block")
	}
	m.pushWebWorkspace()
	q := f.lastQuestion()
	if q == nil || len(q.Rows) != 1 || q.Rows[0].Name != "shop" {
		t.Fatalf("question %+v", q)
	}
	if !strings.Contains(q.Repo, home.name) {
		t.Fatalf("repo %q", q.Repo)
	}
	// A flip to the TUI moves it to the terminal, and back to the page.
	m, cmd := m.activateTUI()
	m = settleWorkspace(t, m, cmd) // its reload ends before a flip back may go
	if _, ok := m.topLayer().(*workspaceLaunchPopup); !ok {
		t.Fatalf("after the flip: %T", m.topLayer())
	}
	m.pushWebWorkspace()
	if f.lastQuestion() != nil {
		t.Fatal("the page must stop showing it")
	}
	m, _, _ = m.flipToWeb()
	if m.topLayer() != nil {
		t.Fatalf("after the flip back: %T", m.topLayer())
	}
	m.pushWebWorkspace()
	if f.lastQuestion() == nil {
		t.Fatal("the page must show it again")
	}
	// Answered: gone. A second answer finds nothing.
	nm, _, _, err := m.workspaceEditApply(domain.WorkspaceRequest{Kind: "launch"})
	if err != nil || nm.launchQuestion != nil {
		t.Fatalf("err %v, question %+v", err, nm.launchQuestion)
	}
	m = nm
	m.pushWebWorkspace()
	if f.lastQuestion() != nil {
		t.Fatal("answered: the page must stop showing it")
	}
	// A repository change drops one still pending.
	m.launchQuestion = &workspaceLaunchPopup{repoKey: "elsewhere"}
	m.pushWebWorkspace()
	if f.lastQuestion() != nil {
		t.Fatal("the repository changed: dropped")
	}
}

func (f *fakeWebHost) lastQuestion() *domain.WorkspaceQuestion {
	if len(f.questions) == 0 {
		return nil
	}
	return f.questions[len(f.questions)-1]
}

// A question stops before anything moves, and a yes moves the page too.
func TestOpenSharingAMemberAsksBeforeMoving(t *testing.T) {
	m, b, f := savedWorkspace(t, true)
	a := m.ws.members[0]
	liveSession(t, a)
	c := t.TempDir()
	if err := workspace.Save(m.workspacesPath(), []workspace.Entry{
		{Name: "shop", Repos: []string{a.mainPath, b.mainPath}},
		{Name: "mix", Repos: []string{b.mainPath, c}},
	}); err != nil {
		t.Fatal(err)
	}
	home := m.home
	nm, _, reply, err := m.workspaceEditApply(domain.WorkspaceRequest{Kind: "open", Name: "mix"})
	if err != nil || len(reply.DropSessions) == 0 {
		t.Fatalf("reply %+v err %v", reply, err)
	}
	if nm.home != home || nm.liveRepo() != a {
		t.Fatalf("moved before the answer: home %q", nm.home)
	}
	nm, cmd, reply, err := nm.workspaceEditApply(domain.WorkspaceRequest{Kind: "open", Name: "mix", Drop: "keep"})
	if err != nil || len(reply.DropSessions) != 0 {
		t.Fatalf("reply %+v err %v", reply, err)
	}
	if nm.ws.name != "mix" || nm.liveRepo() != b {
		t.Fatalf("open %q live %v", nm.ws.name, nm.liveRepo())
	}
	before := len(f.reroots)
	runLeaves(cmd)
	if len(f.reroots) == before {
		t.Fatal("the page was not told to follow")
	}
}

// A member the page only passed through still gets its first read in the terminal.
func TestPageVisitDoesNotSpendAMembersFirstRead(t *testing.T) {
	m, b, _ := savedWorkspace(t, true)
	a := m.ws.members[0]
	m, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal("refused")
	}
	m, ok = m.switchRepo(a, "", true)
	if !ok {
		t.Fatal("refused")
	}
	if b.everLive {
		t.Fatal("b was marked read while the terminal was blocked")
	}
	if m.memberFirstLive {
		t.Fatal("a was already read: nothing owed")
	}
	m, _ = m.switchRepo(b, "", true)
	if !m.memberFirstLive {
		t.Fatal("arriving on b while blocked owes it a first read on activation")
	}
}

func TestLeavingNeverRaisesTheLaunchQuestion(t *testing.T) {
	m, _, _ := savedWorkspace(t, true)
	m.workspaceAsked = false
	m, _ = m.maybeAskWorkspace() // a --workspace launch: a workspace is open
	if !m.workspaceAsked {
		t.Fatal("the process has its answer once a workspace is open")
	}
}

func TestPageEditClosesWhatIsBehindTheBlock(t *testing.T) {
	m, b, _ := savedWorkspace(t, true)
	liveSession(t, b)
	m, _ = m.openSessionsPopup(true)
	home := m.ws.members[0]
	_, _, _, err := m.workspaceEditApply(domain.WorkspaceRequest{Kind: "edit", Edit: "remove", Name: "shop", Args: []string{home.mainPath}})
	if err != nil {
		t.Fatalf("refused for a window the page cannot see: %v", err)
	}
}
