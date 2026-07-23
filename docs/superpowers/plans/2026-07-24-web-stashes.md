# Web Stashes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A stashes sidebar section with left-click drill-in, a right-click apply/pop/drop menu, and a status-pane stash button — ops #8-11 on the web transport.

**Architecture:** One new read endpoint (`GET /api/stashes`, rows carrying ref/subject/sha with the sha resolved server-side at list time) plus four decision-free op cases in `handleOpStart` (`stash` from the message box; `stash-apply`/`stash-pop`/`stash-drop` with the ref resolved against the server's own `StashList` — the remove-worktree allowlist pattern). The client adds the 4th sidebar section riding `fetchBranches`' existing `Promise.all` (so refresh comes free), a `showStashMenu` on the shipped `showCtxMenu`, and a `stash` button beside commit.

**Tech Stack:** Go stdlib HTTP (existing `internal/web`), local real-git fixtures (`newRepoDir`/`gitRun` — gitRun pins committer identity, which `git stash` needs), vanilla-JS SPA.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-24-web-stashes-design.md` (approved).
- The `stash-apply`/`stash-pop`/`stash-drop` `ref` is an IDENTIFIER resolved by exact match against the server's own `svc.StashList` — only the server's `entry.Ref` reaches the op; 400 empty, 404 `unknown stash` on no match.
- `engine.StashDrop` is decision-free — the client MUST gate drop behind `showLocalConfirm`; apply/pop dispatch directly (TUI parity: the TUI confirms only drop).
- The create case is `engine.Stash{Message: req.Message, IncludeUntracked: true}` — `Paths` empty (all changes), message optional, NO nothing-to-stash guard (git's error surfaces as `done{ok:false}`).
- A list-time `StashCommit` resolve failure drops only that row's `sha`, never the row.
- `internal/web` must not import `internal/git` (archtest-enforced).
- Work in worktree `/mnt/t/others/gigagit.worktrees/feat-web-stashes`, branch `feat/web-stashes`. Verify with `git branch --show-current` before ANY edit.

---

### Task 1: `GET /api/stashes` + four stash op cases

**Files:**
- Create: `internal/web/stashes.go`
- Modify: `internal/web/server.go` (route table, after the `GET /api/tags` line)
- Modify: `internal/web/ophttp.go` (the `opStartRequest` struct, the `handleOpStart` switch after the `remove-worktree` case, the doc comment)
- Create: `internal/web/opstash_test.go`

**Interfaces:**
- Consumes: `s.svc.StashList(ctx) ([]model.StashEntry, error)` (`StashEntry{Ref, Subject}`, newest first), `s.svc.StashCommit(ctx, ref) (string, error)`. Engine ops `engine.Stash{Message string, Paths []string, IncludeUntracked bool}`, `engine.StashApply{Ref string}`, `engine.StashPop{Ref string}`, `engine.StashDrop{Ref string}` — all decision-free. Existing test helpers: `newRepoDir`, `gitRun`, `serve`, `getJSON`, `postJSON`, `readSSE`, `startOpBody` (from `opdeletebranch_test.go`), `statusResp`/`findFile` (from `status_test.go` / `oppull_test.go` usage).
- Produces: `GET /api/stashes` → `{"stashes":[{ref,subject,sha}]}`; op contracts `{"op":"stash","message":M}`, `{"op":"stash-apply","ref":R}` (+pop/drop) → 202/400/404. `opStartRequest.Ref` field. Task 2's client relies only on these.

- [ ] **Step 1: Write the failing tests**

Create `internal/web/opstash_test.go`:

```go
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

type stashesResp struct {
	Stashes []struct {
		Ref     string `json:"ref"`
		Subject string `json:"subject"`
		Sha     string `json:"sha"`
	} `json:"stashes"`
}

// dirtyFile rewrites f.txt so the tree has an unstaged tracked change.
func dirtyFile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStashesEndpoint(t *testing.T) {
	dir := newRepoDir(t, 1)
	dirtyFile(t, dir, "one\n")
	gitRun(t, dir, "stash", "push", "-m", "first")
	dirtyFile(t, dir, "two\n")
	gitRun(t, dir, "stash", "push", "-m", "second")
	ts := serve(t, New(domain.Open(dir)))

	var body stashesResp
	if code := getJSON(t, ts, "/api/stashes", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(body.Stashes) != 2 {
		t.Fatalf("stashes = %+v", body.Stashes)
	}
	// newest first: stash@{0} is "second"
	if body.Stashes[0].Ref != "stash@{0}" || !strings.Contains(body.Stashes[0].Subject, "second") {
		t.Errorf("row 0 = %+v", body.Stashes[0])
	}
	if want := gitRun(t, dir, "rev-parse", "stash@{0}"); body.Stashes[0].Sha != want {
		t.Errorf("sha = %q, want %q", body.Stashes[0].Sha, want)
	}
}

func TestOpHTTPStashCreate(t *testing.T) {
	dir := newRepoDir(t, 1)
	dirtyFile(t, dir, "wip\n")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"stash","message":"wip x"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if out := gitRun(t, dir, "status", "--porcelain"); strings.TrimSpace(out) != "" {
		t.Errorf("tree not clean after stash: %q", out)
	}
	var body stashesResp
	getJSON(t, ts, "/api/stashes", &body)
	if len(body.Stashes) != 1 || !strings.Contains(body.Stashes[0].Subject, "wip x") {
		t.Fatalf("stashes after create = %+v", body.Stashes)
	}
}

func TestOpHTTPStashApply(t *testing.T) {
	dir := newRepoDir(t, 1)
	dirtyFile(t, dir, "wip\n")
	gitRun(t, dir, "stash", "push", "-m", "keepme")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"stash-apply","ref":"stash@{0}"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(got) != "wip\n" {
		t.Errorf("f.txt = %q after apply", got)
	}
	var body stashesResp
	getJSON(t, ts, "/api/stashes", &body)
	if len(body.Stashes) != 1 {
		t.Errorf("apply must keep the stash, got %+v", body.Stashes)
	}
}

func TestOpHTTPStashPop(t *testing.T) {
	dir := newRepoDir(t, 1)
	dirtyFile(t, dir, "wip\n")
	gitRun(t, dir, "stash", "push", "-m", "popme")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"stash-pop","ref":"stash@{0}"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(got) != "wip\n" {
		t.Errorf("f.txt = %q after pop", got)
	}
	var body stashesResp
	getJSON(t, ts, "/api/stashes", &body)
	if len(body.Stashes) != 0 {
		t.Errorf("pop must drop the stash, got %+v", body.Stashes)
	}
}

func TestOpHTTPStashDrop(t *testing.T) {
	dir := newRepoDir(t, 1)
	dirtyFile(t, dir, "wip\n")
	gitRun(t, dir, "stash", "push", "-m", "dropme")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"stash-drop","ref":"stash@{0}"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	var body stashesResp
	getJSON(t, ts, "/api/stashes", &body)
	if len(body.Stashes) != 0 {
		t.Errorf("drop must remove the stash, got %+v", body.Stashes)
	}
	// the working tree stays untouched (still the clean c1 content)
	if got, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(got) != "content 1\n" {
		t.Errorf("f.txt = %q after drop", got)
	}
}

func TestOpHTTPStashPopConflict(t *testing.T) {
	dir := newRepoDir(t, 1)
	dirtyFile(t, dir, "stashed\n")
	gitRun(t, dir, "stash", "push", "-m", "will conflict")
	dirtyFile(t, dir, "committed\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "conflicting change")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"stash-pop","ref":"stash@{0}"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false (pop conflict)", done)
	}
	var st statusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if i, ok := findFile(t, st, "f.txt"); !ok || st.Files[i].Kind != "conflicted" {
		t.Errorf("f.txt not conflicted after pop conflict: %+v", st.Files)
	}
}

func TestOpHTTPStashBadRef(t *testing.T) {
	dir := newRepoDir(t, 1)
	dirtyFile(t, dir, "wip\n")
	gitRun(t, dir, "stash", "push", "-m", "only")
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/op", `{"op":"stash-apply"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("empty ref code = %d, want 400", code)
	}
	for _, ref := range []string{"stash@{9}", "--all"} {
		body := `{"op":"stash-apply","ref":"` + ref + `"}`
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusNotFound {
			t.Errorf("ref %q code = %d, want 404", ref, code)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-stashes && go test ./internal/web/ -run 'TestStashesEndpoint|TestOpHTTPStash' -v`
Expected: `TestStashesEndpoint` fails with a 404 code (no route); every op test fails at the start call with `op start code = 400` — `unknown op` (BadRef's empty-ref sub-check may already "pass" — the others must fail).

- [ ] **Step 3: Implement the endpoint and op cases**

a. Create `internal/web/stashes.go`:

```go
package web

import "net/http"

type stashRow struct {
	Ref     string `json:"ref"`
	Subject string `json:"subject"`
	Sha     string `json:"sha,omitempty"`
}

// handleStashes lists stash entries newest-first. Each row carries the
// stash's commit sha, resolved here so the client's left-click needs no
// second request (the tags-row pattern); a resolve failure drops only the
// sha, never the row.
func (s *Server) handleStashes(w http.ResponseWriter, r *http.Request) {
	es, err := s.svc.StashList(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]stashRow, 0, len(es))
	for _, e := range es {
		row := stashRow{Ref: e.Ref, Subject: e.Subject}
		if sha, serr := s.svc.StashCommit(r.Context(), e.Ref); serr == nil {
			row.Sha = sha
		}
		rows = append(rows, row)
	}
	writeJSON(w, map[string]any{"stashes": rows})
}
```

b. In `internal/web/server.go`, after the `GET /api/tags` route line, add:

```go
	mux.HandleFunc("GET /api/stashes", s.handleStashes)
```

c. In `internal/web/ophttp.go`, extend `opStartRequest`:

```go
type opStartRequest struct {
	Op      string `json:"op"`
	Branch  string `json:"branch"`
	Message string `json:"message"`
	Tag     string `json:"tag"`
	Path    string `json:"path"`
	Ref     string `json:"ref"`
}
```

d. After the `case "remove-worktree":` block (before `default:`), insert:

```go
	case "stash":
		// All changes incl. untracked (the common case; path-scoped stashing
		// stays TUI-only). Message optional; nothing-to-stash surfaces git's
		// own error through the op.
		op = engine.Stash{Message: req.Message, IncludeUntracked: true}
	case "stash-apply", "stash-pop", "stash-drop":
		if req.Ref == "" {
			writeErr(w, http.StatusBadRequest, errors.New("ref required"))
			return
		}
		// The client-sent ref is an identifier: resolve it against the
		// server's own stash list so only server-owned values reach git argv
		// (the remove-worktree allowlist pattern). All three ops are
		// decision-free; drop's confirm lives client-side.
		entries, serr := s.svc.StashList(r.Context())
		if serr != nil {
			writeErr(w, http.StatusInternalServerError, serr)
			return
		}
		found := false
		for _, e := range entries {
			if e.Ref == req.Ref {
				switch req.Op {
				case "stash-apply":
					op = engine.StashApply{Ref: e.Ref}
				case "stash-pop":
					op = engine.StashPop{Ref: e.Ref}
				default:
					op = engine.StashDrop{Ref: e.Ref}
				}
				found = true
				break
			}
		}
		if !found {
			writeErr(w, http.StatusNotFound, errors.New("unknown stash"))
			return
		}
```

e. Update the doc comment on `handleOpStart`:

```go
// handleOpStart begins an operation and returns 202 {op_id}. Ops wired so
// far: switch, commit, pull, push, delete-branch, delete-tag,
// remove-worktree, stash, stash-apply, stash-pop, stash-drop; the switch
// statement is where future ops land.
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/web/ -run 'TestStashesEndpoint|TestOpHTTPStash' -v`
Expected: all 8 PASS.

- [ ] **Step 5: Run the package + archtest, then race on the new tests**

Run: `go test ./internal/web/ ./internal/archtest/ && go test -race ./internal/web/ -run 'TestStashesEndpoint|TestOpHTTPStash'`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/stashes.go internal/web/server.go internal/web/ophttp.go internal/web/opstash_test.go
git commit -m "feat(web): stashes read endpoint + stash/apply/pop/drop ops on the transport"
```

---

### Task 2: Stashes sidebar section, menu, stash button + docs

**Files:**
- Modify: `internal/web/static/index.html` (sidebar after `#tags-list`, status pane after `#commit-btn`)
- Modify: `internal/web/static/app.js` (state init, `fetchBranches`, a new `renderStashes`, the section-collapse loop, listeners after the tags contextmenu block, `handleOpEvent`'s commit-msg clear, the status-pane disabled line, button wiring at the bottom)
- Modify: `internal/web/static/style.css` (the sidebar `li`/`.tsub`/hover/collapsed selector lists)
- Modify: `CHANGELOG.md` (top of the unreleased section)
- Modify: `CLAUDE.md` (the `web` row's op list)

**Interfaces:**
- Consumes: Task 1's HTTP contracts. Existing client pieces: `startOp(body, label)`, `showCtxMenu(items, x, y)` (items `{label, act, danger?}`), `showLocalConfirm(prompt, options, cb)`, `openCommitByHash(hash, title)`, `opLine`, `esc`, `state.wt` (null when the tree is clean), `state.op`.
- Produces: `state.stashes`, `renderStashes()`, `showStashMenu(st, x, y)`, `doStash()`; nothing downstream consumes them.

- [ ] **Step 1: index.html — section + button**

a. After the `#tags-list` line (`<ul id="tags-list"></ul>`), add:

```html
    <div id="stashes-header" class="side-header">stashes</div>
    <ul id="stashes-list"></ul>
```

b. After the `#commit-btn` line (`<button id="commit-btn">commit</button>`), add:

```html
      <button id="stash-btn" title="stash all working-tree changes">stash</button>
```

- [ ] **Step 2: app.js — state, fetch, render**

a. In the `state` initializer, after `worktrees: [],` add:

```js
  stashes: [],
```

b. In `fetchBranches`, extend the `Promise.all` and its handling:

```js
async function fetchBranches() {
  const [b, w, tg, st] = await Promise.all([
    getJSON("/api/branches"),
    getJSON("/api/worktrees").catch(() => ({ worktrees: [] })),
    getJSON("/api/tags").catch(() => ({ tags: [], truncated: false })),
    getJSON("/api/stashes").catch(() => ({ stashes: [] })),
  ]);
  state.branches = b.branches || [];
  state.worktrees = w.worktrees || [];
  state.tags = tg.tags || [];
  state.tagsTruncated = !!tg.truncated;
  state.stashes = st.stashes || [];
  renderBranches();
  renderWorktrees();
  renderTags();
  renderStashes();
}
```

c. After `renderTags()` (the function), add:

```js
function renderStashes() {
  $("stashes-list").innerHTML = state.stashes
    .map(
      (s) =>
        `<li data-r="${esc(s.ref)}"${s.sha ? ` data-h="${esc(s.sha)}"` : ""}>${esc(s.ref)}` +
        (s.subject ? `<span class="tsub">${esc(s.subject)}</span>` : "") +
        `</li>`
    )
    .join("");
}
```

- [ ] **Step 3: app.js — collapse loop, listeners, menu, stash button**

a. Extend the section-collapse loop:

```js
["branches", "worktrees", "tags", "stashes"].forEach((n) => {
  $(n + "-header").addEventListener("dblclick", () => toggleSection(n));
});
```

b. After the `$("tags-list").addEventListener("contextmenu", ...)` block, add:

```js
$("stashes-list").addEventListener("click", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.h) return; // a sha-less row ignores left-click
  openCommitByHash(li.dataset.h, "≡ " + li.dataset.r);
});

function showStashMenu(st, x, y) {
  const items = [];
  if (st.sha) items.push({ label: "show changes", act: () => openCommitByHash(st.sha, "≡ " + st.ref) });
  items.push({ label: "apply", act: () => startOp({ op: "stash-apply", ref: st.ref }, "applying " + st.ref) });
  items.push({ label: "pop", act: () => startOp({ op: "stash-pop", ref: st.ref }, "popping " + st.ref) });
  items.push({
    label: "drop " + st.ref,
    danger: true,
    // engine.StashDrop is decision-free — the confirm lives here (the
    // delete-tag precedent; the TUI confirms drop with y/n too).
    act: () =>
      showLocalConfirm("Drop " + st.ref + "?", ["drop", "abort"], (o) => {
        if (o === "drop") startOp({ op: "stash-drop", ref: st.ref }, "dropping " + st.ref);
      }),
  });
  showCtxMenu(items, x, y);
}
$("stashes-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.r) return;
  e.preventDefault();
  const st = state.stashes.find((x) => x.ref === li.dataset.r);
  if (st) showStashMenu(st, e.clientX, e.clientY);
});
```

c. `doStash` — add directly under `doPush()`:

```js
function doStash() {
  if (state.op || !state.wt) return;
  const message = $("commit-msg").value.trim();
  showLocalConfirm("Stash all working-tree changes?", ["stash", "abort"], (o) => {
    if (o === "stash") startOp({ op: "stash", message }, "stashing");
  });
}
```

d. In `handleOpEvent`'s done branch, widen the message-box clear:

```js
    if (ev.ok && (kind === "commit" || kind === "stash")) $("commit-msg").value = "";
```

e. Beside the `$("commit-btn").disabled = ...` line in the status-pane render, add:

```js
  $("stash-btn").disabled = !state.wt || !!state.op;
```

f. Next to `$("pull-btn").addEventListener("click", doPull);` at the bottom, add:

```js
$("stash-btn").addEventListener("click", doStash);
```

- [ ] **Step 4: style.css — extend the sidebar selector lists**

Extend these four existing rules (selector lists only; declaration blocks unchanged):

```css
#worktrees-list li, #tags-list li, #stashes-list li { padding: 2px 10px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
#worktrees-list li .wpath, #tags-list li .tsub, #stashes-list li .tsub { color: var(--dim); font-size: 11px; padding-left: 6px; }
#tags-list li, #stashes-list li { cursor: pointer; }
#tags-list li:hover, #stashes-list li:hover { background: var(--bg-alt); }
```

And the collapse rule:

```css
#branches-list.collapsed, #worktrees-list.collapsed, #tags-list.collapsed, #stashes-list.collapsed { display: none; }
```

- [ ] **Step 5: Build, test, JS syntax check**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-stashes && go build ./cmd/gg && go test ./internal/web/ && node --check internal/web/static/app.js`
Expected: build OK, tests PASS, no JS syntax error.

- [ ] **Step 6: Update CHANGELOG.md and CLAUDE.md**

CHANGELOG.md — add at the top of the current unreleased section:

```markdown
- `gg web`: stashes. A 4th sidebar section lists stash entries (left-click
  opens the stash's changes in the commit detail); right-click offers
  apply / pop / drop (drop confirms — the engine op is decision-free). A
  `stash` button beside commit stashes all working-tree changes incl.
  untracked, taking the message box's text as the optional stash message.
  Apply/pop/drop refs are resolved against the server's own stash list
  (allowlist); a pop/apply conflict surfaces in the status pane as usual.
```

CLAUDE.md — in the `web` row of the package map, after the Ops #5-7 sentence, append (keep the row ONE physical line):

```
Ops #8-11: `op:"stash"` (all changes + untracked, message from the commit box) and `op:"stash-apply"/"stash-pop"/"stash-drop"` (ref allowlist-resolved against the server's own StashList; drop confirmed client-side — decision-free op) behind a 4th `stashes` sidebar section (`GET /api/stashes`, rows ref/subject/sha with the sha resolved at list time so left-click opens the existing commit detail).
```

- [ ] **Step 7: Commit**

```bash
git add internal/web/static/index.html internal/web/static/app.js internal/web/static/style.css CHANGELOG.md CLAUDE.md
git commit -m "feat(web): stashes sidebar section, apply/pop/drop menu, stash button"
```
