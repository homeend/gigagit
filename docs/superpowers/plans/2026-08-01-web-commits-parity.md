# Web Commits-Panel Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the gg web UI's commits surface to TUI parity: a `/` quick filter over loaded rows, goto-sha that reveals a commit in the list, and file history / blame overlays reachable from file rows, the palette, and the diff toolbar.

**Architecture:** One new domain query (`ResolveRev`, full-sha resolve). Three new read-only web endpoints in a new `internal/web/history.go` (`/api/resolve`, `/api/filelog`, `/api/blame`) over existing domain queries. All UI is client-side in `internal/web/static/` (app.js / index.html / style.css): the filter and goto operate on the existing virtualized commits pane; history and blame are full-screen overlay layers (the report-viewer precedent, NOT new pane-layout modes).

**Tech Stack:** Go 1.26 (stdlib net/http, httptest + real git in t.TempDir), vanilla JS/HTML/CSS (no framework, no build step).

**Spec:** `docs/superpowers/specs/2026-08-01-web-commits-parity-design.md`

## Global Constraints

- Branch `feat/web-commits-parity`, worktree `/mnt/t/others/gigagit.worktrees/feat-web-commits-parity`. Prefix EVERY build/test command with `cd /mnt/t/others/gigagit.worktrees/feat-web-commits-parity && `.
- Run all go tests in the FOREGROUND with a generous timeout (10+ min). Never background a test run and wait.
- `internal/web` imports `internal/domain` only — never `internal/git`/`tui`/`cli` (archtest-enforced).
- All three endpoints are read-only GETs: no `writeGuard` (the `/api/compare`, `/api/versions` precedent). `isGitArgSafe` (server.go:180 — non-empty and no leading `-`) on every `rev`/`path` that reaches git argv; empty `rev` is allowed where documented (means HEAD / working tree) and must NOT be passed through `isGitArgSafe` in that case.
- The quick filter narrows LOADED rows only; deeper paging is always an explicit click, never automatic on keystroke.
- Filtered commit rows render flat (one-cell dot), never lane glyphs.
- History/blame are overlay layers on the client layer stack; they never change `state.layout`/`state.filesMode`.
- The `/api/diff` COMMIT form (`sha`+`path`+`status`+`old`) already serves parent-vs-commit diffs including A/D handling — history reuses it; do not add a new diff form.
- JS: every overlay close path must leave no focused input behind (blur before close — the palette's lesson); `hideCtxMenu` semantics, `esc()` for all interpolated HTML, gen-guards on every async open.
- TDD for all Go code. Frequent commits, one per task minimum.

---

### Task 1: `domain.ResolveRev` — full-sha resolve query

**Files:**
- Modify: `internal/domain/query.go` (append after `CommitLookup`, ~line 529)
- Test: `internal/domain/export_test.go` (append after `TestCommitLookup`, ~line 212)

**Interfaces:**
- Consumes: existing `s.repo.ResolveCommit(ctx, ref) (string, error)` (the `GitOps` interface already includes it — verify with `grep -n "ResolveCommit" internal/domain/*.go`; if it is NOT on the interface used by `query.go`, add it there exactly as `CommitLine` is listed).
- Produces: `func (s *Service) ResolveRev(ctx context.Context, rev string) (string, bool, error)` — full 40-hex sha, found=false (nil error) when git can't resolve, error only on context cancellation. Task 2's `/api/resolve` calls this.

- [ ] **Step 1: Write the failing test** (in `export_test.go`, using the file's existing `newRealRepo`/`writeCommit`/`headHash` helpers — read `TestCommitLookup` at line 193 first, this test mirrors it):

```go
func TestResolveRev(t *testing.T) {
	repoDir, svc := newRealRepo(t)
	ctx := context.Background()

	writeCommit(t, repoDir, "a.txt", "a\n", "subject here")
	sha := headHash(t, repoDir)

	got, found, err := svc.ResolveRev(ctx, "HEAD")
	if err != nil || !found {
		t.Fatalf("ResolveRev(HEAD): found=%v err=%v", found, err)
	}
	if got != sha {
		t.Fatalf("ResolveRev(HEAD) = %q, want full sha %q", got, sha)
	}
	if len(got) != 40 {
		t.Fatalf("want FULL sha (40 hex), got %d chars", len(got))
	}

	_, found, err = svc.ResolveRev(ctx, "no-such-rev-anywhere")
	if err != nil || found {
		t.Fatalf("missing rev: found=%v err=%v, want false, nil", found, err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-commits-parity && go test ./internal/domain/ -run TestResolveRev -v`
Expected: FAIL — `svc.ResolveRev undefined`.

- [ ] **Step 3: Implement** (append to `internal/domain/query.go` after `CommitLookup`):

```go
// ResolveRev resolves rev to its FULL commit sha, reporting found=false when
// git cannot resolve it. Missing is an expected state here (a typo, a gc'd
// sha), so it is not an error and never recorded to the failure log
// (queryQuiet) — the CommitLookup convention. CommitLookup stays the
// display-facing short-sha read; this exists for callers that must match
// feed rows by full hash (the web goto-sha).
func (s *Service) ResolveRev(ctx context.Context, rev string) (string, bool, error) {
	sha, err := queryQuiet(ctx, s, "resolveRev:"+rev, func(ctx context.Context) (string, error) {
		return s.repo.ResolveCommit(ctx, rev)
	})
	if err != nil {
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		return "", false, nil
	}
	return sha, true, nil
}
```

If the compiler reports `ResolveCommit` missing from the repo interface in `query.go`'s type, add `ResolveCommit(ctx context.Context, ref string) (string, error)` to that interface next to `CommitLine` — `*git.Repo` already implements it (`internal/git/resolve.go:17`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-commits-parity && go test ./internal/domain/ -run "TestResolveRev|TestCommitLookup" -v`
Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/query.go internal/domain/export_test.go
git commit -m "feat(domain): ResolveRev full-sha resolve query"
```

---

### Task 2: Web endpoints — `/api/resolve`, `/api/filelog`, `/api/blame`

**Files:**
- Create: `internal/web/history.go`
- Create: `internal/web/history_test.go`
- Modify: `internal/web/server.go` (route block, after the `GET /api/rebase-range` line ~78)

**Interfaces:**
- Consumes: `svc.ResolveRev(ctx, rev) (string, bool, error)` (Task 1); existing `svc.CommitLookup(ctx, rev) (model.LogLine, bool, error)`, `svc.FileLog(ctx, rev, path string, limit int) ([]model.FileCommit, error)` (rev "" = HEAD, follows renames), `svc.Blame(ctx, rev, path string) ([]model.BlameLine, error)` (rev "" = working tree, uncached); helpers `writeJSON`, `writeErr`, `isGitArgSafe` (server.go).
- Produces (wire, consumed by Tasks 4-6):
  - `GET /api/resolve?rev=` → `{hash, subject}`; 400 invalid rev, 404 unknown.
  - `GET /api/filelog?path=&rev=&limit=` → `{rows: [{hash, short, subject, author, time, status, path, old_path?}]}`; 400 invalid; empty rows + 200 for a path with no history.
  - `GET /api/blame?path=&rev=` → `{lines: [{hash, short, author, time, summary, line, text}]}`; hash/short "" = not yet committed; 400 invalid, 500 on git blame failure (untracked path, binary).

- [ ] **Step 1: Write the failing tests** (`internal/web/history_test.go`; same package — reuses `gitRun`, `newRepoDir`, `serve`, and `getJSON(t, ts, path, out) int` from `server_test.go`; `getJSON` decodes only on 200 and returns the status code, so error-status checks pass `nil` for out):

```go
package web

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestResolveEndpoint(t *testing.T) {
	dir := newRepoDir(t, 3)
	head := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))

	var r struct{ Hash, Subject string }
	if code := getJSON(t, ts, "/api/resolve?rev=HEAD", &r); code != http.StatusOK {
		t.Fatalf("resolve HEAD: code %d", code)
	}
	if r.Hash != head {
		t.Fatalf("hash = %q, want %q", r.Hash, head)
	}
	if len(r.Hash) != 40 {
		t.Fatalf("want FULL sha, got %d chars", len(r.Hash))
	}
	if r.Subject != "c3" {
		t.Fatalf("subject = %q, want c3", r.Subject)
	}

	if code := getJSON(t, ts, "/api/resolve?rev=nope-nothing", nil); code != http.StatusNotFound {
		t.Fatalf("unknown rev: code %d, want 404", code)
	}
	if code := getJSON(t, ts, "/api/resolve?rev=--all", nil); code != http.StatusBadRequest {
		t.Fatalf("option-shaped rev: code %d, want 400", code)
	}
	if code := getJSON(t, ts, "/api/resolve", nil); code != http.StatusBadRequest {
		t.Fatalf("empty rev: code %d, want 400", code)
	}
}

