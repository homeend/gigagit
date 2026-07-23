# Web Sidebar Context Menus Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Right-click context menus on all three sidebar sections — branches gain delete-branch, worktrees gain copy-path/remove-worktree, tags gain show-commit/copy-name/delete-tag — over the existing op transport and decision modal.

**Architecture:** Three new `handleOpStart` cases (`delete-branch`, `delete-tag` with the `switch` case's `isGitArgSafe` guard; `remove-worktree` resolving the client-sent path against the server's own worktree list so only server values reach argv). The client extracts a generic `showCtxMenu(items,x,y)` from the branch menu, adds `contextmenu` listeners to the two remaining lists, flags destructive rows `danger`, and confirms delete-tag client-side because `engine.DeleteTag` is decision-free.

**Tech Stack:** Go stdlib HTTP (existing `internal/web`), local real-git fixtures (`newRepoDir`/`gitRun`), vanilla-JS SPA in `internal/web/static/`.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-23-web-ctx-menus-design.md` (approved).
- `delete-branch`/`delete-tag` inputs pass `isGitArgSafe` (non-empty, no leading `-`) or 400; the remove-worktree path is an IDENTIFIER resolved against `svc.Worktrees` — only server-owned `wt.Path`/`wt.Branch` reach the op; no match → 404 `unknown worktree`.
- `engine.DeleteTag` has NO engine confirm — the client MUST gate it behind `showLocalConfirm` before `startOp`.
- Engine guard refusals (checked-out branch, branch-in-worktree, current/main worktree) surface as `done{ok:false}` with the engine's message — no duplicate server-side checks.
- Client gating: delete-branch hidden on `is_head` rows; remove-worktree hidden on the row whose `path === state.worktree`.
- `internal/web` must not import `internal/git` (archtest-enforced).
- Work in worktree `/mnt/t/others/gigagit.worktrees/feat-web-ctx-menus`, branch `feat/web-ctx-menus`. Verify with `git branch --show-current` before ANY edit.

---

### Task 1: `delete-branch` + `delete-tag` on the transport

**Files:**
- Modify: `internal/web/ophttp.go` (the `opStartRequest` struct ~line 13, the `handleOpStart` switch after the `push` case, the doc comment ~line 19)
- Create: `internal/web/opdeletebranch_test.go`
- Create: `internal/web/opdeletetag_test.go`

**Interfaces:**
- Consumes: existing test helpers `newRepoDir(t, n) string`, `gitRun(t, dir string, args ...string) string`, `serve(t, *Server) *httptest.Server`, `postJSON(t, ts, path, body, contentType, origin string, out any) int`, `readSSE(t, ts, opID string, timeout) []wireEvent`, `waitDecision(t, run *opRun)`, `srv.opByID(id) *opRun`. Engine ops `engine.DeleteBranch{Name string}` (decisions `"delete-branch"`: delete/abort, then `"branch-unmerged"`: force-delete/keep on an unmerged branch) and `engine.DeleteTag{Name string}` (decision-free). `isGitArgSafe(s string) bool` (`server.go:139`).
- Produces: HTTP contract `POST /api/op {"op":"delete-branch","branch":N}` and `{"op":"delete-tag","tag":N}` → 202/400. Test helper `startOpBody(t, ts, body string) string` (Task 2 reuses it). `opStartRequest.Tag` field.

- [ ] **Step 1: Write the failing tests**

Create `internal/web/opdeletebranch_test.go`:

```go
package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// startOpBody starts any op from a raw JSON body and returns the op id.
func startOpBody(t *testing.T, ts *httptest.Server, body string) string {
	t.Helper()
	var out struct {
		OpID string `json:"op_id"`
	}
	if code := postJSON(t, ts, "/api/op", body, "application/json", "", &out); code != http.StatusAccepted {
		t.Fatalf("op start code = %d (body %s)", code, body)
	}
	return out.OpID
}

