# Web Track B (worktrees + tags panels) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Worktrees and tags in the web sidebar (read-only), tags linking into commit detail.

**Architecture:** Two new read endpoints over existing domain queries, three-section sidebar, and `openCommitByHash` for feed-independent commit detail.

**Tech Stack:** Go stdlib + `internal/domain`; hand-written JS.

**Spec:** `docs/superpowers/specs/2026-07-23-web-panels-design.md`. Read it first.

## Global Constraints

- Branch `feat/web-panels` (worktree `/mnt/t/others/gigagit.worktrees/feat-web-panels`), off `web-dev`. Merge target = web-dev (controller). NEVER main; never push.
- **Parallel-track discipline:** track A is concurrently editing `ophttp.go`, the files-pane/status region, and the keydown handler. Touch ONLY: NEW files `internal/web/worktrees.go`/`tags.go`(+tests), `internal/web/server.go` (two route lines), `static/index.html` (sidebar region only), `static/style.css` (append only), `static/app.js` (`fetchBranches` body, `loadRepo` one line, `renderBranches` region additions, new functions — NOT the keydown handler, NOT `startSwitch`/op client, NOT `renderFiles`/status code), `CHANGELOG.md`, `CLAUDE.md`.
- Tags capped at `maxTagRows = 100` + `truncated` flag. Both endpoints read-only (no writeGuard).
- `internal/web` never imports `internal/git`; JSON field names are contract; real-git tests.
- Commands run from the worktree root.

## File Structure

| File | Responsibility |
|---|---|
| `internal/web/worktrees.go` (new) | `GET /api/worktrees` |
| `internal/web/tags.go` (new) | `GET /api/tags` (cap + truncated) |
| `internal/web/panels_test.go` (new) | both endpoints' tests |
| `internal/web/server.go` (modify) | two routes |
| `static/index.html` (modify) | sidebar sections |
| `static/app.js` (modify) | sidebar loader + renders + `openCommitByHash` |
| `static/style.css` (modify) | section styles (append) |
| `CHANGELOG.md`, `CLAUDE.md` (modify) | docs |

---

### Task 1: the two endpoints

**Files:**
- Create: `internal/web/worktrees.go`, `internal/web/tags.go`, `internal/web/panels_test.go`
- Modify: `internal/web/server.go` (two route lines)

**Interfaces:**
- Consumes: `s.svc.Worktrees(ctx) ([]model.Worktree, error)` (`model.Worktree{Path, Branch, Head string; Detached, Bare bool}`); `s.svc.Tags(ctx) ([]model.Tag, error)` (`model.Tag{Name, Target string; Annotated bool; Subject string}`); `writeJSON`/`writeErr`; test helpers `newRepoDir`, `gitRun`, `serve`, `getJSON`.
- Produces: `GET /api/worktrees` → `{"worktrees":[{path,branch,head,detached,bare}]}`; `GET /api/tags` → `{"tags":[{name,target,annotated,subject}],"truncated":bool}`. Task 2's JS consumes both.

- [ ] **Step 1: Write the failing tests**

Create `internal/web/panels_test.go`:

```go
package web

import (
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

type worktreesResp struct {
	Worktrees []struct {
		Path     string `json:"path"`
		Branch   string `json:"branch"`
		Head     string `json:"head"`
		Detached bool   `json:"detached"`
		Bare     bool   `json:"bare"`
	} `json:"worktrees"`
}

type tagsResp struct {
	Tags []struct {
		Name      string `json:"name"`
		Target    string `json:"target"`
		Annotated bool   `json:"annotated"`
		Subject   string `json:"subject"`
	} `json:"tags"`
	Truncated bool `json:"truncated"`
}

func TestWorktreesEndpoint(t *testing.T) {
	dir := newRepoDir(t, 1)
	second := filepath.Join(t.TempDir(), "wt2")
	gitRun(t, dir, "worktree", "add", "-b", "w2", second)
	ts := serve(t, New(domain.Open(dir)))

	var body worktreesResp
	if code := getJSON(t, ts, "/api/worktrees", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(body.Worktrees) != 2 {
		t.Fatalf("worktrees = %+v", body.Worktrees)
	}
	byBranch := map[string]bool{}
	for _, w := range body.Worktrees {
		if w.Path == "" || w.Head == "" {
			t.Errorf("missing path/head: %+v", w)
		}
		if w.Bare || w.Detached {
			t.Errorf("unexpected bare/detached: %+v", w)
		}
		byBranch[w.Branch] = true
	}
	if !byBranch["main"] || !byBranch["w2"] {
		t.Errorf("branches seen = %v, want main and w2", byBranch)
	}
}

func TestTagsEndpoint(t *testing.T) {
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "tag", "light1")
	gitRun(t, dir, "tag", "light2")
	gitRun(t, dir, "tag", "-a", "annot1", "-m", "release notes here")
	ts := serve(t, New(domain.Open(dir)))

	var body tagsResp
	if code := getJSON(t, ts, "/api/tags", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if body.Truncated {
		t.Error("truncated=true for 3 tags")
	}
	if len(body.Tags) != 3 {
		t.Fatalf("tags = %+v", body.Tags)
	}
	seenAnnot := false
	for _, tg := range body.Tags {
		if tg.Name == "" || tg.Target == "" {
			t.Errorf("missing name/target: %+v", tg)
		}
		if tg.Name == "annot1" {
			seenAnnot = true
			if !tg.Annotated || tg.Subject != "release notes here" {
				t.Errorf("annot1 = %+v", tg)
			}
		}
	}
	if !seenAnnot {
		t.Error("annot1 missing")
	}
}

func TestTagsEndpointCap(t *testing.T) {
	dir := newRepoDir(t, 1)
	for i := 0; i < 105; i++ {
		gitRun(t, dir, "tag", fmt.Sprintf("t%03d", i))
	}
	ts := serve(t, New(domain.Open(dir)))
	var body tagsResp
	if code := getJSON(t, ts, "/api/tags", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(body.Tags) != 100 {
		t.Errorf("len = %d, want 100 (maxTagRows)", len(body.Tags))
	}
	if !body.Truncated {
		t.Error("truncated = false, want true")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web/ -run 'TestWorktreesEndpoint|TestTagsEndpoint' 2>&1 | tail -5`
Expected: FAIL (404s).

- [ ] **Step 3: Implement**

Create `internal/web/worktrees.go`:

```go
package web

import "net/http"

type worktreeRow struct {
	Path     string `json:"path"`
	Branch   string `json:"branch"`
	Head     string `json:"head"`
	Detached bool   `json:"detached"`
	Bare     bool   `json:"bare"`
}

func (s *Server) handleWorktrees(w http.ResponseWriter, r *http.Request) {
	ws, err := s.svc.Worktrees(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]worktreeRow, 0, len(ws))
	for _, wt := range ws {
		rows = append(rows, worktreeRow{Path: wt.Path, Branch: wt.Branch, Head: wt.Head, Detached: wt.Detached, Bare: wt.Bare})
	}
	writeJSON(w, map[string]any{"worktrees": rows})
}
```

Create `internal/web/tags.go`:

```go
package web

import "net/http"

// maxTagRows caps the sidebar payload: big repos carry hundreds of tags
// (linux: 937) and the sidebar is not the place for all of them.
const maxTagRows = 100

type tagRow struct {
	Name      string `json:"name"`
	Target    string `json:"target"`
	Annotated bool   `json:"annotated"`
	Subject   string `json:"subject,omitempty"`
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	ts, err := s.svc.Tags(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	truncated := false
	if len(ts) > maxTagRows {
		ts = ts[:maxTagRows]
		truncated = true
	}
	rows := make([]tagRow, 0, len(ts))
	for _, tg := range ts {
		rows = append(rows, tagRow{Name: tg.Name, Target: tg.Target, Annotated: tg.Annotated, Subject: tg.Subject})
	}
	writeJSON(w, map[string]any{"tags": rows, "truncated": truncated})
}
```