type fileLogTestRow struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	Time    int64  `json:"time"`
	Status  string `json:"status"`
	Path    string `json:"path"`
	OldPath string `json:"old_path"`
}

func TestFileLogEndpoint(t *testing.T) {
	dir := newRepoDir(t, 3) // c1..c3 each rewriting f.txt
	gitRun(t, dir, "mv", "f.txt", "g.txt")
	gitRun(t, dir, "commit", "-m", "rename it")
	ts := serve(t, New(domain.Open(dir)))

	var r struct{ Rows []fileLogTestRow }
	if code := getJSON(t, ts, "/api/filelog?path=g.txt", &r); code != http.StatusOK {
		t.Fatalf("filelog: code %d", code)
	}
	// newest first: rename, c3, c2, c1 (--follow crosses the rename)
	if len(r.Rows) != 4 {
		t.Fatalf("rows = %d, want 4 (follow must cross the rename): %+v", len(r.Rows), r.Rows)
	}
	if r.Rows[0].Status != "R" || r.Rows[0].OldPath != "f.txt" || r.Rows[0].Path != "g.txt" {
		t.Fatalf("rename row = %+v", r.Rows[0])
	}
	if r.Rows[0].Subject != "rename it" || r.Rows[3].Status != "A" {
		t.Fatalf("order/status wrong: first %+v last %+v", r.Rows[0], r.Rows[3])
	}
	if len(r.Rows[0].Hash) != 40 || len(r.Rows[0].Short) != 8 || r.Rows[0].Time == 0 {
		t.Fatalf("row fields: %+v", r.Rows[0])
	}

	// no history is an EMPTY list, not an error (the TUI "(no history)" rule)
	r.Rows = nil
	if code := getJSON(t, ts, "/api/filelog?path=never-existed.txt", &r); code != http.StatusOK {
		t.Fatalf("no-history path: code %d", code)
	}
	if len(r.Rows) != 0 {
		t.Fatalf("no-history rows = %+v, want empty", r.Rows)
	}

	if code := getJSON(t, ts, "/api/filelog?path=--foo", nil); code != http.StatusBadRequest {
		t.Fatalf("option-shaped path: code %d, want 400", code)
	}
	if code := getJSON(t, ts, "/api/filelog?path=g.txt&rev=--all", nil); code != http.StatusBadRequest {
		t.Fatalf("option-shaped rev: code %d, want 400", code)
	}
}

type blameTestRow struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Author  string `json:"author"`
	Time    int64  `json:"time"`
	Summary string `json:"summary"`
	Line    int    `json:"line"`
	Text    string `json:"text"`
}