func TestOpHTTPDeleteBranchMerged(t *testing.T) {
	dir := newRepoDir(t, 2)
	gitRun(t, dir, "branch", "feature") // at HEAD: fully merged
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, `{"op":"delete-branch","branch":"feature"}`)
	run := srv.opByID(opID)
	if run == nil {
		t.Fatal("run not found")
	}
	waitDecision(t, run)
	run.mu.Lock()
	req := run.pending
	run.mu.Unlock()
	if req.ID != "delete-branch" || len(req.Options) != 2 {
		t.Fatalf("pending = %+v", req)
	}
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"delete"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if !strings.Contains(done["summary"].(string), "deleted branch feature") {
		t.Errorf("summary = %v", done["summary"])
	}
	if out := gitRun(t, dir, "branch", "--list", "feature"); strings.TrimSpace(out) != "" {
		t.Errorf("branch still listed: %q", out)
	}
}

func TestOpHTTPDeleteBranchConfirmAbort(t *testing.T) {
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "branch", "feature")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, `{"op":"delete-branch","branch":"feature"}`)
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"abort"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != false {
		t.Fatalf("done = %v (abort is a clean no-change)", done)
	}
	if strings.TrimSpace(gitRun(t, dir, "branch", "--list", "feature")) == "" {
		t.Error("branch gone after abort")
	}
}

func TestOpHTTPDeleteBranchUnmergedKeep(t *testing.T) {
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "checkout", "-b", "feature")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "unmerged work")
	gitRun(t, dir, "checkout", "main")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, `{"op":"delete-branch","branch":"feature"}`)
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"delete"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide delete code = %d", code)
	}
	waitDecision(t, run) // the unmerged fork parks next
	run.mu.Lock()
	req := run.pending
	run.mu.Unlock()
	if req.ID != "branch-unmerged" || len(req.Options) != 2 {
		t.Fatalf("pending = %+v", req)
	}
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"keep"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide keep code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != false {
		t.Fatalf("done = %v", done)
	}
	if strings.TrimSpace(gitRun(t, dir, "branch", "--list", "feature")) == "" {
		t.Error("branch gone after keep")
	}
}

func TestOpHTTPDeleteBranchUnmergedForce(t *testing.T) {
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "checkout", "-b", "feature")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "unmerged work")
	gitRun(t, dir, "checkout", "main")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, `{"op":"delete-branch","branch":"feature"}`)
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"delete"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide delete code = %d", code)
	}
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"force-delete"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide force-delete code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if out := gitRun(t, dir, "branch", "--list", "feature"); strings.TrimSpace(out) != "" {
		t.Errorf("branch still listed after force-delete: %q", out)
	}
}

func TestOpHTTPDeleteBranchCurrent(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"delete-branch","branch":"main"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false (checked-out branch)", done)
	}
	if err, _ := done["error"].(string); !strings.Contains(err, "checked-out branch") {
		t.Errorf("error = %v", done["error"])
	}
}

func TestOpHTTPDeleteBranchInWorktree(t *testing.T) {
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "branch", "feature")
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, dir, "worktree", "add", wt, "feature")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"delete-branch","branch":"feature"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false (branch in worktree)", done)
	}
	if err, _ := done["error"].(string); !strings.Contains(err, "worktree") {
		t.Errorf("error = %v", done["error"])
	}
}

