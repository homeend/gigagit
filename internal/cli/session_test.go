package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/steer"
)

// livePresence writes a fresh tui.json so the routing table's "TUI only" row
// applies. Liveness is the mtime, so simply writing the file makes it live.
func livePresence(t *testing.T, dir string) {
	t.Helper()
	if err := steer.Touch(dir, steer.TUIPresence, steer.Presence{PID: 4242, Worktree: "/w", Started: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
}

// liveWebPresence writes a fresh web.json pointing at url, so the routing
// table's "web" rows apply.
func liveWebPresence(t *testing.T, dir, url string) {
	t.Helper()
	if err := steer.Touch(dir, steer.WebPresence, steer.Presence{PID: 77, Worktree: "/w", URL: url}); err != nil {
		t.Fatal(err)
	}
}

// answer runs a fake consumer: it drains one command and replies. Returns the
// command it saw.
func answer(t *testing.T, dir string, reply func(steer.Command) steer.Reply) <-chan steer.Command {
	t.Helper()
	out := make(chan steer.Command, 1)
	go func() {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			cmds := steer.Drain(dir)
			if len(cmds) == 0 {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			c := cmds[0]
			if c.Wait {
				_ = steer.PostReply(dir, reply(c))
			}
			out <- c
			return
		}
		close(out)
	}()
	return out
}

func TestSessionStatusExitsOneWithNoPresence(t *testing.T) {
	t.Parallel()
	var out, errb bytes.Buffer
	svc := domain.Open(newCLIRepo(t))
	if code := runSession(t.TempDir(), svc, []string{"status"}, &out, &errb); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "no gg session") {
		t.Errorf("stderr = %q, want it to say there is no session", errb.String())
	}
}

func TestSessionStatusPrintsTheLiveTUI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	var out, errb bytes.Buffer
	svc := domain.Open(newCLIRepo(t))
	if code := runSession(dir, svc, []string{"status"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q), want 0", code, errb.String())
	}
	if !strings.Contains(out.String(), "4242") {
		t.Errorf("stdout = %q, want the TUI pid", out.String())
	}
}

func TestSessionStatusJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	var out, errb bytes.Buffer
	svc := domain.Open(newCLIRepo(t))
	if code := runSession(dir, svc, []string{"status", "--json"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("--json is not valid JSON (%v): %s", err, out.String())
	}
	if _, ok := got["tui"]; !ok {
		t.Errorf("json = %s, want a \"tui\" member", out.String())
	}
}

func TestSessionNavigateNoWaitPrintsTheIDAndExitsZero(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	var out, errb bytes.Buffer
	svc := domain.Open(newCLIRepo(t))
	code := runSession(dir, svc, []string{"navigate", "--file", "a.txt", "--new-line", "3", "--no-wait"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q), want 0", code, errb.String())
	}
	id := strings.TrimSpace(out.String())
	if id == "" {
		t.Fatal("--no-wait must print the command id")
	}
	got := steer.Drain(dir)
	if len(got) != 1 {
		t.Fatalf("inbox = %+v, want one command", got)
	}
	c := got[0]
	if c.ID != id || c.Cmd != "navigate" || c.File != "a.txt" || c.Wait {
		t.Fatalf("posted %+v, want navigate a.txt with wait:false and id %q", c, id)
	}
	if c.Line == nil || c.Line.Side != "new" || c.Line.No != 3 {
		t.Fatalf("line = %+v, want new:3", c.Line)
	}
	if c.Target == nil || c.Target.State != "unstaged" {
		t.Fatalf("target = %+v, want the unstaged working tree", c.Target)
	}
}

func TestSessionNavigateWaitsForTheReply(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	seen := answer(t, dir, func(c steer.Command) steer.Reply {
		return steer.Reply{ID: c.ID, OK: true, Detail: "opened a.txt:3"}
	})
	var out, errb bytes.Buffer
	svc := domain.Open(newCLIRepo(t))
	code := runSession(dir, svc, []string{"navigate", "--file", "a.txt", "--new-line", "3"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q), want 0", code, errb.String())
	}
	if !strings.Contains(out.String(), "opened a.txt:3") {
		t.Errorf("stdout = %q, want the reply's detail", out.String())
	}
	c, ok := <-seen
	if !ok || !c.Wait {
		t.Fatalf("consumer saw %+v ok=%v, want wait:true", c, ok)
	}
}

func TestSessionNavigateFailedReplyExitsOne(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	answer(t, dir, func(c steer.Command) steer.Reply {
		return steer.Reply{ID: c.ID, OK: false, Error: "a.txt is not in the working-tree diff"}
	})
	var out, errb bytes.Buffer
	svc := domain.Open(newCLIRepo(t))
	code := runSession(dir, svc, []string{"navigate", "--file", "a.txt", "--new-line", "3"}, &out, &errb)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "not in the working-tree diff") {
		t.Errorf("stderr = %q, want the reply's error", errb.String())
	}
}