func TestBlameEndpoint(t *testing.T) {
	dir := newRepoDir(t, 3)
	// one uncommitted appended line on top of committed content
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("content 3\nuncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	// working-tree blame: rev omitted
	var r struct{ Lines []blameTestRow }
	if code := getJSON(t, ts, "/api/blame?path=f.txt", &r); code != http.StatusOK {
		t.Fatalf("blame: code %d", code)
	}
	if len(r.Lines) != 2 {
		t.Fatalf("lines = %d, want 2: %+v", len(r.Lines), r.Lines)
	}
	if r.Lines[0].Hash == "" || r.Lines[0].Summary != "c3" || r.Lines[0].Text != "content 3" {
		t.Fatalf("committed line = %+v", r.Lines[0])
	}
	if r.Lines[1].Hash != "" || r.Lines[1].Short != "" || r.Lines[1].Text != "uncommitted" {
		t.Fatalf("uncommitted line must have empty hash: %+v", r.Lines[1])
	}

	// blame AT a commit ignores the working tree; a second identical call is
	// served from the blame LRU and must return the same rows (equality, not
	// timing — the cache is an implementation detail)
	var atHead, again struct{ Lines []blameTestRow }
	if code := getJSON(t, ts, "/api/blame?path=f.txt&rev=HEAD", &atHead); code != http.StatusOK {
		t.Fatalf("blame@HEAD: code %d", code)
	}
	if len(atHead.Lines) != 1 || atHead.Lines[0].Text != "content 3" {
		t.Fatalf("blame@HEAD lines = %+v", atHead.Lines)
	}
	if code := getJSON(t, ts, "/api/blame?path=f.txt&rev=HEAD", &again); code != http.StatusOK {
		t.Fatalf("blame@HEAD (repeat): code %d", code)
	}
	if !reflect.DeepEqual(atHead, again) {
		t.Fatalf("repeat blame differs: %+v vs %+v", atHead, again)
	}

	// untracked path: git blame fails -> 500, overlay never opens
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := getJSON(t, ts, "/api/blame?path=untracked.txt", nil); code != http.StatusInternalServerError {
		t.Fatalf("untracked blame: code %d, want 500", code)
	}
	if code := getJSON(t, ts, "/api/blame?path=--foo", nil); code != http.StatusBadRequest {
		t.Fatalf("option-shaped path: code %d, want 400", code)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-commits-parity && go test ./internal/web/ -run "TestResolveEndpoint|TestFileLogEndpoint|TestBlameEndpoint" -v`
Expected: FAIL — handlers/routes undefined (compile error mentioning `handleResolve` comes in Step 3; the first failure is 404s from unregistered routes if you stub, or compile errors — either is the expected red).

- [ ] **Step 3: Implement `internal/web/history.go`:**

```go
package web

import (
	"errors"
	"net/http"
	"strconv"
)

// Commit-feed parity reads: resolve a rev to its full sha (goto-sha), one
// file's commit history (the history overlay), and per-line blame (the blame
// overlay). All read-only GETs — hostGuard applies as everywhere, no
// writeGuard (the /api/compare, /api/versions precedent). rev/path values
// reach git argv, so isGitArgSafe gates each one; an EMPTY rev is legal
// where documented (filelog: HEAD; blame: the working tree) and is not run
// through isGitArgSafe, which rejects empty strings.

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	rev := r.URL.Query().Get("rev")
	if !isGitArgSafe(rev) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid rev"))
		return
	}
	svc := s.service()
	sha, found, err := svc.ResolveRev(r.Context(), rev)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !found {
		writeErr(w, http.StatusNotFound, errors.New("unknown revision: "+rev))
		return
	}
	// Subject is display garnish on the fallback path; a failed lookup only
	// costs the subject, never the resolve.
	subject := ""
	if line, ok, _ := svc.CommitLookup(r.Context(), sha); ok {
		subject = line.Subject
	}
	writeJSON(w, map[string]any{"hash": sha, "subject": subject})
}

type fileLogRow struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	Time    int64  `json:"time"`
	Status  string `json:"status"`   // A M D R C T — the file's change at this commit
	Path    string `json:"path"`     // the file's name AT this commit (post-rename)
	OldPath string `json:"old_path,omitempty"`
}

func (s *Server) handleFileLog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path, rev := q.Get("path"), q.Get("rev")
	if !isGitArgSafe(path) || (rev != "" && !isGitArgSafe(rev)) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path/rev"))
		return
	}
	limit := 200
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 && n <= 1000 {
		limit = n
	}
	fcs, err := s.service().FileLog(r.Context(), rev, path, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]fileLogRow, 0, len(fcs))
	for _, fc := range fcs {
		short := fc.Hash
		if len(short) > 8 {
			short = short[:8]
		}
		rows = append(rows, fileLogRow{
			Hash: fc.Hash, Short: short, Subject: fc.Subject,
			Author: fc.Author, Time: fc.UnixTime,
			Status: fc.Status, Path: fc.Path, OldPath: fc.OldPath,
		})
	}
	writeJSON(w, map[string]any{"rows": rows})
}

type blameRow struct {
	Hash    string `json:"hash"`  // "" = not yet committed
	Short   string `json:"short"` // "" when Hash is ""
	Author  string `json:"author"`
	Time    int64  `json:"time"`
	Summary string `json:"summary"`
	Line    int    `json:"line"`
	Text    string `json:"text"`
}

func (s *Server) handleBlame(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path, rev := q.Get("path"), q.Get("rev")
	if !isGitArgSafe(path) || (rev != "" && !isGitArgSafe(rev)) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path/rev"))
		return
	}
	lines, err := s.service().Blame(r.Context(), rev, path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]blameRow, 0, len(lines))
	for _, l := range lines {
		short := l.Hash
		if len(short) > 8 {
			short = short[:8]
		}
		rows = append(rows, blameRow{
			Hash: l.Hash, Short: short, Author: l.Author,
			Time: l.Time, Summary: l.Summary, Line: l.LineNo, Text: l.Content,
		})
	}
	writeJSON(w, map[string]any{"lines": rows})
}
```

Register in `internal/web/server.go` after the `GET /api/rebase-range` line:

```go
	mux.HandleFunc("GET /api/resolve", s.handleResolve)
	mux.HandleFunc("GET /api/filelog", s.handleFileLog)
	mux.HandleFunc("GET /api/blame", s.handleBlame)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-commits-parity && go test ./internal/web/ -run "TestResolveEndpoint|TestFileLogEndpoint|TestBlameEndpoint" -v`
Expected: PASS. Then the whole package: `go test ./internal/web/` — PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/history.go internal/web/history_test.go internal/web/server.go
git commit -m "feat(web): /api/resolve, /api/filelog, /api/blame read endpoints"
```

---

### Task 3: Client quick filter (`/`)

**Files:**
- Modify: `internal/web/static/index.html` (inside `<section id="commits-pane">`, BEFORE `<div id="commits-scroll">`)
- Modify: `internal/web/static/app.js` (state init; `renderCommits`/`rowHTML`; `loadCommits`; the global keydown; the `#commits-window` click listener; new filter functions)
- Modify: `internal/web/static/style.css`

**Interfaces:**
- Consumes: existing `state.rows` (feed rows `{hash, short, subject, author, time, cells, refs, parents}`), `renderCommits()`, `rowHTML(row, i)`, `flatDotSVG(color)`, `laneColor(i)`, `runes()`, `wtCount()`, `loadCommits(more)`, `state.canLoadMore`, `ROW_H`, `esc()`.
- Produces: `state.cfilter` (`null` or `{q, matches: [feedIdx…]}`), `openCommitFilter()`, `closeCommitFilter()`, `applyCommitFilter()` — Task 4's `revealCommit` calls `closeCommitFilter()`; `rowHTML(row, i, flat)` gains an optional third param.

