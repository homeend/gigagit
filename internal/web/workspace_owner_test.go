package web

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
	"github.com/homeend/gigagit/internal/workspace"
)

// ownerFixture is a standalone server on a, with shop = [a, b] on disk.
func ownerFixture(t *testing.T) (srv *Server, o *workspaceOwner, a, b string) {
	t.Helper()
	isolateGlobal(t)
	a, b = newRepoDir(t, 1), newRepoDir(t, 2)
	srv = New(domain.Open(a))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	t.Cleanup(srv.Close)
	if err := workspace.Save(srv.workspacesPath(), []workspace.Entry{{Name: "shop", Repos: []string{a, b}}}); err != nil {
		t.Fatal(err)
	}
	o = newWorkspaceOwner(srv)
	t.Cleanup(o.close)
	return srv, o, a, b
}

func TestOwnerLaunchQuestionOnlyForMember(t *testing.T) {
	srv, o, a, _ := ownerFixture(t)
	if err := o.start(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	q := srv.ws.question
	srv.mu.Unlock()
	if q == nil || len(q.Rows) != 1 || q.Rows[0].Name != "shop" || q.Repo != filepath.Base(a) {
		t.Fatalf("question %+v", q)
	}
	if srv.workspaceInfo() != nil {
		t.Fatal("the repository alone is served until it is answered")
	}

	other := New(domain.Open(newRepoDir(t, 3)))
	other.reposPath = srv.reposPath
	t.Cleanup(other.Close)
	oo := newWorkspaceOwner(other)
	t.Cleanup(oo.close)
	if err := oo.start(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	if other.ws.question != nil {
		t.Fatalf("not a member, no question: %+v", other.ws.question)
	}
}

func TestOwnerLaunchAnswers(t *testing.T) {
	srv, o, _, _ := ownerFixture(t)
	if err := o.start(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "launch", Name: "shop"}); err != nil {
		t.Fatal(err)
	}
	if info := srv.workspaceInfo(); info == nil || info.Name != "shop" {
		t.Fatalf("info %+v", info)
	}
	if srv.ws.question != nil {
		t.Fatal("answered: over")
	}
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "launch"}); err != nil {
		t.Fatalf("a second answer finds nothing: %v", err)
	}
	if srv.workspaceInfo() == nil {
		t.Fatal("a second answer must change nothing")
	}
}

func TestOwnerStartUnknownNameFailsBeforeBind(t *testing.T) {
	_, o, _, _ := ownerFixture(t)
	err := o.start(t.Context(), "nope")
	if err == nil || !strings.Contains(err.Error(), `no workspace named "nope"`) {
		t.Fatalf("err %v", err)
	}
}

func TestOwnerAfterAdoptUpdatesLiveAndLastViewed(t *testing.T) {
	srv, o, _, b := ownerFixture(t)
	if err := o.start(t.Context(), "shop"); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "linked")
	gitRun(t, b, "worktree", "add", "-b", "feat", linked)
	if err := srv.adoptService(t.Context(), domain.Open(linked)); err != nil {
		t.Fatal(err)
	}
	info := srv.workspaceInfo()
	if info == nil || info.Live != 1 || !model.SamePath(info.Members[1].LastViewed, linked) {
		t.Fatalf("info %+v", info)
	}
}

func TestOwnerAdoptOutsideLeaves(t *testing.T) {
	srv, o, _, _ := ownerFixture(t)
	if err := o.start(t.Context(), "shop"); err != nil {
		t.Fatal(err)
	}
	if err := srv.adoptService(t.Context(), domain.Open(newRepoDir(t, 3))); err != nil {
		t.Fatal(err)
	}
	if srv.workspaceInfo() != nil {
		t.Fatal("a switch outside every member leaves")
	}
}