// NO t.Parallel(): this is the one test that writes the package-level
// steerReplyWaitForTest, and every other session test reads it. Under
// t.Parallel() that is a data race ./test.sh race would catch.
func TestSessionNavigateTimeoutIsQueuedNotAFailure(t *testing.T) {
	dir := t.TempDir()
	livePresence(t, dir)
	var out, errb bytes.Buffer
	svc := domain.Open(newCLIRepo(t))
	old := steerReplyWaitForTest
	steerReplyWaitForTest = 150 * time.Millisecond
	defer func() { steerReplyWaitForTest = old }()
	code := runSession(dir, svc, []string{"navigate", "--file", "a.txt", "--new-line", "3"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — the command is still in the inbox", code)
	}
	if !strings.Contains(out.String(), "queued") {
		t.Errorf("stdout = %q, want a \"queued\" line", out.String())
	}
}

func TestSessionHunkResolvesToTheFirstLineOfTheHunk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	repo := hunkCLIRepo(t)
	var out, errb bytes.Buffer
	svc := domain.Open(repo)
	code := runSession(dir, svc, []string{"navigate", "--file", "a.txt", "--hunk", "2", "--no-wait"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q), want 0", code, errb.String())
	}
	got := steer.Drain(dir)
	if len(got) != 1 || got[0].Line == nil {
		t.Fatalf("posted %+v, want a command carrying a line", got)
	}
	if got[0].Line.Side != "new" {
		t.Errorf("side = %q, want new", got[0].Line.Side)
	}
	// A hunk's New span INCLUDES git's three lines of leading context, so an
	// edit on line 30 yields `@@ -27,7 +27,7 @@` and New[0] == 27. Landing on
	// the hunk's first line means its first line, context and all — that is
	// what phase 2's numbering hands out, and the change is then on screen.
	if got[0].Line.No != 27 {
		t.Errorf("line = %d, want 27 — --hunk lands on the FIRST line of hunk 2's new span, which starts 3 context lines above the edit", got[0].Line.No)
	}
}

func TestSessionHunkOnAnUntrackedPathIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	repo := newCLIRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "brand-new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := runSession(dir, domain.Open(repo), []string{"navigate", "--file", "brand-new.txt", "--hunk", "1", "--no-wait"}, &out, &errb)
	if code == 0 {
		t.Fatal("exit = 0, want a refusal")
	}
	if !strings.Contains(errb.String(), "--new-line") {
		t.Errorf("stderr = %q, want it to point at --new-line", errb.String())
	}
	if got := steer.Drain(dir); len(got) != 0 {
		t.Errorf("a refused command must never be posted, got %+v", got)
	}
}

func TestSessionNavigateBareRevPostsAFullSha(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	var out, errb bytes.Buffer
	code := runSession(dir, domain.Open(newCLIRepo(t)), []string{"navigate", "--rev", "HEAD", "--no-wait"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q), want 0", code, errb.String())
	}
	got := steer.Drain(dir)
	if len(got) != 1 {
		t.Fatalf("inbox = %+v, want one command", got)
	}
	// The consumer compares HASHES, so "HEAD" on the wire could never match.
	if len(got[0].Commit) != 40 {
		t.Fatalf("commit = %q, want a full 40-hex sha", got[0].Commit)
	}
}

func TestSessionNavigateBadRevExitsOne(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	var out, errb bytes.Buffer
	if code := runSession(dir, domain.Open(newCLIRepo(t)), []string{"navigate", "--rev", "no-such-rev", "--no-wait"}, &out, &errb); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got := steer.Drain(dir); len(got) != 0 {
		t.Errorf("an unresolvable rev must never be posted, got %+v", got)
	}
}

func TestSessionFlagUsageErrorsExitTwo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	svc := domain.Open(newCLIRepo(t))
	for _, args := range [][]string{
		{"navigate", "--file", "a.txt", "--cached", "--rev", "HEAD", "--new-line", "1"},
		{"navigate", "--file", "a.txt"}, // no line, no step
		{"navigate", "--file", "a.txt", "--new-line", "1", "--old-line", "2"},
		{"focus"},     // no panel
		{"highlight"}, // no add/clear
		{"nonsense"},
	} {
		args := args
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			var out, errb bytes.Buffer
			if code := runSession(dir, svc, args, &out, &errb); code != 2 {
				t.Errorf("exit = %d, want 2 (usage error); stderr %q", code, errb.String())
			}
		})
	}
}

