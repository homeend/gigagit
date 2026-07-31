# Web branch drag-and-drop merge/rebase — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Dragging one branch onto another in the `gg web` sidebar opens a menu offering "merge A into B" / "rebase A onto B", each dispatching the existing engine op over the existing op transport.

**Architecture:** Two new cases on the server's `/api/op` switch (`merge`, `rebase`) mapping to `engine.SmartMerge` / `engine.SmartRebase`, plus one new request field (`onto`). The client adds HTML5 drag events to the branch `<li>` rows it already renders and reuses `showCtxMenu` for the drop menu — no new overlay component, no new layer type. A fourth, independent task aligns the line-mode commit dot with the graph's leftmost lane.

**Tech Stack:** Go 1.26 (`internal/web`), vanilla ES modules-free JS + CSS in `internal/web/static/` (no node toolchain, files are `go:embed`ed).

**Spec:** `docs/superpowers/specs/2026-07-26-web-branch-dnd-and-parity-design.md`

## Global Constraints

- Branch is `feat/web-branch-dnd`, off `web-dev`. **All web-UI work merges into `web-dev`, never into `main`.**
- Every branch name that reaches git argv must pass `isGitArgSafe` first — the `delete-branch` / `switch` precedent in `internal/web/ophttp.go`.
- `engine.Push`'s `Force` field stays non-wire-settable. Nothing in this plan touches push.
- Ops are never constructed by hand in a frontend: the handler builds an `engine.Operation` value and the existing `runOp` path executes it via `domain.Execute`.
- Go tests use a real `git` in `t.TempDir()` via the package's own `newRepoDir` / `gitRun` / `serve` / `startOpBody` / `readSSE` helpers. Do not add new helpers.
- Run `./test.sh unit` before each commit; run `./test.sh race` once before the branch is merged.
- Client files are embedded — after editing `app.js`/`style.css` the binary must be rebuilt (`go build ./cmd/gg`) before a browser check shows the change.

---

### Task 1: Server — `merge` op

**Files:**
- Modify: `internal/web/ophttp.go` (the `opStartRequest` struct at lines 14-22; the op switch, after the `case "switch":` block)
- Test: `internal/web/opmerge_test.go` (create)

**Interfaces:**
- Consumes: `startOpBody(t, ts, body) string`, `readSSE(t, ts, opID, timeout) []map[string]any`, `newRepoDir(t, n) string`, `gitRun(t, dir, args...) string`, `serve(t, srv) *httptest.Server`, `postJSON(t, ts, path, body, contentType, origin, out) int` — all already defined in the package's test files.
- Produces: the wire contract `{"op":"merge","branch":"<source>","onto":"<target>"}`, consumed by Task 3. `branch` is the dragged branch (merge source), `onto` is the branch it was dropped on (merge target). The op ends checked out on `onto`.

- [ ] **Step 1: Write the failing tests**

Create `internal/web/opmerge_test.go`:

```go
package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// divergedRepo builds main and feature with one unique commit each, leaving
// main checked out — a non-fast-forward merge and a real rebase both need it.
func divergedRepo(t *testing.T) string {
	t.Helper()
	dir := newRepoDir(t, 2)
	gitRun(t, dir, "checkout", "-b", "feature")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "feature work")
	gitRun(t, dir, "checkout", "main")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "main work")
	return dir
}

func TestOpHTTPMerge(t *testing.T) {
	dir := divergedRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	opID := startOpBody(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if log := gitRun(t, dir, "log", "--oneline", "main"); !strings.Contains(log, "feature work") {
		t.Errorf("feature work not merged into main:\n%s", log)
	}
	// SmartMerge ends on the target.
	if head := strings.TrimSpace(gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD")); head != "main" {
		t.Errorf("HEAD = %q, want main", head)
	}
}

// Target need not be checked out: SmartMerge's ladder switches to it and
// ends there. This is what makes an arbitrary drop pair work.
func TestOpHTTPMergeTargetNotCheckedOut(t *testing.T) {
	dir := divergedRepo(t)
	gitRun(t, dir, "checkout", "feature") // main is now the non-checked-out target
	ts := serve(t, New(domain.Open(dir)))

	opID := startOpBody(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if log := gitRun(t, dir, "log", "--oneline", "main"); !strings.Contains(log, "feature work") {
		t.Errorf("feature work not merged into main:\n%s", log)
	}
}

func TestOpHTTPMergeSameBranch(t *testing.T) {
	dir := divergedRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"merge","branch":"main","onto":"main"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false (source == target)", done)
	}
}

func TestOpHTTPMergeBadNames(t *testing.T) {
	dir := divergedRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	for _, body := range []string{
		`{"op":"merge","onto":"main"}`,
		`{"op":"merge","branch":"feature"}`,
		`{"op":"merge","branch":"--exec=id","onto":"main"}`,
		`{"op":"merge","branch":"feature","onto":"--exec=id"}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want 400", body, code)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/web/ -run TestOpHTTPMerge -v`
