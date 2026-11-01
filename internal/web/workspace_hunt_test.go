package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/workspace"
)

// Bugs replicated on the merged web workspaces, one test each.

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func saveWS(t *testing.T, srv *Server, es ...workspace.Entry) {
	t.Helper()
	if err := workspace.Save(srv.workspacesPath(), es); err != nil {
		t.Fatal(err)
	}
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal(what)
}

// sessionsSeam makes every member report one running session and counts kills.
func sessionsSeam(o *workspaceOwner) *int {
	kills := 0
	o.sessionsIn = func(context.Context, *domain.Service) []domain.SessionInfo {
		return []domain.SessionInfo{{ID: "s1", Label: "agent"}}
	}
	o.kill = func(domain.SessionID) error { kills++; return nil }
	return &kills
}

func TestRefusedOpenKillsNoSessions(t *testing.T) {
	srv, o, a, b := ownerFixture(t)
	saveWS(t, srv, workspace.Entry{Name: "shop", Repos: []string{a, b}}, workspace.Entry{Name: "empty", Repos: []string{filepath.Join(t.TempDir(), "nope")}})
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "open", Name: "shop"}); err != nil {
		t.Fatal(err)
	}
	kills := sessionsSeam(o)
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "open", Name: "empty", Drop: "stop"}); err == nil {
		t.Fatal("opening a workspace with no member must be refused")
	}
	if *kills != 0 {
		t.Fatalf("%d sessions killed by a refused open", *kills)
	}
}

