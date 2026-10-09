package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// publishViewing writes a live registry file for a TUI rooted at home whose
// panels show viewed (what domain.PublishSessions writes on a look).
func publishViewing(t *testing.T, home, viewed string) {
	t.Helper()
	dir := domain.SessionRegistryDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"pid":` + strconv.Itoa(os.Getpid()) + `,"worktree":` + jsonString(home) + `,"viewed":` + jsonString(viewed) + `,"sessions":[]}`
	if err := os.WriteFile(filepath.Join(dir, "viewtest-"+filepath.Base(viewed)+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// An agent in worktree B that gg did not start: no inbox is live in B, but
// a TUI rooted elsewhere SHOWS B — the command goes to that TUI's inbox.
func TestPreferredInboxReachesTheTUIShowingThisWorktree(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	livePresence(t, home)
	publishViewing(t, home, cwd)
	if got := preferredInbox(cwd); got != home {
		t.Fatalf("got %s, want the inbox of the TUI showing this worktree, %s", got, home)
	}
}

// A TUI live in this very worktree still wins over one that merely shows it.
func TestPreferredInboxPrefersTheTUIRootedHere(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	livePresence(t, home)
	livePresence(t, cwd)
	publishViewing(t, home, cwd)
	if got := preferredInbox(cwd); got != cwd {
		t.Fatalf("got %s, want this worktree's own inbox", got)
	}
}

// gg session status says which worktree the TUI shows when that is not its own.
func TestSessionStatusSaysWhatTheTUIShows(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	snap := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(snap, []byte(`{"repo":{"worktree":"/w/home","viewed":"/w/other"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := domain.Open(newCLIRepo(t))
	var out, errb bytes.Buffer
	if code := sessionStatusAt(dir, svc, nil, &out, &errb, snap); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}
	if !strings.Contains(out.String(), "showing: /w/other") {
		t.Fatalf("stdout = %q, want a showing: line", out.String())
	}
	out.Reset()
	if code := sessionStatusAt(dir, svc, []string{"--json"}, &out, &errb, snap); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["showing"] != "/w/other" {
		t.Fatalf("json showing = %v, want /w/other", got["showing"])
	}
}