- [ ] **Step 1: Markup** — in `index.html` inside `<section id="commits-pane" class="pane focused">`, before `#commits-scroll`:

```html
    <div id="cfilter" class="hidden">
      <input id="cfilter-input" type="text" autocomplete="off" spellcheck="false"
             placeholder="filter loaded commits — subject, author, sha">
      <span id="cfilter-count"></span>
    </div>
```

- [ ] **Step 2: CSS** — append to `style.css`:

```css
/* commits quick filter (/) — narrows LOADED rows only; the hint row makes
   coverage explicit and deeper paging an explicit click. */
#cfilter { display: flex; gap: 8px; align-items: center; padding: 4px 8px; border-bottom: 1px solid var(--border); }
#cfilter.hidden { display: none; }
#cfilter input { flex: 1; background: var(--bg-alt); color: var(--fg); border: 1px solid var(--border); border-radius: 4px; padding: 3px 8px; font: inherit; }
#cfilter input:focus { border-color: var(--accent); outline: none; }
#cfilter-count { color: var(--dim); font-size: 11px; white-space: nowrap; }
.crow.hintrow { color: var(--dim); cursor: default; }
.crow.hintrow a { color: var(--accent); cursor: pointer; }
```

- [ ] **Step 3: app.js — filter state + functions.** Add `cfilter: null,` to the `state` object literal (find it: `grep -n "const state = {" app.js`). Add near `renderCommits`:

```js
// --- commits quick filter (/) ----------------------------------------------
// Client-only narrowing of the LOADED feed rows: case-insensitive substring
// on subject and author, sha PREFIX when the query is hex. Filtered rows
// render flat — lanes are meaningless on a subset. Deeper search is always
// an explicit click on the hint row, never an automatic git walk.
function openCommitFilter() {
  if (state.layout === "diff") return; // commits pane is off-screen
  $("cfilter").classList.remove("hidden");
  const input = $("cfilter-input");
  input.value = state.cfilter ? state.cfilter.q : "";
  applyCommitFilter();
  input.focus();
}

function closeCommitFilter() {
  const open = !$("cfilter").classList.contains("hidden");
  if (!open && !state.cfilter) return;
  state.cfilter = null;
  $("cfilter-input").value = "";
  $("cfilter-input").blur(); // a focused input would trap all global keys
  $("cfilter").classList.add("hidden");
  // moveCursor(0) re-renders AND rescrolls to the selected row — but only
  // steers the commits list while that pane has focus; otherwise plain render.
  if (state.pane === "commits") moveCursor(0);
  else renderCommits();
}

function applyCommitFilter() {
  const q = $("cfilter-input").value.trim().toLowerCase();
  if (!q) {
    state.cfilter = null; // empty query = unfiltered, bar stays open
    $("cfilter-count").textContent = "";
    renderCommits();
    return;
  }
  const hexish = /^[0-9a-f]+$/.test(q);
  const matches = [];
  state.rows.forEach((r, i) => {
    if (
      r.subject.toLowerCase().includes(q) ||
      (r.author || "").toLowerCase().includes(q) ||
      (hexish && r.hash.startsWith(q))
    )
      matches.push(i);
  });
  state.cfilter = { q, matches };
  $("cfilter-count").textContent = matches.length + " / " + state.rows.length;
  $("commits-scroll").scrollTop = 0;
  renderCommits();
}

// Filtered render: the same virtualized window, over the match list, plus a
// trailing hint row stating coverage. The working-tree row is not a commit
// and stays out of a filtered list.
function renderFilteredCommits() {
  const scroll = $("commits-scroll");
  const m = state.cfilter.matches;
  const total = m.length + 1; // + hint row
  $("commits-spacer").style.height = total * ROW_H + "px";
  const first = Math.max(0, Math.floor(scroll.scrollTop / ROW_H) - 10);
  const last = Math.min(total, Math.ceil((scroll.scrollTop + scroll.clientHeight) / ROW_H) + 10);
  const win = $("commits-window");
  win.style.top = first * ROW_H + "px";
  let html = "";
  for (let i = first; i < last; i++) {
    if (i === m.length) {
      const tail = state.canLoadMore
        ? ` — <a id="cfilter-more">load more</a>`
        : " — all loaded commits searched";
      html += `<div class="crow hintrow">${m.length} of ${state.rows.length} loaded commits match${tail}</div>`;
      continue;
    }
    html += rowHTML(state.rows[m[i]], m[i] + wtCount(), true);
  }
  win.innerHTML = html;
}
```

- [ ] **Step 4: app.js — wire into existing functions.**

`renderCommits()` first line:

```js
function renderCommits() {
  if (state.cfilter) return renderFilteredCommits();
  ...
```

`rowHTML` gains a `flat` param — the graph cell becomes:

```js
function rowHTML(row, i, flat) {
  ...
  const graph = flat
    ? (() => { const col = runes(row.cells || "").indexOf("●"); return flatDotSVG(laneColor(col >= 0 ? col >> 1 : 0)); })()
    : graphHTML(row, i - wtCount());
  ...
    `<span class="graph">${graph}</span>` +
```

(Existing two callers pass no third arg — unchanged behavior.)

`loadCommits` — recompute matches instead of rendering stale ones:

```js
  setSoloChip(body.solo || "");
  if (state.cfilter) applyCommitFilter(); // recompute over the grown/reloaded feed (ends in renderCommits)
  else renderCommits();
```

Global keydown (after the `"s"/"u"` branch):

```js
  } else if (e.key === "/") {
    e.preventDefault(); // the browser's quick-find would grab it
    openCommitFilter();
  }
```

Filter input events (place with the other listeners at the bottom):

```js
$("cfilter-input").addEventListener("input", applyCommitFilter);
// Escape must be handled HERE: the global router's form-field guard eats
// every key typed in an input, so it can never see this one.
$("cfilter-input").addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    e.preventDefault();
    closeCommitFilter();
  }
});
```

`#commits-window` click listener — the hint row has no `data-i`; guard and handle load-more:

```js
$("commits-window").addEventListener("click", async (e) => {
  if (e.target.id === "cfilter-more") {
    await loadCommits(true); // appends server-side; loadCommits re-filters
    return;
  }
  const row = e.target.closest(".crow");
  if (row && row.dataset.i !== undefined) openCommit(Number(row.dataset.i));
});
```

