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
)

func moveWtBody(path, dest string) string {
	return fmt.Sprintf(`{"op":"move-worktree","path":%q,"dest":%q}`, path, dest)
}

// The happy path is a RENAME: same parent, new basename — which is the same
// op as a move, and the row the sidebar offers first.
func TestOpHTTPMoveWorktreeRename(t *testing.T) {
	dir := newRepoDir(t, 1)
	wt := addWorktree(t, dir, "feature")
	dest := filepath.Join(filepath.Dir(wt), "renamed")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, moveWtBody(wt, dest)), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("destination missing: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("source still exists (stat err %v)", err)
	}
	// git's own record has to follow, or the worktree list still names the
	// old path and the sidebar row would point at nothing.
	if list := gitRun(t, dir, "worktree", "list"); !strings.Contains(list, dest) {
		t.Errorf("worktree list does not mention %s:\n%s", dest, list)
	}
}

// The main worktree cannot be moved. The engine refuses it, and the refusal
// has to reach the browser as a failed op rather than a silent no-op.
func TestOpHTTPMoveWorktreeRefusesMain(t *testing.T) {
	dir := newRepoDir(t, 1)
	dest := filepath.Join(t.TempDir(), "elsewhere")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, moveWtBody(dir, dest)), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] == true {
		t.Fatalf("moving the main worktree succeeded: %v", done)
	}
	if msg, _ := done["error"].(string); !strings.Contains(msg, "main worktree") {
		t.Errorf("error = %q, want it to name the main worktree", msg)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Errorf("main worktree damaged: %v", err)
	}
}

// A destination INSIDE the worktree being moved would move a directory into
// itself. Refused before git is invoked.
func TestOpHTTPMoveWorktreeRefusesNestedDest(t *testing.T) {
	dir := newRepoDir(t, 1)
	wt := addWorktree(t, dir, "feature")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, moveWtBody(wt, filepath.Join(wt, "inner"))), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] == true {
		t.Fatalf("nested destination succeeded: %v", done)
	}
	if msg, _ := done["error"].(string); !strings.Contains(msg, "inside the worktree") {
		t.Errorf("error = %q", msg)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("source worktree gone: %v", err)
	}
}

// A relative path never reaches the engine: it would be resolved against the
// server's cwd, which is not necessarily this repository at all.
func TestOpHTTPMoveWorktreeRefusesRelativePaths(t *testing.T) {
	dir := newRepoDir(t, 1)
	wt := addWorktree(t, dir, "feature")
	ts := serve(t, New(domain.Open(dir)))

	for _, tc := range []struct{ name, body string }{
		{"relative source", moveWtBody("wt-feature", filepath.Join(t.TempDir(), "d"))},
		{"relative dest", moveWtBody(wt, "renamed")},
		{"empty dest", moveWtBody(wt, "")},
	} {
		if code := postJSON(t, ts, "/api/op", tc.body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("%s: code = %d, want 400", tc.name, code)
		}
	}
}

// A LOCKED worktree is the interesting fork: the engine parks a decision
// rather than failing, and the browser has to be able to answer it.
func TestOpHTTPMoveWorktreeLockedParksDecision(t *testing.T) {
	dir := newRepoDir(t, 1)
	wt := addWorktree(t, dir, "feature")
	gitRun(t, dir, "worktree", "lock", wt)
	dest := filepath.Join(filepath.Dir(wt), "renamed")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, moveWtBody(wt, dest))
	run := srv.opByID(opID)
	if run == nil {
		t.Fatal("run not found")
	}
	waitDecision(t, run)
	run.mu.Lock()
	req := run.pending
	run.mu.Unlock()
	if req.ID != "move-worktree-locked" {
		t.Fatalf("pending = %+v", req)
	}
	if len(req.Options) != 2 || req.Options[0] != "unlock-and-move" || req.Options[1] != "abort" {
		t.Fatalf("options = %v", req.Options)
	}
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"unlock-and-move"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	// The decision has to reach the CLIENT, not just park server-side: the
	// browser's modal is driven off this event in the stream.
	dec, ok := findEvent(events, "decision")
	if !ok {
		t.Fatal("no decision event in the SSE stream")
	}
	if dec["id"] != "move-worktree-locked" {
		t.Errorf("decision event = %v", dec)
	}
	if done := events[len(events)-1]; done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("destination missing after unlock-and-move: %v", err)
	}
}

