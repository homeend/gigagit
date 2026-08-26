package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// Cherry-pick a commit from another branch onto the checked-out one.
func TestOpHTTPCherryPick(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 2)
	gitRun(t, dir, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "picked.txt"), []byte("from side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "picked.txt")
	gitRun(t, dir, "commit", "-m", "add picked.txt")
	pick := gitRun(t, dir, "rev-parse", "HEAD")
	gitRun(t, dir, "checkout", "main")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"cherry-pick","sha":"`+pick+`"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if subj := gitRun(t, dir, "log", "-1", "--format=%s", "main"); subj != "add picked.txt" {
		t.Errorf("main tip = %q, want the picked commit", subj)
	}
	if code := postJSON(t, ts, "/api/op", `{"op":"cherry-pick","sha":"main"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("a ref name: code = %d, want 400 (hex only)", code)
	}
}

// Revert undoes a commit by adding a new one on top.
func TestOpHTTPRevert(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 2)
	if err := os.WriteFile(filepath.Join(dir, "gone.txt"), []byte("doomed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "gone.txt")
	gitRun(t, dir, "commit", "-m", "add gone.txt")
	bad := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"revert","sha":"`+bad+`"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if out := gitRun(t, dir, "log", "-1", "--format=%s"); !strings.Contains(out, "Revert") {
		t.Errorf("tip = %q, want a revert commit", out)
	}
	if files := gitRun(t, dir, "ls-files", "gone.txt"); files != "" {
		t.Errorf("gone.txt still tracked after the revert")
	}
}

// Reword replaces a commit's message. The full text comes from the wire, so a
// multi-line body has to survive the round trip.
func TestOpHTTPReword(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3)
	head := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"reword","sha":"`+head+`","message":"new subject\n\nand a body line"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := gitRun(t, dir, "log", "-1", "--format=%B"); !strings.Contains(got, "new subject") || !strings.Contains(got, "and a body line") {
		t.Errorf("message = %q, want the subject AND the body", got)
	}
	if code := postJSON(t, ts, "/api/op", `{"op":"reword","sha":"`+head+`","message":"   "}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("blank message: code = %d, want 400", code)
	}
}

// Undo the last commit: the ref moves back, the work stays staged.
func TestOpHTTPUndoLastCommit(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3)
	parent := gitRun(t, dir, "rev-parse", "HEAD~1")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"undo-last-commit"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := gitRun(t, dir, "rev-parse", "HEAD"); got != parent {
		t.Errorf("HEAD = %s, want the parent %s", got, parent)
	}
}

// A worktree cut from a COMMIT (not a branch): a new branch is created there.
func TestOpHTTPCreateWorktreeAtCommit(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3)
	old := gitRun(t, dir, "rev-parse", "HEAD~2")
	ts := serve(t, New(domain.Open(dir)))
	path := filepath.Join(t.TempDir(), "wt-from-commit")
	pathJSON, _ := json.Marshal(path) // JSON-escapes Windows backslashes

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"create-worktree","sha":"`+old+`","name":"from-commit","path":`+string(pathJSON)+`}`), 60*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := gitRun(t, dir, "rev-parse", "from-commit"); got != old {
		t.Errorf("from-commit = %s, want the start point %s", got, old)
	}
	if code := postJSON(t, ts, "/api/op", `{"op":"create-worktree","sha":"HEAD~2","name":"x","path":`+string(mustJSON(path+"2"))+`}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("a rev expression: code = %d, want 400 (hex only)", code)
	}
}

// The reword prompt prefills with the commit's CURRENT message, so there is a
// read for it.
func TestCommitMessageEndpoint(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "commit", "--allow-empty", "-m", "subject line\n\nbody line")
	head := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))

	var got struct {
		Message string `json:"message"`
	}
	if code := getJSON(t, ts, "/api/commit-message?rev="+head, &got); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(got.Message, "subject line") || !strings.Contains(got.Message, "body line") {
		t.Errorf("message = %q, want the whole message", got.Message)
	}
	var ignored any
	if code := getJSON(t, ts, "/api/commit-message?rev=HEAD~1", &ignored); code != http.StatusBadRequest {
		t.Errorf("a rev expression: code = %d, want 400 (hex only)", code)
	}
}

func mustJSON(v string) []byte {
	b, _ := json.Marshal(v)
	return b
}