Re-root must drop the filter (spec: never survives a re-root): find `function doReroot` and add `closeCommitFilter();` as its first statement.

- [ ] **Step 5: Build + sanity**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-commits-parity && go build ./cmd/gg && go vet ./internal/web/...`
Expected: clean. (Static files are embedded; a build proves embedding still works. Browser behavior is verified by the controller's CDP pass.)

- [ ] **Step 6: Commit**

```bash
git add internal/web/static/index.html internal/web/static/app.js internal/web/static/style.css
git commit -m "feat(web): / quick filter over loaded commit rows"
```

---

### Task 4: Client goto-sha (`#` + palette)

**Files:**
- Modify: `internal/web/static/app.js` (new functions; palette row; global keydown; `rowHTML` flash class; `doReroot`)
- Modify: `internal/web/static/style.css` (flash animation)

**Interfaces:**
- Consumes: `/api/resolve` (Task 2), `closeCommitFilter()` (Task 3), existing `openPrompt`, `openCommitByHash(hash, title)`, `loadCommits(more)`, `opLine(text, isErr)`, `wtCount()`, `wtExtra()`, `ROW_H`, `state.canLoadMore`, `paletteCommands()`.
- Produces: `gotoCommitPrompt()`, `gotoCommit(rev)`, `revealCommit(feedIdx)`, `state.gotoGen`, `state.flashHash`.

- [ ] **Step 1: CSS** — append:

```css
/* goto-sha reveal: a brief flash so the eye lands on the revealed row */
.crow.flash { animation: gflash 1.6s ease-out; }
@keyframes gflash {
  0%, 55% { background: var(--sel); }
  100% { background: transparent; }
}
```

- [ ] **Step 2: app.js** — add `gotoGen: 0, flashHash: "",` to the `state` literal. New functions near `openCommitByHash`:

```js
// --- goto commit (#) --------------------------------------------------------
// Reveal-first: the point is the commit IN ITS PLACE in history. Paging stops
// the moment a page adds nothing (feed exhausted — e.g. a solo scope that
// excludes the commit), then falls back to opening the detail directly so the
// user always lands on the commit.
function gotoCommitPrompt() {
  openPrompt({
    title: "Goto commit — sha, branch, tag, or any rev",
    placeholder: "e.g. a1b2c3d or main~3",
    onSubmit: (rev) => gotoCommit(rev),
  });
}

async function gotoCommit(rev) {
  let res;
  try {
    res = await getJSON("/api/resolve?rev=" + encodeURIComponent(rev));
  } catch (e) {
    opLine("cannot resolve " + rev + ": " + (e.message || e), true);
    return;
  }
  const gen = ++state.gotoGen;
  for (;;) {
    const idx = state.rows.findIndex((r) => r.hash === res.hash);
    if (idx >= 0) return revealCommit(idx);
    if (!state.canLoadMore) break;
    const before = state.rows.length;
    await loadCommits(true);
    if (gen !== state.gotoGen) return; // superseded: re-root or a second goto
    if (state.rows.length === before) break; // no growth: feed exhausted
  }
  opLine("commit is not in the current list (scope?) — opening its detail", false);
  openCommitByHash(res.hash, res.hash.slice(0, 8) + " " + (res.subject || ""));
}

function revealCommit(feedIdx) {
  closeCommitFilter(); // reveal happens in the FULL list
  const i = feedIdx + wtCount();
  state.cursor = i;
  const scroll = $("commits-scroll");
  scroll.scrollTop = Math.max(0, i * ROW_H + wtExtra() - scroll.clientHeight / 2);
  state.flashHash = state.rows[feedIdx].hash;
  renderCommits();
  setTimeout(() => {
    state.flashHash = "";
    renderCommits();
  }, 1700);
}
```

`rowHTML` — add the flash class beside `sel`:

```js
  const fl = row.hash === state.flashHash ? " flash" : "";
  ...
    `<div class="crow${sel}${fl}" data-i="${i}">` +
```

Global keydown (beside the `/` branch): `} else if (e.key === "#") { gotoCommitPrompt(); }`

Palette row in `paletteCommands()` (before "refresh"): `{ label: "goto commit…", detail: "#", run: () => gotoCommitPrompt() },`

`doReroot`: add `state.gotoGen++;` next to the `closeCommitFilter()` call from Task 3 (a goto mid-flight across a re-root must die).

- [ ] **Step 3: Build + sanity**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-commits-parity && go build ./cmd/gg && go vet ./internal/web/...`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add internal/web/static/app.js internal/web/static/style.css
git commit -m "feat(web): goto-sha reveals the commit in the list (# / palette)"
```

---

### Task 5: File history overlay + `diffHTML` extraction

**Files:**
- Modify: `internal/web/static/app.js` (extract `diffHTML(d, paneWidth)` from `renderDiff`; new history overlay code; palette row)
- Modify: `internal/web/static/index.html` (the `#history` overlay, next to `#versions`)
- Modify: `internal/web/static/style.css`

**Interfaces:**
- Consumes: `/api/filelog` (Task 2), existing `/api/diff` COMMIT form (`sha`,`path`,`status`,`old`), `pushLayer`/`closeLayer`, `openPrompt`, `versionWhen(unix)`, `esc()`, `getJSON`, `openCommitByHash`.
- Produces: `openFileHistory(path, rev)` (Task 7 wires more entry points), `diffHTML(d, paneWidth) -> html string` (also used by nothing else today, but it is the seam that keeps renderDiff single-source).

- [ ] **Step 1: Extract `diffHTML`.** In app.js, `renderDiff(d)` (~line 2556) both builds HTML and writes `#diff-body`. Split it:

```js
// diffHTML builds the diff table for a /api/diff response — shared by the
// main diff pane and the history overlay. paneWidth picks side-by-side vs
// unified exactly as before. Hunk classes no-op when diffHunks is null, so
// non-staging consumers get a plain read-only table.
function diffHTML(d, paneWidth) {
  if (d.binary) return `<div class="notice">binary file</div>`;
  if (d.too_large) return `<div class="notice">diff too large</div>`;
  const rows = d.rows || [];
  /* … the ENTIRE existing html-building body of renderDiff, verbatim,
     with `$("diff-pane").clientWidth < 950` replaced by `paneWidth < 950`,
     ending with `return html + "</table>";` (match the existing closing) … */
}

function renderDiff(d) {
  state.lastDiff = d; // re-rendered on window resize (layout is width-dependent)
  state.diffBlockIdx = -1;
  $("diff-body").innerHTML = diffHTML(d, $("diff-pane").clientWidth);
  updateDiffNav();
}
```