Expected: FAIL. The `merge` op is unknown, so `/api/op` returns 400 and `startOpBody` fails with `op start code = 400`.

- [ ] **Step 3: Add the `onto` request field**

In `internal/web/ophttp.go`, add one field to `opStartRequest` (keep the existing fields untouched):

```go
type opStartRequest struct {
	Op      string `json:"op"`
	Branch  string `json:"branch"`
	Onto    string `json:"onto"`
	Message string `json:"message"`
	Tag     string `json:"tag"`
	Path    string `json:"path"`
	Ref     string `json:"ref"`
	Sha     string `json:"sha"`
}
```

- [ ] **Step 4: Add the `merge` case**

In the same file, insert directly after the `case "switch":` block:

```go
	case "merge":
		// Drag-and-drop pair: Branch is the dragged source, Onto the branch
		// it was dropped on. Both names are validated here; the engine then
		// checks they exist and refuses source == target itself.
		if req.Branch == "" || !isGitArgSafe(req.Branch) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
			return
		}
		if req.Onto == "" || !isGitArgSafe(req.Onto) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid target branch"))
			return
		}
		// SmartMerge is worktree-aware: it merges in place, or inside the
		// worktree holding the target, or autostashes and switches — so an
		// arbitrary pair works with no client-side precondition. A conflict
		// forks "merge-conflict" into the parking modal.
		op = engine.SmartMerge{Source: req.Branch, Target: req.Onto}
```

- [ ] **Step 5: Update the handler doc comment**

Replace the op list in `handleOpStart`'s comment so it stays accurate:

```go
// handleOpStart begins an operation and returns 202 {op_id}. Ops wired so
// far: switch, commit, pull, push, merge, delete-branch, delete-tag,
// remove-worktree, stash, stash-apply, stash-pop, stash-drop, discard; the
// switch statement is where future ops land.
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/web/ -run TestOpHTTPMerge -v`
Expected: PASS, all four tests.

- [ ] **Step 7: Run the package suite**

Run: `go test ./internal/web/`
Expected: PASS (no regressions in the existing op tests).

- [ ] **Step 8: Commit**

```bash
git add internal/web/ophttp.go internal/web/opmerge_test.go
git commit -m "feat(web): op:\"merge\" dispatches engine.SmartMerge

Branch is the merge source, onto the target; both isGitArgSafe-validated,
existence and source==target left to the engine. Backs the coming
drag-and-drop drop menu."
```

---

### Task 2: Server — `rebase` op

**Files:**
- Modify: `internal/web/ophttp.go` (the op switch, directly after the `case "merge":` block added in Task 1)
- Test: `internal/web/oprebase_test.go` (create)

**Interfaces:**
- Consumes: `divergedRepo(t) string` from Task 1's `opmerge_test.go` (same package, so it is directly callable); the same test helpers as Task 1.
- Produces: the wire contract `{"op":"rebase","branch":"<moving>","onto":"<base>"}`, consumed by Task 3. `branch` is the dragged branch (the one rewritten), `onto` is the branch it was dropped on. The op ends checked out on `branch`.

- [ ] **Step 1: Write the failing tests**

Create `internal/web/oprebase_test.go`:

