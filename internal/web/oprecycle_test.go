package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

func recycleBody(path, branch, ref, name string) string {
	return fmt.Sprintf(`{"op":"recycle-worktree","path":%q,"branch":%q,"ref":%q,"name":%q}`, path, branch, ref, name)
}

func headIn(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(gitRun(t, dir, "symbolic-ref", "--short", "HEAD"))
}

func TestOpHTTPRecycleLocalBranch(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	wt := addWorktree(t, dir, "feature")
	gitRun(t, dir, "branch", "loose")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, recycleBody(wt, "loose", "", ""))
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := headIn(t, wt); got != "loose" {
		t.Fatalf("worktree HEAD = %q, want loose", got)
	}
}

func TestOpHTTPRecycleDirtyParksTheDecision(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	wt := addWorktree(t, dir, "feature")
	gitRun(t, dir, "branch", "loose")
	os.WriteFile(filepath.Join(wt, "wip.txt"), []byte("wip\n"), 0o644)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, recycleBody(wt, "loose", "", ""))
	run := srv.opByID(opID)
	waitDecision(t, run)
	run.mu.Lock()
	req := run.pending
	run.mu.Unlock()
	if req.ID != engine.RecycleDirtyDecisionID || strings.Join(req.Options, ",") != "commit,shelve,discard,abort" {
		t.Fatalf("pending = %+v", req)
	}
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"discard"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := headIn(t, wt); got != "loose" {
		t.Fatalf("worktree HEAD = %q, want loose", got)
	}
	if _, err := os.Stat(filepath.Join(wt, "wip.txt")); !os.IsNotExist(err) {
		t.Fatalf("discard kept wip.txt (stat err %v)", err)
	}
}

func TestOpHTTPRecycleRemoteBranch(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "config", "remote.origin.url", "file://"+dir)
	gitRun(t, dir, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	gitRun(t, dir, "update-ref", "refs/remotes/origin/foo", "HEAD")
	wt := addWorktree(t, dir, "feature")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, recycleBody(wt, "", "origin/foo", "bar"))
	events := readSSE(t, ts, opID, 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := headIn(t, wt); got != "bar" {
		t.Fatalf("worktree HEAD = %q, want bar (the prompted local name)", got)
	}
	if up := strings.TrimSpace(gitRun(t, dir, "rev-parse", "--abbrev-ref", "bar@{upstream}")); up != "origin/foo" {
		t.Fatalf("bar upstream = %q, want origin/foo", up)
	}
}

// Every wire value is an identifier resolved against a fresh read: nothing
// the page sends reaches git argv unless the server listed it.
func TestOpHTTPRecycleRefusals(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "config", "remote.origin.url", "file://"+dir)
	gitRun(t, dir, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	gitRun(t, dir, "update-ref", "refs/remotes/origin/foo", "HEAD")
	wt := addWorktree(t, dir, "feature")
	gitRun(t, dir, "branch", "loose")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)
	for _, tc := range []struct {
		name, body string
		code       int
	}{
		{"no path", recycleBody("", "loose", "", ""), http.StatusBadRequest},
		{"unknown worktree", recycleBody(filepath.Join(t.TempDir(), "nope"), "loose", "", ""), http.StatusNotFound},
		{"neither branch nor ref", recycleBody(wt, "", "", ""), http.StatusBadRequest},
		{"both branch and ref", recycleBody(wt, "loose", "origin/foo", "foo"), http.StatusBadRequest},
		{"unknown branch", recycleBody(wt, "nope", "", ""), http.StatusNotFound},
		{"unknown remote branch", recycleBody(wt, "", "origin/nope", "nope"), http.StatusNotFound},
		{"remote without a name", recycleBody(wt, "", "origin/foo", ""), http.StatusBadRequest},
		{"unsafe local name", recycleBody(wt, "", "origin/foo", "-x"), http.StatusBadRequest},
	} {
		if code := postJSON(t, ts, "/api/op", tc.body, "application/json", "", nil); code != tc.code {
			t.Errorf("%s: code = %d, want %d", tc.name, code, tc.code)
		}
	}
}