Read the existing `renderDiff` to its end first — anything after the table close (scroll reset, hunk bar refresh) STAYS in `renderDiff`. The extraction must be behavior-neutral for the main pane.

- [ ] **Step 2: Markup** — in `index.html`, after the `#vbranches` block:

```html
<div id="history" class="hidden">
  <div id="history-box">
    <div id="history-title"></div>
    <div id="history-main">
      <ul id="history-list"></ul>
      <div id="history-diff"></div>
    </div>
    <div id="history-hint">click a commit for the file's change there · show opens the full commit · esc closes</div>
  </div>
</div>
```

- [ ] **Step 3: CSS** — append (same overlay family as `#versions`; near-fullscreen because a diff needs room):

```css
/* file-history overlay: commits left, that commit's change to the file right */
#history { position: fixed; inset: 0; background: rgba(0,0,0,.55); display: flex; align-items: center; justify-content: center; z-index: 21; }
#history.hidden { display: none; }
#history-box { background: var(--bg-alt); border: 1px solid var(--accent); border-radius: 6px; width: 94vw; height: 88vh; display: flex; flex-direction: column; }
#history-title { padding: 10px 14px; border-bottom: 1px solid var(--border); }
#history-hint { padding: 6px 14px; border-top: 1px solid var(--border); color: var(--dim); font-size: 11px; }
#history-main { display: flex; flex: 1; min-height: 0; }
#history-list { list-style: none; margin: 0; padding: 6px 0; width: 360px; flex: none; overflow-y: auto; border-right: 1px solid var(--border); }
#history-list li { padding: 5px 14px; cursor: pointer; }
#history-list li:hover { background: var(--sel); }
#history-list li.sel { background: var(--sel); }
#history-list li.empty { color: var(--dim); cursor: default; }
#history-list .hsubj { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
#history-list .hmeta { display: block; color: var(--dim); font-size: 11px; }
#history-list .hshow { float: right; background: none; color: var(--dim); border: 1px solid var(--border); border-radius: 4px; font-size: 10px; cursor: pointer; }
#history-list .hshow:hover { border-color: var(--accent); color: var(--accent); }
#history-diff { flex: 1; overflow: auto; }
```

- [ ] **Step 4: app.js — overlay code** (place after the versions-overlay section):

```js
// --- file history overlay ----------------------------------------------------
// A layer, not a layout mode: esc drops you exactly where you were. Gen-guarded
// like every async open (a slow filelog racing a close must not resurrect it).
let hist = null; // {path, rev, rows, sel, gen}
let histGen = 0;

async function openFileHistory(path, rev) {
  const gen = ++histGen;
  hist = { path, rev: rev || "", rows: [], sel: 0, gen };
  $("history-title").textContent = "history — " + path + (rev ? " @ " + rev.slice(0, 8) : "");
  $("history-list").innerHTML = `<li class="empty">loading…</li>`;
  $("history-diff").innerHTML = "";
  pushLayer("history", $("history"), { onKey: historyKey });
  let body;
  try {
    body = await getJSON(
      "/api/filelog?path=" + encodeURIComponent(path) + "&rev=" + encodeURIComponent(rev || "")
    );
  } catch (e) {
    if (hist && hist.gen === gen)
      $("history-list").innerHTML = `<li class="empty">error: ${esc(e.message || e)}</li>`;
    return;
  }
  if (!hist || hist.gen !== gen) return; // closed or superseded meanwhile
  hist.rows = body.rows || [];
  if (!hist.rows.length) {
    $("history-list").innerHTML = `<li class="empty">(no history)</li>`;
    return;
  }
  renderHistoryList();
  openHistoryDiff(0);
}

function closeHistory() {
  hist = null;
  closeLayer("history");
}

function historyKey(e) {
  if (e.key === "Escape") {
    closeHistory();
    return true;
  }
  if (["j", "ArrowDown", "k", "ArrowUp"].includes(e.key)) {
    if (hist && hist.rows.length) {
      const d = e.key === "j" || e.key === "ArrowDown" ? 1 : -1;
      openHistoryDiff(Math.max(0, Math.min(hist.rows.length - 1, hist.sel + d)));
    }
    e.preventDefault();
    return true;
  }
  return true; // the overlay owns the keyboard entirely while open
}

function renderHistoryList() {
  $("history-list").innerHTML = hist.rows
    .map(
      (r, i) =>
        `<li data-i="${i}" class="${i === hist.sel ? "sel" : ""}">` +
        `<button class="hshow" data-i="${i}">show</button>` +
        `<span class="hsubj"><span class="st ${esc(r.status)}">${esc(r.status)}</span> ${esc(r.subject)}</span>` +
        `<span class="hmeta">${esc(r.short)} · ${esc(r.author)} · ${versionWhen(r.time)}</span></li>`
    )
    .join("");
  const sel = $("history-list").querySelector("li.sel");
  if (sel) sel.scrollIntoView({ block: "nearest" });
}

async function openHistoryDiff(i) {
  hist.sel = i;
  renderHistoryList();
  const r = hist.rows[i];
  const gen = hist.gen;
  // The /api/diff COMMIT form is already parent-vs-commit with A/D handling —
  // exactly "this file's change at this commit". path is the file's name AT
  // that commit (post-rename), old the parent-side name.
  const q = new URLSearchParams({ sha: r.hash, path: r.path || hist.path, status: r.status });
  if (r.old_path) q.set("old", r.old_path);
  $("history-diff").innerHTML = `<div class="notice">loading…</div>`;
  try {
    const d = await getJSON("/api/diff?" + q);
    if (!hist || hist.gen !== gen) return;
    $("history-diff").innerHTML = diffHTML(d, $("history-diff").clientWidth);
  } catch (e) {
    if (hist && hist.gen === gen)
      $("history-diff").innerHTML = `<div class="notice">error: ${esc(e.message || e)}</div>`;
  }
}

$("history-list").addEventListener("click", (e) => {
  if (!hist) return;
  const show = e.target.closest("button.hshow");
  if (show) {
    const r = hist.rows[Number(show.dataset.i)];
    closeHistory();
    openCommitByHash(r.hash, r.short + " " + r.subject);
    return;
  }
  const li = e.target.closest("li[data-i]");
  if (li) openHistoryDiff(Number(li.dataset.i));
});
$("history").addEventListener("click", (e) => {
  if (e.target.id === "history") closeHistory(); // backdrop closes, box does not
});
```

