package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// The generate lane runs a configured external command, so these tests
// configure a trivial shell command instead of a real agent (review_test.go's
// pattern): what is under test is the staged gate, tool selection, the
// approval gate, and the captured message's round trip.

const echoMessageTool = `
[[tools.command]]
category = "commit_message"
name = "Echo"
mode = "capture"
command = '''
printf 'describe the change\n\nwritten by a fake agent\n'
'''
`

// genRepo builds a repo whose .gg.toml declares tools, with both config roots
// and the state dir isolated — tool lists CONCATENATE global + repo, so
// without this the developer's own tools would join the fixture's.
func genRepo(t *testing.T, tools string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir()) // the approval store
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := newRepoDir(t, 1)
	if tools != "" {
		if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(tools), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", "-A")
		gitRun(t, dir, "commit", "-m", "config")
	}
	return dir
}

// stage writes and stages a file, leaving it uncommitted — GenerateMessage
// describes the STAGED diff, so a fixture that commits has nothing to describe.
func stage(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", name)
}

func startGenerate(t *testing.T, ts *httptest.Server, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(ts.URL+"/api/commit-message/generate", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST generate: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestGenerateMessageReturnsCapturedText(t *testing.T) {
	dir := genRepo(t, echoMessageTool)
	stage(t, dir, "new.txt", "hello\n")
	ts := serve(t, New(domain.Open(dir)))

	before := gitRun(t, dir, "rev-parse", "HEAD")
	code, out := startGenerate(t, ts, `{"tool":"Echo","approve":true}`)
	if code != http.StatusAccepted {
		t.Fatalf("generate = %d (%v)", code, out)
	}
	events := readSSE(t, ts, out["op_id"].(string), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if done["subject"] != "describe the change" {
		t.Errorf("subject = %v", done["subject"])
	}
	if body, _ := done["body"].(string); !strings.Contains(body, "fake agent") {
		t.Errorf("body = %q", body)
	}
	// Nothing commits: the message is for the human to send.
	if after := gitRun(t, dir, "rev-parse", "HEAD"); after != before {
		t.Error("generating a message moved HEAD")
	}
	if st := gitRun(t, dir, "status", "--porcelain"); !strings.Contains(st, "new.txt") {
		t.Errorf("staged file no longer staged:\n%s", st)
	}
}

// The approval gate: an unapproved command is refused with the resolved text,
// and only then does approve=true run it. This is the boundary that keeps a
// loopback page from running configured commands unannounced.
func TestGenerateMessageApprovalGate(t *testing.T) {
	dir := genRepo(t, echoMessageTool)
	stage(t, dir, "new.txt", "hello\n")
	ts := serve(t, New(domain.Open(dir)))

	code, out := startGenerate(t, ts, `{"tool":"Echo"}`)
	if code != http.StatusForbidden {
		t.Fatalf("unapproved run = %d (%v), want 403", code, out)
	}
	if out["needs_approval"] != true {
		t.Errorf("refusal = %v, want needs_approval", out)
	}
	if cmd, _ := out["command"].(string); !strings.Contains(cmd, "printf") {
		t.Errorf("refusal must show the command it would run, got %q", cmd)
	}

	var tools struct {
		Staged int          `json:"staged"`
		Tools  []genToolRow `json:"tools"`
	}
	getJSON(t, ts, "/api/commit-message/tools", &tools)
	if len(tools.Tools) != 1 || tools.Tools[0].Approved {
		t.Fatalf("tools before approval = %+v", tools.Tools)
	}
	if tools.Staged != 1 {
		t.Errorf("staged = %d, want 1", tools.Staged)
	}

	code, out = startGenerate(t, ts, `{"tool":"Echo","approve":true}`)
	if code != http.StatusAccepted {
		t.Fatalf("approved run = %d (%v)", code, out)
	}
	readSSE(t, ts, out["op_id"].(string), 30*time.Second)

	// The approval is remembered, so the next run needs no approve flag.
	getJSON(t, ts, "/api/commit-message/tools", &tools)
	if len(tools.Tools) != 1 || !tools.Tools[0].Approved {
		t.Errorf("tools after approval = %+v", tools.Tools)
	}
	if code, out = startGenerate(t, ts, `{"tool":"Echo"}`); code != http.StatusAccepted {
		t.Fatalf("remembered approval = %d (%v)", code, out)
	}
	readSSE(t, ts, out["op_id"].(string), 30*time.Second)
}

func TestGenerateMessageRefusals(t *testing.T) {
	t.Run("nothing staged", func(t *testing.T) {
		dir := genRepo(t, echoMessageTool)
		ts := serve(t, New(domain.Open(dir)))
		code, out := startGenerate(t, ts, `{"tool":"Echo","approve":true}`)
		if code != http.StatusBadRequest {
			t.Fatalf("clean tree = %d (%v), want 400", code, out)
		}
		if msg, _ := out["error"].(string); !strings.Contains(msg, "nothing staged") {
			t.Errorf("error = %q", msg)
		}
	})
	t.Run("no tool configured", func(t *testing.T) {
		dir := genRepo(t, "")
		stage(t, dir, "new.txt", "x\n")
		ts := serve(t, New(domain.Open(dir)))
		code, out := startGenerate(t, ts, `{"approve":true}`)
		if code != http.StatusBadRequest {
			t.Fatalf("no tools = %d (%v), want 400", code, out)
		}
		var tools struct {
			Tools []genToolRow `json:"tools"`
		}
		getJSON(t, ts, "/api/commit-message/tools", &tools)
		if len(tools.Tools) != 0 {
			t.Errorf("tools = %+v, want none", tools.Tools)
		}
	})
	t.Run("unknown tool name", func(t *testing.T) {
		dir := genRepo(t, echoMessageTool)
		stage(t, dir, "new.txt", "x\n")
		ts := serve(t, New(domain.Open(dir)))
		if code, out := startGenerate(t, ts, `{"tool":"Nope","approve":true}`); code != http.StatusBadRequest {
			t.Fatalf("unknown tool = %d (%v), want 400", code, out)
		}
	})
}

// A terminal-mode tool is not offered: there is no browser tab to hand over,
// exactly as the conflict lane decided.
func TestGenerateMessageSkipsTerminalTools(t *testing.T) {
	dir := genRepo(t, `
[[tools.command]]
category = "commit_message"
name = "Interactive"
mode = "terminal"
command = "vim"
`)
	stage(t, dir, "new.txt", "x\n")
	ts := serve(t, New(domain.Open(dir)))

	var tools struct {
		Tools []genToolRow `json:"tools"`
	}
	getJSON(t, ts, "/api/commit-message/tools", &tools)
	if len(tools.Tools) != 0 {
		t.Errorf("terminal tool offered: %+v", tools.Tools)
	}
}

// A hanging agent must be stoppable — it holds the single op lane otherwise.
func TestGenerateMessageCancels(t *testing.T) {
	dir := genRepo(t, `
[[tools.command]]
category = "commit_message"
name = "Sleepy"
mode = "capture"
command = "sleep 30"
`)
	stage(t, dir, "new.txt", "x\n")
	ts := serve(t, New(domain.Open(dir)))

	code, out := startGenerate(t, ts, `{"tool":"Sleepy","approve":true}`)
	if code != http.StatusAccepted {
		t.Fatalf("start = %d (%v)", code, out)
	}
	id := out["op_id"].(string)
	if c := postJSON(t, ts, "/api/op/"+id+"/cancel", `{}`, "application/json", "", nil); c != http.StatusOK {
		t.Fatalf("cancel = %d", c)
	}
	events := readSSE(t, ts, id, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false || done["cancelled"] != true {
		t.Errorf("done = %v, want a cancelled run", done)
	}
}

func TestCommitAmendReplacesHeadMessage(t *testing.T) {
	dir := newRepoDir(t, 2)
	ts := serve(t, New(domain.Open(dir)))

	parentBefore := gitRun(t, dir, "rev-parse", "HEAD^")
	// The prefill the browser opens the box with.
	var head struct {
		Message string `json:"message"`
	}
	if code := getJSON(t, ts, "/api/commit-message/head", &head); code != http.StatusOK {
		t.Fatalf("GET head message = %d", code)
	}
	if strings.TrimSpace(head.Message) != "c2" {
		t.Fatalf("prefill = %q, want HEAD's message", head.Message)
	}

	var out struct {
		OpID string `json:"op_id"`
	}
	code := postJSON(t, ts, "/api/op", `{"op":"commit-amend","message":"c2 reworded\n\nwith a body"}`, "application/json", "", &out)
	if code != http.StatusAccepted {
		t.Fatalf("amend = %d", code)
	}
	done := readSSE(t, ts, out.OpID, 20*time.Second)
	last := done[len(done)-1]
	if last["ok"] != true || last["changed"] != true {
		t.Fatalf("done = %v", last)
	}
	if subj := gitRun(t, dir, "log", "-1", "--format=%s"); subj != "c2 reworded" {
		t.Errorf("subject = %q", subj)
	}
	if body := gitRun(t, dir, "log", "-1", "--format=%b"); !strings.Contains(body, "with a body") {
		t.Errorf("body = %q", body)
	}
	// Amend rewrites ONE commit: the parent and the history depth are untouched.
	if p := gitRun(t, dir, "rev-parse", "HEAD^"); p != parentBefore {
		t.Errorf("parent moved: %s != %s", p, parentBefore)
	}
	if n := gitRun(t, dir, "rev-list", "--count", "HEAD"); n != "2" {
		t.Errorf("commit count = %s, want 2", n)
	}
}

func TestCommitAmendRequiresMessage(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	for _, body := range []string{
		`{"op":"commit-amend"}`,
		`{"op":"commit-amend","message":"  \n "}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", body, code)
		}
	}
}

// Nothing to amend is git's refusal, not a re-implemented rule.
func TestCommitAmendWithoutACommit(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	ts := serve(t, New(domain.Open(dir)))

	if code := getJSON(t, ts, "/api/commit-message/head", nil); code != http.StatusConflict {
		t.Errorf("head message on an empty repo = %d, want 409", code)
	}
	var out struct {
		OpID string `json:"op_id"`
	}
	if code := postJSON(t, ts, "/api/op", `{"op":"commit-amend","message":"nope"}`, "application/json", "", &out); code != http.StatusAccepted {
		t.Fatalf("start = %d", code)
	}
	events := readSSE(t, ts, out.OpID, 20*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want git's refusal", done)
	}
}
