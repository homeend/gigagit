# Web Big-Repo Optimization Suggestion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When `gg web` serves a big repository, show a dismissible startup banner suggesting graph-off + plain sort (written to the repo `.gg.toml`) and a commit-graph write (chained with `fetch.writeCommitGraph=true`), with dismissals persisted in the TUI-shared promptstate store.

**Architecture:** Three small server surfaces — read-only `GET /api/health` (RepoHealth projection + effective UI config + dismissal state), `POST /api/ui-config` (enum-allowlisted `.gg.toml` writes + feed reset), `POST /api/notice-dismiss` (id-allowlisted promptstate write) — plus one new op-transport verb `op:"commit-graph"` whose runFunc chains two engine ops server-side. The client renders a plain-DOM banner (the `#conflict-bar` pattern) and honors `[ui] show_graph` as the default graph mode when localStorage has no override.

**Tech Stack:** Go 1.26 (`internal/web`, stdlib mux), vanilla JS module (`app.js`), real-git table tests (`internal/web/*_test.go` helpers).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-08-01-web-bigrepo-suggest-design.md`.
- Big floor: `bigRepoPackBytes = 100 << 20` (mirrors `internal/tui/notify.go`); a `Server.packThreshold int64` field is the test seam (0 = the const).
- Dismissal ids: exactly `commit_graph_recommend` (shared with the TUI) and `web_graph_off_suggest` (web-only). The server allowlists these two; unknown → 400.
- `/api/ui-config` vocabulary: `show_graph` ∈ {"on","off"}, `commit_sort` ∈ {"date-order","plain"}; each key optional, at least one required; anything else → 400. Free config text never crosses the wire.
- The `fetch.writeCommitGraph` key/value pair is hardcoded server-side — `engine.SetGitConfig` is never wire-constructible.
- `.gg.toml` writes target the COMMITTED file: `filepath.Join(TopLevel, ".gg.toml")` (the `feedFor` probe's file).
- The banner is plain DOM (not a layer), never handles keys, and must not block work.
- `internal/web` never imports `internal/git`/`tui`/`cli` (archtest).
- Every task: run its tests in the FOREGROUND (`cd /mnt/t/others/gigagit.worktrees/feat-web-bigrepo-suggest && go test ./internal/web/ -run <Name> -count=1`, generous timeout), commit with a conventional message.

---

### Task 1: `GET /api/health` (server)

**Files:**
- Create: `internal/web/health.go`
- Create: `internal/web/health_test.go`
- Modify: `internal/web/server.go` (route + one struct field)

**Interfaces:**
- Consumes: `domain.Service.RepoHealth(ctx) (model.RepoHealth, error)`, `s.effectiveConfig(ctx, svc) (config.Config, error)` (review.go — returns defaulted values: `UI.ShowGraph` "on", `UI.CommitSort` "date-order" when unset), `s.promptStore() promptstate.Store` (review.go), `writeJSON`/`writeErr` (server.go).
- Produces: consts `bigRepoPackBytes`, `noticeCommitGraph = "commit_graph_recommend"`, `noticeWebGraphOff = "web_graph_off_suggest"`; `Server.packThreshold int64` seam; response shape `{big, pack_mb, has_commit_graph, write_commit_graph_set, show_graph, commit_sort, dismissed{...}}`. Tasks 2, 4, 5 rely on all of these.

- [ ] **Step 1: Write the failing tests**

`internal/web/health_test.go`:

```go
package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/promptstate"
)

type healthOut struct {
	Big                 bool            `json:"big"`
	PackMB              int64           `json:"pack_mb"`
	HasCommitGraph      bool            `json:"has_commit_graph"`
	WriteCommitGraphSet bool            `json:"write_commit_graph_set"`
	ShowGraph           string          `json:"show_graph"`
	CommitSort          string          `json:"commit_sort"`
	Dismissed           map[string]bool `json:"dismissed"`
}

// A small loose-object repo under the real 100MB floor: not big, nothing
// set, defaults reported, both ids present and false.
func TestHealthEndpointDefaults(t *testing.T) {
	dir := newRepoDir(t, 3)
	srv := New(domain.Open(dir))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	ts := serve(t, srv)

	var h healthOut
	if code := getJSON(t, ts, "/api/health", &h); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if h.Big {
		t.Errorf("big = true for a tiny repo")
	}
	if h.HasCommitGraph || h.WriteCommitGraphSet {
		t.Errorf("flags = %v/%v, want false/false", h.HasCommitGraph, h.WriteCommitGraphSet)
	}
	if h.ShowGraph != "on" || h.CommitSort != "date-order" {
		t.Errorf("defaults = %q/%q, want on/date-order", h.ShowGraph, h.CommitSort)
	}
	if len(h.Dismissed) != 2 || h.Dismissed["commit_graph_recommend"] || h.Dismissed["web_graph_off_suggest"] {
		t.Errorf("dismissed = %v, want both ids present and false", h.Dismissed)
	}
}