Palette row in `paletteCommands()`:

```js
    { label: "file history…", detail: "", run: () => openPrompt({ title: "File history — repo-relative path", placeholder: "e.g. internal/web/server.go", onSubmit: (p) => openFileHistory(p, "") }) },
```

- [ ] **Step 5: Build + sanity**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-commits-parity && go build ./cmd/gg && go vet ./internal/web/...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/web/static/index.html internal/web/static/app.js internal/web/static/style.css
git commit -m "feat(web): file-history overlay over /api/filelog + diffHTML extraction"
```

---

### Task 6: Blame overlay

**Files:**
- Modify: `internal/web/static/index.html` (the `#blame` overlay, after `#history`)
- Modify: `internal/web/static/app.js` (overlay code; palette row)
- Modify: `internal/web/static/style.css`

**Interfaces:**
- Consumes: `/api/blame` (Task 2), `pushLayer`/`closeLayer`, `openPrompt`, `openCommitByHash`, `versionWhen`, `esc()`, `opLine`.
- Produces: `openFileBlame(path, rev)` (Task 7 wires more entry points).

- [ ] **Step 1: Markup** — after the `#history` block:

```html
<div id="blame" class="hidden">
  <div id="blame-box">
    <div id="blame-title"></div>
    <div id="blame-body"></div>
    <div id="blame-hint">click a line's commit to open it · esc closes</div>
  </div>
</div>
```

- [ ] **Step 2: CSS:**

```css
/* blame overlay: gutter (sha · author · age) + source line; repeated gutters
   blank so commit blocks read as groups */
#blame { position: fixed; inset: 0; background: rgba(0,0,0,.55); display: flex; align-items: center; justify-content: center; z-index: 21; }
#blame.hidden { display: none; }
#blame-box { background: var(--bg-alt); border: 1px solid var(--accent); border-radius: 6px; width: 94vw; height: 88vh; display: flex; flex-direction: column; }
#blame-title { padding: 10px 14px; border-bottom: 1px solid var(--border); }
#blame-hint { padding: 6px 14px; border-top: 1px solid var(--border); color: var(--dim); font-size: 11px; }
#blame-body { flex: 1; overflow: auto; font-family: ui-monospace, monospace; font-size: 12px; padding: 6px 0; }
.bline { display: flex; white-space: pre; }
.bline.bfirst { border-top: 1px solid var(--border); }
.bgut { flex: none; width: 300px; overflow: hidden; text-overflow: ellipsis; color: var(--dim); padding-left: 10px; }
.bsha { color: var(--accent); cursor: pointer; text-decoration: underline dotted; }
.bwork { color: var(--dim); font-style: italic; }
.bno { flex: none; width: 4em; text-align: right; color: var(--dim); padding-right: 10px; }
.btext { flex: 1; }
```

- [ ] **Step 3: app.js** (after the history section):

```js
// --- blame overlay -----------------------------------------------------------
// Fetch-then-open: a blame failure (untracked path, binary) surfaces on the
// status line and the overlay never opens — nothing worse than an empty modal.
async function openFileBlame(path, rev) {
  let body;
  try {
    body = await getJSON(
      "/api/blame?path=" + encodeURIComponent(path) + "&rev=" + encodeURIComponent(rev || "")
    );
  } catch (e) {
    opLine("blame failed: " + (e.message || e), true);
    return;
  }
  $("blame-title").textContent = "blame — " + path + (rev ? " @ " + rev.slice(0, 8) : " (working tree)");
  const lines = body.lines || [];
  let html = "";
  let prev = null;
  for (const l of lines) {
    const first = l.hash !== prev;
    prev = l.hash;
    const gut = !first
      ? ""
      : l.hash
        ? `<span class="bsha" data-h="${esc(l.hash)}" title="${esc(l.summary)}">${esc(l.short)}</span> ${esc(l.author)} · ${versionWhen(l.time)}`
        : `<span class="bwork">working</span>`;
    html +=
      `<div class="bline${first ? " bfirst" : ""}">` +
      `<span class="bgut">${gut}</span>` +
      `<span class="bno">${l.line}</span>` +
      `<span class="btext">${esc(l.text) || " "}</span></div>`;
  }
  $("blame-body").innerHTML = html || `<div class="notice">(empty file)</div>`;
  pushLayer("blame", $("blame"), {}); // no onKey: the stack's default esc-closes applies
  $("blame-body").scrollTop = 0;
}

$("blame-body").addEventListener("click", (e) => {
  const sha = e.target.closest(".bsha");
  if (!sha) return;
  closeLayer("blame");
  openCommitByHash(sha.dataset.h, sha.dataset.h.slice(0, 8));
});
$("blame").addEventListener("click", (e) => {
  if (e.target.id === "blame") closeLayer("blame"); // backdrop closes, box does not
});
```

Palette row in `paletteCommands()`:

```js
    { label: "file blame…", detail: "", run: () => openPrompt({ title: "File blame — repo-relative path", placeholder: "e.g. internal/web/server.go", onSubmit: (p) => openFileBlame(p, "") }) },
```

- [ ] **Step 4: Build + sanity**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-commits-parity && go build ./cmd/gg && go vet ./internal/web/...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/web/static/index.html internal/web/static/app.js internal/web/static/style.css
git commit -m "feat(web): blame overlay over /api/blame"
```

---

### Task 7: Entry points — file-row ctx menus + diff-toolbar buttons

**Files:**
- Modify: `internal/web/static/app.js` (the `#files-list` contextmenu listener ~line 3104; `openFile`/`openStatusDiff`/`enterFilesStage`/`exitStatusToList`; `updateDiffNav` ~line 2631; new button listeners)
- Modify: `internal/web/static/index.html` (`#diff-nav` span, ~line 63)

**Interfaces:**
- Consumes: `openFileHistory(path, rev)` (Task 5), `openFileBlame(path, rev)` (Task 6), existing `showCtxMenu(items, x, y)`, `copyText`, `state.files`/`state.statusEntries`/`state.filesMode`/`state.fileSha`/`state.compare`, `renderFiles()`, `updateDiffNav()`.
- Produces: `state.diffCtx` (`null` or `{path, rev}` — the file the diff pane currently shows), ctx-menu rows on all file lists, `#hist-btn`/`#blame-btn`.

