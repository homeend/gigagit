package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

const inPlaceToolsConfig = `
[[tools.command]]
category = "conflict"
name = "Mergetool"
mode = "terminal"
per_file = true
command = "printf 'merged by the tool\n' > <merged>"

[[tools.command]]
category = "conflict"
name = "Ask"
mode = "terminal"
command = "printf '<user:Hint>' > hint.txt"

[[tools.command]]
category = "conflict"
name = "Whole"
mode = "capture"
command = "echo not in place"

[[tools.command]]
category = "conflict_complete"
name = "Finish"
mode = "terminal"
command = "printf 'done: <user:Why>\n' > \"$GG_MESSAGE_FILE\""
`

func toolsServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := conflictingRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(inPlaceToolsConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "config")
	conflictedMergeState(t, dir)
	srv := New(domain.Open(dir))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	return serve(t, srv), dir
}

type toolRowResp struct {
	Name     string   `json:"name"`
	Category string   `json:"category"`
	PerFile  bool     `json:"per_file"`
	Labels   []string `json:"labels"`
}

func conflictToolRows(t *testing.T, ts *httptest.Server, file string) []toolRowResp {
	t.Helper()
	var got struct {
		Tools []toolRowResp `json:"tools"`
	}
	if code := getJSON(t, ts, "/api/conflict/tools?file="+file, &got); code != http.StatusOK {
		t.Fatalf("GET tools = %d", code)
	}
	return got.Tools
}

// The picker's in-place rows (the TUI's conflictPickerRows): per-file
// mergetools need a focused both-sides file; a command asking for input runs
// in place; a plain whole-operation command is an agent (the launch rows).
func TestConflictToolRows(t *testing.T) {
	ts, _ := toolsServer(t)
	names := func(rows []toolRowResp) string {
		var n []string
		for _, r := range rows {
			n = append(n, r.Name)
		}
		return strings.Join(n, ",")
	}
	if got := names(conflictToolRows(t, ts, "f.txt")); got != "Mergetool,Ask,Finish" {
		t.Errorf("with f.txt focused = %q, want Mergetool,Ask,Finish", got)
	}
	rows := conflictToolRows(t, ts, "")
	if got := names(rows); got != "Ask,Finish" {
		t.Errorf("no focused file = %q, want Ask,Finish", got)
	}
	if len(rows) > 0 && strings.Join(rows[0].Labels, ",") != "Hint" {
		t.Errorf("Ask's labels = %v, want [Hint]", rows[0].Labels)
	}
}

