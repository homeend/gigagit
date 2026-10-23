package web

import (
	"net/http"
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
