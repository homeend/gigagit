package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

var tourWire = steer.Overview{ID: "f7", Title: "Tour", State: "shown", Anchors: 3, Unresolved: []string{"gone.go:4"}}

func runOverview(t *testing.T, dir string, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	var in *strings.Reader
	if stdin != "" {
		in = strings.NewReader(stdin)
	}
	var code int
	if in != nil {
		code = runSessionIn(dir, nil, append([]string{"overview"}, args...), in, &out, &errb)
	} else {
		code = runSessionIn(dir, nil, append([]string{"overview"}, args...), nil, &out, &errb)
	}
	return code, out.String(), errb.String()
}

func TestSessionOverviewAddFromStdin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	seen := answer(t, dir, func(c steer.Command) steer.Reply {
		return steer.Reply{ID: c.ID, OK: true, Detail: "showing f7", Overviews: []steer.Overview{tourWire}}
	})
	code, out, errs := runOverview(t, dir, "# Tour\n[a](a.go)\n", "add", "--title", "Tour", "--background")
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errs)
	}
	c := <-seen
	if c.Cmd != "overview_add" || c.Title != "Tour" || c.Text != "# Tour\n[a](a.go)\n" || !c.Background || !c.Wait {
		t.Fatalf("posted %+v", c)
	}
	if out != "f7\nunresolved: gone.go:4\n" {
		t.Fatalf("stdout = %q", out)
	}
}

func TestSessionOverviewAddFromFileAsJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	seen := answer(t, dir, func(c steer.Command) steer.Reply {
		return steer.Reply{ID: c.ID, OK: true, Overviews: []steer.Overview{tourWire}}
	})
	f := filepath.Join(t.TempDir(), "tour.md")
	if err := os.WriteFile(f, []byte("from a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runOverview(t, dir, "", "add", "--title", "Tour", "--file", f, "--json")
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errs)
	}
	if c := <-seen; c.Text != "from a file" {
		t.Fatalf("text = %q", c.Text)
	}
	var got steer.Overview
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.ID != "f7" {
		t.Fatalf("json = %q (%v)", out, err)
	}
}

func TestSessionOverviewSetListShowRm(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	shown := tourWire
	shown.Text = "the text"
	reply := func(c steer.Command) steer.Reply {
		switch c.Cmd {
		case "overview_rm":
			return steer.Reply{ID: c.ID, OK: true, Detail: "closed f7"}
		case "overview_show":
			return steer.Reply{ID: c.ID, OK: true, Overviews: []steer.Overview{shown}}
		}
		return steer.Reply{ID: c.ID, OK: true, Detail: "set f7", Overviews: []steer.Overview{tourWire}}
	}
	seen := answer(t, dir, reply)
	if code, out, errs := runOverview(t, dir, "new", "set", "f7", "--title", "T2"); code != 0 || out != "f7\nunresolved: gone.go:4\n" {
		t.Fatalf("set: %d %q %q", code, out, errs)
	}
	if c := <-seen; c.Cmd != "overview_set" || c.FileID != "f7" || c.Text != "new" || c.Title != "T2" {
		t.Fatalf("set posted %+v", c)
	}
	seen = answer(t, dir, reply)
	if code, out, _ := runOverview(t, dir, "", "list"); code != 0 || out != "f7\tshown\t3 anchors\tTour\n" {
		t.Fatalf("list: %d %q", code, out)
	}
	<-seen
	seen = answer(t, dir, reply)
	if code, out, _ := runOverview(t, dir, "", "show", "f7"); code != 0 || out != "f7\tTour\n\nthe text\n" {
		t.Fatalf("show: %d %q", code, out)
	}
	<-seen
	answer(t, dir, reply)
	if code, out, _ := runOverview(t, dir, "", "rm", "f7"); code != 0 || out != "closed f7\n" {
		t.Fatalf("rm: %d %q", code, out)
	}
}