func TestSessionReloadFocusAndHighlightPostTheRightCommand(t *testing.T) {
	t.Parallel()
	svc := domain.Open(newCLIRepo(t))
	cases := []struct {
		name string
		args []string
		want func(t *testing.T, c steer.Command)
	}{
		{"reload defaults to notes", []string{"reload", "--no-wait"}, func(t *testing.T, c steer.Command) {
			if c.Cmd != "reload" || len(c.Sources) != 1 || c.Sources[0] != "notes" {
				t.Errorf("posted %+v, want reload notes", c)
			}
		}},
		{"reload all", []string{"reload", "all", "--no-wait"}, func(t *testing.T, c steer.Command) {
			if len(c.Sources) != 1 || c.Sources[0] != "all" {
				t.Errorf("posted %+v, want reload all", c)
			}
		}},
		{"focus", []string{"focus", "commits", "--no-wait"}, func(t *testing.T, c steer.Command) {
			if c.Cmd != "focus" || c.Panel != "commits" {
				t.Errorf("posted %+v, want focus commits", c)
			}
		}},
		{"highlight add", []string{"highlight", "add", "--file", "a.txt", "--start", "4", "--end", "9", "--tone", "warn", "--no-wait"}, func(t *testing.T, c steer.Command) {
			if c.Cmd != "highlight" || c.Start != 4 || c.End != 9 || c.Tone != "warn" || c.Side != "new" {
				t.Errorf("posted %+v, want highlight a.txt new:4-9 warn", c)
			}
		}},
		{"highlight clear all", []string{"highlight", "clear", "--no-wait"}, func(t *testing.T, c steer.Command) {
			if c.Cmd != "highlight_clear" || c.File != "" {
				t.Errorf("posted %+v, want a file-less highlight_clear", c)
			}
		}},
		{"next comment", []string{"navigate", "--next-comment", "--no-wait"}, func(t *testing.T, c steer.Command) {
			if c.Cmd != "navigate" || c.Step != "next_note" {
				t.Errorf("posted %+v, want navigate step next_note", c)
			}
		}},
		{"prev comment", []string{"navigate", "--prev-comment", "--no-wait"}, func(t *testing.T, c steer.Command) {
			if c.Step != "prev_note" {
				t.Errorf("posted %+v, want step prev_note", c)
			}
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			livePresence(t, dir)
			var out, errb bytes.Buffer
			if code := runSession(dir, svc, tc.args, &out, &errb); code != 0 {
				t.Fatalf("exit = %d (stderr %q), want 0", code, errb.String())
			}
			got := steer.Drain(dir)
			if len(got) != 1 {
				t.Fatalf("inbox = %+v, want one command", got)
			}
			tc.want(t, got[0])
		})
	}
}

// steerServer is a fake `gg web` /api/session/steer endpoint. It records every
// command it is handed and answers with status.
type steerServer struct {
	mu     sync.Mutex
	got    []steer.Command
	ctypes []string
	status int
	body   string
}

func newSteerServer(t *testing.T, status int, body string) (*steerServer, *httptest.Server) {
	t.Helper()
	s := &steerServer{status: status, body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/session/steer" {
			http.Error(w, "wrong path "+r.URL.Path, http.StatusNotFound)
			return
		}
		var c steer.Command
		_ = json.NewDecoder(r.Body).Decode(&c)
		s.mu.Lock()
		s.got = append(s.got, c)
		s.ctypes = append(s.ctypes, r.Header.Get("Content-Type"))
		s.mu.Unlock()
		w.WriteHeader(s.status)
		_, _ = w.Write([]byte(s.body))
	}))
	t.Cleanup(srv.Close)
	return s, srv
}

func (s *steerServer) commands() []steer.Command {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]steer.Command(nil), s.got...)
}

func (s *steerServer) contentTypes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ctypes...)
}

func TestSessionWebOnlyPostsAndNeverTouchesTheInbox(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rec, srv := newSteerServer(t, http.StatusAccepted, "")
	liveWebPresence(t, dir, srv.URL)
	var out, errb bytes.Buffer
	code := runSession(dir, domain.Open(newCLIRepo(t)), []string{"navigate", "--file", "a.txt", "--new-line", "3"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q), want 0", code, errb.String())
	}
	if !strings.Contains(out.String(), "web: sent") {
		t.Errorf("stdout = %q, want a \"web: sent\" line", out.String())
	}
	got := rec.commands()
	if len(got) != 1 {
		t.Fatalf("the web page saw %+v, want one command", got)
	}
	c := got[0]
	if c.Cmd != "navigate" || c.File != "a.txt" || c.ID == "" {
		t.Errorf("posted %+v, want an identified navigate on a.txt", c)
	}
	if c.Line == nil || c.Line.Side != "new" || c.Line.No != 3 {
		t.Errorf("line = %+v, want new:3", c.Line)
	}
	if c.Target == nil || c.Target.State != "unstaged" {
		t.Errorf("target = %+v, want the unstaged working tree", c.Target)
	}
	if ct := rec.contentTypes(); len(ct) != 1 || ct[0] != "application/json" {
		t.Errorf("content types = %v, want application/json", ct)
	}
	// With no live TUI nothing may reach the inbox — and the CLI must not wait.
	if inbox := steer.Drain(dir); len(inbox) != 0 {
		t.Errorf("inbox = %+v, want nothing posted when only web is live", inbox)
	}
}

