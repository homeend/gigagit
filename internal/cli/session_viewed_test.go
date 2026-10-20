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

// A standalone gg web page live in this worktree keeps its commands: a TUI
// that merely shows the worktree does not take them.
func TestPreferredInboxKeepsAStandaloneWebPageHere(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	livePresence(t, home)
	liveWebPresence(t, cwd, "http://127.0.0.1:1/")
	publishViewing(t, home, cwd)
	if got := preferredInbox(cwd); got != cwd {
		t.Fatalf("got %s, want this worktree's own inbox (its web page is live)", got)
	}
	if got := preferredInboxFor(cwd, "tui"); got != home {
		t.Fatalf("--to tui: got %s, want the TUI showing this worktree, %s", got, home)
	}
}

// gg session status leads with the caller's own worktree and whether the
// window shows it, and labels the window's home "runs in:" — a bare
// "worktree: <home>" once read as "the window shows <home>" to an agent
// whose worktree WAS on screen through a look.
func TestSessionStatusSaysWhetherThisWorktreeIsShown(t *testing.T) {
	t.Parallel()
	repo := newCLIRepo(t)
	svc := domain.Open(repo)
	top := callerWorktree(svc)
	for _, tc := range []struct {
		name, viewed, wantLine string
		wantShown              bool
	}{
		{"a look shows it", top, "this worktree: " + top + " — shown", true},
		{"another worktree on screen", "/w/other", "this worktree: " + top + " — not shown (the window shows /w/other)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			livePresence(t, dir) // the window runs in /w
			snap := filepath.Join(t.TempDir(), "session.json")
			if err := os.WriteFile(snap, []byte(`{"repo":{"worktree":"/w","viewed":`+jsonString(tc.viewed)+`}}`), 0o644); err != nil {
				t.Fatal(err)
			}
			var out, errb bytes.Buffer
			if code := sessionStatusAt(dir, svc, nil, &out, &errb, snap); code != 0 {
				t.Fatalf("exit = %d (stderr %q)", code, errb.String())
			}
			lines := strings.Split(strings.TrimSpace(out.String()), "\n")
			if lines[0] != tc.wantLine {
				t.Fatalf("first line = %q, want %q (stdout %q)", lines[0], tc.wantLine, out.String())
			}
			if !strings.Contains(out.String(), "runs in: /w\n") || strings.Contains(out.String(), "worktree: /w\n") {
				t.Fatalf("stdout = %q, want the window's home as runs in:, never a bare worktree: line", out.String())
			}
			out.Reset()
			if code := sessionStatusAt(dir, svc, []string{"--json"}, &out, &errb, snap); code != 0 {
				t.Fatalf("json exit = %d", code)
			}
			var got map[string]any
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got["this_worktree"] != top || got["shown"] != tc.wantShown || got["worktree"] != "/w" {
				t.Fatalf("json = %s, want this_worktree %s, shown %v, worktree /w kept", out.String(), top, tc.wantShown)
			}
		})
	}
}

// No look: the window shows its own worktree, so "shown" compares the
// caller with that.
func TestSessionStatusShownWithoutALook(t *testing.T) {
	t.Parallel()
	svc := domain.Open(newCLIRepo(t))
	dir := t.TempDir()
	livePresence(t, dir) // runs in /w, no snapshot
	var out, errb bytes.Buffer
	if code := sessionStatusAt(dir, svc, nil, &out, &errb, ""); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}
	want := "this worktree: " + callerWorktree(svc) + " — not shown (the window shows /w)"
	if first := strings.SplitN(out.String(), "\n", 2)[0]; first != want {
		t.Fatalf("first line = %q, want %q", first, want)
	}
}
