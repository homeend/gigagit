package web

import (
	"context"
	"encoding/json"
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
