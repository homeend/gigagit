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

// TestOpIgnore adds an untracked file to .gitignore through the op and
// checks the pattern lands (root-anchored) and status stops listing it.
func TestOpIgnore(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "notes.tmp"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ts := serve(t, New(domain.Open(dir)))

	opID := startOpBody(t, ts, `{"op":"ignore","path":"notes.tmp"}`)
	events := readSSE(t, ts, opID, 20*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	gi, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gi), "/notes.tmp") {
		t.Errorf(".gitignore = %q, want /notes.tmp line", gi)
	}
	if out := gitRun(t, dir, "status", "--porcelain"); strings.Contains(out, "notes.tmp") {
		t.Errorf("status still lists notes.tmp: %q", out)
	}
}

// TestOpIgnoreExt ignores the whole extension instead of the one file.
func TestOpIgnoreExt(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "notes.tmp"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ts := serve(t, New(domain.Open(dir)))

	opID := startOpBody(t, ts, `{"op":"ignore","path":"notes.tmp","ext":true}`)
	events := readSSE(t, ts, opID, 20*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	gi, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gi), "*.tmp") {
		t.Errorf(".gitignore = %q, want *.tmp line", gi)
	}
}

// TestOpIgnoreRefusals pins the gate: only an untracked path present in a
// fresh status read may be ignored — a tracked file 422s (git ignores only
// untracked paths, the TUI's untrackedFile gate), an unknown path 404s, a
// leading dash 400s.
func TestOpIgnoreRefusals(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1) // f.txt committed
	// make f.txt dirty so it appears in status as tracked-modified
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ts := serve(t, New(domain.Open(dir)))

	if code, out := postJSONRaw(t, ts, "/api/op", `{"op":"ignore","path":"f.txt"}`); code != http.StatusUnprocessableEntity {
		t.Errorf("tracked: code = %d (%v), want 422", code, out)
	}
	if code, out := postJSONRaw(t, ts, "/api/op", `{"op":"ignore","path":"ghost.tmp"}`); code != http.StatusNotFound {
		t.Errorf("unknown: code = %d (%v), want 404", code, out)
	}
	if code, out := postJSONRaw(t, ts, "/api/op", `{"op":"ignore","path":"--evil"}`); code != http.StatusBadRequest {
		t.Errorf("dash: code = %d (%v), want 400", code, out)
	}
}

// TestOpDiscardAll discards everything unstaged in one op: the tracked edit
// reverts AND the untracked file is deleted (both halves, one run).
func TestOpDiscardAll(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1) // f.txt committed as "content 1\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "junk.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ts := serve(t, New(domain.Open(dir)))

	opID := startOpBody(t, ts, `{"op":"discard","all":true}`)
	events := readSSE(t, ts, opID, 20*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	got, err := os.ReadFile(filepath.Join(dir, "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "content 1\n" {
		t.Errorf("f.txt = %q, want restored content", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "junk.txt")); !os.IsNotExist(err) {
		t.Errorf("junk.txt still exists (err=%v), want deleted", err)
	}
}

// TestOpDiscardAllRefusals: a conflicted tree refuses discard-all (the TUI's
// canDiscardAll rule — a bulk discard during a conflict destroys resolution
// state), and a tree with nothing unstaged refuses rather than no-oping.
func TestOpDiscardAllRefusals(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "side")
	gitRun(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("main2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "main2")
	gitTry(dir, "merge", "side") // conflicts on f.txt by construction

	ts := serve(t, New(domain.Open(dir)))

	code, out := postJSONRaw(t, ts, "/api/op", `{"op":"discard","all":true}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("conflicted: code = %d (%v), want 422", code, out)
	}
	if !strings.Contains(out["error"], "conflict") {
		t.Errorf("error = %v", out)
	}

	// clean tree: nothing to discard
	dir2 := newRepoDir(t, 1)
	ts2 := serve(t, New(domain.Open(dir2)))
	if code, out := postJSONRaw(t, ts2, "/api/op", `{"op":"discard","all":true}`); code != http.StatusUnprocessableEntity {
		t.Errorf("clean: code = %d (%v), want 422", code, out)
	}
}
