package cli

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/steer"
)

// bothLive is a worktree inbox where a TUI and a gg web page are both live
// (the TUI serving its own page): the web server records what it gets.
func bothLive(t *testing.T, body string) (string, *steerServer) {
	t.Helper()
	srv, ts := newSteerServer(t, http.StatusOK, body)
	dir := t.TempDir()
	livePresence(t, dir)
	liveWebPresence(t, dir, ts.URL)
	return dir, srv
}

// --to web: an overview the agent adds goes to the page (which shows it in
// its tabs), not to the TUI, even while the TUI is live.
func TestSessionToWebSkipsTheTUI(t *testing.T) {
	t.Parallel()
	dir, srv := bothLive(t, `{"id":"x","ok":true,"detail":"showing f3","overviews":[{"id":"f3","title":"T","state":"shown","anchors":1}]}`)
	code, out, errs := runOverview(t, dir, "[a](a.go)", "add", "--to", "web", "--title", "T")
	if code != 0 || out != "f3\n" {
		t.Fatalf("exit %d stdout %q stderr %q", code, out, errs)
	}
	if got := srv.commands(); len(got) != 1 || got[0].Cmd != "overview_add" {
		t.Fatalf("web got %+v", got)
	}
	if left := steer.Drain(dir); len(left) != 0 {
		t.Fatalf("the TUI's inbox got %+v", left)
	}
}

// --to tui: a screen verb that goes to both by default reaches the TUI only.
func TestSessionToTUISkipsThePage(t *testing.T) {
	t.Parallel()
	dir, srv := bothLive(t, `{}`)
	seen := answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Detail: "ok"} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"reload", "--to=tui"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d stderr %q", code, errb.String())
	}
	select {
	case c := <-seen:
		if c.Cmd != "reload" {
			t.Fatalf("TUI got %+v", c)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the TUI got nothing")
	}
	if got := srv.commands(); len(got) != 0 {
		t.Fatalf("the page got %+v", got)
	}
}

// --to web on a screen verb: the page alone; the TUI's inbox stays empty.
func TestSessionToWebOnAScreenVerb(t *testing.T) {
	t.Parallel()
	dir, srv := bothLive(t, `{}`)
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"reload", "--to", "web"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d stderr %q", code, errb.String())
	}
	if got := srv.commands(); len(got) != 1 || got[0].Cmd != "reload" {
		t.Fatalf("the page got %+v", got)
	}
	if left := steer.Drain(dir); len(left) != 0 {
		t.Fatalf("the TUI's inbox got %+v", left)
	}
}

// Asking for a side that is not running says so; nothing else is tried.
func TestSessionToASideThatIsNotLive(t *testing.T) {
	t.Parallel()
	srv, ts := newSteerServer(t, http.StatusOK, `{}`)
	webOnly := t.TempDir()
	liveWebPresence(t, webOnly, ts.URL)
	tuiOnly := t.TempDir()
	livePresence(t, tuiOnly)
	for _, tc := range []struct {
		dir, to, want string
	}{
		{webOnly, "tui", "no gg TUI for this worktree (gg web is live)"},
		{tuiOnly, "web", "no gg web page for this worktree (a gg TUI is live)"},
		{t.TempDir(), "web", "no gg session for this worktree"},
	} {
		var out, errb bytes.Buffer
		if code := runSession(tc.dir, nil, []string{"note", "list", "--to", tc.to}, &out, &errb); code != 1 || !strings.Contains(errb.String(), tc.want) {
			t.Errorf("--to %s: exit %d stderr %q, want %q", tc.to, code, errb.String(), tc.want)
		}
	}
	if got := srv.commands(); len(got) != 0 {
		t.Fatalf("the page got %+v", got)
	}
	if left := steer.Drain(tuiOnly); len(left) != 0 {
		t.Fatalf("the TUI got %+v", left)
	}
}

func TestSessionToMisuse(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"reload", "--to", "both"},
		{"reload", "--to"},
		{"reload", "--to="},
		{"status", "--to", "web"},
		{"--to", "web"},
	} {
		var out, errb bytes.Buffer
		if code := runSession(t.TempDir(), nil, args, &out, &errb); code != 2 {
			t.Errorf("%v: exit %d, want 2 (stderr %q)", args, code, errb.String())
		}
	}
}