func TestOpHTTPDeleteBranchBadName(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	for _, body := range []string{
		`{"op":"delete-branch"}`,
		`{"op":"delete-branch","branch":"--delete"}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want 400", body, code)
		}
	}
}
```

Create `internal/web/opdeletetag_test.go`:

```go
package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestOpHTTPDeleteTag(t *testing.T) {
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "tag", "v1")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"delete-tag","tag":"v1"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if !strings.Contains(done["summary"].(string), "deleted tag v1") {
		t.Errorf("summary = %v", done["summary"])
	}
	if out := gitRun(t, dir, "tag", "--list", "v1"); strings.TrimSpace(out) != "" {
		t.Errorf("tag still listed: %q", out)
	}
}

func TestOpHTTPDeleteTagMissing(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"delete-tag","tag":"nope"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false", done)
	}
	if done["error"] == nil || done["error"] == "" {
		t.Error("missing error detail")
	}
}

func TestOpHTTPDeleteTagBadName(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	for _, body := range []string{
		`{"op":"delete-tag"}`,
		`{"op":"delete-tag","tag":"-v1"}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want 400", body, code)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-ctx-menus && go test ./internal/web/ -run 'TestOpHTTPDelete' -v`
Expected: every test FAILS at the start call with `op start code = 400` — `unknown op "delete-branch"` / `"delete-tag"` (BadName tests fail with 400-want-400 already passing is fine; the others must fail).

- [ ] **Step 3: Add the cases to handleOpStart**

In `internal/web/ophttp.go`:

a. Extend `opStartRequest`:

```go
type opStartRequest struct {
	Op      string `json:"op"`
	Branch  string `json:"branch"`
	Message string `json:"message"`
	Tag     string `json:"tag"`
}
```

b. After the `case "push":` block, insert:

```go
	case "delete-branch":
		if req.Branch == "" || !isGitArgSafe(req.Branch) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
			return
		}
		// The engine confirms ("delete-branch") and forks on an unmerged
		// branch ("branch-unmerged") — both park in the browser modal.
		op = engine.DeleteBranch{Name: req.Branch}
	case "delete-tag":
		if req.Tag == "" || !isGitArgSafe(req.Tag) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid tag"))
			return
		}
		// Decision-free op: the client shows its own confirm before starting.
		op = engine.DeleteTag{Name: req.Tag}
```

c. Update the doc comment on `handleOpStart`:

```go
// handleOpStart begins an operation and returns 202 {op_id}. Ops wired so
// far: switch, commit, pull, push, delete-branch, delete-tag; the switch
// statement is where future ops land.
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/web/ -run 'TestOpHTTPDelete' -v`
Expected: all 10 PASS.

- [ ] **Step 5: Run the package + archtest, then race on the new tests**

Run: `go test ./internal/web/ ./internal/archtest/ && go test -race ./internal/web/ -run 'TestOpHTTPDelete'`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/ophttp.go internal/web/opdeletebranch_test.go internal/web/opdeletetag_test.go
git commit -m "feat(web): op:delete-branch + op:delete-tag on the transport"
```

---

### Task 2: `remove-worktree` on the transport (allowlist resolution)

**Files:**
- Modify: `internal/web/ophttp.go` (the `opStartRequest` struct, the `handleOpStart` switch after the `delete-tag` case, the doc comment)
- Create: `internal/web/opremoveworktree_test.go`

**Interfaces:**
- Consumes: `startOpBody(t, ts, body string) string` from `opdeletebranch_test.go` (Task 1). `s.svc.Worktrees(ctx) ([]model.Worktree, error)` — each row has `.Path` (absolute) and `.Branch` (short name, `""` when detached); the served main repo is itself a listed worktree. `engine.RemoveWorktree{Path, Branch string}` — decisions `"remove-scope"` (`worktree-only`/`worktree-and-branch`/`abort`; 3 options when `Branch != ""`), plus `"worktree-locked"` and a dirty-force fork the engine raises reactively. Its guards refuse the current and main worktree with clear errors.
- Produces: HTTP contract `POST /api/op {"op":"remove-worktree","path":P}` → 202, 400 (empty path), 404 (path not in the server's worktree list). `opStartRequest.Path` field.

- [ ] **Step 1: Write the failing tests**

Create `internal/web/opremoveworktree_test.go`:

```go
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

// addWorktree creates <branch> at HEAD and checks it out in a fresh linked
// worktree, returning the worktree path.
func addWorktree(t *testing.T, dir, branch string) string {
	t.Helper()
	gitRun(t, dir, "branch", branch)
	wt := filepath.Join(t.TempDir(), "wt-"+branch)
	gitRun(t, dir, "worktree", "add", wt, branch)
	return wt
}

func removeWtBody(path string) string {
	return fmt.Sprintf(`{"op":"remove-worktree","path":%q}`, path)
}

func TestOpHTTPRemoveWorktreeOnly(t *testing.T) {
	dir := newRepoDir(t, 1)
	wt := addWorktree(t, dir, "feature")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, removeWtBody(wt))
	run := srv.opByID(opID)
	if run == nil {
		t.Fatal("run not found")
	}
	waitDecision(t, run)
	run.mu.Lock()
	req := run.pending
	run.mu.Unlock()
	if req.ID != "remove-scope" || len(req.Options) != 3 {
		t.Fatalf("pending = %+v", req)
	}
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"worktree-only"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("worktree dir still exists (stat err %v)", err)
	}
	if strings.TrimSpace(gitRun(t, dir, "branch", "--list", "feature")) == "" {
		t.Error("worktree-only removed the branch too")
	}
}