func TestUnreadableRegistryRefusesBeforeAnythingMoves(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"newer", "format = 99\n", "newer"},
		{"corrupt", "this is = = not toml\n", "cannot be read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, o, a, b := ownerFixture(t)
			if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "open", Name: "shop"}); err != nil {
				t.Fatal(err)
			}
			kills := sessionsSeam(o)
			if err := os.WriteFile(srv.workspacesPath(), []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			for _, body := range []string{
				`{"kind":"edit","edit":"delete","name":"shop","drop":"stop"}`,
				`{"kind":"edit","edit":"remove","name":"shop","drop":"stop","args":["` + a + `"]}`,
				`{"kind":"open","name":"shop2"}`,
			} {
				w := uiRequest(srv, "POST", "/api/workspace", body)
				if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), tc.want) {
					t.Fatalf("%s: %d %s", body, w.Code, w.Body)
				}
			}
			if *kills != 0 {
				t.Fatalf("%d sessions killed", *kills)
			}
			if got := srv.service().Root(); got != a {
				t.Fatalf("moved to %s", got)
			}
			if data, _ := os.ReadFile(srv.workspacesPath()); string(data) != tc.body {
				t.Fatalf("file rewritten: %s", data)
			}
			if info := srv.workspaceInfo(); info == nil || len(info.Members) != 2 || info.Members[1].Path != b {
				t.Fatalf("info %+v", info)
			}
			w := uiRequest(srv, "GET", "/api/workspace", "")
			var out struct {
				DefinedError string `json:"defined_error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || !strings.Contains(out.DefinedError, tc.want) {
				t.Fatalf("listing hides it: %s", w.Body)
			}
		})
	}
}

func TestFailedOpenKeepsTheWorkspace(t *testing.T) {
	srv, o, a, b := ownerFixture(t)
	plain := t.TempDir()
	saveWS(t, srv, workspace.Entry{Name: "shop", Repos: []string{a, b}}, workspace.Entry{Name: "broken", Repos: []string{plain}})
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "open", Name: "shop"}); err != nil {
		t.Fatal(err)
	}
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "open", Name: "broken"}); err == nil {
		t.Fatal("a plain directory opened")
	}
	if info := srv.workspaceInfo(); info == nil || info.Name != "shop" {
		t.Fatalf("the workspace was closed: %+v", info)
	}
}

func TestOpenSkipsAnUnusableFirstMember(t *testing.T) {
	srv, o, a, b := ownerFixture(t)
	plain, c := t.TempDir(), newRepoDir(t, 3)
	saveWS(t, srv, workspace.Entry{Name: "shop", Repos: []string{a, b}}, workspace.Entry{Name: "mix", Repos: []string{plain, c}})
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "open", Name: "mix"}); err != nil {
		t.Fatal(err)
	}
	if got := srv.service().Root(); got != c {
		t.Fatalf("on %s, want %s", got, c)
	}
	if info := srv.workspaceInfo(); info == nil || info.Name != "mix" || info.Live != 1 {
		t.Fatalf("info %+v", info)
	}
}

func TestOpenABareFirstMemberUsesItsWorktree(t *testing.T) {
	srv, o, a, b := ownerFixture(t)
	root := t.TempDir()
	bare := filepath.Join(root, "bare.git")
	gitIn(t, a, "clone", "--bare", a, bare)
	wt := filepath.Join(root, "wt")
	gitIn(t, bare, "worktree", "add", "-b", "wtb", wt)
	saveWS(t, srv, workspace.Entry{Name: "shop", Repos: []string{a, b}}, workspace.Entry{Name: "b", Repos: []string{bare}})
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "open", Name: "b"}); err != nil {
		t.Fatal(err)
	}
	if got := srv.service().Root(); got != wt {
		t.Fatalf("on %s, want %s", got, wt)
	}
	if info := srv.workspaceInfo(); info == nil || info.Name != "b" {
		t.Fatalf("info %+v", info)
	}
}

func TestOpenWithLiveRepositoryDeleted(t *testing.T) {
	srv, o, _, b := ownerFixture(t)
	gone := newRepoDir(t, 4)
	srv.svc.Store(domain.Open(gone))
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "open", Name: "shop"}); err != nil {
		t.Fatal(err)
	}
	if info := srv.workspaceInfo(); info == nil || info.Name != "shop" {
		t.Fatalf("info %+v", info)
	}
	_ = b
}

func TestFetchTellsThePage(t *testing.T) {
	srv, o, a, b := ownerFixture(t)
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "open", Name: "shop"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "b never watched", func() bool { return len(o.watched()) == 1 })
	_ = a
	if err := os.WriteFile(filepath.Join(b, ".gg.toml"), []byte("[refresh]\nenabled = true\nfetch = 10\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	o.fetch = func(context.Context, *domain.Service) error { return nil }
	now := time.Now()
	o.fetchOnce(t.Context(), now)
	o.fetchOnce(t.Context(), now.Add(time.Minute))
	srv.mu.Lock()
	moved := srv.ws.changes[model.KeyOf(b)]
	srv.mu.Unlock()
	if moved == 0 {
		t.Fatal("a landed fetch did not tell the page")
	}
}

func TestStartDoesNotWaitForASleepingMember(t *testing.T) {
	srv, o, a, b := ownerFixture(t)
	release := make(chan struct{})
	defer close(release)
	srv.opener = func(path string) *domain.Service {
		if path == b {
			<-release
		}
		return domain.Open(path)
	}
	done := make(chan error, 1)
	go func() { done <- o.start(context.Background(), "shop") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("start waited for a sleeping member")
	}
	if info := srv.workspaceInfo(); info == nil || info.Members[0].Path != a || info.Members[1].Path != b {
		t.Fatalf("info %+v", info)
	}
}

func TestMemberComingBackIsNoLongerMissing(t *testing.T) {
	srv, o, a, _ := ownerFixture(t)
	c := filepath.Join(t.TempDir(), "c")
	saveWS(t, srv, workspace.Entry{Name: "shop", Repos: []string{a, c}})
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "open", Name: "shop"}); err != nil {
		t.Fatal(err)
	}
	if info := srv.workspaceInfo(); !info.Members[1].Missing {
		t.Fatalf("info %+v", info)
	}
	if err := os.MkdirAll(c, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, c, "init", "-q")
	o.recheckMembers()
	if info := srv.workspaceInfo(); info.Members[1].Missing {
		t.Fatalf("still missing: %+v", info.Members[1])
	}
	eventually(t, "c never watched", func() bool { return len(o.watched()) == 1 })
}

func TestMemberGoneIsMissingAtOnce(t *testing.T) {
	srv, o, _, b := ownerFixture(t)
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "open", Name: "shop"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "b never watched", func() bool { return len(o.watched()) == 1 })
	if err := os.Rename(b, b+".away"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(b+".away", b) })
	o.recheckMembers()
	if info := srv.workspaceInfo(); !info.Members[1].Missing {
		t.Fatalf("info %+v", info.Members[1])
	}
	if len(o.watched()) != 0 {
		t.Fatal("still watching a directory that is gone")
	}
}

func TestMemberRecreatedIsWatchedAgain(t *testing.T) {
	_, o, _, b := ownerFixture(t)
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "open", Name: "shop"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "b never watched", func() bool { return len(o.watched()) == 1 })
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, b, "init", "-q")
	o.recheckMembers()
	eventually(t, "b not re-watched", func() bool {
		o.mu.Lock()
		defer o.mu.Unlock()
		now, err := os.Stat(b)
		m := o.members[1]
		return err == nil && m.stop != nil && !m.missing && m.dir != nil && os.SameFile(m.dir, now)
	})
}

func TestSettingsRepoGoneIsNotRecreated(t *testing.T) {
	srv, o, a, _ := ownerFixture(t)
	c := newRepoDir(t, 3)
	saveWS(t, srv, workspace.Entry{Name: "shop", Repos: []string{a, c}, SettingsRepo: c})
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "open", Name: "shop"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "c never opened", func() bool {
		m, ok := srv.settingsMember()
		return ok && m.Svc != nil
	})
	if err := os.RemoveAll(c); err != nil {
		t.Fatal(err)
	}
	uiRequest(srv, "POST", "/api/settings", `{"show_graph":"off"}`)
	if _, err := os.Stat(c); err == nil {
		t.Fatal("saving a setting recreated a deleted repository")
	}
}

func TestLastViewedSetWhenOpenedInALinkedWorktree(t *testing.T) {
	a, b := newRepoDir(t, 1), newRepoDir(t, 2)
	isolateGlobal(t)
	wt := filepath.Join(t.TempDir(), "a-wt")
	gitIn(t, a, "worktree", "add", "-b", "linked", wt)
	srv := New(domain.Open(wt))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	t.Cleanup(srv.Close)
	saveWS(t, srv, workspace.Entry{Name: "shop", Repos: []string{a, b}})
	o := newWorkspaceOwner(srv)
	t.Cleanup(o.close)
	if err := o.start(t.Context(), "shop"); err != nil {
		t.Fatal(err)
	}
	if info := srv.workspaceInfo(); info.Members[0].LastViewed != wt {
		t.Fatalf("last viewed %q, want %q", info.Members[0].LastViewed, wt)
	}
}

func TestLaunchAnswerRereadsTheRegistry(t *testing.T) {
	srv, o, _, _ := ownerFixture(t)
	if err := o.start(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "edit", Edit: "delete", Name: "shop"}); err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	q := srv.ws.question
	srv.mu.Unlock()
	if q != nil {
		t.Fatalf("question still up: %+v", q)
	}
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "launch", Name: "shop"}); err != nil {
		t.Fatal(err)
	}
	if srv.workspaceInfo() != nil {
		t.Fatal("a deleted workspace opened")
	}
}

func TestAddWithoutAPathIsRefused(t *testing.T) {
	srv, o, _, _ := ownerFixture(t)
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "open", Name: "shop"}); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"kind":"edit","edit":"add","name":"shop"}`, `{"kind":"edit","edit":"add","name":"shop","args":["  "]}`} {
		if w := uiRequest(srv, "POST", "/api/workspace", body); w.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body)
		}
	}
	if got := workspace.Load(srv.workspacesPath()); len(got[0].Repos) != 2 {
		t.Fatalf("repos %v", got[0].Repos)
	}
}