func TestSessionOverviewMisuse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	for _, tc := range []struct {
		stdin string
		args  []string
		want  string
	}{
		{"x", []string{"add"}, "--title is required"},
		{"", []string{"add", "--title", "T"}, "give the text with --file or on stdin"},
		{"  \n", []string{"add", "--title", "T"}, "the text is empty"},
		{strings.Repeat("x", 64<<10+1), []string{"add", "--title", "T"}, "the text is over 64 KiB"},
		{"x", []string{"add", "--title", "a\nb"}, "the title must be one line"},
		{"x", []string{"set"}, "usage"},
		{"", []string{"show", "t7"}, "usage"},
		{"", []string{"frob"}, "usage"},
	} {
		code, _, errs := runOverview(t, dir, tc.stdin, tc.args...)
		if code != 2 || !strings.Contains(errs, tc.want) {
			t.Errorf("%v: exit %d stderr %q, want 2 and %q", tc.args, code, errs, tc.want)
		}
	}
}

// With only gg web live the page answers the overview verbs itself.
func TestSessionOverviewGoesToAWebOnlySession(t *testing.T) {
	t.Parallel()
	if code, _, errs := runOverview(t, t.TempDir(), "", "list"); code != 1 || !strings.Contains(errs, "no gg session for this worktree") {
		t.Fatalf("nothing live: exit %d stderr %q", code, errs)
	}
	srv, ts := newSteerServer(t, http.StatusOK, `{"id":"x","ok":true,"detail":"showing f1","overviews":[{"id":"f1","title":"T","state":"shown","anchors":1}]}`)
	dir := t.TempDir()
	liveWebPresence(t, dir, ts.URL)
	code, out, errs := runOverview(t, dir, "[a](a.go:2)", "add", "--title", "T")
	if code != 0 || out != "f1\n" {
		t.Fatalf("web only: exit %d stdout %q stderr %q", code, out, errs)
	}
	if got := srv.commands(); len(got) != 1 || got[0].Cmd != "overview_add" || got[0].Text != "[a](a.go:2)" {
		t.Fatalf("posted %+v", got)
	}
}

// A background add or an eviction is news the agent must see in plain mode:
// the reply's detail follows the id when it says more than "showing <id>".
func TestSessionOverviewAddPrintsANewsworthyDetail(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ detail, want string }{
		{"showing f7", "f7\nunresolved: gone.go:4\n"},
		{"added f7 in the background (the action menu is open); closed a.go (20 files open)",
			"f7\nunresolved: gone.go:4\nadded f7 in the background (the action menu is open); closed a.go (20 files open)\n"},
	} {
		dir := t.TempDir()
		livePresence(t, dir)
		answer(t, dir, func(c steer.Command) steer.Reply {
			return steer.Reply{ID: c.ID, OK: true, Detail: tc.detail, Overviews: []steer.Overview{tourWire}}
		})
		code, out, errs := runOverview(t, dir, "x", "add", "--title", "T")
		if code != 0 || out != tc.want {
			t.Errorf("detail %q: exit %d stdout %q stderr %q, want %q", tc.detail, code, out, errs, tc.want)
		}
	}
}

// set, show and rm name the caller's worktree, so a live session showing
// another one refuses instead of acting on its own overview of that id.
func TestSessionOverviewVerbsSendTheCallersWorktree(t *testing.T) {
	t.Parallel()
	repo := newCLIRepo(t)
	svc := domain.Open(repo)
	dir := t.TempDir()
	livePresence(t, dir)
	for _, args := range [][]string{{"set", "f7", "--title", "T"}, {"show", "f7"}, {"rm", "f7"}} {
		seen := answer(t, dir, func(c steer.Command) steer.Reply {
			return steer.Reply{ID: c.ID, OK: true, Detail: "ok", Overviews: []steer.Overview{tourWire}}
		})
		var out, errb bytes.Buffer
		runSessionIn(dir, svc, append([]string{"overview"}, args...), strings.NewReader("x"), &out, &errb)
		if c := <-seen; c.Worktree == "" {
			t.Errorf("%v posted no worktree: %+v", args, c)
		}
	}
}