func TestOpHTTPRemoveWorktreeAbort(t *testing.T) {
	dir := newRepoDir(t, 1)
	wt := addWorktree(t, dir, "feature")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, removeWtBody(wt))
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"abort"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != false {
		t.Fatalf("done = %v", done)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("worktree dir gone after abort: %v", err)
	}
}

func TestOpHTTPRemoveWorktreeAndBranch(t *testing.T) {
	dir := newRepoDir(t, 1)
	wt := addWorktree(t, dir, "feature")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, removeWtBody(wt))
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"worktree-and-branch"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("worktree dir still exists (stat err %v)", err)
	}
	if out := gitRun(t, dir, "branch", "--list", "feature"); strings.TrimSpace(out) != "" {
		t.Errorf("branch still listed: %q", out)
	}
}

func TestOpHTTPRemoveWorktreeUnknownPath(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/op", removeWtBody("/nonexistent/wt"), "application/json", "", nil); code != http.StatusNotFound {
		t.Fatalf("unknown path code = %d, want 404", code)
	}
	if code := postJSON(t, ts, "/api/op", `{"op":"remove-worktree","path":""}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Fatalf("empty path code = %d, want 400", code)
	}
}

func TestOpHTTPRemoveWorktreeMain(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	// The main worktree IS in the server's list, so the allowlist passes and
	// the ENGINE guard must refuse it.
	events := readSSE(t, ts, startOpBody(t, ts, removeWtBody(dir)), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false (main worktree)", done)
	}
	if done["error"] == nil || done["error"] == "" {
		t.Error("missing error detail")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-ctx-menus && go test ./internal/web/ -run TestOpHTTPRemoveWorktree -v`
Expected: FAIL — the start calls get 400 `unknown op "remove-worktree"` (UnknownPath's first sub-check gets 400 where 404 is wanted).

Caveat: `t.TempDir()` can sit behind a symlink on some systems (`/tmp` → `/private/tmp` on macOS), making the fixture path differ from git's reported worktree path. If `TestOpHTTPRemoveWorktreeMain` or `TestOpHTTPRemoveWorktreeOnly` fails on a path mismatch at Step 4, resolve the FIXTURE path before building the body:

```go
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	// then: startOpBody(t, ts, removeWtBody(resolved))
```

The handler's exact-match contract must NOT gain path normalization for this — the real client round-trips the server's own strings, which always match.

- [ ] **Step 3: Add the case to handleOpStart**

In `internal/web/ophttp.go`:

a. Extend `opStartRequest`:

```go
type opStartRequest struct {
	Op      string `json:"op"`
	Branch  string `json:"branch"`
	Message string `json:"message"`
	Tag     string `json:"tag"`
	Path    string `json:"path"`
}
```

b. After the `case "delete-tag":` block, insert:

```go
	case "remove-worktree":
		if req.Path == "" {
			writeErr(w, http.StatusBadRequest, errors.New("path required"))
			return
		}
		// The client-sent path is an identifier, not an argument: resolve it
		// against the server's own worktree list so only server-owned values
		// reach git argv (worktree paths legitimately contain characters
		// isGitArgSafe would reject). The engine still guards the current and
		// main worktree.
		wts, werr := s.svc.Worktrees(r.Context())
		if werr != nil {
			writeErr(w, http.StatusInternalServerError, werr)
			return
		}
		found := false
		for _, wt := range wts {
			if wt.Path == req.Path {
				op = engine.RemoveWorktree{Path: wt.Path, Branch: wt.Branch}
				found = true
				break
			}
		}
		if !found {
			writeErr(w, http.StatusNotFound, errors.New("unknown worktree"))
			return
		}
```

c. Update the doc comment op list to `switch, commit, pull, push, delete-branch, delete-tag, remove-worktree`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/web/ -run TestOpHTTPRemoveWorktree -v`
Expected: all 5 PASS.

- [ ] **Step 5: Run the package + archtest, then race on the new tests**

Run: `go test ./internal/web/ ./internal/archtest/ && go test -race ./internal/web/ -run TestOpHTTPRemoveWorktree`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/ophttp.go internal/web/opremoveworktree_test.go
git commit -m "feat(web): op:remove-worktree — allowlist path resolution on the transport"
```

---

### Task 3: Context menus in the client + docs

**Files:**
- Modify: `internal/web/static/app.js` (renderWorktrees ~line 100-109, the menu block ~lines 285-324, the tags-list listener area ~line 345)
- Modify: `internal/web/static/style.css` (after the `#ctx-menu button:hover` rule, line 127)
- Modify: `CHANGELOG.md` (top of the current unreleased section)
- Modify: `CLAUDE.md` (the `web` row's op list)

**Interfaces:**
- Consumes: Tasks 1-2's HTTP contracts. Existing client pieces: `startOp(body, label)`, `showLocalConfirm(prompt, options, cb)`, `openCommitByHash(hash, title)`, `gotoBranchTip(b)`, `startSwitch(name)`, `opLine(text, isErr)`, `esc(s)`, `state.worktree` (the served worktree's path, set by `loadRepo`), `state.worktrees` (rows with `.path`/`.branch`), `state.tags` (rows with `.name`/`.target`). NOTE: `refreshAfterOp` already calls `fetchBranches()`, which reloads worktrees AND tags — the sidebar refresh after these ops needs NO new code (verify only).
- Produces: `showCtxMenu(items, x, y)` (items: `{label, act, danger?}`), `showWorktreeMenu`, `showTagMenu`, `copyText(text)`; nothing downstream consumes them.

- [ ] **Step 1: Generalize the menu and grow the branch menu**

In `app.js`, replace the whole `showBranchMenu` function (lines 289-300) with:

```js
// showCtxMenu renders the shared right-click menu at (x,y): safe actions
// first; rows flagged danger render red.
function showCtxMenu(items, x, y) {
  const menu = $("ctx-menu");
  menu._items = items;
  menu.innerHTML = items
    .map((it, i) => `<button data-i="${i}"${it.danger ? ' class="danger"' : ""}>${esc(it.label)}</button>`)
    .join("");
  menu.style.left = Math.min(x, window.innerWidth - 200) + "px";
  menu.style.top = Math.min(y, window.innerHeight - 120) + "px";
  menu.classList.remove("hidden");
}

function showBranchMenu(b, x, y) {
  const items = [{ label: "go to tip", act: () => gotoBranchTip(b) }];
  if (!b.is_head) {
    items.push({ label: "switch to " + b.name, act: () => startSwitch(b.name) });
    items.push({
      label: "delete " + b.name,
      danger: true,
      act: () => startOp({ op: "delete-branch", branch: b.name }, "deleting " + b.name),
    });
  }
  showCtxMenu(items, x, y);
}
```

(The `$("ctx-menu")` click handler and the outside-click dismisser at lines 302-310 stay unchanged — they read `menu._items`, which `showCtxMenu` still sets.)

- [ ] **Step 2: Worktree menu + data attribute + listener**

a. In `renderWorktrees` (line 106), add `data-p` to the row:

```js
      return `<li class="${cur.trim()}" data-p="${esc(w.path)}" title="${esc(w.path)}">${cur ? "● " : ""}${esc(label)}<span class="wpath">${esc(base)}</span></li>`;
```

b. After the `$("branches-list").addEventListener("contextmenu", ...)` block (line 324), add:

```js
function copyText(text) {
  navigator.clipboard.writeText(text).catch(() => opLine("copy failed (clipboard unavailable)", true));
}

function showWorktreeMenu(w, x, y) {
  const items = [{ label: "copy path", act: () => copyText(w.path) }];
  // The served worktree's row gets no remove (the engine would refuse it
  // anyway); main is engine-guarded too.
  if (!(state.worktree && w.path === state.worktree)) {
    items.push({
      label: "remove worktree",
      danger: true,
      act: () => startOp({ op: "remove-worktree", path: w.path }, "removing " + w.path.split("/").pop()),
    });
  }
  showCtxMenu(items, x, y);
}
$("worktrees-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.p) return;
  e.preventDefault();
  const w = state.worktrees.find((x) => x.path === li.dataset.p);
  if (w) showWorktreeMenu(w, e.clientX, e.clientY);
});
```

- [ ] **Step 3: Tag menu + listener**

After the `$("tags-list").addEventListener("click", ...)` block (line 349), add:

```js
function showTagMenu(tg, x, y) {
  showCtxMenu(
    [
      { label: "show commit", act: () => openCommitByHash(tg.target, "🏷 " + tg.name) },
      { label: "copy name", act: () => copyText(tg.name) },
      {
        label: "delete " + tg.name,
        danger: true,
        // engine.DeleteTag is decision-free, so the confirm lives here — a
        // right-click plus one click must never delete a ref unconfirmed.
        act: () =>
          showLocalConfirm("Delete tag " + tg.name + "?", ["delete", "abort"], (o) => {
            if (o === "delete") startOp({ op: "delete-tag", tag: tg.name }, "deleting tag " + tg.name);
          }),
      },
    ],
    x,
    y
  );
}
$("tags-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.n) return;
  e.preventDefault();
  const tg = state.tags.find((x) => x.name === li.dataset.n);
  if (tg) showTagMenu(tg, e.clientX, e.clientY);
});
```

- [ ] **Step 4: Danger row CSS**

In `style.css`, after the `#ctx-menu button:hover` rule (line 127), add:

```css
#ctx-menu button.danger { color: #f27a6a; } /* matches #op-line.err */
```

- [ ] **Step 5: Build, test, JS syntax check**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-ctx-menus && go build ./cmd/gg && go test ./internal/web/ && node --check internal/web/static/app.js`
Expected: build OK, tests PASS, no JS syntax error (embedded assets don't fail the Go build on one).

- [ ] **Step 6: Update CHANGELOG.md and CLAUDE.md**

CHANGELOG.md — add at the top of the current unreleased section:

```markdown
- `gg web`: right-click context menus on all three sidebar sections. Branches
  gain **delete branch** (engine confirm + unmerged force fork in the modal;
  the tip is snapshotted to `refs/gg/versions` first, so it's recoverable via
  `gg versions`). Worktrees gain **copy path** and **remove worktree** (scope /
  locked / dirty forks in the modal; the served worktree's row is exempt).
  Tags gain **show commit**, **copy name**, and **delete tag** (client-side
  confirm — the engine op is decision-free). Destructive rows render red; the
  remove-worktree path is resolved against the server's own worktree list, so
  no client string reaches git argv.
```

CLAUDE.md — in the `web` row of the package map, after the sentence describing `op:"push"` (op #4), append:

```
Ops #5-7: `op:"delete-branch"`/`op:"delete-tag"` (isGitArgSafe-guarded names; DeleteTag is decision-free so the CLIENT confirms via showLocalConfirm) and `op:"remove-worktree"` (client path is an identifier resolved against the server's own Worktrees list — allowlist, not sanitization; 404 on no match) — all three behind sidebar right-click menus (generic `showCtxMenu`, destructive rows red, served-worktree row exempt from remove).
```

Keep the row one physical line.

- [ ] **Step 7: Commit**

```bash
git add internal/web/static/app.js internal/web/static/style.css CHANGELOG.md CLAUDE.md
git commit -m "feat(web): sidebar context menus — delete branch, remove worktree, tag actions"
```
