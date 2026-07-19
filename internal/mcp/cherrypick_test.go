package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// commitOnBranch adds a commit changing path to content and returns its sha.
func commitOnBranch(t *testing.T, e *testEnv, path, content, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.dir, path), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, e.dir, "add", "-A")
	gitRun(t, e.dir, "commit", "-m", msg)
	return gitRun(t, e.dir, "rev-parse", "HEAD")
}

// gcAway makes sha unreachable and prunes it, then proves it is gone
// (the internal/cli/shelf_test.go recipe). Uses exec directly for the
// verification probe because gitRun t.Fatal's on the expected failure.
func gcAway(t *testing.T, e *testEnv, sha string) {
	t.Helper()
	gitRun(t, e.dir, "reflog", "expire", "--expire=all", "--all")
	gitRun(t, e.dir, "gc", "--prune=now")
	cat := exec.Command("git", "-C", e.dir, "cat-file", "-e", sha)
	if err := cat.Run(); err == nil {
		t.Fatalf("commit %s was not pruned; fixture does not exercise the gc'd lane", sha)
	}
}

// seedShelfCommitAt shelves sha as a commit entry with label.
func seedShelfCommitAt(t *testing.T, e *testEnv, sha, label string) model.ShelfEntry {
	t.Helper()
	entry, err := e.svc.ShelfAddCommit(context.Background(), sha, label)
	if err != nil {
		t.Fatalf("ShelfAddCommit: %v", err)
	}
	return entry
}

// seedCommitBookmark stores a path-less commit-pointer bookmark for sha.
func seedCommitBookmark(t *testing.T, e *testEnv, sha string) model.Bookmark {
	t.Helper()
	b, err := e.svc.BookmarkAdd(context.Background(), model.Bookmark{
		State: model.StateCommitted, Commit: sha,
	})
	if err != nil {
		t.Fatalf("BookmarkAdd: %v", err)
	}
	return b
}

func TestCherryPickLiveShelf(t *testing.T) {
	e := newTestEnv(t)
	sha := commitOnBranch(t, e, "b.txt", "shelved content\n", "feat: b")
	ce := seedShelfCommitAt(t, e, sha, "b label")
	gitRun(t, e.dir, "reset", "--hard", "HEAD~1") // move branch back; commit still reachable? keep it simple: pick onto a sibling state
	out := e.call(t, "gg_cherry_pick", map[string]any{"source": map[string]any{"shelf": ce.ID}})
	if out["lane"] != "live" || out["conflicts"] == true {
		t.Fatalf("reply = %v", out)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "b.txt")); err != nil {
		t.Fatalf("picked file missing: %v", err)
	}
	subj := gitRun(t, e.dir, "log", "-1", "--format=%s")
	if subj != "feat: b" {
		t.Fatalf("subject = %q", subj)
	}
}

func TestCherryPickLiveBookmark(t *testing.T) {
	e := newTestEnv(t)
	sha := commitOnBranch(t, e, "c.txt", "c\n", "feat: c")
	cb := seedCommitBookmark(t, e, sha)
	gitRun(t, e.dir, "reset", "--hard", "HEAD~1")
	out := e.call(t, "gg_cherry_pick", map[string]any{"source": map[string]any{"bookmark": cb.ID}})
	if out["lane"] != "live" {
		t.Fatalf("reply = %v", out)
	}
	if gitRun(t, e.dir, "log", "-1", "--format=%s") != "feat: c" {
		t.Fatalf("commit not applied")
	}
}

func TestCherryPickPatchLaneAfterGC(t *testing.T) {
	e := newTestEnv(t)
	sha := commitOnBranch(t, e, "d.txt", "d\n", "feat: d")
	ce := seedShelfCommitAt(t, e, sha, "d label")
	gitRun(t, e.dir, "reset", "--hard", "HEAD~1")
	gcAway(t, e, sha)
	out := e.call(t, "gg_cherry_pick", map[string]any{"source": map[string]any{"shelf": ce.ID}})
	if out["lane"] != "patch" {
		t.Fatalf("expected patch lane, reply = %v", out)
	}
	if out["subject"] != "d label" {
		t.Fatalf("patch-lane subject must be the entry label, got %v", out["subject"])
	}
	if gitRun(t, e.dir, "log", "-1", "--format=%s") != "feat: d" {
		t.Fatalf("replayed commit missing")
	}
}

