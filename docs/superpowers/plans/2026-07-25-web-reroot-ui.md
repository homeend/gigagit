# Web Re-root UI + MRU Recording Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Worktree right-click "switch here" driving `POST /api/reroot`; MRU recording on serve and on re-root; `GET /api/repos`.

**Architecture:** Small server additions (`touchMRU` helper in serve.go, one call in reroot.go, a new repos.go endpoint) + one client menu row and helper. No engine/domain changes.

**Tech Stack:** Go 1.26 stdlib, `internal/repos`, vanilla ES JavaScript.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-25-web-reroot-ui-design.md` (this worktree).
- MRU recording is best-effort — an error must never block serving or a re-root. `New()` must NOT touch the registry (tests construct Servers freely); only `Serve` and a successful re-root record.
- Client touches ONLY `showWorktreeMenu`, one new `doReroot` function, and the help worktree row (the parallel popup track owns the rest of app.js; consume `showCtxMenu`/`copyText`/`opLine`/`postJSON` as-is).
- Working dir: `/mnt/t/others/gigagit.worktrees/feat-web-reroot-ui`, branch `feat/web-reroot-ui`. Verify with `git branch --show-current` before any edit.
- Test helpers to reuse: `newRepoDir`, `gitRun`, `serve`, `getJSON`, `postJSON(t, ts, path, body, contentType, origin, out)`, `addWorktree`, `rerootBody` (reroot_test.go). If a `TopLevel`-vs-fixture path comparison fails on symlink expansion, compare via `filepath.EvalSymlinks` on both sides and note it in the report.

---

### Task 1: Server + client + tests + docs

**Files:**
- Modify: `internal/web/serve.go`, `internal/web/reroot.go`, `internal/web/server.go` (route)
- Create: `internal/web/repos.go`
- Test: `internal/web/mru_test.go` (new)
- Modify: `internal/web/static/app.js`, `internal/web/static/index.html` (help row)
- Modify: `CHANGELOG.md`, `CLAUDE.md`

**Interfaces:**
- Consumes: `s.reposStatePath()` (reroot.go), `repos.Load/Touch/Name/DefaultStatePath`, `writeRepoInfo`, the frozen client APIs above.
- Produces: `GET /api/repos` → `{"repos":[{"path","name"}]}`; `touchMRU(ctx, svc, statePath)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/web/mru_test.go`:

```go
package web

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/repos"
)

func TestTouchMRURecordsServedRepo(t *testing.T) {
	dir := newRepoDir(t, 1)
	sp := filepath.Join(t.TempDir(), "repos.toml")
	touchMRU(context.Background(), domain.Open(dir), sp)
	es := repos.Load(sp)
	if len(es) != 1 || es[0].Path != dir {
		t.Fatalf("registry = %+v, want [%s]", es, dir)
	}
}

func TestRerootRecordsNewRoot(t *testing.T) {
	dir := newRepoDir(t, 2)
	wt := addWorktree(t, dir, "side")
	srv := New(domain.Open(dir))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	ts := serve(t, srv)

	if code := postJSON(t, ts, "/api/reroot", rerootBody(wt), "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("reroot code = %d", code)
	}
	found := false
	for _, e := range repos.Load(srv.reposPath) {
		if e.Path == wt {
			found = true
		}
	}
	if !found {
		t.Fatalf("new root not recorded: %+v", repos.Load(srv.reposPath))
	}
}