func TestOwnerWatchesOnlySleepingMembers(t *testing.T) {
	srv, o, a, b := ownerFixture(t)
	if err := o.start(t.Context(), "shop"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "b never watched", func() bool {
		w := o.watched()
		return len(w) == 1 && model.SamePath(w[0], b)
	})
	if err := srv.adoptService(t.Context(), domain.Open(b)); err != nil {
		t.Fatal(err)
	}
	if w := o.watched(); len(w) != 1 || !model.SamePath(w[0], a) {
		t.Fatalf("after the switch: watched %v, want %s", w, a)
	}
}

func TestOwnerFetchesOneAtATime(t *testing.T) {
	isolateGlobal(t)
	a, b, c := newRepoDir(t, 1), newRepoDir(t, 2), newRepoDir(t, 3)
	for _, d := range []string{b, c} {
		if err := os.WriteFile(filepath.Join(d, ".gg.toml"), []byte("[refresh]\nenabled = true\nfetch = 60\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv := New(domain.Open(a))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	t.Cleanup(srv.Close)
	if err := workspace.Save(srv.workspacesPath(), []workspace.Entry{{Name: "shop", Repos: []string{a, b, c}}}); err != nil {
		t.Fatal(err)
	}
	o := newWorkspaceOwner(srv)
	t.Cleanup(o.close)
	var fetched []string
	o.fetch = func(_ context.Context, svc *domain.Service) error {
		fetched = append(fetched, svc.Root())
		return nil
	}
	if err := o.start(t.Context(), "shop"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "members never read", func() bool { return len(o.watched()) == 2 })
	t0 := time.Unix(3_000_000, 0)
	o.fetchOnce(t.Context(), t0) // first sight is stamped: no burst when a workspace opens
	if len(fetched) != 0 {
		t.Fatalf("burst: %v", fetched)
	}
	o.fetchOnce(t.Context(), t0.Add(61e9))
	if len(fetched) != 1 {
		t.Fatalf("one at a time: %v", fetched)
	}
	o.fetchOnce(t.Context(), t0.Add(61e9))
	if len(fetched) != 2 || fetched[0] == fetched[1] {
		t.Fatalf("then the other: %v", fetched)
	}
	o.fetchOnce(t.Context(), t0.Add(62e9))
	if len(fetched) != 2 {
		t.Fatalf("not due again: %v", fetched)
	}
}

func TestOwnerDropAsksAndStops(t *testing.T) {
	srv, o, _, b := ownerFixture(t)
	if err := o.start(t.Context(), "shop"); err != nil {
		t.Fatal(err)
	}
	asked := []domain.SessionInfo{{ID: "s1", Dir: b, State: domain.SessionRunning}}
	var killed []domain.SessionID
	o.sessionsIn = func(context.Context, *domain.Service) []domain.SessionInfo { return asked }
	o.kill = func(id domain.SessionID) error { killed = append(killed, id); return nil }
	before, _ := os.ReadFile(srv.workspacesPath())
	reply, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "edit", Edit: "delete", Name: "shop"})
	if err != nil || len(reply.DropSessions) != 1 {
		t.Fatalf("err %v, reply %+v", err, reply)
	}
	after, _ := os.ReadFile(srv.workspacesPath())
	if string(before) != string(after) || srv.workspaceInfo() == nil {
		t.Fatal("asking changes nothing")
	}
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "leave", Drop: "keep"}); err != nil || len(killed) != 0 || srv.workspaceInfo() != nil {
		t.Fatalf("keep: err %v, killed %v, info %+v", err, killed, srv.workspaceInfo())
	}
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "open", Name: "shop"}); err != nil {
		t.Fatal(err)
	}
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "leave", Drop: "stop"}); err != nil || len(killed) != 1 {
		t.Fatalf("stop: err %v, killed %v", err, killed)
	}
}