func TestCherryPickModePatchForced(t *testing.T) {
	e := newTestEnv(t)
	sha := commitOnBranch(t, e, "f.txt", "f\n", "feat: f")
	ce := seedShelfCommitAt(t, e, sha, "")
	gitRun(t, e.dir, "reset", "--hard", "HEAD~1")
	out := e.call(t, "gg_cherry_pick", map[string]any{
		"source": map[string]any{"shelf": ce.ID}, "mode": "patch",
	})
	if out["lane"] != "patch" {
		t.Fatalf("mode:patch must force the replay, reply = %v", out)
	}
}

func TestCherryPickConflictAbortDefault(t *testing.T) {
	e := newTestEnv(t)
	sha := commitOnBranch(t, e, "a.txt", "hello\nconflicting\n", "feat: conflict")
	ce := seedShelfCommitAt(t, e, sha, "")
	gitRun(t, e.dir, "reset", "--hard", "HEAD~1")
	commitOnBranch(t, e, "a.txt", "hello\ndiverged\n", "feat: diverge")
	msg := e.callErr(t, "gg_cherry_pick", map[string]any{"source": map[string]any{"shelf": ce.ID}})
	if !strings.Contains(msg, "on_conflict") {
		t.Fatalf("abort error must name the retry: %s", msg)
	}
	if gitRun(t, e.dir, "status", "--porcelain") != "" {
		t.Fatalf("abort must leave a clean tree")
	}
}

func TestCherryPickConflictKeep(t *testing.T) {
	e := newTestEnv(t)
	sha := commitOnBranch(t, e, "a.txt", "hello\nconflicting\n", "feat: conflict")
	ce := seedShelfCommitAt(t, e, sha, "")
	gitRun(t, e.dir, "reset", "--hard", "HEAD~1")
	commitOnBranch(t, e, "a.txt", "hello\ndiverged\n", "feat: diverge")
	out := e.call(t, "gg_cherry_pick", map[string]any{
		"source": map[string]any{"shelf": ce.ID}, "on_conflict": "keep",
	})
	if out["conflicts"] != true {
		t.Fatalf("keep must report conflicts, reply = %v", out)
	}
	files := out["conflicted_files"].([]any)
	if len(files) != 1 || files[0] != "a.txt" {
		t.Fatalf("conflicted_files = %v", files)
	}
	data, _ := os.ReadFile(filepath.Join(e.dir, "a.txt"))
	if !strings.Contains(string(data), "<<<<<<<") {
		t.Fatalf("markers missing: %s", data)
	}
}

func TestCherryPickRefusals(t *testing.T) {
	e := newTestEnv(t)
	fe := seedShelfFile(t, e)
	fb := seedBookmark(t, e)

	msg := e.callErr(t, "gg_cherry_pick", map[string]any{"source": map[string]any{"shelf": fe.ID}})
	if !strings.Contains(msg, "gg_write_to_worktree") {
		t.Fatalf("file shelf entry refusal: %s", msg)
	}
	msg = e.callErr(t, "gg_cherry_pick", map[string]any{"source": map[string]any{"bookmark": fb.ID}})
	if !strings.Contains(msg, "gg_write_to_worktree") {
		t.Fatalf("file bookmark refusal: %s", msg)
	}
	msg = e.callErr(t, "gg_cherry_pick", map[string]any{"source": map[string]any{}})
	if !strings.Contains(msg, "exactly one") {
		t.Fatalf("source validation: %s", msg)
	}
	msg = e.callErr(t, "gg_cherry_pick", map[string]any{
		"source": map[string]any{"bookmark": "x"}, "mode": "patch",
	})
	if !strings.Contains(msg, "shelf") {
		t.Fatalf("mode:patch+bookmark must be a usage error: %s", msg)
	}
	msg = e.callErr(t, "gg_cherry_pick", map[string]any{
		"source": map[string]any{"shelf": "nope"},
	})
	if !strings.Contains(msg, "not found") {
		t.Fatalf("unknown id: %s", msg)
	}
	msg = e.callErr(t, "gg_cherry_pick", map[string]any{
		"source": map[string]any{"shelf": seedShelfCommit(t, e).ID}, "on_conflict": "merge",
	})
	if !strings.Contains(msg, `"keep"`) {
		t.Fatalf("bad on_conflict must list the options: %s", msg)
	}
}

func TestCherryPickBookmarkGoneCommit(t *testing.T) {
	e := newTestEnv(t)
	sha := commitOnBranch(t, e, "g.txt", "g\n", "feat: g")
	cb := seedCommitBookmark(t, e, sha)
	gitRun(t, e.dir, "reset", "--hard", "HEAD~1")
	gcAway(t, e, sha)
	msg := e.callErr(t, "gg_cherry_pick", map[string]any{"source": map[string]any{"bookmark": cb.ID}})
	if !strings.Contains(msg, "no longer exists") {
		t.Fatalf("gone-commit bookmark: %s", msg)
	}
}
