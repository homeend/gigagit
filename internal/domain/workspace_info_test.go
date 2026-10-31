package domain

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestDefaultWorktreeLastViewedThenMain(t *testing.T) {
	t.Parallel()
	wts := []model.Worktree{{Path: "/r/main"}, {Path: "/r/feat"}}
	if got := DefaultWorktree(wts, "/r/feat"); got != "/r/feat" {
		t.Fatalf("listed last viewed: got %q", got)
	}
	if got := DefaultWorktree(wts, "/r/gone"); got != "/r/main" {
		t.Fatalf("unlisted last viewed: got %q", got)
	}
	if got := DefaultWorktree(wts, ""); got != "/r/main" {
		t.Fatalf("none viewed: got %q", got)
	}
	bare := []model.Worktree{{Path: "/r.git", Bare: true}, {Path: "/r/wt"}}
	if got := DefaultWorktree(bare, ""); got != "/r/wt" {
		t.Fatalf("bare main: got %q", got)
	}
	if got := DefaultWorktree(nil, ""); got != "" {
		t.Fatalf("no list: got %q", got)
	}
}

func TestRunningSessionsInOnlyListedWorktrees(t *testing.T) {
	t.Parallel()
	infos := []SessionInfo{
		{Dir: "/r/main", State: SessionRunning},
		{Dir: "/r/feat", State: SessionExited},
		{Dir: "/other", State: SessionRunning},
	}
	got := runningSessionsAmong(infos, []model.Worktree{{Path: "/r/main"}, {Path: "/r/feat"}})
	if len(got) != 1 || got[0].Dir != "/r/main" {
		t.Fatalf("got %+v", got)
	}
}