func TestOwnerEdits(t *testing.T) {
	srv, o, a, b := ownerFixture(t)
	if err := o.start(t.Context(), "shop"); err != nil {
		t.Fatal(err)
	}
	do := func(req domain.WorkspaceRequest) {
		t.Helper()
		if _, err := o.act(t.Context(), req); err != nil {
			t.Fatalf("%+v: %v", req, err)
		}
	}
	do(domain.WorkspaceRequest{Kind: "edit", Edit: "settings", Name: "shop", Args: []string{b}})
	if !model.SamePath(srv.workspaceInfo().SettingsRepo, b) {
		t.Fatalf("settings: %+v", srv.workspaceInfo())
	}
	do(domain.WorkspaceRequest{Kind: "edit", Edit: "move", Name: "shop", Args: []string{b, "-1"}})
	info := srv.workspaceInfo()
	if !model.SamePath(info.Members[0].Path, b) || info.Live != 1 {
		t.Fatalf("move: %+v", info)
	}
	do(domain.WorkspaceRequest{Kind: "edit", Edit: "rename", Name: "shop", Args: []string{"store"}})
	if srv.workspaceInfo().Name != "store" {
		t.Fatalf("rename: %+v", srv.workspaceInfo())
	}
	c := newRepoDir(t, 3)
	do(domain.WorkspaceRequest{Kind: "edit", Edit: "add", Name: "store", Args: []string{c}})
	if len(srv.workspaceInfo().Members) != 3 {
		t.Fatalf("add: %+v", srv.workspaceInfo())
	}
	do(domain.WorkspaceRequest{Kind: "edit", Edit: "remove", Name: "store", Args: []string{a}}) // the member on screen
	info = srv.workspaceInfo()
	if len(info.Members) != 2 || model.SamePath(srv.service().Root(), a) {
		t.Fatalf("remove the live member: %+v on %s", info, srv.service().Root())
	}
	do(domain.WorkspaceRequest{Kind: "edit", Edit: "new", Name: "second"})
	if _, ok := workspace.Find(workspace.Load(srv.workspacesPath()), "second"); !ok {
		t.Fatal("new: not on disk")
	}
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "edit", Edit: "new", Name: "second"}); err == nil {
		t.Fatal("a taken name refuses")
	}
	do(domain.WorkspaceRequest{Kind: "edit", Edit: "delete", Name: "store"})
	if srv.workspaceInfo() != nil {
		t.Fatal("deleting the open workspace leaves it")
	}
}

func TestOwnerEditWorkspaceGoneElsewhere(t *testing.T) {
	srv, o, _, b := ownerFixture(t)
	if err := o.start(t.Context(), "shop"); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Save(srv.workspacesPath(), nil); err != nil { // another gg deleted it
		t.Fatal(err)
	}
	_, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "edit", Edit: "settings", Name: "shop", Args: []string{b}})
	if err == nil || !strings.Contains(err.Error(), "removed elsewhere") {
		t.Fatalf("err %v", err)
	}
	if w := uiRequest(srv, "GET", "/api/workspace", ""); w.Code != 200 {
		t.Fatalf("the page stays usable: %d", w.Code)
	}
}

func TestOwnerRefusedSwitchKeepsFile(t *testing.T) {
	srv, o, a, _ := ownerFixture(t)
	if err := o.start(t.Context(), "shop"); err != nil {
		t.Fatal(err)
	}
	srv.cur = &opRun{}
	before, _ := os.ReadFile(srv.workspacesPath())
	_, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "edit", Edit: "remove", Name: "shop", Args: []string{a}})
	if !errors.Is(err, errOpBusy) {
		t.Fatalf("err %v", err)
	}
	after, _ := os.ReadFile(srv.workspacesPath())
	if string(before) != string(after) {
		t.Fatal("a refused switch leaves the file as it was")
	}
}

func TestStartAtLandsThenAsks(t *testing.T) {
	srv, o, _, _ := ownerFixture(t)
	if err := srv.setStartAt(steer.Command{Cmd: "navigate", File: "f.txt", Target: &steer.Target{State: "unstaged"}}); err != nil {
		t.Fatal(err)
	}
	if err := o.start(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	if srv.ws.question == nil {
		t.Fatal("the question still asks")
	}
	if w := uiRequest(srv, "GET", "/api/start-at", ""); w.Code >= 500 {
		t.Fatalf("start-at: %d", w.Code)
	}
}
