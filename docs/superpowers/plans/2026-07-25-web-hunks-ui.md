# Web hunk-staging UI — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A hunk view in the diff pane: list a file's index↔worktree change blocks with checkboxes, stage the picked ones via `POST /api/stage-hunks`, refetch after every round.

**Architecture:** Client-only; the backend shipped in wave 2. The view is PANE CONTENT (module state `hunkView`, rendered into `#diff-body`), not a layer. Picks are positional against the exact bytes the server hashed — every successful round changes the hash, so the client refetches before offering more picks.

**Tech Stack:** Vanilla JS (`internal/web/static/app.js`), HTML, CSS. No Go changes.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-25-web-hunks-ui-design.md` (committed in this worktree) — binding. Deviation noted there vs here: the spec says `state.hunks`; implement as module-scoped `hunkView` in this feature's own region (same behavior, conflict-free merges).
- Work ONLY in `/mnt/t/others/gigagit.worktrees/feat-web-hunks-ui` on branch `feat/web-hunks-ui`.
- Do NOT touch: the keydown router, the footer, `#top`, the layer stack, any `.go` file (other wave-3 tracks own those).
- Eligibility = status entry with `section === "changes"` and `kind === "tracked"`.
- Exact server strings relied on: 409 body message `file changed; refresh`.
- Gate: `node --check internal/web/static/app.js` must pass.
- Commit messages end with:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01HJ4EsSe6QUvrEADAwdC9HG`

---

### Task 1: hunk view

**Files:**
- Modify: `internal/web/static/index.html` (`#hunks-btn` in `#diff-nav` only)
- Modify: `internal/web/static/style.css` (append `.hunk-*` rules)
- Modify: `internal/web/static/app.js` (new region after the diff-nav wiring; small edits inside `openFile`, `openStatusDiff`, `updateDiffNav`, `drillOut`, `reconcileStatusView`, the `#files-list` contextmenu handler)

**Interfaces:**
- Consumes (existing, unchanged): `getJSON/postJSON/esc`, `opLine`, `applyStatus` (accepts a `/api/status`-shaped payload), `reconcileStatusView`, `renderFiles`, `openStatusDiff(i)`, `updateDiffNav`, `state.statusEntries`, `state.fileCursor`, `state.filesMode`.
- Produces: `hunkEligible(f)`, `enterHunkView(path)`, `exitHunkView()`, `renderHunks()`, `refetchHunks()`, `stagePicked()` — used only within this feature plus the five small call-site edits below.

- [ ] **Step 1: index.html**

Inside `#diff-nav`, after `<button id="next-change" title="next change">change ›</button>`, add:

```html
<button id="hunks-btn" title="stage hunks">hunks</button>
```

- [ ] **Step 2: style.css — append at end of file**

```css
/* hunk staging view (pane content in #diff-body) */
.hunk-bar { display: flex; gap: 10px; padding: 8px 10px; position: sticky; top: 0; background: var(--bg); z-index: 1; }
.hunk-bar button { background: var(--bg-alt); color: var(--fg); border: 1px solid var(--border); border-radius: 4px; padding: 3px 10px; font: inherit; cursor: pointer; }
.hunk-bar button:hover:not(:disabled) { border-color: var(--accent); }
.hunk-bar button:disabled { opacity: .45; cursor: default; }
.hunk-block { margin: 6px 10px; border: 1px solid var(--border); border-radius: 4px; overflow: hidden; }
.hunk-head { padding: 4px 10px; background: var(--bg-alt); cursor: pointer; user-select: none; }
.hunk-line { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 12px; white-space: pre-wrap; word-break: break-all; padding: 0 10px; }
.hunk-line.del { background: var(--del); }
.hunk-line.add { background: var(--add); }
.hunk-sep { text-align: center; color: var(--dim); padding: 2px 0; }
```

- [ ] **Step 3: app.js — new region**