- [ ] **Step 1: ctx menu for commit/compare file rows.** The existing `#files-list` contextmenu listener returns early for non-status modes. Restructure its head (keep the whole status body unchanged below):

```js
$("files-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li || li.dataset.i === undefined) return;
  if (state.filesMode !== "status") {
    // commit / compare rows: read-only file actions. rev picks what "here"
    // means — the commit being viewed, or the compare's right tip.
    const f = state.files[Number(li.dataset.i)];
    if (!f) return;
    e.preventDefault();
    state.fileCursor = Number(li.dataset.i);
    renderFiles();
    const rev = state.filesMode === "compare" ? state.compare.bHash : f.sha || state.fileSha;
    showCtxMenu(
      [
        { label: "file history", act: () => openFileHistory(f.path, rev) },
        { label: "blame at this commit", act: () => openFileBlame(f.path, rev) },
        { label: "copy path", act: () => copyText(f.path) },
      ],
      e.clientX,
      e.clientY
    );
    return;
  }
  /* …existing status-mode body unchanged… */
});
```

- [ ] **Step 2: status-mode rows.** Inside the existing status branch, right after its `copy path` row:

```js
  items.push({ label: "file history", act: () => openFileHistory(f.path, "") });
  items.push({ label: "blame (working tree)", act: () => openFileBlame(f.path, "") });
```

(Keep the destructive discard/delete rows LAST — the standing ctx-menu rule; these two insert before them because they follow `copy path` which already precedes the destructive block. Verify the final order: actions, copy path, history, blame, mass rows, destructive rows is acceptable — destructive still last.)

- [ ] **Step 3: diff-toolbar buttons.** In `index.html`, inside `<span id="diff-nav">` after the `next-change` button:

```html
        <button id="hist-btn" title="this file's history">history</button>
        <button id="blame-btn" title="blame this file">blame</button>
```

app.js: add `diffCtx: null,` to the `state` literal. Set it where a file diff opens, clear it where the diff pane empties:

- In `openFile(i)` (non-status branch, right after `const f = state.files[i];`):
  ```js
  state.diffCtx = { path: f.path, rev: state.filesMode === "compare" ? state.compare.bHash : f.sha || state.fileSha };
  ```
- In `openStatusDiff(i)` (right after `const f = state.statusEntries[i];`):
  ```js
  state.diffCtx = f.section === "conflicts" ? null : { path: f.path, rev: "" };
  ```
  (A conflict picker is not a plain file diff; the buttons stay disabled there.)
- In `enterFilesStage()` and `exitStatusToList()`, next to the existing `state.lastDiff = null;`: `state.diffCtx = null;`

`updateDiffNav()` — append:

```js
  $("hist-btn").disabled = $("blame-btn").disabled = !state.diffCtx;
```

Listeners (with the other button listeners):

```js
$("hist-btn").addEventListener("click", () => {
  if (state.diffCtx) openFileHistory(state.diffCtx.path, state.diffCtx.rev);
});
$("blame-btn").addEventListener("click", () => {
  if (state.diffCtx) openFileBlame(state.diffCtx.path, state.diffCtx.rev);
});
```

- [ ] **Step 4: Build + sanity**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-commits-parity && go build ./cmd/gg && go vet ./internal/web/...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/web/static/index.html internal/web/static/app.js
git commit -m "feat(web): history/blame entry points — file ctx menus + diff toolbar"
```

---

### Task 8: CHANGELOG + package-map doc

**Files:**
- Modify: `CHANGELOG.md` (new entry at the top, matching the existing entry style — read the top two entries first)
- Modify: `CLAUDE.md` (the `web` row of the package map: append a sentence)

- [ ] **Step 1: CHANGELOG entry** (top of file, today's date, following the file's existing format):

```markdown
## 2026-08-01 — web: commits-panel parity (/ filter, goto-sha, file history/blame)

- `/` quick filter narrows the LOADED commit rows (subject/author substring,
  sha prefix); filtered rows render flat; a hint row states coverage and
  offers explicit deeper paging — never an automatic git walk.
- `#` / palette "goto commit…": `GET /api/resolve` (new, full-sha via the new
  `domain.ResolveRev`) then reveal-in-list with flash highlight, paging until
  the feed stops growing; a commit outside the current scope falls back to
  opening its detail.
- File history (`GET /api/filelog` over `domain.FileLog`) and blame
  (`GET /api/blame` over `domain.Blame`) as full-screen overlays; entry
  points: file-row right-click (commit/compare/status modes), palette path
  prompts, and history/blame buttons in the diff toolbar. The history diff
  reuses the /api/diff commit form; `diffHTML` extracted from `renderDiff`
  so both panes share one renderer.
```

- [ ] **Step 2: CLAUDE.md** — in the package-map `web` row, append:

```
**Commits-panel parity:** `/` quick filter (client-only narrowing of loaded rows; flat rendering; explicit load-more hint row), `#`/palette goto-sha (`GET /api/resolve` → reveal-in-list with fallback to detail), and file history/blame overlays (`GET /api/filelog`, `GET /api/blame` — read-only GETs over `domain.FileLog`/`Blame`; `domain.ResolveRev` is the full-sha resolve `CommitLookup` can't provide). Entry points: file-row ctx menus in all three files modes, palette path prompts, diff-toolbar buttons driven by `state.diffCtx`. The history overlay reuses the `/api/diff` commit form for parent-vs-commit file diffs; `diffHTML(d, paneWidth)` is the extracted shared diff renderer.
```

- [ ] **Step 3: Full test suite**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-commits-parity && ./test.sh unit`
Expected: PASS (e2e unchanged by this feature; the controller runs the full `./test.sh race` gate before merge).

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md CLAUDE.md
git commit -m "docs: changelog + package map for web commits-panel parity"
```

---

## Post-plan (controller work, not tasks)

- CDP browser verification (both loopback hosts, `elementFromPoint` visibility, old-build regression runs ×2): filter narrows + hint pages deeper; `#` reveals + flashes an unloaded commit; goto falls back under solo; history overlay from a file row shows a visible diff (incl. a rename row and the root/A row); blame overlay from the palette; esc closes each overlay back to the prior screen.
- `./test.sh race` on a quiet machine, then ask the user before merging into web-dev; `./build.sh web` in the web-dev worktree after the merge; repair/flip the worktree link per the environment gotcha.