// The packThreshold seam: gc packs the objects, threshold 1 makes it "big".
func TestHealthBigViaSeam(t *testing.T) {
	dir := newRepoDir(t, 3)
	gitRun(t, dir, "gc", "--quiet")
	srv := New(domain.Open(dir))
	srv.packThreshold = 1
	ts := serve(t, srv)

	var h healthOut
	if code := getJSON(t, ts, "/api/health", &h); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if !h.Big {
		t.Errorf("big = false with a pack present and threshold 1")
	}
}

// Real flags + configured .gg.toml values are projected, not defaulted.
func TestHealthFlagsAndConfig(t *testing.T) {
	dir := newRepoDir(t, 3)
	gitRun(t, dir, "commit-graph", "write", "--reachable")
	gitRun(t, dir, "config", "fetch.writeCommitGraph", "true")
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"),
		[]byte("[ui]\nshow_graph = \"off\"\ncommit_sort = \"plain\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	var h healthOut
	if code := getJSON(t, ts, "/api/health", &h); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if !h.HasCommitGraph || !h.WriteCommitGraphSet {
		t.Errorf("flags = %v/%v, want true/true", h.HasCommitGraph, h.WriteCommitGraphSet)
	}
	if h.ShowGraph != "off" || h.CommitSort != "plain" {
		t.Errorf("config = %q/%q, want off/plain", h.ShowGraph, h.CommitSort)
	}
}

// A dismissal seeded in the shared prompts store (keyed by git common dir,
// the TUI's key) is reported; the other id stays false.
func TestHealthDismissed(t *testing.T) {
	dir := newRepoDir(t, 3)
	srv := New(domain.Open(dir))
	stateDir := t.TempDir()
	srv.reposPath = filepath.Join(stateDir, "repos.toml")

	key, err := domain.Open(dir).GitCommonDir(context.Background())
	if err != nil || key == "" {
		t.Fatalf("GitCommonDir: %v %q", err, key)
	}
	store := promptstate.NewFileStore(filepath.Join(stateDir, "prompts.toml"))
	if err := store.DismissNotice(key, "commit_graph_recommend"); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, srv)

	var h healthOut
	if code := getJSON(t, ts, "/api/health", &h); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if !h.Dismissed["commit_graph_recommend"] {
		t.Errorf("seeded dismissal not reported: %v", h.Dismissed)
	}
	if h.Dismissed["web_graph_off_suggest"] {
		t.Errorf("unseeded id reported dismissed: %v", h.Dismissed)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-bigrepo-suggest && go test ./internal/web/ -run TestHealth -count=1`
Expected: FAIL — `srv.packThreshold undefined` (compile error), then 404s once it compiles.

- [ ] **Step 3: Implement**

`internal/web/health.go`:

```go
package web

import (
	"net/http"
)

// bigRepoPackBytes mirrors the TUI's floor (internal/tui/notify.go): below
// it the commit-graph win doesn't matter enough to suggest anything.
const bigRepoPackBytes = 100 << 20

// noticeCommitGraph is the TUI's commit-graph notice id — shared on purpose:
// dismissing the recommendation in either frontend silences both (same
// advice, same repo).
const noticeCommitGraph = "commit_graph_recommend"

// noticeWebGraphOff is the web-only graph-off suggestion id; the TUI has no
// equivalent auto-notice (its coupling fires on the Settings toggle).
const noticeWebGraphOff = "web_graph_off_suggest"

type healthResp struct {
	Big                 bool            `json:"big"`
	PackMB              int64           `json:"pack_mb"`
	HasCommitGraph      bool            `json:"has_commit_graph"`
	WriteCommitGraphSet bool            `json:"write_commit_graph_set"`
	ShowGraph           string          `json:"show_graph"`
	CommitSort          string          `json:"commit_sort"`
	Dismissed           map[string]bool `json:"dismissed"`
}

// packFloor is the effective big-repo threshold (test seam over the const).
func (s *Server) packFloor() int64 {
	if s.packThreshold > 0 {
		return s.packThreshold
	}
	return bigRepoPackBytes
}

// handleHealth projects domain.RepoHealth plus the effective UI settings and
// the banner ids' dismissal state. Read-only; config/promptstate failures
// degrade to defaults rather than erroring (the TUI's "health never surfaces
// errors" posture applies to the enrichments — only the core health read can
// fail the request).
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	h, err := svc.RepoHealth(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	resp := healthResp{
		Big:                 h.PackBytes >= s.packFloor(),
		PackMB:              h.PackBytes / (1 << 20),
		HasCommitGraph:      h.HasCommitGraph,
		WriteCommitGraphSet: h.WriteCommitGraphSet,
		ShowGraph:           "on",
		CommitSort:          "date-order",
		Dismissed:           map[string]bool{noticeCommitGraph: false, noticeWebGraphOff: false},
	}
	if cfg, cerr := s.effectiveConfig(r.Context(), svc); cerr == nil {
		if cfg.UI.ShowGraph != "" {
			resp.ShowGraph = cfg.UI.ShowGraph
		}
		if cfg.UI.CommitSort != "" {
			resp.CommitSort = cfg.UI.CommitSort
		}
	}
	if store := s.promptStore(); store != nil && h.GitCommonDir != "" {
		d := store.DismissedNotices(h.GitCommonDir)
		resp.Dismissed[noticeCommitGraph] = d[noticeCommitGraph]
		resp.Dismissed[noticeWebGraphOff] = d[noticeWebGraphOff]
	}
	writeJSON(w, resp)
}
```

`internal/web/server.go` — add to the `Server` struct, after the `reposPath` field:

```go
	// packThreshold overrides bigRepoPackBytes for /api/health's "big"
	// verdict (test seam); 0 = the production const.
	packThreshold int64
```

And register the route beside the other GET reads (after the `/api/blame` line):

```go
	mux.HandleFunc("GET /api/health", s.handleHealth)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-bigrepo-suggest && go test ./internal/web/ -run TestHealth -count=1`
Expected: PASS (all four).

- [ ] **Step 5: Commit**

```bash
git add internal/web/health.go internal/web/health_test.go internal/web/server.go
git commit -m "feat(web): GET /api/health — big-repo flag, UI settings, dismissal state"
```

---

### Task 2: `POST /api/notice-dismiss` (server)

**Files:**
- Modify: `internal/web/health.go` (append handler)
- Modify: `internal/web/health_test.go` (append tests)
- Modify: `internal/web/server.go` (route)

**Interfaces:**
- Consumes: Task 1's `noticeCommitGraph`/`noticeWebGraphOff` consts, `s.promptStore()`, `svc.GitCommonDir(ctx)`, `writeGuard` (server.go).
- Produces: `POST /api/notice-dismiss {"id": "<allowlisted>"}` → `{"ok": true}`. Task 5's "never" button relies on it.

- [ ] **Step 1: Write the failing tests**

Append to `internal/web/health_test.go`:

```go
// A known id lands in the shared prompts store under the git-common-dir key
// and /api/health reflects it; an unknown id is refused with the store
// untouched.
func TestNoticeDismiss(t *testing.T) {
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	stateDir := t.TempDir()
	srv.reposPath = filepath.Join(stateDir, "repos.toml")
	ts := serve(t, srv)

	if code := postJSON(t, ts, "/api/notice-dismiss",
		`{"id":"web_graph_off_suggest"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("dismiss status = %d, want 200", code)
	}
	key, err := domain.Open(dir).GitCommonDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	store := promptstate.NewFileStore(filepath.Join(stateDir, "prompts.toml"))
	if !store.DismissedNotices(key)["web_graph_off_suggest"] {
		t.Errorf("dismissal not persisted: %v", store.DismissedNotices(key))
	}

	if code := postJSON(t, ts, "/api/notice-dismiss",
		`{"id":"evil_id"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("unknown id status = %d, want 400", code)
	}
	if store.DismissedNotices(key)["evil_id"] {
		t.Errorf("unknown id polluted the store")
	}

	var h healthOut
	if code := getJSON(t, ts, "/api/health", &h); code != http.StatusOK {
		t.Fatalf("health status = %d", code)
	}
	if !h.Dismissed["web_graph_off_suggest"] {
		t.Errorf("health does not reflect the dismissal: %v", h.Dismissed)
	}
}

// The writeGuard applies: wrong content type is refused before the handler.
func TestNoticeDismissGuard(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	if code := postJSON(t, ts, "/api/notice-dismiss",
		`{"id":"web_graph_off_suggest"}`, "text/plain", "", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain status = %d, want 415", code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-bigrepo-suggest && go test ./internal/web/ -run TestNoticeDismiss -count=1`
Expected: FAIL — 404 (route not registered).

- [ ] **Step 3: Implement**

Append to `internal/web/health.go` (add `"encoding/json"` and `"errors"` to its imports):

```go
// handleNoticeDismiss persists a "never for this repo" banner dismissal into
// the TUI-shared prompts store. The id is allowlisted to the two banner ids —
// a frontend bug can never pollute prompts.toml with garbage keys (the
// DeleteBranchVersion refuse-outside-the-namespace precedent).
func (s *Server) handleNoticeDismiss(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad request body"))
		return
	}
	if req.ID != noticeCommitGraph && req.ID != noticeWebGraphOff {
		writeErr(w, http.StatusBadRequest, errors.New("unknown notice id"))
		return
	}
	svc := s.service()
	key, err := svc.GitCommonDir(r.Context())
	if err != nil || key == "" {
		writeErr(w, http.StatusInternalServerError, errors.New("cannot resolve repo key"))
		return
	}
	store := s.promptStore()
	if store == nil {
		writeErr(w, http.StatusInternalServerError, errors.New("no state dir for dismissals"))
		return
	}
	if err := store.DismissNotice(key, req.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
```

Register in `internal/web/server.go` beside the other writeGuard POSTs:

```go
	mux.HandleFunc("POST /api/notice-dismiss", writeGuard(s.handleNoticeDismiss))
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-bigrepo-suggest && go test ./internal/web/ -run TestNoticeDismiss -count=1`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
git add internal/web/health.go internal/web/health_test.go internal/web/server.go
git commit -m "feat(web): POST /api/notice-dismiss — allowlisted ids into shared promptstate"
```

---

### Task 3: `POST /api/ui-config` (server)

**Files:**
- Create: `internal/web/uiconfig.go`
- Create: `internal/web/uiconfig_test.go`
- Modify: `internal/web/server.go` (route)

**Interfaces:**
- Consumes: `config.SetShowGraph(path, value) error` / `config.SetCommitSort(path, mode) error` (internal/config/write.go), `svc.TopLevel(ctx)`, `s.resetFeed()` (oprun.go), `writeGuard`.
- Produces: `POST /api/ui-config {"show_graph":"off","commit_sort":"plain"}` → `{"ok": true}`; writes the committed `.gg.toml`; a `commit_sort` write drops the feed. Task 5's accept button relies on it.

- [ ] **Step 1: Write the failing tests**

`internal/web/uiconfig_test.go`:

```go
package web

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

func feedIsNil(srv *Server) bool {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return srv.feed == nil
}

// Both keys land in the committed .gg.toml and a commit_sort write drops the
// cached feed so the next /api/commits rebuilds with the new sort.
func TestUIConfigWrite(t *testing.T) {
	dir := newRepoDir(t, 3)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	if code := getJSON(t, ts, "/api/commits", nil); code != http.StatusOK {
		t.Fatalf("prime commits: %d", code)
	}
	if feedIsNil(srv) {
		t.Fatal("feed not built by /api/commits")
	}
	if code := postJSON(t, ts, "/api/ui-config",
		`{"show_graph":"off","commit_sort":"plain"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("ui-config status = %d, want 200", code)
	}
	cfg, err := config.Load("", filepath.Join(dir, ".gg.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.ShowGraph != "off" || cfg.UI.CommitSort != "plain" {
		t.Errorf("written config = %q/%q, want off/plain", cfg.UI.ShowGraph, cfg.UI.CommitSort)
	}
	if !feedIsNil(srv) {
		t.Error("commit_sort write did not reset the feed")
	}
	if code := getJSON(t, ts, "/api/commits", nil); code != http.StatusOK {
		t.Errorf("commits after reset: %d", code)
	}
}

// A show_graph-only write must NOT reset the feed (sort unchanged; graph
// rendering is client-side).
func TestUIConfigShowGraphOnlyKeepsFeed(t *testing.T) {
	dir := newRepoDir(t, 3)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	if code := getJSON(t, ts, "/api/commits", nil); code != http.StatusOK {
		t.Fatalf("prime commits: %d", code)
	}
	if code := postJSON(t, ts, "/api/ui-config",
		`{"show_graph":"off"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("ui-config status = %d, want 200", code)
	}
	if feedIsNil(srv) {
		t.Error("show_graph-only write reset the feed")
	}
}

// The enum vocabulary is enforced; nothing outside it reaches the file.
func TestUIConfigRefusals(t *testing.T) {
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	cases := []string{
		`{"show_graph":"maybe"}`,
		`{"commit_sort":"topo"}`,
		`{}`,
		`not json`,
	}
	for _, body := range cases {
		if code := postJSON(t, ts, "/api/ui-config", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, code)
		}
	}
	if code := postJSON(t, ts, "/api/ui-config",
		`{"show_graph":"off"}`, "text/plain", "", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain status = %d, want 415", code)
	}
	if code := getJSON(t, ts, "/api/ui-config", nil); code != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", code)
	}
	if _, err := config.Load("", filepath.Join(dir, ".gg.toml")); err != nil {
		t.Fatalf("load after refusals: %v", err)
	}
	cfg, _ := config.Load("", filepath.Join(dir, ".gg.toml"))
	if cfg.UI.ShowGraph == "maybe" || cfg.UI.CommitSort == "topo" {
		t.Errorf("refused value reached the file: %q/%q", cfg.UI.ShowGraph, cfg.UI.CommitSort)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-bigrepo-suggest && go test ./internal/web/ -run TestUIConfig -count=1`
Expected: FAIL — 404 (route not registered).

- [ ] **Step 3: Implement**

`internal/web/uiconfig.go`:

```go
package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"

	"github.com/homeend/gigagit/internal/config"
)

// handleUIConfig writes [ui] show_graph / commit_sort to the COMMITTED repo
// .gg.toml (the feedFor probe's file) — the web's accept path for the
// big-repo banner's "graph off + plain sort". Values are allowlisted to the
// exact enum vocabulary: free config text never crosses the wire (the
// commit-edit "wire carries a verb" rule). Not an engine op — no git, no
// repogate; the same standing as the TUI Settings rows.
func (s *Server) handleUIConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ShowGraph  string `json:"show_graph"`
		CommitSort string `json:"commit_sort"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad request body"))
		return
	}
	if req.ShowGraph == "" && req.CommitSort == "" {
		writeErr(w, http.StatusBadRequest, errors.New("nothing to set"))
		return
	}
	if req.ShowGraph != "" && req.ShowGraph != "on" && req.ShowGraph != "off" {
		writeErr(w, http.StatusBadRequest, errors.New("invalid show_graph"))
		return
	}
	if req.CommitSort != "" && req.CommitSort != "date-order" && req.CommitSort != "plain" {
		writeErr(w, http.StatusBadRequest, errors.New("invalid commit_sort"))
		return
	}
	svc := s.service()
	top, err := svc.TopLevel(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	path := filepath.Join(top, ".gg.toml")
	if req.ShowGraph != "" {
		if err := config.SetShowGraph(path, req.ShowGraph); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	if req.CommitSort != "" {
		if err := config.SetCommitSort(path, req.CommitSort); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		// feedFor re-reads commit_sort at the next build, so dropping the
		// feed is what makes the new sort take effect.
		s.resetFeed()
	}
	writeJSON(w, map[string]bool{"ok": true})
}
```

Register in `internal/web/server.go` beside the other writeGuard POSTs:

```go
	mux.HandleFunc("POST /api/ui-config", writeGuard(s.handleUIConfig))
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-bigrepo-suggest && go test ./internal/web/ -run TestUIConfig -count=1`
Expected: PASS (all three).

- [ ] **Step 5: Commit**

```bash
git add internal/web/uiconfig.go internal/web/uiconfig_test.go internal/web/server.go
git commit -m "feat(web): POST /api/ui-config — enum-allowlisted show_graph/commit_sort writes"
```

---

### Task 4: `op:"commit-graph"` (op transport)

**Files:**
- Modify: `internal/web/ophttp.go` (new case in `handleOpStart`'s switch, before `default:`)
- Create: `internal/web/opcommitgraph_test.go`

**Interfaces:**
- Consumes: `s.startRun(kind, fn)` (oprun.go; `runFunc = func(ctx, svc, events, dec) (engine.Result, map[string]any, error)`), `engine.WriteCommitGraph{}`, `engine.SetGitConfig{Key, Value string, ...}`, `svc.Execute`.
- Produces: `POST /api/op {"op":"commit-graph"}` → `202 {"op_id":...}`; on done the repo has a commit-graph and `fetch.writeCommitGraph=true` local. Task 5's accept button relies on it.

- [ ] **Step 1: Write the failing test**

`internal/web/opcommitgraph_test.go`:

```go
package web

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// op:"commit-graph" chains WriteCommitGraph → SetGitConfig server-side: after
// done, the graph file (or chain dir) exists AND fetch.writeCommitGraph is
// true — and /api/health reports both flags flipped, which is what retires
// the banner group.
func TestOpCommitGraph(t *testing.T) {
	dir := newRepoDir(t, 3)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpJSON(t, ts, `{"op":"commit-graph"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true {
		t.Fatalf("done = %v", done)
	}

	gitDir := gitRun(t, dir, "rev-parse", "--absolute-git-dir")
	_, ferr := os.Stat(filepath.Join(gitDir, "objects", "info", "commit-graph"))
	_, cerr := os.Stat(filepath.Join(gitDir, "objects", "info", "commit-graphs"))
	if ferr != nil && cerr != nil {
		t.Errorf("no commit-graph file or chain dir: %v / %v", ferr, cerr)
	}
	if v := gitRun(t, dir, "config", "fetch.writeCommitGraph"); v != "true" {
		t.Errorf("fetch.writeCommitGraph = %q, want true", v)
	}

	var h healthOut
	if code := getJSON(t, ts, "/api/health", &h); code != http.StatusOK {
		t.Fatalf("health status = %d", code)
	}
	if !h.HasCommitGraph || !h.WriteCommitGraphSet {
		t.Errorf("health flags = %v/%v, want true/true", h.HasCommitGraph, h.WriteCommitGraphSet)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-bigrepo-suggest && go test ./internal/web/ -run TestOpCommitGraph -count=1`
Expected: FAIL — start returns 400 `unknown op "commit-graph"`.

- [ ] **Step 3: Implement**

In `internal/web/ophttp.go`, add a case before `default:` in `handleOpStart`'s switch. Unlike the other cases it does not build a single `op` — it starts its own run and returns:

```go
	case "commit-graph":
		// Write the commit-graph now, then keep it fresh — the TUI notice's
		// write+enable chain (tui.startCommitGraphWriteAndEnable), run
		// server-side inside ONE run so the config key/value never come off
		// the wire: a client cannot write arbitrary git config through this.
		run, err := s.startRun("op", func(ctx context.Context, svc *domain.Service, events chan<- engine.Event, dec engine.Decider) (engine.Result, map[string]any, error) {
			res, err := svc.Execute(ctx, engine.WriteCommitGraph{}, events, dec)
			if err != nil {
				return res, nil, err
			}
			if _, err := svc.Execute(ctx, engine.SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true"}, events, dec); err != nil {
				return res, nil, err
			}
			return res, nil, nil
		})
		if err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"op_id": run.id})
		return
```

If `context` is not already imported in ophttp.go, add it.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-bigrepo-suggest && go test ./internal/web/ -run TestOpCommitGraph -count=1`
Expected: PASS.

- [ ] **Step 5: Run the full web package once (routes + op transport touched)**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-bigrepo-suggest && go test ./internal/web/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/ophttp.go internal/web/opcommitgraph_test.go
git commit -m "feat(web): op commit-graph — server-side WriteCommitGraph + fetch.writeCommitGraph chain"
```

---

### Task 5: client banner + `[ui] show_graph` default

**Files:**
- Modify: `internal/web/static/index.html` (banner div after `#conflict-bar`)
- Modify: `internal/web/static/style.css` (banner rules)
- Modify: `internal/web/static/app.js` (health fetch, banner render/actions, boot order, done hook)

**Interfaces:**
- Consumes: Task 1-4's endpoints; existing client helpers `getJSON(url)`, `postJSON(url, body)`, `lsGet`/`lsSet`, `$()`, `opLine(text, isErr)`, `startOp(body, label)`, `loadCommits(more)`, `state.graphMode`, `handleOpEvent`'s done branch, `boot()`.
- Produces: `state.health`, `fetchHealth(applyDefault)`, `renderBigRepoBanner()`, `ssGet`/`ssSet`. No later task consumes these.

- [ ] **Step 1: index.html — banner markup**

Directly after the closing `</div>` of `#conflict-bar` (before `<main id="panes"...>`):

```html
<div id="bigrepo-bar" class="hidden">
  <span id="bigrepo-msg"></span>
  <button id="bigrepo-graphoff">turn graph off + plain sort</button>
  <button id="bigrepo-cgraph">write commit-graph + keep it fresh</button>
  <button id="bigrepo-later">not now</button>
  <button id="bigrepo-never">never for this repo</button>
</div>
```

- [ ] **Step 2: style.css — banner rules**

Append (matching `#conflict-bar`'s bar pattern; adjust colors only if they clash with the existing palette — the intent is an amber "advice" bar distinct from the conflict bar):

```css
#bigrepo-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 10px;
  font-size: 13px;
  background: #2b2417;
  border-bottom: 1px solid #6b5d2e;
  color: #e8d48b;
}
#bigrepo-bar.hidden { display: none; }
#bigrepo-bar button { font-size: 12px; }
```

- [ ] **Step 3: app.js — sessionStorage helpers, health state, fetch + render**

Beside `lsGet`/`lsSet` (~line 57):

```js
function ssGet(k) { try { return sessionStorage.getItem(k); } catch { return null; } }
function ssSet(k, v) { try { sessionStorage.setItem(k, v); } catch {} }
```

Add `health: null,` to the `state` object literal (top of app.js, beside `graphMode`).

Add near `loadRepo()`:

```js
// fetchHealth loads /api/health and re-renders the big-repo banner. With
// applyDefault (boot only), a repo-config show_graph=off becomes the initial
// graph mode when this browser has no localStorage override — [ui] show_graph
// honored as the web default, the TUI-parity fix. Failures are silent: no
// banner, existing localStorage-or-default behavior (the TUI's "health never
// surfaces errors" rule).
async function fetchHealth(applyDefault) {
  try {
    state.health = await getJSON("/api/health");
  } catch {
    state.health = null;
  }
  if (applyDefault && state.health && lsGet("gg.graph") === null && state.health.show_graph === "off") {
    state.graphMode = "off";
  }
  renderBigRepoBanner();
}

// bigRepoGroups derives which action groups still apply — empty means no
// banner. "graphoff": the effective graph state is on (config not off AND no
// per-browser off override) OR the sort is not plain (either misalignment
// keeps the offer; accepting writes both keys). "cgraph": exactly the TUI
// notice's conditions.
function bigRepoGroups() {
  const h = state.health;
  if (!h || !h.big) return [];
  const groups = [];
  const graphOn = h.show_graph !== "off" && lsGet("gg.graph") !== "off";
  if (!h.dismissed.web_graph_off_suggest && (graphOn || h.commit_sort !== "plain")) groups.push("graphoff");
  if (!h.dismissed.commit_graph_recommend && !h.has_commit_graph && !h.write_commit_graph_set) groups.push("cgraph");
  return groups;
}

function renderBigRepoBanner() {
  const bar = $("bigrepo-bar");
  if (ssGet("gg.bigrepo.later") === "1") { bar.classList.add("hidden"); return; }
  const groups = bigRepoGroups();
  if (!groups.length) { bar.classList.add("hidden"); return; }
  $("bigrepo-msg").textContent =
    "big repository (" + state.health.pack_mb + " MB of packs) — commit browsing can be faster:";
  $("bigrepo-graphoff").classList.toggle("hidden", !groups.includes("graphoff"));
  $("bigrepo-cgraph").classList.toggle("hidden", !groups.includes("cgraph"));
  bar.classList.remove("hidden");
}
```

- [ ] **Step 4: app.js — action handlers**

With the other one-time element wiring (near the `#conflict-bar` button handlers):

```js
$("bigrepo-graphoff").onclick = async () => {
  try {
    await postJSON("/api/ui-config", { show_graph: "off", commit_sort: "plain" });
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    return; // banner stays; the action can be retried
  }
  state.graphMode = "off";
  lsSet("gg.graph", "off"); // this browser matches immediately and keeps matching
  await loadCommits(false); // sort changed server-side — reload the feed
  fetchHealth();
};
$("bigrepo-cgraph").onclick = () => startOp({ op: "commit-graph" }, "write commit-graph");
$("bigrepo-later").onclick = () => {
  ssSet("gg.bigrepo.later", "1"); // session-only, re-evaluated next visit
  $("bigrepo-bar").classList.add("hidden");
};
$("bigrepo-never").onclick = async () => {
  // dismiss only the ids the banner is currently showing
  const groups = bigRepoGroups();
  const ids = [];
  if (groups.includes("graphoff")) ids.push("web_graph_off_suggest");
  if (groups.includes("cgraph")) ids.push("commit_graph_recommend");
  try {
    for (const id of ids) await postJSON("/api/notice-dismiss", { id });
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    return;
  }
  fetchHealth();
};
```

- [ ] **Step 5: app.js — boot order + op-done hook**

In `boot()`, insert the health fetch BEFORE `loadCommits` so a config-off repo's first render is already flat:

```js
async function boot() {
  await loadRepo();
  await fetchStatus().catch(() => {}); // status failing must not block browse
  await fetchBranches().catch(() => {});
  await fetchHealth(true); // banner + [ui] show_graph default (self-catching)
  await loadCommits(false);
  focusPane();
}
```

In `handleOpEvent`'s `done` branch, after the generic outcome lines (right before the `if (ev.changed) refreshAfterOp();` pair):

```js
    if (kind === "commit-graph") fetchHealth(); // retires the banner group
```

(A re-root reloads the page — `doReroot` calls `location.reload()` — so boot re-runs `fetchHealth` there for free.)

- [ ] **Step 6: Syntax-check and eyeball**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-bigrepo-suggest && node --check internal/web/static/app.js && go build ./cmd/gg`
Expected: both clean.

Then a quick manual smoke: `./gg web --addr 127.0.0.1:8899` against any repo, confirm the page loads and no console error appears (the banner will be hidden on a small repo — that IS the expected state).

- [ ] **Step 7: Commit**

```bash
git add internal/web/static/index.html internal/web/static/style.css internal/web/static/app.js
git commit -m "feat(web): big-repo suggestion banner + [ui] show_graph honored as web default"
```

---

### Task 6: docs

**Files:**
- Modify: `CHANGELOG.md` (new entry at top)
- Modify: `CLAUDE.md` (append to the `web` package-map row)

**Interfaces:**
- Consumes: nothing — text only.
- Produces: nothing.

- [ ] **Step 1: CHANGELOG entry**

Add at the top of `CHANGELOG.md`, matching the existing entry style, dated 2026-08-02:

```markdown
## 2026-08-02 — web: big-repo optimization suggestion

- `gg web` now detects a big repository (pack bytes ≥ 100 MB, the TUI's
  floor) and shows a dismissible banner after load suggesting the settings
  that make commit browsing fast: **turn graph off + plain sort** (writes
  `[ui] show_graph = "off"` + `commit_sort = "plain"` to the repo
  `.gg.toml` — shared with the TUI) and **write commit-graph + keep it
  fresh** (a new `op:"commit-graph"` chaining `git commit-graph write
  --reachable` with `fetch.writeCommitGraph=true`, the TUI notice's
  write+enable pair, run server-side).
- "Never for this repo" persists via the TUI-shared promptstate store; the
  commit-graph recommendation reuses the TUI's dismissal id, so dismissing
  it in either frontend silences both. "Not now" is session-only.
- The web now honors `[ui] show_graph` as its default graph mode when the
  browser has no local `g`-toggle override — a config-off repo renders the
  flat list on first load.
- New endpoints: `GET /api/health`, `POST /api/ui-config` (enum-allowlisted
  values only), `POST /api/notice-dismiss` (allowlisted ids only).
```

- [ ] **Step 2: CLAUDE.md web row**

Append to the end of the `web` row's text in the package map (one sentence block, matching the row's style):

```
**Big-repo suggestion** (`health.go`, `uiconfig.go`, op #22): `GET /api/health` projects `domain.RepoHealth` (big = pack ≥ 100MB, the TUI's `bigRepoPackBytes` floor; `Server.packThreshold` is the test seam) + effective `[ui] show_graph`/`commit_sort` + the two banner ids' promptstate dismissal state; a dismissible client banner (plain DOM after `#conflict-bar`, never a layer) offers "graph off + plain sort" → `POST /api/ui-config` (enum-allowlisted values only, writes the COMMITTED `.gg.toml`, a commit_sort write drops the feed) and "write commit-graph + keep fresh" → `op:"commit-graph"` (one server-side run chaining `engine.WriteCommitGraph` → `engine.SetGitConfig{fetch.writeCommitGraph=true}` — the key/value never come off the wire); "never" → `POST /api/notice-dismiss` (ids allowlisted to `commit_graph_recommend` — the TUI's id, shared dismissal — and `web_graph_off_suggest`); "not now" is sessionStorage. The client also honors `[ui] show_graph` as the default graph mode when localStorage has no `gg.graph` override (boot fetches health before the first commits render).
```

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md CLAUDE.md
git commit -m "docs: changelog + CLAUDE.md for web big-repo suggestion"
```

---

## Post-plan gates (controller-owned, not tasks)

- CDP verification per the spec's section 8 (big fixture with ~110 MB of
  incompressible random bytes; visibility via elementFromPoint; run the
  bugless checks against the built branch binary).
- `./test.sh race` on a quiet machine + poll before any merge.
- Merge only on explicit user approval; `./build.sh web` in the web-dev
  worktree after the merge; deliver the exe via SendUserFile.