func TestWorkspacePostBodyIsCapped(t *testing.T) {
	srv, _, _, _ := ownerFixture(t)
	w := uiRequest(srv, "POST", "/api/workspace", `{"kind":"`+strings.Repeat("x", 2<<20)+`"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("%d", w.Code)
	}
}

func TestHandEditedSpellingsAreEditable(t *testing.T) {
	srv, o, a, b := ownerFixture(t)
	wt := filepath.Join(t.TempDir(), "b-wt")
	gitIn(t, b, "worktree", "add", "-b", "linked", wt)
	saveWS(t, srv, workspace.Entry{Name: "shop", Repos: []string{a, wt}})
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "edit", Edit: "remove", Name: "shop", Args: []string{b}}); err != nil {
		t.Fatal(err)
	}
	if got := workspace.Load(srv.workspacesPath()); len(got[0].Repos) != 1 {
		t.Fatalf("remove by the normalized path changed nothing: %v", got[0].Repos)
	}
}

func TestSymlinkSpellingIsAMember(t *testing.T) {
	srv, o, a, b := ownerFixture(t)
	lnk := filepath.Join(t.TempDir(), "lnk")
	if err := os.Symlink(a, lnk); err != nil {
		t.Skip(err)
	}
	saveWS(t, srv, workspace.Entry{Name: "shop", Repos: []string{lnk, b}})
	if err := o.start(t.Context(), "shop"); err != nil {
		t.Fatal(err)
	}
	if info := srv.workspaceInfo(); info == nil || info.Members[0].Path != a {
		t.Fatalf("info %+v", info)
	}
}

func TestSettingsRepoGovernsCommitSortAndGraph(t *testing.T) {
	srv, o, a, b := ownerFixture(t)
	saveWS(t, srv, workspace.Entry{Name: "shop", Repos: []string{a, b}, SettingsRepo: b})
	if _, err := o.act(t.Context(), domain.WorkspaceRequest{Kind: "open", Name: "shop"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "b never opened", func() bool {
		m, ok := srv.settingsMember()
		return ok && m.Svc != nil
	})
	if w := uiRequest(srv, "POST", "/api/settings", `{"show_graph":"off","commit_sort":"plain"}`); w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var h struct {
		ShowGraph  string `json:"show_graph"`
		CommitSort string `json:"commit_sort"`
	}
	if err := json.Unmarshal(uiRequest(srv, "GET", "/api/health", "").Body.Bytes(), &h); err != nil {
		t.Fatal(err)
	}
	if h.ShowGraph != "off" || h.CommitSort != "plain" {
		t.Fatalf("health %+v", h)
	}
}