func TestReposEndpoint(t *testing.T) {
	dir := newRepoDir(t, 1)
	other := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(srv.reposPath, other, time.Now()); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, srv)

	var out struct {
		Repos []struct{ Path, Name string } `json:"repos"`
	}
	if code := getJSON(t, ts, "/api/repos", &out); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(out.Repos) != 1 || out.Repos[0].Path != other || out.Repos[0].Name == "" {
		t.Fatalf("repos = %+v", out.Repos)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-reroot-ui && go test ./internal/web/ -run 'TestTouchMRU|TestRerootRecords|TestReposEndpoint' -count=1`
Expected: compile FAIL (`touchMRU` undefined), then 404s once stubbed.

- [ ] **Step 3: Implement the server side**

`internal/web/serve.go` — add to the import block: `"time"` and `"github.com/homeend/gigagit/internal/repos"`. After the preflight block in `Serve`:

```go
	if err := preflight(ctx, svc, workdir); err != nil {
		return err
	}
```

insert:

```go
	touchMRU(ctx, svc, repos.DefaultStatePath())
```

and append at the end of serve.go:

```go
// touchMRU records the served repo in the machine's MRU registry so a
// later re-root can always navigate back. Best-effort: recording must
// never block serving.
func touchMRU(ctx context.Context, svc *domain.Service, statePath string) {
	top, err := svc.TopLevel(ctx)
	if err != nil {
		return
	}
	_ = repos.Touch(statePath, top, time.Now())
}
```

`internal/web/reroot.go` — in `handleReroot`'s success tail, replace:

```go
	s.mu.Lock()
	s.feed = nil
	s.mu.Unlock()
	writeRepoInfo(w, r, cand)
```

with:

```go
	s.mu.Lock()
	s.feed = nil
	s.mu.Unlock()
	// The new root becomes navigable-back-to forever (touchMRU on serve
	// covers the original root).
	touchMRU(r.Context(), cand, s.reposStatePath())
	writeRepoInfo(w, r, cand)
```

Create `internal/web/repos.go`:

```go
package web

import (
	"net/http"

	"github.com/homeend/gigagit/internal/repos"
)

// handleRepos lists the machine's MRU registry (previously-opened repos) —
// the allowlist source a re-root picker chooses from.
func (s *Server) handleRepos(w http.ResponseWriter, r *http.Request) {
	entries := repos.Load(s.reposStatePath())
	rows := make([]map[string]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, map[string]string{"path": e.Path, "name": repos.Name(e)})
	}
	writeJSON(w, map[string]any{"repos": rows})
}
```

`internal/web/server.go` — in `Handler()`, after the `POST /api/reroot` route, add:

```go
	mux.HandleFunc("GET /api/repos", s.handleRepos)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-reroot-ui && go test ./internal/web/ -run 'TestTouchMRU|TestRerootRecords|TestReposEndpoint' -count=1`
Expected: PASS (all 3).

- [ ] **Step 5: Client — the menu row + doReroot**

`internal/web/static/app.js` — replace:

```js
function showWorktreeMenu(w, x, y) {
  const items = [{ label: "copy path", act: () => copyText(w.path) }];
```

with:

```js
function showWorktreeMenu(w, x, y) {
  const items = [{ label: "copy path", act: () => copyText(w.path) }];
  // Every row except the served worktree can be switched to (the same
  // exemption the remove row uses).
  if (!(state.worktree && w.path === state.worktree)) {
    items.unshift({ label: "switch here", act: () => doReroot(w.path) });
  }
```

and add next to `doPush`:

```js
// doReroot points the server at another root. The whole client state is
// repo-scoped, so a clean reload is the honest reset on success
// (localStorage prefs survive); errors land on the status strip.
async function doReroot(path) {
  if (state.op) return;
  try {
    await postJSON("/api/reroot", { path });
    location.reload();
  } catch (e) {
    opLine("error: " + (e.message || e), true);
  }
}
```

`internal/web/static/index.html` — replace the help row:

```html
    <div class="hrow"><span class="hkey">worktree</span><span>right-click: copy path, remove</span></div>
```

with:

```html
    <div class="hrow"><span class="hkey">worktree</span><span>right-click: switch here (re-root the server), copy path, remove</span></div>
```

- [ ] **Step 6: Full gate**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-reroot-ui && node --check internal/web/static/app.js && go build ./... && go test ./internal/web/ ./internal/archtest/ -count=1 && go vet ./internal/web/ && gofmt -l internal/web/`
Expected: all PASS, vet clean, gofmt silent.

- [ ] **Step 7: Docs**

`CHANGELOG.md` (top of Unreleased):

```
- web: right-click a worktree → "switch here" re-points the running server
  at it (the page reloads into the new root). Served and switched-to repos
  are recorded in the MRU registry (`GET /api/repos` lists it), so
  re-rooting away is always reversible.
```

`CLAUDE.md`: in the `web` row, right after the existing re-root sentence's final `.` (the sentence ending `blocks a concurrent op start.`), insert:

```
 The worktree ctx-menu's "switch here" drives it (client reloads into the new root); serve + successful re-root `repos.Touch` the active root (best-effort) and `GET /api/repos` lists the MRU for the coming picker.
```

- [ ] **Step 8: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-web-reroot-ui
git add internal/web/ CHANGELOG.md CLAUDE.md
git commit -m "feat(web): worktree 'switch here' + MRU recording + GET /api/repos"
```
