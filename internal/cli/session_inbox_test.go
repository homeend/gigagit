package cli

import (
	"bytes"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// Serial: swaps the sessionGetenv seam.
func TestPreferredInboxUsesLiveGGInbox(t *testing.T) {
	own, cwd := t.TempDir(), t.TempDir()
	livePresence(t, own)
	withGGInbox(t, own)
	if got := preferredInbox(cwd); got != own {
		t.Fatalf("got %s, want the live GG_INBOX %s", got, own)
	}
}

// Serial: swaps the sessionGetenv seam.
func TestPreferredInboxFallsBackWhenNotLive(t *testing.T) {
	stale, cwd := t.TempDir(), t.TempDir()
	withGGInbox(t, stale)
	if got := preferredInbox(cwd); got != cwd {
		t.Fatalf("got %s, want the cwd inbox", got)
	}
}

// Serial: swaps the sessionGetenv seam. The agent runs in one worktree, the
// gg that started it shows another: the command goes to GG_INBOX and names
// the agent's worktree.
func TestSessionNavigateGoesToGGInboxAndNamesTheWorktree(t *testing.T) {
	own, cwd := t.TempDir(), t.TempDir()
	livePresence(t, own)
	withGGInbox(t, own)
	repo := newCLIRepo(t)
	svc := domain.Open(repo)
	var out, errb bytes.Buffer
	if code := runSession(cwd, svc, []string{"navigate", "--file", "README.md", "--new-line", "2", "--no-wait"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	got := steer.Drain(own)
	if len(got) != 1 || !domain.SamePath(got[0].Worktree, repo) {
		t.Fatalf("GG_INBOX got %+v, want one navigate naming %s", got, repo)
	}
	if left := steer.Drain(cwd); len(left) != 0 {
		t.Fatalf("the cwd inbox must stay empty: %+v", left)
	}
}

func withGGInbox(t *testing.T, dir string) {
	t.Helper()
	old := sessionGetenv
	sessionGetenv = func(k string) string {
		if k == "GG_INBOX" {
			return dir
		}
		return ""
	}
	t.Cleanup(func() { sessionGetenv = old })
}