Insert immediately AFTER the diff-nav button click wirings (the four `$("prev-file")…$("next-change")` `addEventListener` lines) and BEFORE `focusPane`:

```js
// ---- hunk staging view (wave 3) ------------------------------------------
// Pane content, not a layer: replaces #diff-body while active. Picks are
// POSITIONAL against the exact bytes the server hashed — after every staged
// round the index moves and the hash changes, so the view REFETCHES before
// offering more picks (a 409 means someone else moved the file: same
// refetch). 422 messages from the server are user-ready and shown verbatim.

let hunkView = null; // {path, hash, blocks, picks: Set<int>}

function hunkEligible(f) {
  return !!f && f.section === "changes" && f.kind === "tracked";
}

async function enterHunkView(path) {
  let j;
  try {
    j = await getJSON("/api/hunks?" + new URLSearchParams({ path }));
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    return;
  }
  hunkView = { path, hash: j.hash, blocks: j.blocks || [], picks: new Set() };
  renderHunks();
}

function exitHunkView() {
  hunkView = null;
  const f = state.statusEntries[state.fileCursor];
  if (state.filesMode === "status" && f) {
    openStatusDiff(state.fileCursor);
  } else {
    $("diff-title").textContent = "";
    $("diff-body").innerHTML = "";
    updateDiffNav();
  }
}

function renderHunks() {
  if (!hunkView) return;
  const v = hunkView;
  $("diff-title").textContent = v.path + " — stage hunks";
  const n = v.picks.size;
  const bar =
    `<div class="hunk-bar">` +
    `<button id="hunk-stage"${n ? "" : " disabled"}>Stage selected (${n})</button>` +
    `<button id="hunk-all">Select all</button>` +
    `<button id="hunk-none">Clear</button>` +
    `<button id="hunk-back">‹ back to diff</button>` +
    `</div>`;
  const blocks = v.blocks
    .map((b, i) => {
      const lines =
        (b.del || []).map((l) => `<div class="hunk-line del">- ${esc(l)}</div>`).join("") +
        (b.add || []).map((l) => `<div class="hunk-line add">+ ${esc(l)}</div>`).join("");
      return (
        `<div class="hunk-block" data-i="${i}">` +
        `<div class="hunk-head"><input type="checkbox"${v.picks.has(i) ? " checked" : ""}> hunk ${i + 1}/${v.blocks.length}</div>` +
        lines +
        `</div>`
      );
    })
    .join(`<div class="hunk-sep">⋯</div>`);
  $("diff-body").innerHTML = bar + (blocks || `<div class="notice">no hunks</div>`);
  updateDiffNav();
}

async function stagePicked() {
  const v = hunkView;
  if (!v || !v.picks.size) return;
  let resp;
  try {
    resp = await postJSON("/api/stage-hunks", {
      path: v.path,
      picks: [...v.picks].sort((a, b) => a - b),
      hash: v.hash,
    });
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    // 409 = stale picks: refetch fresh blocks; other errors keep the picks
    if (/file changed/.test(e.message || "")) await refetchHunks();
    return;
  }
  applyStatus(resp); // the 200 body IS a fresh /api/status payload
  reconcileStatusView(); // may exit the view via its eligibility guard
  renderFiles();
  await refetchHunks();
}

async function refetchHunks() {
  if (!hunkView) return;
  const path = hunkView.path;
  let j;
  try {
    j = await getJSON("/api/hunks?" + new URLSearchParams({ path }));
  } catch {
    exitHunkView(); // 404/422: the file left the eligible set (fully staged)
    return;
  }
  if (!j.count) {
    exitHunkView();
    return;
  }
  hunkView = { path, hash: j.hash, blocks: j.blocks || [], picks: new Set() };
  renderHunks();
}

$("hunks-btn").addEventListener("click", () => {
  const f = state.statusEntries[state.fileCursor];
  if (state.filesMode === "status" && hunkEligible(f)) enterHunkView(f.path);
});

$("diff-body").addEventListener("click", (e) => {
  if (!hunkView) return;
  if (e.target.id === "hunk-back") return exitHunkView();
  if (e.target.id === "hunk-all") { hunkView.picks = new Set(hunkView.blocks.map((_, i) => i)); return renderHunks(); }
  if (e.target.id === "hunk-none") { hunkView.picks = new Set(); return renderHunks(); }
  if (e.target.id === "hunk-stage") return void stagePicked();
  const head = e.target.closest(".hunk-head");
  if (head) {
    const i = Number(head.parentElement.dataset.i);
    if (hunkView.picks.has(i)) hunkView.picks.delete(i); else hunkView.picks.add(i);
    renderHunks();
  }
});
```

