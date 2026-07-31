# Web Conflict Surface Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the gg web UI a real conflict story: a paused-op banner with continue/abort, and an in-browser per-block ours/theirs picker replacing the dead "conflicted — resolve in the TUI" row.

**Architecture:** Pure `internal/web` + static client work — the engine already has everything (`ContinueOp{}`, `AbortOp{}`, `ResolveConflictHunks{Path, Content}`, `hunkpick.ParseConflict`). `/api/status` grows a `conflict` object from `domain.Conflict`; two new endpoints twin the hunk-staging pair (`/api/conflict-hunks` GET, `/api/resolve-hunks` POST); `POST /api/op` gains `continue`/`abort`. Spec: `docs/superpowers/specs/2026-07-31-web-conflict-surface-design.md`.

**Tech Stack:** Go 1.26, net/http, real-git table tests (`internal/web` test helpers), vanilla JS/CSS SPA (`internal/web/static/`).

## Global Constraints

- Work in the worktree: prefix EVERY build/test command with `cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && ` (bash cwd drifts between calls; zsh glob hazards — never rely on inherited cwd).
- Branch: `feat/web-conflict-surface` (off `web-dev`). Commit per task; message style `feat(web): …` / `test(web): …`; end every commit message with the two trailers used repo-wide:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01Uor9TNevUmrisqeZ9BUwUm`.
- `internal/web` is a domain-only frontend: import `internal/domain`, `internal/engine`, `internal/model`, `internal/hunkpick`, `internal/textdiff` — NEVER `internal/git`/`tui`/`cli` (archtest enforces).
- Every mutating endpoint is registered through `writeGuard(...)`. Every request value that could reach git argv passes `isGitArgSafe` first.
- Protocol values (op names, pick values, decision options) are English; only rendered text is display.
- Tests use a REAL git in `t.TempDir()` via the existing helpers (`gitRun`, `newRepoDir`, `serve`, `getJSON`, `postJSON`, `startOpBody`, `readSSE`). No FakeRunner here.
- Run `gofmt -l` before each commit (test.sh gates on it).
- No new dependencies.

---

### Task 1: `/api/status` carries the conflict object

**Files:**
- Create: `internal/web/conflict.go`
- Create: `internal/web/conflict_test.go`
- Modify: `internal/web/status.go` (`writeStatus`, currently ~line 26-50)

**Interfaces:**
- Consumes: `svc.Conflict(ctx, st) domain.ConflictState` (fields `Op/Source/Target`, method `Describe() string`); `st.Counts().Conflicted`.
- Produces: JSON key `"conflict"` on `/api/status` — `{op, source, target, desc, conflicted}` — present ONLY when a sequencer op is paused. Test helper `conflictedMergeState(t, dir)` (reused by Tasks 2-4). Later tasks rely on `conflictingRepo(t)` which ALREADY EXISTS in `internal/web/opmerge_test.go` (same package): main and feature both edit the same line of `f.txt` (`main\n` vs `feature\n`), main checked out.

- [ ] **Step 1: Write the failing tests**

`internal/web/conflict_test.go`:

```go
package web

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// conflictStatusResp decodes the parts of /api/status these tests assert on.
type conflictStatusResp struct {
	Counts   map[string]int `json:"counts"`
	Conflict *struct {
		Op         string `json:"op"`
		Source     string `json:"source"`
		Target     string `json:"target"`
		Desc       string `json:"desc"`
		Conflicted int    `json:"conflicted"`
	} `json:"conflict"`
}

// conflictedMergeState runs `git merge feature`, expecting the conflict to
// leave a paused merge in the tree. gitRun can't be used — it fails the test
// on any non-zero exit, and a conflicted merge exits 1 by design.
func conflictedMergeState(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "-c", "commit.gpgsign=false", "merge", "feature")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("merge feature unexpectedly succeeded:\n%s", out)
	}
	if gitRun(t, dir, "ls-files", "-u") == "" {
		t.Fatal("no unmerged entries after conflicted merge")
	}
}

func TestStatusConflictObject(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	ts := serve(t, New(domain.Open(dir)))

	var st conflictStatusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	c := st.Conflict
	if c == nil {
		t.Fatal("no conflict object on a paused merge")
	}
	if c.Op != "merge" || c.Source != "feature" || c.Target != "main" {
		t.Errorf("conflict = %+v, want merge feature→main", c)
	}
	if c.Desc != "merging feature into main" {
		t.Errorf("desc = %q", c.Desc)
	}
	if c.Conflicted != 1 {
		t.Errorf("conflicted = %d, want 1", c.Conflicted)
	}
}

func TestStatusConflictAbsentWhenClean(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	var st conflictStatusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if st.Conflict != nil {
		t.Errorf("conflict = %+v, want absent", st.Conflict)
	}
}

