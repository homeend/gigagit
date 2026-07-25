# Web Server Re-root Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `POST /api/reroot` re-points the running `gg web` server at another worktree of the current repo or an MRU-registered repo, resetting repo-scoped state; no client UI.

**Architecture:** Task 1 makes the domain service swappable (`atomic.Pointer` behind a `service()` accessor — today `s.svc` is read unguarded everywhere). Task 2 adds the endpoint: allowlist resolution (own worktrees + `internal/repos` MRU), preflight-before-swap, feed/op-record reset, tests, docs.

**Tech Stack:** Go 1.26 stdlib, `internal/repos` (archtest-verified importable from web), real-git test fixtures.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-25-web-reroot-design.md` (this worktree).
- The client string is an IDENTIFIER resolved by allowlist (exact match against `Worktrees()` paths or `repos.Load()` entry paths) — it must never reach `domain.Open`/git argv unmatched. Never sanitize it.
- Preflight the candidate BEFORE swapping; on preflight failure the current service keeps serving untouched (409).
- The swap and the live-op refusal happen under `s.opMu` in one critical section, so no op can start mid-swap (`startOp` also holds `opMu`).
- Every handler reads the service ONCE per request (`svc := s.service()` first line) — no mid-request tearing, no caching on the Server.
- Working dir: `/mnt/t/others/gigagit.worktrees/feat-web-reroot`, branch `feat/web-reroot`. Verify with `git branch --show-current` before any edit.
- Test helpers to reuse (never re-implement): `newRepoDir`, `gitRun`, `serve`, `getJSON`, `postJSON(t, ts, path, body, contentType, origin, out)`, `startOpBody`, `readSSE`, `waitDecision`, `headBranchOf`, `twoBranchRepo` (oprun_test.go:72), `addWorktree(t, dir, branch) string` (opremoveworktree_test.go:17 — creates a branch + worktree in t.TempDir and returns its path).
- `repos` API facts: `repos.Load(statePath) []Entry` (Entry has `.Path`; entries whose path fails `os.Stat` are pruned from the result), `repos.Touch(statePath, repoPath, now) error`, `repos.DefaultStatePath() string`.

---

### Task 1: Swappable service pointer

**Files:**
- Modify: `internal/web/server.go` (struct, New, accessor, handleRepo)
- Modify: `internal/web/branches.go`, `commits.go`, `diff.go`, `ophttp.go`, `oprun.go`, `stage.go`, `stashes.go`, `status.go`, `tags.go`, `worktrees.go` (mechanical sweep)

**Interfaces:**
- Produces: `func (s *Server) service() *domain.Service` — the only way to reach the service; Task 2's handler and swap rely on it and on the field being `atomic.Pointer[domain.Service]` named `svc`.

- [ ] **Step 1: Change the field and constructor**

In `internal/web/server.go`, replace:

```go
// Server serves the probe's JSON API and static assets for one repository.
type Server struct {
	svc *domain.Service
```

with:

```go
// Server serves the probe's JSON API and static assets for one repository.
type Server struct {
	// svc is swappable at runtime (POST /api/reroot) — handlers read it
	// once per request via service(), never cache it.
	svc atomic.Pointer[domain.Service]
```

and replace:

```go
func New(svc *domain.Service) *Server { return &Server{svc: svc} }
```

with:

```go
func New(svc *domain.Service) *Server {
	s := &Server{}
	s.svc.Store(svc)
	return s
}

// service returns the current domain service. Read it once at the top of a
// handler and use the local for the whole request.
func (s *Server) service() *domain.Service { return s.svc.Load() }
```

Add `"sync/atomic"` to server.go's imports.

- [ ] **Step 2: Sweep every `s.svc` read**

Rule, applied per function: insert `svc := s.service()` as the first statement of the function, and replace every `s.svc.` in that function with `svc.`. The functions (from `grep -rn "s\.svc" internal/web/*.go | grep -v _test`):

| File | Function(s) |
|---|---|
| `server.go` | `handleRepo` (lines 65, 70) |
| `branches.go` | `handleBranches` (16) |
| `commits.go` | `feedFor` (39, 41), `handleCommitFiles` (119) |
| `diff.go` | the commit-diff handler (48–53) and the working-tree diff handler (111–127) |
| `ophttp.go` | `handleOpStart` (52, 90, 121 — one local covers all three cases) |
| `oprun.go` | `runOpStream` (104) |
| `stage.go` | `handleStage` (43) |
| `stashes.go` | `handleStashes` (17, 25, 33) |
| `status.go` | `handleStatus` (26) |
| `tags.go` | `handleTags` (14/26 area) |
| `worktrees.go` | `handleWorktrees` (14) |

Worked example (`branches.go`):

```go
func (s *Server) handleBranches(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	bs, err := svc.Branches(r.Context())
```

Worked example (`oprun.go` `runOpStream` — the op deliberately keeps the service it started with):

```go
func (s *Server) runOpStream(ctx context.Context, run *opRun, op engine.Operation) {
	svc := s.service() // pinned: the op runs against the repo it started on
	events := make(chan engine.Event, 32)
	...
	res, err := svc.Execute(ctx, op, events, webDecider{run: run, timeout: timeout})
```

After the sweep: `grep -rn "s\.svc\." internal/web/*.go | grep -v _test` must return nothing.

- [ ] **Step 3: Gate**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-reroot && go build ./... && go test ./internal/web/ -count=1 && go vet ./internal/web/ && gofmt -l internal/web/`
Expected: build OK, all existing web tests PASS unchanged, vet clean, gofmt silent.

- [ ] **Step 4: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-web-reroot
git add internal/web/
git commit -m "refactor(web): service behind atomic.Pointer accessor (re-root prep)"
```

---

### Task 2: POST /api/reroot + tests + docs

**Files:**
- Create: `internal/web/reroot.go`
- Modify: `internal/web/server.go` (route, `writeRepoInfo` extraction, `reposPath` seam)
- Test: `internal/web/reroot_test.go` (new)
- Modify: `CHANGELOG.md`, `CLAUDE.md`

**Interfaces:**
- Consumes: `s.service()` and the `atomic.Pointer` field from Task 1; the existing `preflight(ctx, svc, workdir)` (serve.go — already package-internal, callable as-is).
- Produces: `POST /api/reroot` `{"path": string}` → 200 with the `/api/repo` payload of the new root; 404 unknown target; 409 op-busy or preflight failure.

- [ ] **Step 1: Write the failing tests**

Create `internal/web/reroot_test.go`:

```go
package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/repos"
)

func rerootBody(path string) string {
	return fmt.Sprintf(`{"path":%q}`, path)
}

type repoResp struct {
	Name     string `json:"name"`
	Worktree string `json:"worktree"`
	Branch   string `json:"branch"`
}

func TestRerootToWorktree(t *testing.T) {
	dir := newRepoDir(t, 2)
	wt := addWorktree(t, dir, "side")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	if hb := headBranchOf(t, ts); hb != "main" { // warms the feed cache
		t.Fatalf("head before reroot = %q", hb)
	}
	var out repoResp
	if code := postJSON(t, ts, "/api/reroot", rerootBody(wt), "application/json", "", &out); code != http.StatusOK {
		t.Fatalf("reroot code = %d", code)
	}
	if out.Worktree != wt || out.Branch != "side" {
		t.Fatalf("reroot resp = %+v", out)
	}
	var repo repoResp
	getJSON(t, ts, "/api/repo", &repo)
	if repo.Worktree != wt {
		t.Errorf("/api/repo worktree = %q, want %q", repo.Worktree, wt)
	}
	// feed rebuilt against the new root: HEAD decoration moved to side
	if hb := headBranchOf(t, ts); hb != "side" {
		t.Errorf("head after reroot = %q, want side (feed not reset?)", hb)
	}
}

func TestRerootToMRURepo(t *testing.T) {
	dir := newRepoDir(t, 1)
	other := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(srv.reposPath, other, time.Now()); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, srv)

	var out repoResp
	if code := postJSON(t, ts, "/api/reroot", rerootBody(other), "application/json", "", &out); code != http.StatusOK {
		t.Fatalf("reroot code = %d", code)
	}
	if out.Worktree != other {
		t.Errorf("worktree = %q, want %q", out.Worktree, other)
	}
}

func TestRerootUnknownTarget(t *testing.T) {
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	if code := postJSON(t, ts, "/api/reroot", rerootBody(filepath.Join(t.TempDir(), "nope")), "application/json", "", nil); code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", code)
	}
	var repo repoResp
	getJSON(t, ts, "/api/repo", &repo)
	if repo.Worktree != dir {
		t.Errorf("old root gone: %q", repo.Worktree)
	}
}

func TestRerootRefusedWhileOpLive(t *testing.T) {
	dir := newRepoDir(t, 2)
	wt := addWorktree(t, dir, "side")
	gitRun(t, dir, "branch", "feature")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, `{"op":"delete-branch","branch":"feature"}`)
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/reroot", rerootBody(wt), "application/json", "", nil); code != http.StatusConflict {
		t.Fatalf("reroot during op = %d, want 409", code)
	}
	postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"abort"}`, "application/json", "", nil)
	readSSE(t, ts, opID, 30*time.Second)
	if code := postJSON(t, ts, "/api/reroot", rerootBody(wt), "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("reroot after op = %d, want 200", code)
	}
}

func TestRerootBrokenTargetKeepsServing(t *testing.T) {
	dir := newRepoDir(t, 1)
	notARepo := t.TempDir() // exists (survives repos.Load pruning) but is no repository
	srv := New(domain.Open(dir))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(srv.reposPath, notARepo, time.Now()); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, srv)

	if code := postJSON(t, ts, "/api/reroot", rerootBody(notARepo), "application/json", "", nil); code != http.StatusConflict {
		t.Fatalf("broken target code = %d, want 409", code)
	}
	var repo repoResp
	getJSON(t, ts, "/api/repo", &repo)
	if repo.Worktree != dir {
		t.Errorf("old root gone after failed reroot: %q", repo.Worktree)
	}
}

func TestRerootDropsOldOpRecord(t *testing.T) {
	dir := newRepoDir(t, 2)
	wt := addWorktree(t, dir, "side")
	ts := serve(t, New(domain.Open(dir)))

	opID := startOpBody(t, ts, `{"op":"stash","message":"x"}`) // fails fast (nothing to stash) — a finished run
	readSSE(t, ts, opID, 30*time.Second)
	if code := postJSON(t, ts, "/api/reroot", rerootBody(wt), "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("reroot code = %d", code)
	}
	resp, err := http.Get(ts.URL + "/api/op/" + opID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("old op events after reroot = %d, want 404", resp.StatusCode)
	}
}

func TestRerootWriteGuard(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/reroot", rerootBody(dir), "text/plain", "", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("non-JSON = %d, want 415", code)
	}
	if code := postJSON(t, ts, "/api/reroot", rerootBody(dir), "application/json", "http://evil.example", nil); code != http.StatusForbidden {
		t.Errorf("cross-origin = %d, want 403", code)
	}
}
```

Note: `newRepoDir` paths may traverse symlinks (macOS /tmp); if `repo.Worktree` comparisons fail on symlink expansion, compare with `filepath.EvalSymlinks` on both sides — mention it in your report if you needed this.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-reroot && go test ./internal/web/ -run 'TestReroot' -count=1`
Expected: FAIL — compile error first (`srv.reposPath` undefined); after adding the field stub, 404s from the missing route.

- [ ] **Step 3: Implement**

In `internal/web/server.go`, add to the `Server` struct after `decideTimeout time.Duration ...`:

```go
	// reposPath overrides the MRU registry location (test seam); empty =
	// repos.DefaultStatePath().
	reposPath string
```

Add the route to `Handler()` after the decide route:

```go
	mux.HandleFunc("POST /api/reroot", writeGuard(s.handleReroot))
```

Replace `handleRepo`'s body with a shared helper so `/api/repo` and the reroot response cannot drift:

```go
func (s *Server) handleRepo(w http.ResponseWriter, r *http.Request) {
	writeRepoInfo(w, r, s.service())
}

// writeRepoInfo writes the repo-identity payload for svc — shared by GET
// /api/repo and the POST /api/reroot success response.
func writeRepoInfo(w http.ResponseWriter, r *http.Request, svc *domain.Service) {
	top, err := svc.TopLevel(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	branch, err := svc.CurrentBranch(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{
		"name":     filepath.Base(top),
		"worktree": top,
		"branch":   branch,
	})
}
```

Create `internal/web/reroot.go`:

```go
package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/repos"
)

type rerootRequest struct {
	Path string `json:"path"`
}

func (s *Server) reposStatePath() string {
	if s.reposPath != "" {
		return s.reposPath
	}
	return repos.DefaultStatePath()
}

// handleReroot points the running server at a different worktree of the
// current repo, or a previously-opened repo from the MRU registry. The
// client string is an identifier resolved by allowlist — only server-owned
// values ever reach domain.Open.
func (s *Server) handleReroot(w http.ResponseWriter, r *http.Request) {
	var req rerootRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	target := ""
	if wts, err := s.service().Worktrees(r.Context()); err == nil {
		for _, wt := range wts {
			if wt.Path == req.Path {
				target = wt.Path
				break
			}
		}
	}
	if target == "" {
		for _, e := range repos.Load(s.reposStatePath()) {
			if e.Path == req.Path {
				target = e.Path
				break
			}
		}
	}
	if target == "" {
		writeErr(w, http.StatusNotFound, errors.New("unknown target"))
		return
	}
	// Preflight BEFORE swapping: a broken target must never take down a
	// working server (the startup preflight, reused — same friendly
	// cross-environment error).
	cand := domain.Open(target)
	if err := preflight(r.Context(), cand, target); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	// Swap under opMu in one critical section with the live-op check:
	// startOp holds opMu too, so no op can begin mid-swap. The finished op
	// record is dropped — a late SSE read of the previous repo's op 404s,
	// which is correct (that op belongs to the old root).
	s.opMu.Lock()
	if s.cur != nil {
		s.cur.mu.Lock()
		live := !s.cur.done
		s.cur.mu.Unlock()
		if live {
			s.opMu.Unlock()
			writeErr(w, http.StatusConflict, errOpBusy)
			return
		}
	}
	s.svc.Store(cand)
	s.cur = nil
	s.opMu.Unlock()
	s.mu.Lock()
	s.feed = nil
	s.mu.Unlock()
	writeRepoInfo(w, r, cand)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-reroot && go test ./internal/web/ -run 'TestReroot' -count=1`
Expected: PASS (all 7).

- [ ] **Step 5: Full gate**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-reroot && go build ./... && go test ./internal/web/ ./internal/archtest/ -count=1 && go vet ./internal/web/ && gofmt -l internal/web/`
Expected: all PASS (archtest confirms the `internal/repos` import is legal), vet clean, gofmt silent.

- [ ] **Step 6: Docs**

`CHANGELOG.md`: add, following the file's existing newest-first convention:

```
- web: `POST /api/reroot` — the running server can switch to another
  worktree of the repo or a previously-opened repo (MRU registry);
  allowlist-resolved, preflighted before the swap, refused while an
  operation runs. No client UI yet.
```

`CLAUDE.md`: append to the END of the `web` package-map row, before its closing ` |`:

```
 Re-root: the domain service lives behind an `atomic.Pointer` accessor (`s.service()`, read once per request); `POST /api/reroot` (writeGuard) resolves the client path by allowlist (own `Worktrees()` + the `internal/repos` MRU registry — archtest-legal for frontends), preflights the candidate BEFORE swapping (broken target → 409, old root keeps serving), then swaps + drops the feed and the finished op record under the same `opMu` section that blocks a concurrent op start.
```

- [ ] **Step 7: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-web-reroot
git add internal/web/ CHANGELOG.md CLAUDE.md
git commit -m "feat(web): POST /api/reroot — switch the served repo/worktree at runtime"
```