```go
package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestOpHTTPRebase(t *testing.T) {
	dir := divergedRepo(t) // main checked out; feature diverged
	ts := serve(t, New(domain.Open(dir)))

	opID := startOpBody(t, ts, `{"op":"rebase","branch":"feature","onto":"main"}`)
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	// feature now sits on top of main's tip.
	if log := gitRun(t, dir, "log", "--oneline", "feature"); !strings.Contains(log, "main work") {
		t.Errorf("feature not rebased onto main:\n%s", log)
	}
	// SmartRebase pivots on the moving branch and ends there.
	if head := strings.TrimSpace(gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD")); head != "feature" {
		t.Errorf("HEAD = %q, want feature", head)
	}
}

func TestOpHTTPRebaseSameBranch(t *testing.T) {
	dir := divergedRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"rebase","branch":"main","onto":"main"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false (branch == base)", done)
	}
}

func TestOpHTTPRebaseBadNames(t *testing.T) {
	dir := divergedRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	for _, body := range []string{
		`{"op":"rebase","onto":"main"}`,
		`{"op":"rebase","branch":"feature"}`,
		`{"op":"rebase","branch":"--exec=id","onto":"main"}`,
		`{"op":"rebase","branch":"feature","onto":"--exec=id"}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want 400", body, code)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/web/ -run TestOpHTTPRebase -v`
Expected: FAIL with `op start code = 400` — `rebase` is not a known op.

- [ ] **Step 3: Add the `rebase` case**

In `internal/web/ophttp.go`, insert directly after the `case "merge":` block:

```go
	case "rebase":
		// Branch is the dragged branch — the one REWRITTEN and ended on —
		// and Onto the branch it was dropped on. Unlike merge, the ladder
		// pivots on Branch, so the labels must not be swapped.
		if req.Branch == "" || !isGitArgSafe(req.Branch) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
			return
		}
		if req.Onto == "" || !isGitArgSafe(req.Onto) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid base branch"))
			return
		}
		// A conflict pauses the replay and forks "rebase-conflict" into the
		// parking modal.
		op = engine.SmartRebase{Branch: req.Branch, Onto: req.Onto}
```

- [ ] **Step 4: Update the handler doc comment**

Add `rebase` to the op list in `handleOpStart`'s comment, after `merge`:

```go
// far: switch, commit, pull, push, merge, rebase, delete-branch, delete-tag,
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/web/ -run TestOpHTTPRebase -v`
Expected: PASS, all three tests.

- [ ] **Step 6: Run the package suite**

Run: `go test ./internal/web/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/web/ophttp.go internal/web/oprebase_test.go
git commit -m "feat(web): op:\"rebase\" dispatches engine.SmartRebase

branch is the moving/rewritten branch and onto the base — the ladder pivots
on branch, so the two fields are not interchangeable with merge's."
```

---

### Task 3: Client — drag a branch onto another, drop menu, dispatch

**Files:**
- Modify: `internal/web/static/app.js` (`renderBranches` at lines 142-153; the branches-list event handlers near line 521; `showBranchMenu` at line 498 stays as-is)
- Modify: `internal/web/static/style.css` (add a `.drop-target` rule near the sidebar list rules)

**Interfaces:**
- Consumes: `startOp(body, label)` (line 221 — one live op at a time, it returns early if `state.op` is set); `showCtxMenu(items, x, y)` (line 482 — `items` is `[{label, act, danger?}]`); `state.branches` (array of `{name, is_head, hash, ahead, behind, upstream, time}`); `esc(s)`; `$(id)`. The op wire contracts from Tasks 1 and 2.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Make branch rows draggable**

In `renderBranches`, add the `draggable` attribute to the `<li>`. Change the return expression to:

```js
      return (
        `<li class="${b.is_head ? "head" : ""}" draggable="true" data-n="${esc(b.name)}">` +
        `${b.is_head ? "✓ " : ""}${esc(b.name)}${ab ? `<span class="ab">${ab}</span>` : ""}</li>`
      );
```

- [ ] **Step 2: Add the drag state field**

In the `state` object (starts line 3), add one field alongside the existing ones:

```js
  dragBranch: null, // name of the branch being dragged, else null
```

- [ ] **Step 3: Add the drag handlers**

In `app.js`, directly after the existing `$("branches-list").addEventListener("contextmenu", …)` block (ends around line 533), append:

```js
// Drag a branch onto another to merge or rebase. The drop opens the shared
// ctx-menu naming the pair in both directions — the menu row IS the
// confirmation, the same standing the TUI's pair-op popup has.
const branchesList = $("branches-list");

branchesList.addEventListener("dragstart", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.n) return;
  state.dragBranch = li.dataset.n;
  // Required for a drag to start at all in Firefox; also gives the browser
  // its default drag image.
  e.dataTransfer.setData("text/plain", li.dataset.n);
  e.dataTransfer.effectAllowed = "move";
});

branchesList.addEventListener("dragover", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.n) return;
  if (!state.dragBranch || li.dataset.n === state.dragBranch) return;
  // preventDefault is what marks this element as a valid drop target.
  e.preventDefault();
  e.dataTransfer.dropEffect = "move";
  li.classList.add("drop-target");
});

branchesList.addEventListener("dragleave", (e) => {
  const li = e.target.closest("li");
  if (li) li.classList.remove("drop-target");
});

branchesList.addEventListener("dragend", () => {
  state.dragBranch = null;
  clearDropTargets();
});

branchesList.addEventListener("drop", (e) => {
  const li = e.target.closest("li");
  const src = state.dragBranch;
  state.dragBranch = null;
  clearDropTargets();
  if (!li || !li.dataset.n || !src) return;
  const dst = li.dataset.n;
  if (dst === src) return;
  e.preventDefault();
  showBranchPairMenu(src, dst, e.clientX, e.clientY);
});

function clearDropTargets() {
  for (const el of $("branches-list").querySelectorAll(".drop-target")) {
    el.classList.remove("drop-target");
  }
}

// showBranchPairMenu offers the two-branch operations on (dragged, dropped-on).
// Directions are spelled out in the labels so the pair never carries implicit
// meaning: merge ends on dst, rebase rewrites and ends on src.
function showBranchPairMenu(src, dst, x, y) {
  showCtxMenu(
    [
      {
        label: "merge " + src + " into " + dst,
        act: () => startOp({ op: "merge", branch: src, onto: dst }, "merging " + src + " into " + dst),
      },
      {
        label: "rebase " + src + " onto " + dst,
        act: () => startOp({ op: "rebase", branch: src, onto: dst }, "rebasing " + src + " onto " + dst),
      },
    ],
    x,
    y
  );
}
```

- [ ] **Step 4: Style the drop target**

In `internal/web/static/style.css`, add after the sidebar list rules (near the `.ab` / `#branches-list li` rules):

```css
/* A branch row accepting a drop — the only feedback that the pair is valid. */
#branches-list li.drop-target {
  outline: 1px solid var(--lane0);
  outline-offset: -1px;
  background: rgba(110, 168, 254, 0.12);
}
```

- [ ] **Step 5: Build and check it by hand**

Run:

```bash
go build -o ./gg ./cmd/gg && ./gg web
```

In the browser (hard-reload, the JS is embedded):
1. Drag a non-checked-out branch onto another — the hovered row outlines, the source row does not.
2. Release — the menu appears at the pointer with both rows, naming the branches in the right order.
3. Click "merge …" — the status line shows `⟳ merging … `, the op runs, the sidebar refreshes.
4. Drag a branch onto itself — no outline, no menu on release.
5. Press Escape with the menu open — it closes without running anything.

Expected: all five behave as described. If a drop does nothing, check that `dragover` called `preventDefault` (without it the browser never fires `drop`).

- [ ] **Step 6: Verify the conflict path once**

Create a genuine conflict (two branches editing the same line), drag one onto the other, choose merge. Expected: the decision modal appears with the `merge-conflict` options, and choosing `keep-conflicts` leaves the conflict in the tree. Note in the commit message what the browser reported — the spec flags that `keep-conflicts` returns `Changed:true` **and** an error, and the message should read as conflicts-left-in-tree, not a bare failure. **If the wording is wrong, do not fix it here** — record it as a follow-up; it is transport-wide behavior, not drag-and-drop behavior.

- [ ] **Step 7: Commit**

```bash
git add internal/web/static/app.js internal/web/static/style.css
git commit -m "feat(web): drag a branch onto another to merge or rebase

Drop opens the shared ctx-menu with both directions spelled out; the menu
row is the confirmation, matching the TUI pair-op popup. Reuses showCtxMenu
and startOp — no new overlay component or layer type."
```