In `server.go` `Handler()`, directly after the `/api/branches` route:

```go
	mux.HandleFunc("GET /api/worktrees", s.handleWorktrees)
	mux.HandleFunc("GET /api/tags", s.handleTags)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/web/ -run 'TestWorktreesEndpoint|TestTagsEndpoint' -v 2>&1 | tail -8`
Expected: PASS (3 tests).

- [ ] **Step 5: Package + archtest + gofmt, commit**

Run: `go test ./internal/web/ ./internal/archtest/ 2>&1 | tail -3 && gofmt -l internal/web/`
Expected: clean.

```bash
git add internal/web/worktrees.go internal/web/tags.go internal/web/panels_test.go internal/web/server.go
git commit -m "feat(web): GET /api/worktrees + GET /api/tags (capped)"
```

---

### Task 2: three-section sidebar + tag → commit detail; docs

**Files:**
- Modify: `static/index.html`, `static/app.js`, `static/style.css`, `CHANGELOG.md`, `CLAUDE.md`

JS/HTML/CSS + docs only. Go change needed → BLOCKED.

- [ ] **Step 1: index.html — sidebar sections**

Replace the `#branches-pane` aside with:

```html
  <aside id="branches-pane">
    <div id="branches-header">branches</div>
    <ul id="branches-list"></ul>
    <div class="side-header">worktrees</div>
    <ul id="worktrees-list"></ul>
    <div class="side-header">tags</div>
    <ul id="tags-list"></ul>
  </aside>
```

- [ ] **Step 2: style.css — append**

```css
.side-header { padding: 6px 8px 2px; color: var(--dim); border-top: 1px solid var(--border); margin-top: 6px; }
#worktrees-list, #tags-list { list-style: none; }
#worktrees-list li, #tags-list li { padding: 2px 10px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
#worktrees-list li .wpath, #tags-list li .tsub { color: var(--dim); font-size: 11px; padding-left: 6px; }
#worktrees-list li.cur { color: var(--accent); }
#tags-list li { cursor: pointer; }
#tags-list li:hover { background: var(--bg-alt); }
#tags-list li.more { color: var(--dim); cursor: default; }
```

- [ ] **Step 3: app.js — state + loader + renders**

State additions (after `branches: [],`):

```js
  worktrees: [],
  tags: [],
  tagsTruncated: false,
  tagsTotalHint: 0,
```

Wait — no total is served; drop `tagsTotalHint` (YAGNI): add only the first three.

In `loadRepo`, after the `document.title` line, add:

```js
  state.worktree = repo.worktree;
```

Replace `fetchBranches` with the combined sidebar loader (branches must never be blocked by the panels):

```js
async function fetchBranches() {
  const [b, w, tg] = await Promise.all([
    getJSON("/api/branches"),
    getJSON("/api/worktrees").catch(() => ({ worktrees: [] })),
    getJSON("/api/tags").catch(() => ({ tags: [], truncated: false })),
  ]);
  state.branches = b.branches || [];
  state.worktrees = w.worktrees || [];
  state.tags = tg.tags || [];
  state.tagsTruncated = !!tg.truncated;
  renderBranches();
  renderWorktrees();
  renderTags();
}
```

Add after `renderBranches`:

```js
function renderWorktrees() {
  $("worktrees-list").innerHTML = state.worktrees
    .map((w) => {
      const label = w.bare ? "(bare)" : w.detached ? "(detached)" : w.branch || "(?)";
      const base = w.path.split("/").pop();
      const cur = state.worktree && w.path === state.worktree ? " cur" : "";
      return `<li class="${cur.trim()}" title="${esc(w.path)}">${cur ? "● " : ""}${esc(label)}<span class="wpath">${esc(base)}</span></li>`;
    })
    .join("");
}

function renderTags() {
  let html = state.tags
    .map(
      (t) =>
        `<li data-h="${esc(t.target)}" data-n="${esc(t.name)}">${esc(t.name)}` +
        (t.subject ? `<span class="tsub">${esc(t.subject)}</span>` : "") +
        `</li>`
    )
    .join("");
  if (state.tagsTruncated) html += `<li class="more">… more (capped at 100)</li>`;
  $("tags-list").innerHTML = html;
}
```

