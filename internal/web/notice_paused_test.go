package web

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// A merge stopped in another worktree shows on its /api/worktrees row and raises
// a notice with a switch; the worktree the page shows raises none.
func TestPausedWorktreeRowAndNotice(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1) // f.txt committed
	side := filepath.Join(t.TempDir(), "side")
	gitRun(t, dir, "worktree", "add", "-q", "-b", "side", side)
	write := func(d, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(d, "f.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(side, "side\n")
	gitRun(t, side, "commit", "-qam", "side")
	write(dir, "here\n")
	gitRun(t, dir, "commit", "-qam", "here")
	cur := strings.TrimSpace(gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD"))
	merge := exec.Command("git", "merge", cur)
	merge.Dir = side
	_ = merge.Run() // exits 1: the conflict

	ts := serve(t, New(domain.Open(dir)))
	var rows struct {
		Worktrees []worktreeRow `json:"worktrees"`
	}
	if code := getJSON(t, ts, "/api/worktrees", &rows); code != http.StatusOK {
		t.Fatalf("worktrees: %d", code)
	}
	found := false
	for _, w := range rows.Worktrees {
		if filepath.Base(w.Path) == "side" {
			found = w.Paused == "merge" && w.Conflicted
		} else if w.Paused != "" {
			t.Errorf("idle worktree row: %+v", w)
		}
	}
	if !found {
		t.Errorf("side row not paused: %+v", rows.Worktrees)
	}

	var got noticesResp
	if code := getJSON(t, ts, "/api/notifications", &got); code != http.StatusOK {
		t.Fatalf("notifications: %d", code)
	}
	var hit *noticeWire
	for i, n := range got.Notices {
		if strings.HasPrefix(n.ID, "worktree_paused_") {
			if hit != nil {
				t.Fatalf("two paused notices: %+v", got.Notices)
			}
			hit = &got.Notices[i]
		}
	}
	if hit == nil || !strings.Contains(hit.Title, "merge conflict in side") {
		t.Fatalf("notice = %+v", hit)
	}
	if !sessionNoticeID.MatchString(hit.ID) {
		t.Errorf("id %q would be refused by the session dismissal", hit.ID)
	}
	if hit.Actions[0].Reroot == "" || filepath.Base(hit.Actions[0].Reroot) != "side" {
		t.Errorf("first action = %+v", hit.Actions[0])
	}
}

// A sleeping workspace member's paused worktree raises its notice too.
func TestPausedWorktreeOfASleepingMember(t *testing.T) {
	t.Parallel()
	a := newRepoDir(t, 1)
	b := newRepoDir(t, 1)
	side := filepath.Join(t.TempDir(), "other")
	gitRun(t, b, "worktree", "add", "-q", "-b", "side", side)
	if err := os.WriteFile(filepath.Join(side, "f.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, side, "commit", "-qam", "side")
	if err := os.WriteFile(filepath.Join(b, "f.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, b, "commit", "-qam", "main")
	rebase := exec.Command("git", "rebase", strings.TrimSpace(gitRun(t, b, "rev-parse", "HEAD")))
	rebase.Dir = side
	_ = rebase.Run() // exits 1: the conflict

	svcA := domain.Open(a)
	srv := New(svcA)
	t.Cleanup(srv.Close)
	srv.SetWorkspace(&domain.WorkspaceInfo{Name: "shop", Members: []domain.WorkspaceMember{
		{Path: a, Label: "a", Svc: svcA},
		{Path: b, Label: "b", Svc: domain.Open(b)},
	}})
	var got noticesResp
	if code := getJSON(t, serve(t, srv), "/api/notifications", &got); code != http.StatusOK {
		t.Fatalf("notifications: %d", code)
	}
	for _, n := range got.Notices {
		if strings.Contains(n.Title, "rebase conflict in other") {
			return
		}
	}
	t.Errorf("no notice for the sleeping member: %+v", got.Notices)
}
