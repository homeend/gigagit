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