- [ ] **Step 4: app.js — five small call-site edits**

1. `openFile(i)` — first line of the function body: `hunkView = null;`
2. `openStatusDiff(i)` — first line of the function body: `hunkView = null;`
   (Both clear the view when any file/diff is opened; they must NOT call
   `exitHunkView()` — it would recurse into `openStatusDiff`.)
3. `updateDiffNav()` — append at the end:

```js
  const f = state.filesMode === "status" ? list[state.fileCursor] : null;
  $("hunks-btn").disabled = !hunkEligible(f);
```

4. `drillOut()` — first line of the function body:

```js
  if (hunkView) { exitHunkView(); return; } // esc exits the hunk view, not the status screen
```

5. `reconcileStatusView()` — append at the end:

```js
  // a status re-read can invalidate an open hunk view (file fully staged or
  // gone): exit rather than offer stale positional picks
  if (hunkView && !state.statusEntries.some((f) => f.path === hunkView.path && hunkEligible(f))) exitHunkView();
```

Note on ordering: `hunkView`, `hunkEligible`, and `exitHunkView` are
`let`/`function` declarations in a region ABOVE `drillOut`? No — `drillOut`
sits earlier in the file than the new region. This is fine: `function`
declarations hoist module-wide, and `let hunkView` is initialized at module
load before any user event can fire (`drillOut` only runs from
clicks/keys). Do not reorder existing code for this.

- [ ] **Step 5: app.js — contextmenu item**

In the `#files-list` contextmenu handler, directly AFTER the line

```js
  else if (f.section !== "conflicts") items.push({ label: "stage " + f.path, act: () => stage({ paths: [f.path] }) });
```

insert:

```js
  if (hunkEligible(f)) items.push({ label: "stage hunks…", act: () => enterHunkView(f.path) });
```

- [ ] **Step 6: verify**

Run: `node --check internal/web/static/app.js` — expect pass.
Then `go build ./cmd/gg`.

- [ ] **Step 7: CHANGELOG + commit**

Add to `CHANGELOG.md` under `## [Unreleased]` (top of the list):

```markdown
- web: hunk staging UI — a `hunks` button in the diff toolbar (and a
  `stage hunks…` right-click entry) opens a per-file hunk view: tick the
  change blocks to stage and hit *Stage selected*; the view refetches after
  every round (picks are positional against a freshness hash; a concurrent
  edit surfaces as "file changed; refresh" and reloads the blocks).
```

```bash
git add -A && git commit -m "feat(web): hunk-staging UI in the diff pane"
```
(with the Global Constraints trailers)

## Self-review notes

- `stagePicked`'s 409 detection matches the server's exact message
  `file changed; refresh` via `/file changed/` — if `postJSON` exposes the
  HTTP status, prefer `e.status === 409`; check the helper first (it likely
  throws `Error(body.error)`).
- `reconcileStatusView` guard runs during `stagePicked` (via `applyStatus` →
  `reconcileStatusView`) — if it exits the view, the following
  `refetchHunks()` no-ops on `hunkView === null`. Intended.
- Spec coverage check: both entry points, refetch-after-round, 409 path,
  auto-exit on empty, esc guard, background-refresh guard — all present.