// waitToolFinished waits until the server has finished the run (its session
// ended and the watcher recorded the outcome).
func waitToolFinished(t *testing.T, id string) {
	t.Helper()
	waitSessionExit(t, id)
	deadline := time.Now().Add(5 * time.Second)
	for {
		toolRunsMu.Lock()
		run := toolRuns[domain.SessionID(id)]
		fin := run != nil && run.finished
		toolRunsMu.Unlock()
		if fin {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the run was never finished")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitSessionExit(t *testing.T, id string) {
	t.Helper()
	s, ok := domain.Sessions().Get(domain.SessionID(id))
	if !ok {
		t.Fatalf("no session %s", id)
	}
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the tool session never ended")
	}
}

// A per-file mergetool runs in a session; when it ends, done says the merged
// file changed (the page then offers to mark it resolved).
func TestConflictToolRunPerFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	ts, dir := toolsServer(t)
	body := `{"name":"Mergetool","category":"conflict","file":"f.txt"}`
	code, got := postJSONAny(t, ts, "/api/conflict/tool", body)
	if code != http.StatusForbidden || got["needs_approval"] != true || !strings.Contains(got["command"].(string), filepath.Join(dir, "f.txt")) {
		t.Fatalf("unapproved run = %d %v, want 403 with the resolved command", code, got)
	}
	code, got = postJSONAny(t, ts, "/api/conflict/tool", strings.Replace(body, "}", `,"approve":true}`, 1))
	if code != http.StatusOK {
		t.Fatalf("approved run = %d %v", code, got)
	}
	sid := got["session"].(string)
	waitToolFinished(t, sid)
	code, done := postJSONAny(t, ts, "/api/conflict/tool/done", `{"session":"`+sid+`"}`)
	if code != http.StatusOK || done["changed"] != true || done["file"] != "f.txt" || done["per_file"] != true || done["failed"] == true {
		t.Fatalf("done = %d %v, want changed f.txt", code, done)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(b) != "merged by the tool\n" {
		t.Errorf("f.txt = %q", b)
	}
	if code, _ := postJSONAny(t, ts, "/api/conflict/tool/done", `{"session":"`+sid+`"}`); code != http.StatusNotFound {
		t.Errorf("a second done = %d, want 404 (the run is gone)", code)
	}
}

// A command asking for input needs every value; a resolve & complete tool's
// $GG_MESSAGE_FILE comes back as its overview.
func TestConflictToolInputsAndOverview(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	ts, dir := toolsServer(t)
	if code, got := postJSONAny(t, ts, "/api/conflict/tool", `{"name":"Ask","category":"conflict","approve":true}`); code != http.StatusBadRequest {
		t.Fatalf("missing input = %d %v, want 400", code, got)
	}
	code, got := postJSONAny(t, ts, "/api/conflict/tool", `{"name":"Ask","category":"conflict","inputs":{"Hint":"take ours"},"approve":true}`)
	if code != http.StatusOK {
		t.Fatalf("run = %d %v", code, got)
	}
	waitToolFinished(t, got["session"].(string))
	if b, _ := os.ReadFile(filepath.Join(dir, "hint.txt")); string(b) != "take ours" {
		t.Errorf("hint.txt = %q, want the input", b)
	}
	code, got = postJSONAny(t, ts, "/api/conflict/tool", `{"name":"Finish","category":"conflict_complete","inputs":{"Why":"tested"},"approve":true}`)
	if code != http.StatusOK {
		t.Fatalf("finish run = %d %v", code, got)
	}
	sid := got["session"].(string)
	waitToolFinished(t, sid)
	code, done := postJSONAny(t, ts, "/api/conflict/tool/done", `{"session":"`+sid+`"}`)
	if code != http.StatusOK || done["overview"] != "done: tested\n" {
		t.Fatalf("done = %d %v, want the overview", code, done)
	}
}

func TestConflictToolRefusals(t *testing.T) {
	ts, _ := toolsServer(t)
	for _, tc := range []struct{ name, body string }{
		{"a whole-operation agent is not an in-place tool", `{"name":"Whole","category":"conflict","approve":true}`},
		{"a per-file tool needs a both-sides file", `{"name":"Mergetool","category":"conflict","file":"","approve":true}`},
		{"an unknown tool", `{"name":"Nope","category":"conflict","approve":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code, got := postJSONAny(t, ts, "/api/conflict/tool", tc.body); code != http.StatusBadRequest {
				t.Fatalf("status = %d %v, want 400", code, got)
			}
		})
	}
	if code, _ := postJSONAny(t, ts, "/api/conflict/tool/done", `{"session":"nope"}`); code != http.StatusNotFound {
		t.Errorf("unknown session = %d, want 404", code)
	}
	for _, p := range []string{"/api/conflict/tool", "/api/conflict/tool/done"} {
		if code := postJSON(t, ts, p, `{}`, "application/json", "http://evil.example", nil); code != http.StatusForbidden {
			t.Errorf("%s cross-origin = %d, want 403", p, code)
		}
	}
}

// The picker runs in-place tools the TUI's way, in a console session, and
// asks about the merged file when the session ends.
func TestConflictToolsClientWired(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ file, want, why string }{
		{"review.js", `"/api/conflict/tools?file=" + encodeURIComponent(file)`, "the picker lists the in-place tools for the focused file"},
		{"review.js", "act: () => runConflictTool(t, file)", "an in-place row runs the tool"},
		{"review.js", "for (const l of t.labels || []) inputs[l] = await askValue(l);", "every <user:…> value is asked first"},
		{"review.js", "await askApproval(e.data.tool, e.data.command)", "an unapproved command is shown before it runs"},
		{"review.js", "openConsole(got.session)", "the tool runs in a console session"},
		{"review.js", `"/api/conflict/tool/done", { session: id }`, "its end is asked about once"},
		{"review.js", "sessionStorage.setItem(TOOL_RUNS_KEY", "this tab's runs survive a reload"},
		{"review.js", "if (done.failed) return opLine(", "a failed run is reported, never offered for marking"},
		{"review.js", "toolSessions(state.sessions || []); // a tool that already ended", "a tool that ended before the reply is caught up"},
		{"review.js", `Mark it as resolved (git add)?"`, "a changed file is offered for marking (the TUI's words)"},
		{"review.js", `startOp({ op: "resolve-conflict", path: done.file, mode: "mark" }`, "mark resolved stages the file"},
		{"live.js", "toolSessions(msg.sessions || [])", "a sessions event reaches the tool runs"},
		{"launch.js", "function askApproval(tool, command)", "the approval stage stands alone for in-place tools"},
	} {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}

// The server finishes a run when its session ends, whether or not a page
// asks: the temp files go at once (a reloaded tab, or a second one, can
// still collect the outcome — the sessions list marks tool sessions).
func TestConflictToolFinishesWithoutThePage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	ts, _ := toolsServer(t)
	code, got := postJSONAny(t, ts, "/api/conflict/tool", `{"name":"Mergetool","category":"conflict","file":"f.txt","approve":true}`)
	if code != http.StatusOK {
		t.Fatalf("run = %d %v", code, got)
	}
	sid := got["session"].(string)
	toolRunsMu.Lock()
	temps := append([]string(nil), toolRuns[domain.SessionID(sid)].temps...)
	toolRunsMu.Unlock()
	if !sessionsMarkTool(t, ts, sid) {
		t.Error("the sessions list does not mark the tool's session")
	}
	waitToolFinished(t, sid)
	for _, f := range temps {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("%s survived the run's end", f)
		}
	}
}

func sessionsMarkTool(t *testing.T, ts *httptest.Server, sid string) bool {
	t.Helper()
	var got struct {
		Sessions []struct {
			ID   string `json:"id"`
			Tool bool   `json:"tool"`
		} `json:"sessions"`
	}
	getJSON(t, ts, "/api/sessions", &got)
	for _, s := range got.Sessions {
		if s.ID == sid {
			return s.Tool
		}
	}
	return false
}

// A failed run is a failure (no mark-resolved, no overview); a run killed by
// a signal (ctrl-C in the console) is an interrupt, not a failure — the TUI's
// toolFinished / toolInterruptExit.
func TestConflictToolFailureAndInterrupt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	ts, dir := toolsServer(t)
	edited := inPlaceToolsConfig + `
[[tools.command]]
category = "conflict"
name = "Fails"
mode = "terminal"
per_file = true
command = "printf 'half done\n' > <merged>; exit 3"

[[tools.command]]
category = "conflict"
name = "Sleeps"
mode = "terminal"
per_file = true
command = "sleep 30"
`
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	code, got := postJSONAny(t, ts, "/api/conflict/tool", `{"name":"Fails","category":"conflict","file":"f.txt","approve":true}`)
	if code != http.StatusOK {
		t.Fatalf("run = %d %v", code, got)
	}
	sid := got["session"].(string)
	// the wrapper holds a failure until Enter: answer it
	if s, ok := domain.Sessions().Get(domain.SessionID(sid)); ok {
		time.Sleep(300 * time.Millisecond)
		s.Paste("\r")
	}
	waitToolFinished(t, sid)
	_, done := postJSONAny(t, ts, "/api/conflict/tool/done", `{"session":"`+sid+`"}`)
	if done["failed"] != true || done["interrupted"] == true {
		t.Errorf("a failed run's done = %v, want failed", done)
	}
	code, got = postJSONAny(t, ts, "/api/conflict/tool", `{"name":"Sleeps","category":"conflict","file":"f.txt","approve":true}`)
	if code != http.StatusOK {
		t.Fatalf("sleeper run = %d %v", code, got)
	}
	sid = got["session"].(string)
	if s, ok := domain.Sessions().Get(domain.SessionID(sid)); ok {
		time.Sleep(200 * time.Millisecond)
		_ = domain.Sessions().Kill(s.Info().ID)
	}
	waitToolFinished(t, sid)
	_, done = postJSONAny(t, ts, "/api/conflict/tool/done", `{"session":"`+sid+`"}`)
	if done["interrupted"] != true || done["failed"] == true {
		t.Errorf("a killed run's done = %v, want interrupted", done)
	}
}