// A paused op whose conflicts were all resolved by hand (file written + staged
// outside gg, merge never continued) must STILL report — that is the
// resume-paused-op parity the banner's Continue depends on.
func TestStatusConflictPausedZeroConflicts(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("resolved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "f.txt")
	ts := serve(t, New(domain.Open(dir)))

	var st conflictStatusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if st.Conflict == nil || st.Conflict.Op != "merge" {
		t.Fatalf("conflict = %+v, want paused merge reported", st.Conflict)
	}
	if st.Conflict.Conflicted != 0 {
		t.Errorf("conflicted = %d, want 0", st.Conflict.Conflicted)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && go test ./internal/web/ -run TestStatusConflict -v`
Expected: FAIL — `st.Conflict` nil in the first test (writeStatus doesn't emit the key yet).

- [ ] **Step 3: Implement**

`internal/web/conflict.go` (new file — this task adds only the payload; Tasks 3-4 grow it):

```go
package web

// conflictPayload is the paused-op object /api/status carries whenever a
// sequencer op (merge/rebase/cherry-pick/revert) is in progress — including
// paused with every conflict resolved (conflicted == 0), which is what lets
// the client's Continue light up. Absent entirely when nothing is paused.
type conflictPayload struct {
	Op         string `json:"op"`
	Source     string `json:"source,omitempty"`
	Target     string `json:"target,omitempty"`
	Desc       string `json:"desc,omitempty"` // domain's human phrase ("merging feature into main")
	Conflicted int    `json:"conflicted"`
}
```

In `internal/web/status.go`, `writeStatus`: replace the final `writeJSON(w, map[string]any{...})` call with a variable so the conflict key can be added conditionally:

```go
	c := st.Counts()
	resp := map[string]any{
		"files": files,
		"counts": map[string]int{
			"staged": c.Staged, "unstaged": c.Unstaged,
			"untracked": c.Untracked, "conflicted": c.Conflicted,
		},
	}
	// domain.Conflict derives the paused-op state from the status just read
	// (no second status round-trip); the clean steady state costs zero git
	// invocations.
	if cs := svc.Conflict(r.Context(), st); cs.Op != "" {
		resp["conflict"] = conflictPayload{
			Op: cs.Op, Source: cs.Source, Target: cs.Target,
			Desc: cs.Describe(), Conflicted: c.Conflicted,
		}
	}
	writeJSON(w, resp)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && go test ./internal/web/ -run 'TestStatusConflict|TestStatus' -v`
Expected: PASS (including the pre-existing status tests — the response is additive).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && gofmt -l internal/web/ && git add internal/web/conflict.go internal/web/conflict_test.go internal/web/status.go && git commit -m "feat(web): /api/status reports the paused-op conflict state"
```
(append the two Global Constraints trailers to the message; gofmt must print nothing)

---

### Task 2: ops #22-23 — `op:"continue"` / `op:"abort"`

**Files:**
- Modify: `internal/web/ophttp.go` (the `switch req.Op` in `handleOpStart`, cases around line 105+; also the wired-ops doc comment above `handleOpStart`)
- Create: `internal/web/opconflict_test.go`

**Interfaces:**
- Consumes: `engine.ContinueOp{}` / `engine.AbortOp{}` (zero-arg ops; the engine probes which sequencer op is paused and errors with "no merge, rebase, cherry-pick, or revert in progress" when none is); test helpers `conflictingRepo`, `conflictedMergeState` (Task 1), `startOpBody`, `readSSE`.
- Produces: wire ops `{"op":"continue"}` and `{"op":"abort"}` on `POST /api/op` — the client banner (Task 5) sends exactly these.

- [ ] **Step 1: Write the failing tests**

`internal/web/opconflict_test.go`:

```go
package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestOpHTTPContinueMerge(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	// resolve by hand, stage — exactly the state Continue is for
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("resolved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "f.txt")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"continue"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if out := gitRun(t, dir, "ls-files", "-u"); out != "" {
		t.Errorf("unmerged entries survived continue:\n%s", out)
	}
	if log := gitRun(t, dir, "log", "--merges", "--oneline"); log == "" {
		t.Error("no merge commit after continue")
	}
}

func TestOpHTTPAbortMerge(t *testing.T) {
	dir := conflictingRepo(t)
	pre := gitRun(t, dir, "rev-parse", "HEAD")
	conflictedMergeState(t, dir)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"abort"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if out := gitRun(t, dir, "ls-files", "-u"); out != "" {
		t.Errorf("unmerged entries survived abort:\n%s", out)
	}
	if head := gitRun(t, dir, "rev-parse", "HEAD"); head != pre {
		t.Errorf("HEAD = %s, want pre-merge %s", head, pre)
	}
	if got := strings.TrimSpace(gitRun(t, dir, "show", "HEAD:f.txt")); got != "main" {
		t.Errorf("f.txt @HEAD = %q", got)
	}
}

// Nothing paused: the engine refuses; the wire reports ok=false, repo untouched.
func TestOpHTTPContinueNothingPaused(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"continue"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false", done)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && go test ./internal/web/ -run TestOpHTTPContinue -v`
Expected: FAIL — `startOpBody` gets a 400 "unknown op" (assertions inside the helper or on the first event).

- [ ] **Step 3: Implement**

In `handleOpStart`'s `switch req.Op` (place after the `"fetch"` case — the other zero-arg ops live there):

```go
	case "continue":
		// The engine probes which of merge/rebase/cherry-pick/revert is
		// paused and dispatches; nothing paused is its own clear refusal.
		op = engine.ContinueOp{}
	case "abort":
		op = engine.AbortOp{}
```

Also append `continue, abort` to the wired-ops list in the doc comment above `handleOpStart`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && go test ./internal/web/ -run 'TestOpHTTPContinue|TestOpHTTPAbort' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && gofmt -l internal/web/ && git add internal/web/ophttp.go internal/web/opconflict_test.go && git commit -m "feat(web): continue/abort ops for a paused sequencer operation"
```
(append the trailers)

---

### Task 3: `GET /api/conflict-hunks`

**Files:**
- Modify: `internal/web/conflict.go` (from Task 1)
- Modify: `internal/web/conflict_test.go`
- Modify: `internal/web/server.go` (route table, `GET` routes block around line 64-84)

**Interfaces:**
- Consumes: `svc.Status(ctx)` (eligibility), `svc.WorktreeFile(ctx, path) ([]byte, error)`, `hunkpick.ParseConflict(content) (*hunkpick.Doc, error)`, `textdiff.IsBinary([]byte) bool`, `model.KindUnmerged`, `isGitArgSafe`, `writeErr`, `writeJSON`.
- Produces: `loadConflictDoc(w, r, svc, path) (*hunkpick.Doc, string, bool)` — shared with Task 4; wire shape `{count, hash, items:[{kind:"text",lines:[…]} | {kind:"block",index,ours,theirs}]}`. NOTE for the client (Task 6): `ours`/`theirs`/`lines` carry `omitempty` — an empty side arrives as ABSENT, read with `|| []`; `index` deliberately has NO omitempty (block 0 must survive).

- [ ] **Step 1: Write the failing tests**

Append to `internal/web/conflict_test.go` (add `"strings"` to its imports):

```go
type conflictHunksResp struct {
	Count int    `json:"count"`
	Hash  string `json:"hash"`
	Items []struct {
		Kind   string   `json:"kind"`
		Lines  []string `json:"lines"`
		Index  int      `json:"index"`
		Ours   []string `json:"ours"`
		Theirs []string `json:"theirs"`
	} `json:"items"`
}

func TestConflictHunks(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	ts := serve(t, New(domain.Open(dir)))

	var d conflictHunksResp
	if code := getJSON(t, ts, "/api/conflict-hunks?path=f.txt", &d); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if d.Count != 1 || len(d.Hash) != 64 {
		t.Fatalf("count = %d hash = %q", d.Count, d.Hash)
	}
	var blocks int
	for _, it := range d.Items {
		if it.Kind != "block" {
			continue
		}
		blocks++
		if it.Index != 0 || strings.Join(it.Ours, "\n") != "main" || strings.Join(it.Theirs, "\n") != "feature" {
			t.Errorf("block = %+v, want ours=[main] theirs=[feature]", it)
		}
	}
	if blocks != 1 {
		t.Errorf("blocks = %d, want 1", blocks)
	}
}

func TestConflictHunksEligibility(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	ts := serve(t, New(domain.Open(dir)))

	if code := getJSON(t, ts, "/api/conflict-hunks?path=nope.txt", nil); code != http.StatusNotFound {
		t.Errorf("unknown path code = %d, want 404", code)
	}
	// a tracked-but-clean file is known yet not conflicted → 422
	cleanDir := newRepoDir(t, 1)
	gitRun(t, cleanDir, "checkout", "-b", "x") // any repo state; f.txt is clean
	tsClean := serve(t, New(domain.Open(cleanDir)))
	if code := getJSON(t, tsClean, "/api/conflict-hunks?path=f.txt", nil); code != http.StatusNotFound {
		t.Errorf("clean-repo code = %d, want 404 (clean file is not even in status)", code)
	}
}

// The user removed the markers by hand (or the file is binary): the picker
// has nothing to pick — typed 422, the client falls back to mark-resolved.
func TestConflictHunksMalformed(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hand-resolved, no markers\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	if code := getJSON(t, ts, "/api/conflict-hunks?path=f.txt", nil); code != http.StatusUnprocessableEntity {
		t.Errorf("code = %d, want 422", code)
	}
}
```

Note the eligibility nuance the second test pins: a CLEAN file never appears in `status.Files` at all, so it 404s as "unknown" — only a file that IS in the status but NOT unmerged (e.g. modified) takes the 422 branch. Both are correct refusals; the test documents the split.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && go test ./internal/web/ -run TestConflictHunks -v`
Expected: FAIL — 404 from the mux (route not registered).

- [ ] **Step 3: Implement**

Append to `internal/web/conflict.go` (imports grow to: `context`, `crypto/sha256`, `encoding/hex`, `errors`, `net/http`, plus `internal/domain`, `internal/hunkpick`, `internal/model`, `internal/textdiff`):

```go
// conflictItem is one run of the conflicted file in order: passthrough text
// (kind "text") or a decidable block (kind "block"). index has NO omitempty —
// block 0 must reach the client.
type conflictItem struct {
	Kind   string   `json:"kind"`
	Lines  []string `json:"lines,omitempty"`
	Index  int      `json:"index"`
	Ours   []string `json:"ours,omitempty"`
	Theirs []string `json:"theirs,omitempty"`
}

// loadConflictDoc resolves path's eligibility against a FRESH status (the
// discard precedent: unknown → 404, known-but-not-conflicted → 422), reads
// the working-tree bytes, and parses the conflict markers. The hash is the
// freshness token resolve-hunks must echo — picks are positional, valid only
// against the exact bytes the client saw.
func loadConflictDoc(w http.ResponseWriter, r *http.Request, svc *domain.Service, path string) (*hunkpick.Doc, string, bool) {
	ctx := r.Context()
	if !isGitArgSafe(path) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
		return nil, "", false
	}
	st, err := svc.Status(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return nil, "", false
	}
	known, unmerged := false, false
	for _, f := range st.Files {
		if f.Path == path {
			known, unmerged = true, f.Kind == model.KindUnmerged
			break
		}
	}
	if !known {
		writeErr(w, http.StatusNotFound, errors.New("unknown path"))
		return nil, "", false
	}
	if !unmerged {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("not conflicted"))
		return nil, "", false
	}
	work, err := svc.WorktreeFile(ctx, path)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return nil, "", false
	}
	if textdiff.IsBinary(work) {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("binary file — resolve in your editor, then mark resolved"))
		return nil, "", false
	}
	doc, perr := hunkpick.ParseConflict(work)
	if perr != nil {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("no usable conflict markers — resolve in your editor, then mark resolved"))
		return nil, "", false
	}
	sum := sha256.Sum256(work)
	return doc, hex.EncodeToString(sum[:]), true
}

// handleConflictHunks lists a conflicted file's pickable blocks with the
// passthrough text between them, plus the freshness hash a resolve POST must
// echo back.
func (s *Server) handleConflictHunks(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	doc, hash, ok := loadConflictDoc(w, r, svc, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	items := make([]conflictItem, 0, len(doc.Items))
	idx := 0
	for _, it := range doc.Items {
		if it.Block == nil {
			items = append(items, conflictItem{Kind: "text", Lines: it.Literal})
			continue
		}
		items = append(items, conflictItem{Kind: "block", Index: idx, Ours: it.Block.Current, Theirs: it.Block.Incoming})
		idx++
	}
	writeJSON(w, map[string]any{"count": idx, "hash": hash, "items": items})
}
```

Note `hunkpick.ParseConflict` returns an empty-blocks doc (not an error) for a file with NO markers at all — that would render a picker with nothing to pick. Guard it: after parsing, treat `len(doc.Blocks()) == 0` the same as a parse error (fold into the `perr != nil` branch condition):

```go
	doc, perr := hunkpick.ParseConflict(work)
	if perr != nil || len(doc.Blocks()) == 0 {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("no usable conflict markers — resolve in your editor, then mark resolved"))
		return nil, "", false
	}
```

Route in `internal/web/server.go`, next to the hunks pair:

```go
	mux.HandleFunc("GET /api/conflict-hunks", s.handleConflictHunks)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && go test ./internal/web/ -run TestConflictHunks -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && gofmt -l internal/web/ && git add internal/web/conflict.go internal/web/conflict_test.go internal/web/server.go && git commit -m "feat(web): GET /api/conflict-hunks — a conflicted file's pickable blocks"
```
(append the trailers)

---

### Task 4: `POST /api/resolve-hunks`

**Files:**
- Modify: `internal/web/conflict.go`
- Modify: `internal/web/conflict_test.go`
- Modify: `internal/web/server.go`

**Interfaces:**
- Consumes: `loadConflictDoc` (Task 3), `doc.Blocks() []*hunkpick.Block`, `hunkpick.TakeCurrent`/`TakeIncoming`, `doc.Resolved() ([]byte, bool)`, `runOp(ctx, svc, op)` (the drain-events runner used by `handleStageHunks`), `engine.ResolveConflictHunks{Path, Content}`, `s.writeStatus`.
- Produces: `POST /api/resolve-hunks {path, picks:["ours"|"theirs", …], hash}` — picks POSITIONAL, one per block, ALL required; 200 body = fresh `/api/status` payload (the stage-hunks convention, so the client applies it directly); 409 on hash drift.

- [ ] **Step 1: Write the failing tests**

Append to `internal/web/conflict_test.go`:

```go
func TestResolveHunks(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	ts := serve(t, New(domain.Open(dir)))

	var d conflictHunksResp
	if code := getJSON(t, ts, "/api/conflict-hunks?path=f.txt", &d); code != http.StatusOK {
		t.Fatalf("hunks code = %d", code)
	}
	var st conflictStatusResp
	code := postJSON(t, ts, "/api/resolve-hunks",
		`{"path":"f.txt","picks":["theirs"],"hash":"`+d.Hash+`"}`,
		"application/json", "", &st)
	if code != http.StatusOK {
		t.Fatalf("resolve code = %d", code)
	}
	// file resolved to the incoming side and staged
	b, err := os.ReadFile(filepath.Join(dir, "f.txt"))
	if err != nil || string(b) != "feature\n" {
		t.Errorf("f.txt = %q, %v; want feature", b, err)
	}
	if out := gitRun(t, dir, "ls-files", "-u"); out != "" {
		t.Errorf("still unmerged:\n%s", out)
	}
	// the merge is still paused with zero conflicts — Continue's moment
	if st.Conflict == nil || st.Conflict.Op != "merge" || st.Conflict.Conflicted != 0 {
		t.Errorf("response conflict = %+v, want paused merge with 0 conflicted", st.Conflict)
	}
}

// Two regions, opposite picks — the positional contract end-to-end. The
// working-tree content is ours to write (the index, not the file, is what
// keeps the path unmerged), so a synthetic two-block marker file works.
func TestResolveHunksMixedPicks(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	two := "keep\n<<<<<<< HEAD\nm1\n=======\nf1\n>>>>>>> feature\nmid\n<<<<<<< HEAD\nm2\n=======\nf2\n>>>>>>> feature\nend\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(two), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	var d conflictHunksResp
	if code := getJSON(t, ts, "/api/conflict-hunks?path=f.txt", &d); code != http.StatusOK || d.Count != 2 {
		t.Fatalf("hunks code = %d count = %d, want 200/2", code, d.Count)
	}
	if code := postJSON(t, ts, "/api/resolve-hunks",
		`{"path":"f.txt","picks":["ours","theirs"],"hash":"`+d.Hash+`"}`,
		"application/json", "", nil); code != http.StatusOK {
		t.Fatalf("resolve code = %d", code)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(b) != "keep\nm1\nmid\nf2\nend\n" {
		t.Errorf("f.txt = %q", b)
	}
}

func TestResolveHunksHashDrift(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	ts := serve(t, New(domain.Open(dir)))

	var d conflictHunksResp
	if code := getJSON(t, ts, "/api/conflict-hunks?path=f.txt", &d); code != http.StatusOK {
		t.Fatalf("hunks code = %d", code)
	}
	// the file moves under the client's feet
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("<<<<<<< HEAD\nx\n=======\ny\n>>>>>>> feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := postJSON(t, ts, "/api/resolve-hunks",
		`{"path":"f.txt","picks":["theirs"],"hash":"`+d.Hash+`"}`,
		"application/json", "", nil); code != http.StatusConflict {
		t.Errorf("code = %d, want 409", code)
	}
}

func TestResolveHunksPickCount(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	ts := serve(t, New(domain.Open(dir)))

	var d conflictHunksResp
	getJSON(t, ts, "/api/conflict-hunks?path=f.txt", &d)
	for _, bad := range []string{`[]`, `["theirs","ours"]`, `["sideways"]`} {
		if code := postJSON(t, ts, "/api/resolve-hunks",
			`{"path":"f.txt","picks":`+bad+`,"hash":"`+d.Hash+`"}`,
			"application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("picks %s: code = %d, want 400", bad, code)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && go test ./internal/web/ -run TestResolveHunks -v`
Expected: FAIL — 404 (route not registered).

- [ ] **Step 3: Implement**

Append to `internal/web/conflict.go` (imports also gain `encoding/json`, `fmt`, `internal/engine`):

```go
type resolveHunksRequest struct {
	Path  string   `json:"path"`
	Picks []string `json:"picks"` // positional: picks[i] resolves block i — "ours" | "theirs"
	Hash  string   `json:"hash"`
}

// handleResolveHunks resolves a conflicted file from a full set of per-block
// picks: recompute the doc fresh, verify the freshness hash (409 on drift),
// require EVERY block picked (a partial resolve would stage a file still
// containing markers), assemble Doc.Resolved(), and write+stage through
// engine.ResolveConflictHunks. Success response = fresh status (the
// stage-hunks convention).
func (s *Server) handleResolveHunks(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	var req resolveHunksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	doc, hash, ok := loadConflictDoc(w, r, svc, req.Path)
	if !ok {
		return
	}
	if req.Hash != hash {
		writeErr(w, http.StatusConflict, errors.New("file changed; refresh"))
		return
	}
	blocks := doc.Blocks()
	if len(req.Picks) != len(blocks) {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("picks: got %d, want %d (every block must be picked)", len(req.Picks), len(blocks)))
		return
	}
	for i, p := range req.Picks {
		switch p {
		case "ours":
			blocks[i].Mode = hunkpick.TakeCurrent
		case "theirs":
			blocks[i].Mode = hunkpick.TakeIncoming
		default:
			writeErr(w, http.StatusBadRequest, fmt.Errorf("pick %d: %q (want ours|theirs)", i, p))
			return
		}
	}
	content, resolved := doc.Resolved()
	if !resolved {
		writeErr(w, http.StatusInternalServerError, errors.New("unresolved blocks"))
		return
	}
	if _, err := runOp(r.Context(), svc, engine.ResolveConflictHunks{Path: req.Path, Content: content}); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.writeStatus(w, r)
}
```

Route in `server.go`, beside `stage-hunks`:

```go
	mux.HandleFunc("POST /api/resolve-hunks", writeGuard(s.handleResolveHunks))
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && go test ./internal/web/ -v`
Expected: PASS — the whole package, not just the new tests (the status shape changed in Task 1; make sure nothing else in the package asserted on the old map).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && gofmt -l internal/web/ && git add internal/web/conflict.go internal/web/conflict_test.go internal/web/server.go && git commit -m "feat(web): POST /api/resolve-hunks — per-block ours/theirs conflict resolution"
```
(append the trailers)

---

### Task 5: client — conflict banner + continue/abort + mark-resolved

**Files:**
- Modify: `internal/web/static/index.html` (after `</header>`, before `<main id="panes"…>`)
- Modify: `internal/web/static/app.js` (`applyStatus` ~line 121; the boot-time listener block; the `#files-list` contextmenu handler ~line 2815; the `DANGER_OPTIONS` set)
- Modify: `internal/web/static/style.css`

**Interfaces:**
- Consumes: `status.conflict` (Task 1 shape), `startOp(body, label)`, `opBusy()`, `showLocalConfirm(prompt, options, cb)`, `DANGER_OPTIONS`, `stage({paths})`, `$()`, `esc()`.
- Produces: `state.conflict` (the raw payload or `null`) and `renderConflictBar()` — Task 6's picker relies on `state.conflict` being maintained by every `applyStatus`.

- [ ] **Step 1: Markup + styles**

`index.html`, directly after `</header>`:

```html
<div id="conflict-bar" class="hidden">
  <span id="conflict-msg"></span>
  <button id="conflict-continue">continue</button>
  <button id="conflict-abort" class="danger">abort</button>
</div>
```

`style.css` (match the existing button/bar idiom — reuse the ctx-menu `.danger` red if a shared class exists; otherwise scope it):

```css
#conflict-bar {
  display: flex; gap: 10px; align-items: center;
  padding: 4px 12px;
  background: #38222261; border-bottom: 1px solid #6b3a3a;
  font-size: 13px;
}
#conflict-bar.hidden { display: none; }
#conflict-bar #conflict-abort { color: #e06c75; border-color: #6b3a3a; }
```

- [ ] **Step 2: Wire state + render**

In `app.js`, extend `applyStatus` and add the renderer next to it:

```js
function applyStatus(st) {
  state.wt = st.files && st.files.length ? st : null;
  state.conflict = st.conflict || null;
  buildStatusEntries();
  renderConflictBar();
}

// The banner shows whenever a sequencer op is paused — including with zero
// conflicted files (resolved by hand, never continued): that is exactly when
// Continue lights up. Never leave the user in a paused op with no way out.
function renderConflictBar() {
  const bar = $("conflict-bar"), c = state.conflict;
  if (!c) { bar.classList.add("hidden"); return; }
  bar.classList.remove("hidden");
  $("conflict-msg").textContent =
    "⏸ " + c.op + " paused" + (c.desc ? " (" + c.desc + ")" : "") +
    (c.conflicted ? " — " + c.conflicted + " conflicted" : " — all conflicts resolved");
  $("conflict-continue").disabled = !!c.conflicted;
}
```

Add `conflict: null` to the initial `state` object literal (find it near the top of app.js; keep the existing key style).

- [ ] **Step 3: Buttons**

In the boot-time listener block (near the other `$("…").addEventListener("click", …)` wiring):

```js
$("conflict-continue").addEventListener("click", () => {
  if (opBusy() || !state.conflict) return;
  startOp({ op: "continue" }, "continue " + state.conflict.op);
});
$("conflict-abort").addEventListener("click", () => {
  if (opBusy() || !state.conflict) return;
  const op = state.conflict.op;
  showLocalConfirm(
    "Abort the paused " + op + "? Conflict resolutions so far are discarded.",
    ["abort " + op, "cancel"],
    (o) => { if (o !== "cancel") startOp({ op: "abort" }, "abort " + op); }
  );
});
```

Add the four action strings to `DANGER_OPTIONS` so the confirm's destructive option renders red:

```js
"abort merge", "abort rebase", "abort cherry-pick", "abort revert",
```

(Check the set's existing literal style and match it. The engine decision option named plain `"abort"` must NOT be added — in engine decisions "abort" is the SAFE option.)

- [ ] **Step 4: Mark-resolved in the file ctx-menu**

In the `#files-list` `contextmenu` handler, the stage/unstage branch currently skips conflicts (`else if (f.section !== "conflicts")`). Add the conflicts branch:

```js
  if (f.section === "staged") items.push({ label: "unstage " + f.path, act: () => stage({ paths: [f.path], unstage: true }) });
  else if (f.section === "conflicts") items.push({ label: "mark resolved (stage as-is)", act: () => stage({ paths: [f.path] }) });
  else items.push({ label: "stage " + f.path, act: () => stage({ paths: [f.path] }) });
```

(`stage()` already applies the fresh-status response, which re-renders the banner via `applyStatus` — no extra wiring.)

- [ ] **Step 5: Manual smoke check**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && go build ./cmd/gg && go vet ./internal/web/
```
Expected: clean build. (Full browser verification is Task 7 — a JS typo would only surface there, so eyeball the diff once: every new `$("id")` matches an id added in Step 1.)

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && git add internal/web/static/ && git commit -m "feat(web): paused-op banner with continue/abort + mark-resolved row"
```
(append the trailers)

---

### Task 6: client — the conflict block picker

**Files:**
- Modify: `internal/web/static/index.html` (diff header, next to `#hunk-bar` ~line 54)
- Modify: `internal/web/static/app.js` (`openStatusDiff` conflicts branch ~line 2324; `clearDiffHunks` ~line 2546; `reconcileStatusView` ~line 455; new picker block after the hunk-staging block ~line 2612)
- Modify: `internal/web/static/style.css`

**Interfaces:**
- Consumes: `GET /api/conflict-hunks` + `POST /api/resolve-hunks` (Tasks 3-4 wire shapes — remember `ours`/`theirs` may be ABSENT for an empty side: always `(it.ours || [])`), `applyStatus`, `reconcileStatusView`, `renderFiles`, `openStatusDiff`, `updateDiffNav`, `getJSON`, `postJSON`, `opLine`, `esc`, `$`.
- Produces: module-level `conflictPick` (`{path, hash, count, choices: Array<null|"ours"|"theirs">}` or `null`), functions `renderResolveBar()`, `openConflictPicker(f)`.

- [ ] **Step 1: Markup + styles**

`index.html`, inside the diff header `<span>` group, directly after the `#hunk-bar` span:

```html
<span id="resolve-bar" class="hidden"><span id="resolve-count"></span><button id="resolve-ours">all ours</button><button id="resolve-theirs">all theirs</button><button id="resolve-go" disabled>resolve</button></span>
```

`style.css`:

```css
#resolve-bar.hidden { display: none; }
#cf-doc { padding: 8px 12px; font-family: var(--mono, monospace); font-size: 12px; }
#cf-doc pre { margin: 0; white-space: pre-wrap; word-break: break-all; }
.cf-text { color: #9aa3b2; padding: 2px 0; }
.cf-block { border: 1px solid #3a4150; border-radius: 4px; margin: 6px 0; overflow: hidden; }
.cf-side { padding: 4px 8px; cursor: pointer; border-left: 3px solid transparent; }
.cf-side .cf-tag { font-size: 10px; text-transform: uppercase; letter-spacing: .08em; color: #9aa3b2; }
.cf-ours { background: #1d2a1d55; }
.cf-theirs { background: #1d2432aa; border-top: 1px dashed #3a4150; }
.cf-side.picked { border-left-color: #61afef; background: #2a3548; }
.cf-block.decided .cf-side:not(.picked) { opacity: .45; }
```

(Adjust exact colors to sit well with the existing dark palette; the structural selectors are the contract.)

- [ ] **Step 2: Route the conflicted row into the picker**

In `openStatusDiff`, replace the dead-end branch:

```js
  if (f.section === "conflicts") return openConflictPicker(f);
```

(The removed lines are the `"conflicted — resolve in the TUI"` notice + `updateDiffNav(); return;`.)

- [ ] **Step 3: The picker block**

Add after the inline-hunk-staging block (~line 2612), same comment style:

```js
// ---- conflict block picker (conflict surface) -----------------------------
// A conflicted row opens the file's marker regions as pickable ours/theirs
// blocks (GET /api/conflict-hunks). Picks are POSITIONAL against the exact
// bytes the server hashed; resolving POSTs the full pick set and the server
// writes + stages via engine.ResolveConflictHunks. A 409 means the file
// moved: reload the picker (the stage-hunks rule).

let conflictPick = null; // {path, hash, count, choices: Array<null|"ours"|"theirs">} — set only while the picker is open

async function openConflictPicker(f) {
  clearDiffHunks(); // also nulls conflictPick — order matters, set it after
  $("diff-title").textContent = f.path + " — resolve";
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  updateDiffNav();
  let d;
  try {
    d = await getJSON("/api/conflict-hunks?" + new URLSearchParams({ path: f.path }));
  } catch (e) {
    // typed 422 refusal (binary / markers gone): show the reason + the way out
    $("diff-body").innerHTML =
      `<div class="notice">${esc(e.message || e)}</div>` +
      `<div class="notice">right-click the file → mark resolved when it is done</div>`;
    return;
  }
  conflictPick = { path: f.path, hash: d.hash, count: d.count, choices: new Array(d.count).fill(null) };
  let html = '<div id="cf-doc">';
  for (const it of d.items) {
    if (it.kind === "text") {
      html += `<pre class="cf-text">${esc((it.lines || []).join("\n"))}</pre>`;
    } else {
      html += `<div class="cf-block" data-b="${it.index}">` +
        `<div class="cf-side cf-ours" data-side="ours"><div class="cf-tag">ours</div><pre>${esc((it.ours || []).join("\n"))}</pre></div>` +
        `<div class="cf-side cf-theirs" data-side="theirs"><div class="cf-tag">theirs</div><pre>${esc((it.theirs || []).join("\n"))}</pre></div>` +
        `</div>`;
    }
  }
  $("diff-body").innerHTML = html + "</div>";
  renderResolveBar();
}

function paintConflictPicks() {
  document.querySelectorAll("#cf-doc .cf-block").forEach((el) => {
    const choice = conflictPick && conflictPick.choices[Number(el.dataset.b)];
    el.classList.toggle("decided", !!choice);
    el.querySelectorAll(".cf-side").forEach((s) =>
      s.classList.toggle("picked", !!choice && s.dataset.side === choice));
  });
  renderResolveBar();
}

function renderResolveBar() {
  const bar = $("resolve-bar");
  if (!conflictPick) { bar.classList.add("hidden"); return; }
  bar.classList.remove("hidden");
  const n = conflictPick.choices.filter(Boolean).length;
  $("resolve-count").textContent = n + "/" + conflictPick.count + " picked";
  $("resolve-go").disabled = n !== conflictPick.count;
}

function setAllConflictPicks(side) {
  if (!conflictPick) return;
  conflictPick.choices.fill(side);
  paintConflictPicks();
}

async function resolveConflictPicked() {
  const v = conflictPick;
  if (!v || v.choices.some((c) => !c)) return;
  let resp;
  try {
    resp = await postJSON("/api/resolve-hunks", { path: v.path, picks: v.choices, hash: v.hash });
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    // 409 = the file moved under the picker: reload it for fresh blocks
    if (/file changed/.test(e.message || "")) {
      const i = state.statusEntries.findIndex((f) => f.path === v.path && f.section === "conflicts");
      if (i >= 0) { state.fileCursor = i; openStatusDiff(i); }
    }
    return;
  }
  const path = v.path;
  conflictPick = null;
  applyStatus(resp); // the 200 body IS a fresh /api/status payload
  reconcileStatusView();
  renderFiles();
  stepToNextConflict(path);
}

// After a resolve: the same path if somehow still conflicted, else the next
// conflicted file, else whatever the cursor lands on (the resolved file
// moved to Staged).
function stepToNextConflict(path) {
  let i = state.statusEntries.findIndex((f) => f.path === path && f.section === "conflicts");
  if (i < 0) i = state.statusEntries.findIndex((f) => f.section === "conflicts");
  if (i >= 0) { state.fileCursor = i; renderFiles(); openStatusDiff(i); return; }
  const f = state.statusEntries[state.fileCursor];
  if (state.filesMode === "status" && f) openStatusDiff(state.fileCursor);
  else { $("diff-title").textContent = ""; $("diff-body").innerHTML = ""; updateDiffNav(); }
}
```

- [ ] **Step 4: Event wiring + lifecycle**

Boot-time listeners (with the others):

```js
$("resolve-ours").addEventListener("click", () => setAllConflictPicks("ours"));
$("resolve-theirs").addEventListener("click", () => setAllConflictPicks("theirs"));
$("resolve-go").addEventListener("click", resolveConflictPicked);
$("diff-body").addEventListener("click", (e) => {
  if (!conflictPick) return;
  if (!getSelection().isCollapsed) return; // selecting text is not a pick
  const side = e.target.closest(".cf-side");
  if (!side) return;
  conflictPick.choices[Number(side.closest(".cf-block").dataset.b)] = side.dataset.side;
  paintConflictPicks();
});
```

Lifecycle — the picker dies whenever another view opens or the file stops being conflicted, exactly like `diffHunks`:

```js
function clearDiffHunks() {
  diffHunks = null;
  conflictPick = null;
  renderHunkBar();
  renderResolveBar();
}
```

And in `reconcileStatusView`, beside the existing `diffHunks` invalidation line:

```js
  if (conflictPick && !state.statusEntries.some((f) => f.path === conflictPick.path && f.section === "conflicts")) {
    conflictPick = null;
    renderResolveBar();
  }
```

- [ ] **Step 5: Build + commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && go build ./cmd/gg && git add internal/web/static/ && git commit -m "feat(web): in-browser conflict block picker (ours/theirs per region)"
```
(append the trailers)

---

### Task 7: headless browser verification

**Files:**
- Create: `/home/homeend/.claude/jobs/62f303cb/tmp/verify-conflict.mjs` (scratch — not committed)
- Fixture: scratch repo under `/home/homeend/.claude/jobs/62f303cb/tmp/`

**Interfaces:**
- Consumes: the raw-CDP harness pattern (see `/home/homeend/.claude/jobs/62f303cb/tmp/verify-sep.mjs` for the WebSocket/evaluate/check scaffolding); chromium at `~/.cache/ms-playwright/chromium-1234/chrome-linux64/chrome --headless=new --remote-debugging-port=9223`; `gg web --addr 127.0.0.1:8899` serving the scratch repo (cwd = the repo; there is no `--repo` flag).
- Produces: PASS/FAIL console output; screenshots on demand.

- [ ] **Step 1: Build gg and a conflicted scratch repo**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && go build -o /home/homeend/.claude/jobs/62f303cb/tmp/gg ./cmd/gg
cd /home/homeend/.claude/jobs/62f303cb/tmp && rm -rf cfrepo && mkdir cfrepo && cd cfrepo && \
  git init -b main && git config user.email t@t && git config user.name t && \
  printf 'a\nb\nc\n' > f.txt && git add -A && git commit -m base && \
  git checkout -b feature && printf 'a\nFEATURE\nc\n' > f.txt && git commit -am feat && \
  git checkout main && printf 'a\nMAIN\nc\n' > f.txt && git commit -am main && \
  (git merge feature || true) && git ls-files -u
```
Expected: unmerged entries listed.

- [ ] **Step 2: Run the dead-row check against the UNFIXED build first**

Serve the scratch repo with a `web-dev`-built gg (the pre-feature build), assert the check FAILS (the "resolve in the TUI" notice is present, no `#conflict-bar`): this proves the check can detect the old behavior. Run it twice (the browser-check feedback rule). Then kill that server.

- [ ] **Step 3: The check script**

`verify-conflict.mjs` asserts, in order (each visibility check via `document.elementFromPoint` at the element's own center hitting the element or a descendant — hidden-class checks alone are banned):
1. App loads; `#conflict-bar` VISIBLE; its text contains "merge paused" and "1 conflicted"; `#conflict-continue` disabled.
2. Open working tree → click the conflicted `f.txt` row → `#cf-doc` visible with exactly 1 `.cf-block`; the old notice text "resolve in the TUI" appears NOWHERE in the DOM.
3. Click the theirs side → `.picked` present on it; `#resolve-go` enabled; `#resolve-count` reads "1/1 picked".
4. Click `#resolve-go` → wait until the Conflicts section is empty; banner now reads "all conflicts resolved"; `#conflict-continue` enabled.
5. Click `#conflict-continue` → wait for the op line; assert via a follow-up `/api/status` fetch (in-page) that `conflict` is absent, and `f.txt` content (via `git show HEAD:f.txt` run OUTSIDE the browser after the script) contains "FEATURE".
6. Abort path (fresh repo state re-made by re-running the fixture): open app, click `#conflict-abort` → the confirm modal's "abort merge" option renders with the danger styling → choose it → banner disappears; outside the browser `git ls-files -u` is empty and `f.txt` is back to MAIN.

- [ ] **Step 4: Run against the fixed build, fix, repeat**

Run: `node /home/homeend/.claude/jobs/62f303cb/tmp/verify-conflict.mjs`
Expected: ALL CHECKS PASSED (twice in a row).

- [ ] **Step 5: Commit any client fixes the check forced**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && git add internal/web/static/ && git commit -m "fix(web): conflict-surface fixes from headless verification"
```
(only if fixes were needed; append the trailers)

---

### Task 8: docs + race gate + merge to web-dev

**Files:**
- Modify: `CHANGELOG.md`, `CLAUDE.md` (the `web` package-map row), `README.md` (web feature list)
- Modify (memory): `/home/homeend/.claude/projects/-mnt-t-others-gigagit/memory/web-ui-next-direction.md`

- [ ] **Step 1: Docs**

CHANGELOG bullet (top, matching existing web bullets' voice): the web conflict surface — paused-op banner with continue/abort, in-browser per-block ours/theirs conflict resolution, mark-resolved; the "resolve in the TUI" dead-end is gone. Extend the CLAUDE.md `web` row with: ops #22-23 (`continue`/`abort` — zero-arg, engine dispatches to the paused sequencer op), the `conflict` object on `/api/status` (from `domain.Conflict` on the same status read), and the `/api/conflict-hunks` + `/api/resolve-hunks` pair (loadConflictDoc's fresh-status eligibility 404/422, sha256 freshness hash, ALL blocks must be picked — a partial resolve would stage a marker-bearing file). README: add the conflict surface to the web frontend's feature list. Commit docs on the feature branch.

- [ ] **Step 2: Race gate (quiet machine, detached)**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-web-conflict-surface && nohup ./test.sh race > /tmp/race-conflict.log 2>&1 & echo $!
```
Poll the pid until it exits; then check the log tail for the pass line. A 10-minute package timeout names an innocent test — rerun on a quiet machine rather than chasing it.

- [ ] **Step 3: Merge to web-dev (NOT main — the user owns main graduations)**

```bash
cd /mnt/t/others/gigagit.worktrees/web-dev && git merge --no-ff feat/web-conflict-surface -m "Merge feat/web-conflict-surface: web conflict surface — paused-op banner + block picker"
```
(body: 2-4 sentence summary; append the trailers; stage only what the merge itself requires — NEVER `git add -A`). Then delete the feature branch after verifying `git merge-base --is-ancestor` against web-dev, remove its worktree, and rebuild the Windows exe:

```bash
cd /mnt/t/others/gigagit.worktrees/web-dev && ./build.sh web
```

- [ ] **Step 4: Memory update**

Append the feature outcome + any new gotchas to the `web-ui-next-direction` memory (and a new topic file if a real gotcha emerged). Do not push anything; report the merge commit to the user.

---

## Self-review notes

- Spec coverage: status conflict object (T1), continue/abort ops + banner gating (T2/T5), conflict-hunks GET with 404/422/hash (T3), resolve POST with 409/full-coverage (T4), picker UI + take-all + refusal fallback (T6), mark-resolved ctx row (T5), browser checks incl. unfixed-build run (T7), docs (T8). Paused-zero-conflicts covered in T1 test + T5 render.
- The `ours`/`theirs` omitempty trap is called out in both the T3 Produces block and T6 code (`(it.ours || [])`).
- `clearDiffHunks` clears `conflictPick` and `openConflictPicker` calls it BEFORE setting `conflictPick` — ordering stated in the code comment.
- `Index` json tag has no omitempty (block 0), stated twice.