// Answering that same decision with abort leaves everything where it was.
func TestOpHTTPMoveWorktreeLockedAbort(t *testing.T) {
	dir := newRepoDir(t, 1)
	wt := addWorktree(t, dir, "feature")
	gitRun(t, dir, "worktree", "lock", wt)
	dest := filepath.Join(filepath.Dir(wt), "renamed")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, moveWtBody(wt, dest))
	run := srv.opByID(opID)
	if run == nil {
		t.Fatal("run not found")
	}
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"abort"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	if done := events[len(events)-1]; done["changed"] != false {
		t.Fatalf("done = %v", done)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("source worktree gone after abort: %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("destination created after abort (stat err %v)", err)
	}
}

func keepBody(sha, name, path, mode string) string {
	return fmt.Sprintf(`{"op":"create-worktree-keep","sha":%q,"name":%q,"path":%q,"mode":%q}`, sha, name, path, mode)
}

// The keep modes land the new branch on the start point's PARENT with the
// commit's diff staged (or unstaged) in the new worktree.
func TestOpHTTPCreateWorktreeKeepStaged(t *testing.T) {
	dir := newRepoDir(t, 3)
	head := gitRun(t, dir, "rev-parse", "HEAD")
	parent := gitRun(t, dir, "rev-parse", "HEAD^")
	dest := filepath.Join(t.TempDir(), "kept")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, keepBody(head, "keep-staged", dest, "staged")), 60*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := gitRun(t, dest, "rev-parse", "HEAD"); got != parent {
		t.Errorf("new worktree HEAD = %s, want the parent %s", got, parent)
	}
	// --soft: the commit's change is in the index, not the working tree only.
	if staged := gitRun(t, dest, "diff", "--cached", "--name-only"); staged != "f.txt" {
		t.Errorf("staged = %q, want f.txt", staged)
	}
}

func TestOpHTTPCreateWorktreeKeepUnstaged(t *testing.T) {
	dir := newRepoDir(t, 3)
	head := gitRun(t, dir, "rev-parse", "HEAD")
	dest := filepath.Join(t.TempDir(), "kept")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, keepBody(head, "keep-unstaged", dest, "unstaged")), 60*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if staged := gitRun(t, dest, "diff", "--cached", "--name-only"); staged != "" {
		t.Errorf("staged = %q, want nothing staged", staged)
	}
	if unstaged := gitRun(t, dest, "diff", "--name-only"); unstaged != "f.txt" {
		t.Errorf("unstaged = %q, want f.txt", unstaged)
	}
}

// The mode is resolved through an allowlist, so a value the wire invents —
// or an integer, which an older client might send for an engine constant it
// does not know — is refused before anything is created.
func TestOpHTTPCreateWorktreeKeepRejectsUnknownMode(t *testing.T) {
	dir := newRepoDir(t, 2)
	head := gitRun(t, dir, "rev-parse", "HEAD")
	dest := filepath.Join(t.TempDir(), "kept")
	ts := serve(t, New(domain.Open(dir)))

	for _, mode := range []string{"", "1", "2", "KeepStaged", "everything"} {
		if code := postJSON(t, ts, "/api/op", keepBody(head, "b", dest, mode), "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("mode %q: code = %d, want 400", mode, code)
		}
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("worktree created despite the refusal (stat err %v)", err)
	}
}

// A root commit has no parent to keep the changes against. The engine says so
// with a typed error BEFORE creating anything — the client hides the rows
// there, and this is what happens if it ever stops.
func TestOpHTTPCreateWorktreeKeepRefusesRootCommit(t *testing.T) {
	dir := newRepoDir(t, 2)
	root := gitRun(t, dir, "rev-list", "--max-parents=0", "HEAD")
	dest := filepath.Join(t.TempDir(), "kept")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, keepBody(root, "from-root", dest, "staged")), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] == true {
		t.Fatalf("keep mode on a root commit succeeded: %v", done)
	}
	if msg, _ := done["error"].(string); !strings.Contains(msg, "root commit") {
		t.Errorf("error = %q", msg)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("worktree created despite the refusal (stat err %v)", err)
	}
}