- [ ] **Step 4: app.js — openCommitByHash + tag clicks**

Add after `openCommit` (do NOT modify `openCommit` itself):

```js
// openCommitByHash enters commit detail without a feed row — the path for
// sidebar tags (and future non-feed jump-ins).
async function openCommitByHash(hash, title) {
  const body = await getJSON("/api/commit/" + hash);
  state.files = body.files || [];
  state.fileCursor = 0;
  state.fileSha = hash;
  state.pane = "files";
  state.filesMode = "commit";
  setLayout("detail");
  $("files-header").textContent = title;
  renderFiles();
  focusPane();
  if (state.files.length) openFile(0);
}
```

Add the click listener beside the others at the bottom:

```js
$("tags-list").addEventListener("click", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.h) return;
  openCommitByHash(li.dataset.h, "🏷 " + li.dataset.n);
});
```

- [ ] **Step 5: Build + smoke**

Run: `node --check internal/web/static/app.js && go build -o ./gg ./cmd/gg && go test ./internal/web/ 2>&1 | tail -2 && gofmt -l internal/web/`
Expected: clean.

Smoke (read-only — any repo works, use a scratch one anyway):

```bash
rm -rf /tmp/ggweb-pn && git init -q -b main /tmp/ggweb-pn && cd /tmp/ggweb-pn
git -c user.email=t@t -c user.name=t commit -q --allow-empty -m init
git tag v1 && git worktree add -b w2 /tmp/ggweb-pn-wt2 >/dev/null 2>&1
(cd /tmp/ggweb-pn && /mnt/t/others/gigagit.worktrees/feat-web-panels/gg web --addr 127.0.0.1:8127 &) && sleep 1
curl -s http://127.0.0.1:8127/api/worktrees; echo
curl -s http://127.0.0.1:8127/api/tags; echo
pkill -f 'gg web --addr 127.0.0.1:8127'; git worktree remove /tmp/ggweb-pn-wt2 --force 2>/dev/null; true
```

Expected: two worktrees (main + w2), one tag v1.

Playwright visual pass: against the same scratch repo (recreate it), adapt `/tmp/claude-1000/-mnt-t-others-gigagit/125bc0e5-080f-41d1-8b68-33968f63bf24/scratchpad/pw/shoot4.js` (run with `node` from that pw directory; GG = this worktree's binary): screenshot the list screen — all three sidebar sections visible — as `shot-10-panels.png`; click the `v1` tag row, wait ~800ms, screenshot `shot-11-tag-detail.png` — the detail screen with `🏷 v1` as the files header. Read both PNGs to verify before reporting.

- [ ] **Step 6: docs**

`CHANGELOG.md`, top of `## [Unreleased]`:

```markdown
- `gg web`: the sidebar grows worktrees and tags sections (read-only; tags
  capped at 100 with a truncation marker). Clicking a tag opens that
  commit's detail screen (`GET /api/worktrees`, `GET /api/tags`).
```

`CLAUDE.md` web row: after the increment-2 sentence, append:

```
`GET /api/worktrees` + `GET /api/tags` (capped at 100, `truncated` flag)
feed the sidebar's worktrees/tags sections; a tag click opens commit
detail via the feed-independent `openCommitByHash`.
```

- [ ] **Step 7: Commit**

```bash
git add internal/web/static/ CHANGELOG.md CLAUDE.md
git commit -m "feat(web): worktrees + tags sidebar sections, tag opens commit detail"
```

---

### Final verification

- [ ] `go build ./... && go test ./internal/web/ ./internal/archtest/` green (controller runs the full suite before merge).
- [ ] Merge target = web-dev, controller-owned. Never main.
