package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// wsServer serves repo a with a workspace of a and b open, a on screen.
func wsServer(t *testing.T) (srv *Server, a, b string) {
	t.Helper()
	a, b = newRepoDir(t, 1), newRepoDir(t, 2)
	srv = New(domain.Open(a))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	t.Cleanup(srv.Close)
	srv.SetWorkspace(wsInfo(a, b))
	return srv, a, b
}

func wsInfo(a, b string) *domain.WorkspaceInfo {
	return &domain.WorkspaceInfo{Name: "shop", Live: 0, Members: []domain.WorkspaceMember{
		{Path: a, Label: "a", Svc: domain.Open(a)},
		{Path: b, Label: "b", Svc: domain.Open(b)},
	}}
}

func memberURL(path string) string { return "/api/workspace/member?path=" + url.QueryEscape(path) }

func decodeBody(t *testing.T, body []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}

func TestWorkspaceRouteNilWhenClosed(t *testing.T) {
	t.Parallel()
	srv := New(domain.Open(newRepoDir(t, 1)))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	t.Cleanup(srv.Close)
	w := uiRequest(srv, http.MethodGet, "/api/workspace", "")
	var out struct {
		Open     *wsOpenWire     `json:"open"`
		Question *wsQuestionWire `json:"question"`
		Defined  []wsDefinedWire `json:"defined"`
	}
	decodeBody(t, w.Body.Bytes(), &out)
	if w.Code != http.StatusOK || out.Open != nil || out.Question != nil || len(out.Defined) != 0 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestWorkspaceRouteListsMembersAndCounters(t *testing.T) {
	t.Parallel()
	srv, a, b := wsServer(t)
	srv.MemberChanged(b)
	srv.MemberChanged(b)
	w := uiRequest(srv, http.MethodGet, "/api/workspace", "")
	var out struct {
		Open *wsOpenWire `json:"open"`
	}
	decodeBody(t, w.Body.Bytes(), &out)
	if out.Open == nil || out.Open.Name != "shop" || out.Open.Live != 0 || len(out.Open.Members) != 2 {
		t.Fatalf("%s", w.Body)
	}
	if m := out.Open.Members[0]; m.Path != a || m.Changes != 0 {
		t.Fatalf("first member %+v", m)
	}
	if m := out.Open.Members[1]; m.Path != b || m.Changes != 2 {
		t.Fatalf("second member %+v", m)
	}
}

func TestWorkspaceMemberRowsMatchPlainRoutes(t *testing.T) {
	t.Parallel()
	srv, _, b := wsServer(t)
	plain := New(domain.Open(b))
	t.Cleanup(plain.Close)
	got := uiRequest(srv, http.MethodGet, memberURL(b), "")
	var member map[string]json.RawMessage
	decodeBody(t, got.Body.Bytes(), &member)
	var wts, brs map[string]json.RawMessage
	decodeBody(t, uiRequest(plain, http.MethodGet, "/api/worktrees", "").Body.Bytes(), &wts)
	decodeBody(t, uiRequest(plain, http.MethodGet, "/api/branches", "").Body.Bytes(), &brs)
	if string(member["worktrees"]) != string(wts["worktrees"]) {
		t.Fatalf("worktrees\n got %s\nwant %s", member["worktrees"], wts["worktrees"])
	}
	if string(member["branches"]) != string(brs["branches"]) {
		t.Fatalf("branches\n got %s\nwant %s", member["branches"], brs["branches"])
	}
	if string(member["filter"]) != string(brs["filter"]) {
		t.Fatalf("filter\n got %s\nwant %s", member["filter"], brs["filter"])
	}
}

func TestWorkspaceMemberRefusesPathOutsideMembers(t *testing.T) {
	t.Parallel()
	srv, _, _ := wsServer(t)
	if w := uiRequest(srv, http.MethodGet, memberURL(newRepoDir(t, 3)), ""); w.Code != http.StatusBadRequest {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestWorkspaceMemberMissingOutranksPush(t *testing.T) {
	t.Parallel()
	srv, a, _ := wsServer(t)
	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	srv.SetWorkspace(&domain.WorkspaceInfo{Name: "shop", Members: []domain.WorkspaceMember{
		{Path: a, Svc: domain.Open(a)}, {Path: gone, Svc: domain.Open(gone)}, // pushed as present
	}})
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	w := uiRequest(srv, http.MethodGet, memberURL(gone), "")
	var out struct {
		Missing bool `json:"missing"`
	}
	decodeBody(t, w.Body.Bytes(), &out)
	if w.Code != http.StatusOK || !out.Missing {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestWorkspaceMemberReadErrorIs200WithError(t *testing.T) {
	t.Parallel()
	srv, a, _ := wsServer(t)
	notRepo := t.TempDir() // exists, but no git can read it
	srv.SetWorkspace(&domain.WorkspaceInfo{Name: "shop", Members: []domain.WorkspaceMember{
		{Path: a, Svc: domain.Open(a)}, {Path: notRepo, Svc: domain.Open(notRepo)},
	}})
	w := uiRequest(srv, http.MethodGet, memberURL(notRepo), "")
	var out struct {
		Error   string `json:"error"`
		Missing bool   `json:"missing"`
	}
	decodeBody(t, w.Body.Bytes(), &out)
	if w.Code != http.StatusOK || out.Error == "" || out.Missing {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestMemberChangedFansOutDuringOp(t *testing.T) {
	t.Parallel()
	srv, _, b := wsServer(t)
	srv.startLive(context.Background())
	hub := srv.liveHubRef()
	hub.gate = func() bool { return true } // an operation is running: emit drops
	ch, cancel := hub.subscribe()
	defer cancel()
	srv.MemberChanged(b)
	select {
	case m := <-ch:
		if m.Reason != "workspace_member" || m.Path != b {
			t.Fatalf("got %+v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no workspace_member event while an operation runs")
	}
}

func TestWorkspaceCountersSurviveHubRestart(t *testing.T) {
	t.Parallel()
	srv, _, b := wsServer(t)
	srv.startLive(context.Background())
	srv.MemberChanged(b)
	srv.restartLive(context.Background())
	var out struct {
		Open *wsOpenWire `json:"open"`
	}
	decodeBody(t, uiRequest(srv, http.MethodGet, "/api/workspace", "").Body.Bytes(), &out)
	if out.Open.Members[1].Changes != 1 {
		t.Fatalf("counter lost: %+v", out.Open.Members[1])
	}
}

func TestWorkspaceRoutesBlockedByUIGate(t *testing.T) {
	t.Parallel()
	srv := blockedServer(t)
	for _, p := range []string{"/api/workspace", memberURL("/x")} {
		if w := uiRequest(srv, http.MethodGet, p, ""); !refusedAsInactive(w) {
			t.Fatalf("%s: %d %s", p, w.Code, w.Body)
		}
	}
}

func TestSwitchedCarriesMemberFlag(t *testing.T) {
	t.Parallel()
	srv, a, b := wsServer(t)
	srv.startLive(context.Background())
	ctx := context.Background()
	if !srv.memberSwitch(ctx, domain.Open(a), domain.Open(b)) {
		t.Fatal("a to b: want a member switch")
	}
	if srv.memberSwitch(ctx, domain.Open(a), domain.Open(a)) {
		t.Fatal("within a: repository-wide lists stay")
	}
	if srv.memberSwitch(ctx, domain.Open(a), domain.Open(newRepoDir(t, 3))) {
		t.Fatal("outside the members: not a member switch")
	}
	ch, cancel := srv.liveHubRef().subscribe()
	defer cancel()
	if err := srv.adoptService(ctx, domain.Open(b)); err != nil {
		t.Fatal(err)
	}
	for m := range ch {
		if m.Reason == "switched" {
			if !m.Member || !model.SamePath(m.Worktree, b) {
				t.Fatalf("got %+v", m)
			}
			return
		}
	}
	t.Fatal("no switched message")
}

func TestWebWorkspacePostBusyRefusedBeforeActor(t *testing.T) {
	t.Parallel()
	srv, _, _ := wsServer(t)
	srv.SetWorkspaceActor(func(context.Context, domain.WorkspaceRequest) (domain.WorkspaceReply, error) {
		t.Error("the owner must not be asked while an operation runs")
		return domain.WorkspaceReply{}, nil
	})
	srv.cur = &opRun{} // a live op (not done)
	if w := uiRequest(srv, http.MethodPost, "/api/workspace", `{"kind":"leave"}`); w.Code != http.StatusConflict {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestWebWorkspacePostAsksThenRepeats(t *testing.T) {
	t.Parallel()
	srv, _, _ := wsServer(t)
	var drops []string
	srv.SetWorkspaceActor(func(_ context.Context, req domain.WorkspaceRequest) (domain.WorkspaceReply, error) {
		drops = append(drops, req.Drop)
		if req.Drop == "" {
			return domain.WorkspaceReply{DropSessions: []domain.SessionInfo{{ID: "s1", Dir: "/x"}}}, nil
		}
		return domain.WorkspaceReply{Status: "left workspace shop"}, nil
	})
	w := uiRequest(srv, http.MethodPost, "/api/workspace", `{"kind":"leave"}`)
	var asked struct {
		Drop []map[string]any `json:"drop_sessions"`
	}
	decodeBody(t, w.Body.Bytes(), &asked)
	if w.Code != http.StatusConflict || len(asked.Drop) != 1 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w = uiRequest(srv, http.MethodPost, "/api/workspace", `{"kind":"leave","drop":"keep"}`)
	var done struct {
		Status string `json:"status"`
	}
	decodeBody(t, w.Body.Bytes(), &done)
	if w.Code != http.StatusOK || done.Status != "left workspace shop" || len(drops) != 2 || drops[1] != "keep" {
		t.Fatalf("%d %s, drops %v", w.Code, w.Body, drops)
	}
}

func TestWebWorkspacePostRefusalAndNoOwner(t *testing.T) {
	t.Parallel()
	srv, _, _ := wsServer(t)
	if w := uiRequest(srv, http.MethodPost, "/api/workspace", `{"kind":"leave"}`); w.Code != http.StatusConflict {
		t.Fatalf("no owner: %d %s", w.Code, w.Body)
	}
	srv.SetWorkspaceActor(func(context.Context, domain.WorkspaceRequest) (domain.WorkspaceReply, error) {
		return domain.WorkspaceReply{}, errors.New("the reason")
	})
	w := uiRequest(srv, http.MethodPost, "/api/workspace", `{"kind":"leave"}`)
	var out struct {
		Error string `json:"error"`
	}
	decodeBody(t, w.Body.Bytes(), &out)
	if w.Code != http.StatusConflict || out.Error != "the reason" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestWebWorkspaceAddResolvesMainFirst(t *testing.T) {
	t.Parallel()
	srv, _, _ := wsServer(t)
	repo := newRepoDir(t, 3)
	linked := filepath.Join(t.TempDir(), "linked")
	gitRun(t, repo, "worktree", "add", "-b", "feat", linked)
	var got []string
	srv.SetWorkspaceActor(func(_ context.Context, req domain.WorkspaceRequest) (domain.WorkspaceReply, error) {
		got = req.Args
		return domain.WorkspaceReply{}, nil
	})
	body, _ := json.Marshal(map[string]any{"kind": "edit", "edit": "add", "name": "shop", "args": []string{linked}})
	if w := uiRequest(srv, http.MethodPost, "/api/workspace", string(body)); w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(got) != 1 || !model.SamePath(got[0], repo) {
		t.Fatalf("args %v, want %s", got, repo)
	}
	body, _ = json.Marshal(map[string]any{"kind": "edit", "edit": "add", "name": "shop", "args": []string{t.TempDir()}})
	if w := uiRequest(srv, http.MethodPost, "/api/workspace", string(body)); w.Code != http.StatusConflict {
		t.Fatalf("not a repository: %d %s", w.Code, w.Body)
	}
}

func TestRerootRepairRunsOnMemberService(t *testing.T) {
	t.Parallel()
	srv, a, b := wsServer(t)
	linked := filepath.Join(t.TempDir(), "linked")
	gitRun(t, b, "worktree", "add", "-b", "feat", linked)
	ctx := context.Background()
	got := srv.repairService(ctx, linked)
	if !model.SamePath(got.Root(), b) {
		t.Fatalf("repair on %s, want the holding member %s", got.Root(), b)
	}
	if got := srv.repairService(ctx, "/nowhere"); !model.SamePath(got.Root(), a) {
		t.Fatalf("no member holds it: repair on %s, want the service on screen", got.Root())
	}
}