func TestSessionWebConflictAndErrorExitOne(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"conflict", http.StatusConflict, "operation in flight", "operation in flight"},
		{"server error", http.StatusInternalServerError, "boom", "500"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			_, srv := newSteerServer(t, tc.status, tc.body)
			liveWebPresence(t, dir, srv.URL)
			var out, errb bytes.Buffer
			code := runSession(dir, domain.Open(newCLIRepo(t)), []string{"focus", "commits"}, &out, &errb)
			if code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			if !strings.Contains(errb.String(), tc.want) {
				t.Errorf("stderr = %q, want it to mention %q", errb.String(), tc.want)
			}
		})
	}
}

func TestSessionBothLiveShareOneIDAndTheTUIDecides(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rec, srv := newSteerServer(t, http.StatusAccepted, "")
	liveWebPresence(t, dir, srv.URL)
	livePresence(t, dir)
	var out, errb bytes.Buffer
	code := runSession(dir, domain.Open(newCLIRepo(t)), []string{"focus", "commits", "--no-wait"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q), want 0", code, errb.String())
	}
	web := rec.commands()
	inbox := steer.Drain(dir)
	if len(web) != 1 || len(inbox) != 1 {
		t.Fatalf("web = %+v, inbox = %+v, want one command each", web, inbox)
	}
	// Both halves of a split delivery must carry the SAME id, or a reply can
	// never be correlated across the two frontends.
	if web[0].ID != inbox[0].ID || web[0].ID == "" {
		t.Errorf("ids = web %q, inbox %q, want one shared id", web[0].ID, inbox[0].ID)
	}
	if !strings.Contains(out.String(), inbox[0].ID) {
		t.Errorf("stdout = %q, want the command id", out.String())
	}
}

func TestSessionBothLiveWebFailureDoesNotOverrideTheTUIReply(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, srv := newSteerServer(t, http.StatusInternalServerError, "boom")
	liveWebPresence(t, dir, srv.URL)
	livePresence(t, dir)
	answer(t, dir, func(c steer.Command) steer.Reply {
		return steer.Reply{ID: c.ID, OK: true, Detail: "focused commits"}
	})
	var out, errb bytes.Buffer
	code := runSession(dir, domain.Open(newCLIRepo(t)), []string{"focus", "commits"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q), want 0 — the TUI reply decides", code, errb.String())
	}
	if !strings.Contains(out.String(), "focused commits") {
		t.Errorf("stdout = %q, want the TUI reply's detail", out.String())
	}
}

func TestPostWebSteerRejectsAnEmptyURL(t *testing.T) {
	t.Parallel()
	if err := postWebSteer("", steer.Command{Cmd: "focus"}); err == nil {
		t.Fatal("an empty base URL must be an error")
	}
}

func TestSessionOpenViewReadsTheSnapshot(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ui-state.json")
	// cursor.commit is an OBJECT in the snapshot ({hash, subject}), never a
	// bare string — decoding it as a string would fail and blank the view.
	snap := `{"focus":{"panel":"commits"},"cursor":{"commit":{"hash":"abc1234","subject":"s"}}}`
	if err := os.WriteFile(path, []byte(snap), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := sessionOpenViewAt(path); got != "commits / abc1234" {
		t.Errorf("view = %q, want \"commits / abc1234\"", got)
	}
	if got := sessionOpenViewAt(filepath.Join(t.TempDir(), "missing.json")); got != "" {
		t.Errorf("view = %q, want \"\" for a missing snapshot", got)
	}
}

// hunkCLIRepo commits 60 numbered lines then edits two far-apart regions, so
// `gg diff --hunks` reports exactly two hunks (git's default 3 lines of context
// cannot bridge lines 5 and 30) and hunk 2's new span starts at line 30.
func hunkCLIRepo(t *testing.T) string {
	t.Helper()
	dir := newCLIRepo(t)
	lines := make([]string, 0, 60)
	for i := 1; i <= 60; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	write := func(ls []string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(strings.Join(ls, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(lines)
	gittest.Run(t, dir, "add", "a.txt")
	gittest.Run(t, dir, "commit", "-m", "seed")
	lines[4] = "EDITED-A"  // new line 5  → hunk 1
	lines[29] = "EDITED-B" // new line 30 → hunk 2
	write(lines)
	return dir
}
