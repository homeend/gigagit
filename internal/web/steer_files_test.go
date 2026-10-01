package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/steer"
)

// steerAsk posts one command and decodes the synchronous answer.
func steerAsk(t *testing.T, s *Server, body string) (int, steer.Reply) {
	t.Helper()
	var rep steer.Reply
	code := postJSON(t, serve(t, s), "/api/session/steer", body, "application/json", "", &rep)
	return code, rep
}

func TestSteerFilesAnswersFromTheList(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	code, rep := steerAsk(t, s, `{"id":"1-1","cmd":"files"}`)
	if code != http.StatusOK || !rep.OK || rep.ID != "1-1" || rep.Detail != "no open files" || len(rep.Files) != 0 {
		t.Fatalf("empty: code=%d rep=%+v", code, rep)
	}
	s.ofs.open(s.service().Root(), ofKey{Src: "worktree", Path: "f.txt"}, "", 3)
	code, rep = steerAsk(t, s, `{"id":"1-2","cmd":"files"}`)
	if code != http.StatusOK || !rep.OK || len(rep.Files) != 1 {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
	if f := rep.Files[0]; f.ID != "f1" || f.Path != "f.txt" || f.Source != "worktree" || f.Line != 3 || f.State != "background" {
		t.Fatalf("file = %+v", f)
	}
}

// files is read-only: an op in flight does not stop it (only a steer the hub
// must deliver is refused with 409).
func TestSteerFilesAnswersWhileAnOpIsInFlight(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	s.cur = &opRun{} // opInFlight() is true
	if code, rep := steerAsk(t, s, `{"id":"1-1","cmd":"files"}`); code != http.StatusOK || !rep.OK {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
}

func TestSteerBackgroundOpensIntoTheList(t *testing.T) {
	isolateGlobal(t)
	s := newSteerServer(t)
	s.startLive(context.Background())
	t.Cleanup(s.Close)
	ts := serve(t, s)
	next, stop := eventsFor(t, ts, "t1")
	defer stop()
	next() // hello
	var rep steer.Reply
	code := postJSON(t, ts, "/api/session/steer",
		`{"id":"1-1","cmd":"navigate","file":"f.txt","hint_kind":"view","hint_id":"content","background":true,"line":{"no":1}}`,
		"application/json", "", &rep)
	if code != http.StatusOK || !rep.OK || rep.Detail != "opened f.txt in the background at line 1" {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
	m := next()
	if m.Reason != "open_files" || m.Opened != "f.txt" || len(m.Files) != 1 || m.Files[0].State != "background" || m.Files[0].Line != 1 {
		t.Fatalf("event = %+v", m)
	}
	// Past the end: the server's own read says so, in the TUI's words.
	_ = postJSON(t, ts, "/api/session/steer",
		`{"id":"1-2","cmd":"navigate","file":"f.txt","hint_kind":"view","hint_id":"content","background":true,"line":{"no":9}}`,
		"application/json", "", &rep)
	if rep.Detail != "opened f.txt in the background at line 1 (line 9 is past the end, 1 lines)" {
		t.Fatalf("past the end: %+v", rep)
	}
}

func TestSteerBackgroundRefusals(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	s.steerWorktree = s.service().Root()
	for _, tc := range []struct{ body, want string }{
		{`{"id":"1","cmd":"navigate","file":"nope.txt","hint_kind":"view","hint_id":"content","background":true}`, "nope.txt is not in the working tree"},
		{`{"id":"2","cmd":"navigate","file":"f.txt","hint_kind":"view","hint_id":"content","background":true,"worktree":"/elsewhere"}`, "gg web is showing worktree " + s.service().Root() + ", not /elsewhere"},
	} {
		code, rep := steerAsk(t, s, tc.body)
		if code != http.StatusOK || rep.OK || rep.Error != tc.want {
			t.Errorf("%s: code=%d rep=%+v, want error %q", tc.body, code, rep, tc.want)
		}
	}
}

// A file a tab is looking at is left exactly as the user has it.
func TestSteerBackgroundLeavesAShownFileAlone(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	wt := s.service().Root()
	s.ofs.open(wt, ofKey{Src: "worktree", Path: "f.txt"}, "t1", 1)
	code, rep := steerAsk(t, s, `{"id":"1","cmd":"navigate","file":"f.txt","hint_kind":"view","hint_id":"content","background":true,"line":{"no":1}}`)
	if code != http.StatusOK || !rep.OK || rep.Detail != "f.txt is already open on screen" {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
}

// A background open touches no screen: an op in flight does not stop it.
func TestSteerBackgroundAnswersWhileAnOpIsInFlight(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	s.cur = &opRun{}
	code, rep := steerAsk(t, s, `{"id":"1","cmd":"navigate","file":"f.txt","hint_kind":"view","hint_id":"content","background":true}`)
	if code != http.StatusOK || !rep.OK || rep.Detail != "opened f.txt in the background" {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
}

func TestSteerLandedWords(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		line, n int
		known   bool
		ev      string
		want    string
	}{
		{0, 5, true, "", "focused a"},
		{3, 5, true, "", "focused a at line 3"},
		{9, 5, true, "", "focused a at line 5 (line 9 is past the end, 5 lines)"},
		{3, 0, false, "", "focused a"},
		{0, 5, true, "b.txt", "focused a; closed b.txt (100 files open)"},
	} {
		if got := steerLanded("focused a", tc.line, tc.n, tc.known, tc.ev); got != tc.want {
			t.Errorf("steerLanded(%d,%d,%v,%q) = %q, want %q", tc.line, tc.n, tc.known, tc.ev, got, tc.want)
		}
	}
}

func TestSteerFileFocusBroadcastsTheIDToEveryTab(t *testing.T) {
	isolateGlobal(t)
	s := newSteerServer(t)
	s.startLive(context.Background())
	t.Cleanup(s.Close)
	ts := serve(t, s)
	wt := s.service().Root()
	s.ofs.open(wt, ofKey{Src: "worktree", Path: "f.txt"}, "", 0)
	next, stop := eventsFor(t, ts, "t1")
	defer stop()
	next() // hello
	var rep steer.Reply
	code := postJSON(t, ts, "/api/session/steer", `{"id":"1-1","cmd":"file_focus","file":"f.txt","line":{"no":1}}`, "application/json", "", &rep)
	if code != http.StatusOK || !rep.OK || rep.Detail != "focused f.txt at line 1" {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
	var sawList, sawSteer bool
	for i := 0; i < 2; i++ {
		m := next()
		switch m.Reason {
		case "open_files":
			sawList = len(m.Files) == 1 && m.Files[0].Line == 1
		case "steer":
			sawSteer = m.Steer != nil && m.Steer.Cmd == "file_focus" && m.Steer.FileID == "f1" && m.Steer.File == "" && m.Steer.Line == 1
		}
	}
	if !sawList || !sawSteer {
		t.Fatalf("list=%v steer=%v — the tabs need the id, never a path", sawList, sawSteer)
	}
}

func TestSteerFileFocusUnknownAndNoTab(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	s.ofs.open(s.service().Root(), ofKey{Src: "worktree", Path: "f.txt"}, "", 0)
	if code, rep := steerAsk(t, s, `{"id":"1","cmd":"file_focus","file_id":"f9"}`); code != http.StatusOK || rep.OK || rep.Error != "no open file f9" {
		t.Fatalf("unknown: code=%d rep=%+v", code, rep)
	}
	// No browser tab streams: the agent must not be told it is on screen.
	if _, rep := steerAsk(t, s, `{"id":"2","cmd":"file_focus","file_id":"f1"}`); !rep.OK || rep.Detail != "focused f.txt; no gg web tab is open to show it" {
		t.Fatalf("no tab: %+v", rep)
	}
}

// A focus DOES ride the hub's steer lane, so it keeps the 409 while an op runs.
func TestSteerFileFocusIs409WhileAnOpIsInFlight(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	s.ofs.open(s.service().Root(), ofKey{Src: "worktree", Path: "f.txt"}, "", 0)
	s.cur = &opRun{}
	if code := steerPost(t, s, `{"id":"1","cmd":"file_focus","file_id":"f1"}`, "application/json"); code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", code)
	}
}

func TestLiveJSRoutesTheOpenFilesVerbs(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "live.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		`case "file_focus":`,
		`openViewer({ id: s.file_id, line: s.line || 0 })`,
		`if (msg.opened) opLine(msg.opened + " opened in the background", false);`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("live.js: missing %q", want)
		}
	}
	// Key hints live in the footer only (ruling): the toast names no key.
	if strings.Contains(src, `opened in the background — ctrl`) {
		t.Error("live.js: the background toast advertises a key")
	}
}