---

### Task 4: Align the line-mode commit dot with the graph's leftmost lane

Independent of Tasks 1-3; it can be done first if preferred.

**Files:**
- Modify: `internal/web/static/app.js` (`graphHTML` at lines 735-743; `wtRowHTML`'s dot at line 697)
- Modify: `internal/web/static/style.css` (line 171, the `.flatdot` margin rule; line 99, the `.crow.wt .graph` margin)

**Interfaces:**
- Consumes: `CELL_W = 14` (line 754), `ROW_H = 22` (line 1), `HALF = CELL_W / 2` (line 755), `MID = ROW_H / 2` (line 756), `laneColor(i)` (line 759), `runes(s)`.
- Produces: nothing consumed by later tasks.

**Why:** graph mode draws `<circle cx="${x + HALF}">`, putting the leftmost lane's centre at 7px. Line mode draws a text `●` glyph starting at x=0, so its centre lands wherever the font's advance width puts it. The working-tree row hard-codes a text `●` in *both* modes and aligns with neither. Matching the geometry — not the margin — makes the alignment immune to font changes.

- [ ] **Step 1: Replace the flat dot with a one-cell SVG**

In `app.js`, replace the body of `graphHTML`'s `off` branch:

```js
function graphHTML(row, feedIdx) {
  if (state.graphMode === "off") {
    // flat mode: one dot per row in the commit's lane color — dots keep
    // rows visually separate (full-height bars merged into one line).
    // Drawn as a ONE-CELL SVG with the graph's own geometry so its centre
    // lands exactly on the leftmost lane's centre; a text glyph would
    // centre wherever the font's advance width happens to put it.
    const col = runes(row.cells || "").indexOf("●");
    return flatDotSVG(laneColor(col >= 0 ? col >> 1 : 0));
  }
  return graphSVG(row, feedIdx);
}

// flatDotSVG draws a single node dot in a one-cell box, identical in
// geometry to graphSVG's leftmost-lane circle. It keeps the .flatdot class
// so the existing spacing rule still applies — graph mode's own spacing
// must not change.
function flatDotSVG(color) {
  return (
    `<svg class="flatdot" width="${CELL_W}" height="${ROW_H}" viewBox="0 0 ${CELL_W} ${ROW_H}">` +
    `<circle cx="${HALF}" cy="${MID}" r="4" fill="${color}"/></svg>`
  );
}
```

The colour is passed in rather than derived from a lane index, because the working-tree row (Step 2) needs its own yellow, not a lane colour.

Note: `CELL_W`/`HALF`/`MID`/`laneColor` are declared at lines 754-765, *below* `graphHTML`. `const` declarations are hoisted-but-uninitialized, so this is only safe because `graphHTML` runs at render time, never at module load — the same reason today's `graphSVG` can reference them. Do not call `flatDotSVG` from top-level code.

- [ ] **Step 2: Use the same dot on the working-tree row, keeping its yellow**

In `wtRowHTML` (line 697), replace the hard-coded glyph:

```js
    `<span class="graph">${flatDotSVG("#e0c06c")}</span>` +
```

The colour is now explicit because `.crow.wt .graph { color: #e0c06c }` styled a *text* glyph; an SVG circle takes its own `fill` and would otherwise render in a lane colour. This aligns the working-tree row in **both** modes, since it uses the same one-cell geometry.

- [ ] **Step 3: Keep the flat-mode spacing, leave graph mode alone**

In `style.css`, exactly one change — line 171 targets a `<span>` that is now an `<svg>`, and `margin-right` on an SVG element needs `display` set to apply reliably:

```css
.crow .graph .flatdot { display: inline-block; margin-right: 10px; }
```

Do **not** add a margin to `.crow .graph` (line 36): that rule covers graph mode too, and widening its gap would shift every subject in graph mode for no reason.

Leave line 99 (`.crow.wt .graph { color: #e0c06c; margin-right: 12px; }`) untouched. The `color` is now inert but harmless, and `margin-right` is on the *right* side of the dot — it cannot move the dot itself, only the subject that follows, which is pre-existing behaviour and out of scope.

- [ ] **Step 4: Build and compare the two modes**

Run:

```bash
go build -o ./gg ./cmd/gg && ./gg web
```

In the browser (hard-reload): note the dot's horizontal position on a commit row in graph mode, toggle to line mode, and confirm the dot has not moved. Repeat with the working-tree row visible (leave an uncommitted change in the repo so the row renders). Check a commit on a non-zero lane too — in line mode it keeps its lane *colour* but sits in the leftmost column, which is the intended behaviour.

Expected: the dot column is identical in both modes, and the subject column starts at the same x.

- [ ] **Step 5: Commit**

```bash
git add internal/web/static/app.js internal/web/static/style.css
git commit -m "fix(web): align the line-mode dot with the graph's leftmost lane

Line mode drew a text ● glyph while graph mode draws an SVG circle at
cx=CELL_W/2, so the gutter shifted on toggle; the working-tree row's bare ●
aligned with neither. Both now use a one-cell SVG with the graph's own
geometry, so alignment holds regardless of font."
```

---

### Task 5: Full verification and docs

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `CLAUDE.md` (the `web` row of the package map)

- [ ] **Step 1: Run the full suite with the race detector**

Run: `./test.sh race`
Expected: PASS. This is the gate before the branch is merged into `web-dev`.

- [ ] **Step 2: Add the CHANGELOG entry**

Under the current unreleased section, add:

```markdown
- **web:** drag a branch onto another in the sidebar to merge or rebase —
  the drop opens a menu offering "merge A into B" / "rebase A onto B",
  dispatching `engine.SmartMerge` / `engine.SmartRebase` over the existing
  op transport (conflicts park in the decision modal).
- **web:** the line-mode commit dot now aligns with the graph's leftmost
  lane; the working-tree row's dot aligns in both modes.
```

- [ ] **Step 3: Update the `web` package-map row in CLAUDE.md**

Append to the `web` row, after the wave-3 material:

```
Ops #12-13: `op:"merge"` / `op:"rebase"` (`engine.SmartMerge{Source,Target}` /
`engine.SmartRebase{Branch,Onto}`; both names isGitArgSafe-validated, existence
and source==target left to the engine) behind sidebar **branch drag-and-drop** —
dragging a branch onto another opens the shared ctx-menu with both directions
spelled out (`merge A into B` / `rebase A onto B`), the menu row being the
confirmation, matching the TUI pair-op popup; a conflict forks
`merge-conflict`/`rebase-conflict` into the existing parking modal. Both ops are
worktree-aware in the engine, so an arbitrary drop pair needs no client-side
precondition. The commits pane's line mode (`graphMode="off"`) now draws its dot
as a ONE-CELL SVG (`flatDotSVG(color)`, the same `CELL_W`/`ROW_H`/`HALF`/`MID`
constants `graphSVG` uses) instead of a text `●` glyph, so the dot sits exactly
on the leftmost lane's centre in both modes and cannot drift with the font;
`wtRowHTML` uses the same dot, passing its yellow explicitly since an SVG
circle takes a `fill` rather than the inherited CSS `color`.
```

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md CLAUDE.md
git commit -m "docs: record web branch drag-and-drop and the line-mode dot fix"
```

- [ ] **Step 5: Report, do not merge**

Report to the user: the branch, the commits, `./test.sh race` output, and what was checked by hand in the browser. **Do not merge** — `feat/web-branch-dnd` merges into `web-dev`, and that merge is the user's call to authorize.

---

## Deferred (from the spec's Part B)

Not in this plan; ordered by verification cost in
`docs/superpowers/specs/2026-07-26-web-branch-dnd-and-parity-design.md`:

**Tier 0 (client only):** resizable sidebar/panes · copy branch name · copy
commit id · copy worktree absolute path
**Tier 1 (one op case each):** fetch · pull `<branch>` (stay here) ·
push `<branch>`
**Tier 1.5:** branch solo mode
**Tier 2 (needs a one-line prompt):** rename branch · create branch ·
create worktree
**Tier 3 (needs a new view):** compare A ↔ B (a third drop-menu row) ·
previous versions…
**Tier 4:** force push (needs a posture decision first) · review branch (AI) ·
interactive rebase
