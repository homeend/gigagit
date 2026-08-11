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

// TestOpDiscardPaths discards a marked set in ONE op: tracked edits revert,
// untracked files are deleted, and an unmarked dirty file is untouched.
func TestOpDiscardPaths(t *testing.T) {
	dir := newRepoDir(t, 1) // f.txt committed as "content 1\n"
	for _, f := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("keep\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "c2")
	// dirty: a.txt + b.txt edited (tracked), junk.txt untracked
	for _, f := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("dirty\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "junk.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ts := serve(t, New(domain.Open(dir)))

	// discard a.txt + junk.txt, leave b.txt dirty
	opID := startOpBody(t, ts, `{"op":"discard","paths":["a.txt","junk.txt"]}`)
	events := readSSE(t, ts, opID, 20*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "keep\n" {
		t.Errorf("a.txt = %q, want reverted", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "junk.txt")); !os.IsNotExist(err) {
		t.Errorf("junk.txt still exists, want deleted")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(got) != "dirty\n" {
		t.Errorf("b.txt = %q, want still dirty (unmarked)", got)
	}
}

// TestOpDiscardPathsRefusals: one unknown path fails the WHOLE batch (404,
// nothing discarded), and a conflicted member 422s the whole batch — the
// per-file rules, applied all-or-nothing so a stale marked set cannot
// half-discard.
func TestOpDiscardPathsRefusals(t *testing.T) {
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ts := serve(t, New(domain.Open(dir)))

	code, out := postJSONRaw(t, ts, "/api/op", `{"op":"discard","paths":["f.txt","ghost.txt"]}`)
	if code != http.StatusNotFound {
		t.Fatalf("unknown member: code = %d (%v), want 404", code, out)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(got) != "dirty\n" {
		t.Errorf("f.txt = %q — a refused batch must discard NOTHING", got)
	}
	if code, out := postJSONRaw(t, ts, "/api/op", `{"op":"discard","paths":["--evil"]}`); code != http.StatusBadRequest {
		t.Errorf("dash member: code = %d (%v), want 400", code, out)
	}
	if code, out := postJSONRaw(t, ts, "/api/op", `{"op":"discard","paths":[]}`); code != http.StatusBadRequest {
		t.Errorf("empty list: code = %d (%v), want 400", code, out)
	}

	// conflicted member → 422 for the whole batch
	dir2 := newRepoDir(t, 1)
	gitRun(t, dir2, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir2, "f.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir2, "add", "-A")
	gitRun(t, dir2, "commit", "-m", "side")
	gitRun(t, dir2, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir2, "f.txt"), []byte("main2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir2, "add", "-A")
	gitRun(t, dir2, "commit", "-m", "main2")
	gitTry(dir2, "merge", "side")
	if err := os.WriteFile(filepath.Join(dir2, "extra.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts2 := serve(t, New(domain.Open(dir2)))
	code, out = postJSONRaw(t, ts2, "/api/op", `{"op":"discard","paths":["extra.txt","f.txt"]}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("conflicted member: code = %d (%v), want 422", code, out)
	}
	if !strings.Contains(out["error"], "conflicted") {
		t.Errorf("error = %v", out)
	}
	if _, err := os.Stat(filepath.Join(dir2, "extra.txt")); err != nil {
		t.Errorf("extra.txt gone — a refused batch must discard NOTHING")
	}
}
