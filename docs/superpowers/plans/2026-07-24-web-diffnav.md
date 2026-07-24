# Web Diff Navigation + Stash Untracked Files Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the empty file list on untracked-only stashes (surface the `^3` untracked parent) and add next/prev file + next/prev change-block arrows to the diff pane header.

**Architecture:** The stash row gains `untracked_sha` (server-resolved `<ref>^3` via the existing `StashCommit` rev-parse); the client's stash drill-in becomes `openStashDetail`, concatenating the tracked and untracked file lists with a per-file sha override (`f.sha || state.fileSha` in `openFile` — the diff endpoint already skips `sha^` for status-`A` files, so the root commit diffs as pure adds unchanged). The diff header splits into `#diff-title` + a `#diff-nav` toolbar of four buttons driving `openFile(fileCursor ± 1)` and DOM-derived change-block scrolling.

**Tech Stack:** Go stdlib HTTP (existing `internal/web`), local real-git fixtures, vanilla-JS SPA.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-24-web-diffnav-design.md` (approved).
- `untracked_sha` is resolved ONLY from the server-owned `e.Ref + "^3"` (nothing client-sent); a resolve error omits the field.
- A failed `/api/commit/{untracked_sha}` fetch degrades to the tracked list alone — never a broken detail.
- Arrows never throw on empty lists/diffs — they disable or no-op. Buttons only, NO new key bindings.
- `internal/web` must not import `internal/git` (archtest-enforced).
- Work in worktree `/mnt/t/others/gigagit.worktrees/feat-web-diffnav`, branch `feat/web-diffnav`. Verify with `git branch --show-current` before ANY edit.

---

### Task 1: `untracked_sha` on the stash row

**Files:**
- Modify: `internal/web/stashes.go` (the `stashRow` struct + the resolve loop)
- Modify: `internal/web/opstash_test.go` (extend `stashesResp` with the new field; new tests appended)

**Interfaces:**
- Consumes: `s.svc.StashCommit(ctx, ref) (string, error)` — a plain `git rev-parse <ref>`, errors when the rev doesn't exist. Existing helpers: `newRepoDir`, `gitRun`, `dirtyFile` (in `opstash_test.go`), `serve`, `getJSON`.
- Produces: `GET /api/stashes` rows gain `untracked_sha,omitempty`. Task 2's client reads `st.untracked_sha`.

- [ ] **Step 1: Write the failing tests**

In `internal/web/opstash_test.go`, extend the existing `stashesResp` struct by adding one field to its row type (keep the others):

```go
		UntrackedSha string `json:"untracked_sha"`
```

Then append these tests:

```go
func TestStashUntrackedOnly(t *testing.T) {
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("brand new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "stash", "push", "-u", "-m", "untracked only")
	ts := serve(t, New(domain.Open(dir)))

	var body stashesResp
	if code := getJSON(t, ts, "/api/stashes", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(body.Stashes) != 1 {
		t.Fatalf("stashes = %+v", body.Stashes)
	}
	row := body.Stashes[0]
	if row.UntrackedSha == "" {
		t.Fatal("untracked_sha missing on a -u stash")
	}
	if want := gitRun(t, dir, "rev-parse", "stash@{0}^3"); row.UntrackedSha != want {
		t.Errorf("untracked_sha = %q, want %q", row.UntrackedSha, want)
	}
	// The untracked parent is a root commit: its file list shows the file as
	// added, and the diff endpoint serves its content (status=A skips sha^).
	var cf struct {
		Files []struct {
			Path   string `json:"path"`
			Status string `json:"status"`
		} `json:"files"`
	}
	if code := getJSON(t, ts, "/api/commit/"+row.UntrackedSha, &cf); code != http.StatusOK {
		t.Fatalf("commit files code = %d", code)
	}
	if len(cf.Files) != 1 || cf.Files[0].Path != "new.txt" || cf.Files[0].Status != "A" {
		t.Fatalf("untracked parent files = %+v", cf.Files)
	}
	var d struct {
		Rows   []map[string]any `json:"rows"`
		Binary bool             `json:"binary"`
	}
	q := "/api/diff?sha=" + row.UntrackedSha + "&path=new.txt&status=A"
	if code := getJSON(t, ts, q, &d); code != http.StatusOK {
		t.Fatalf("diff code = %d", code)
	}
	if len(d.Rows) == 0 {
		t.Error("diff of the untracked file is empty")
	}
}

func TestStashTrackedOnlyHasNoUntrackedSha(t *testing.T) {
	dir := newRepoDir(t, 1)
	dirtyFile(t, dir, "edited\n")
	gitRun(t, dir, "stash", "push", "-m", "tracked only")
	ts := serve(t, New(domain.Open(dir)))

	var body stashesResp
	if code := getJSON(t, ts, "/api/stashes", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(body.Stashes) != 1 || body.Stashes[0].Sha == "" {
		t.Fatalf("stashes = %+v", body.Stashes)
	}
	if body.Stashes[0].UntrackedSha != "" {
		t.Errorf("untracked_sha = %q on a plain stash, want empty", body.Stashes[0].UntrackedSha)
	}
}

func TestStashMixedTrackedAndUntracked(t *testing.T) {
	dir := newRepoDir(t, 1)
	dirtyFile(t, dir, "edited\n")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("brand new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "stash", "push", "-u", "-m", "mixed")
	ts := serve(t, New(domain.Open(dir)))

	var body stashesResp
	getJSON(t, ts, "/api/stashes", &body)
	if len(body.Stashes) != 1 {
		t.Fatalf("stashes = %+v", body.Stashes)
	}
	row := body.Stashes[0]
	if row.Sha == "" || row.UntrackedSha == "" {
		t.Fatalf("row = %+v, want both shas", row)
	}
	var tracked, untracked struct {
		Files []struct {
			Path   string `json:"path"`
			Status string `json:"status"`
		} `json:"files"`
	}
	getJSON(t, ts, "/api/commit/"+row.Sha, &tracked)
	getJSON(t, ts, "/api/commit/"+row.UntrackedSha, &untracked)
	if len(tracked.Files) != 1 || tracked.Files[0].Path != "f.txt" {
		t.Errorf("tracked files = %+v, want [f.txt]", tracked.Files)
	}
	if len(untracked.Files) != 1 || untracked.Files[0].Path != "new.txt" || untracked.Files[0].Status != "A" {
		t.Errorf("untracked files = %+v, want [new.txt A]", untracked.Files)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-diffnav && go test ./internal/web/ -run 'TestStashUntrackedOnly|TestStashTrackedOnly|TestStashMixed' -v`
Expected: `TestStashUntrackedOnly` and `TestStashMixed` FAIL on `untracked_sha missing` / `want both shas`; `TestStashTrackedOnlyHasNoUntrackedSha` may already pass (the field is always empty pre-change) — the other two must fail.

- [ ] **Step 3: Implement**

In `internal/web/stashes.go`:

a. Extend the row struct:

```go
type stashRow struct {
	Ref          string `json:"ref"`
	Subject      string `json:"subject"`
	Sha          string `json:"sha,omitempty"`
	UntrackedSha string `json:"untracked_sha,omitempty"`
}
```

b. In the loop, after the existing `row.Sha` resolve, add:

```go
		// A -u stash stores untracked files in a THIRD parent (a root
		// commit) invisible to the stash commit's first-parent diff;
		// surface it so the client can list and diff those files. The
		// input is the server-owned ref plus a literal — nothing
		// client-sent. No ^3 → rev-parse errors → field omitted.
		if usha, uerr := s.svc.StashCommit(r.Context(), e.Ref+"^3"); uerr == nil {
			row.UntrackedSha = usha
		}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/web/ -run 'TestStashUntrackedOnly|TestStashTrackedOnly|TestStashMixed' -v`
Expected: all 3 PASS.

- [ ] **Step 5: Run the package + archtest**

Run: `go test ./internal/web/ ./internal/archtest/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/stashes.go internal/web/opstash_test.go
git commit -m "fix(web): surface a stash's untracked-files parent as untracked_sha"
```

---

### Task 2: Stash drill-in merge + diff-nav toolbar + docs

**Files:**
- Modify: `internal/web/static/index.html` (the `#diff-header` div, line ~46)
- Modify: `internal/web/static/app.js` (state init; the stashes-list click handler + `showStashMenu`'s "show changes"; a new `openStashDetail` after `openCommitByHash`; `openFile`'s query line + the three `$("diff-header").textContent` sites; a new diff-nav block; `renderDiff`)
- Modify: `internal/web/static/style.css` (after the `#files-header, #diff-header` rule)
- Modify: `CHANGELOG.md` (top of the unreleased section)
- Modify: `CLAUDE.md` (the `web` row — one appended sentence)

**Interfaces:**
- Consumes: Task 1's `st.untracked_sha`. Existing client pieces: `openCommitByHash` (the template for `openStashDetail`), `openFile(i)`, `state.fileCursor`/`state.files`/`state.statusEntries`/`state.filesMode`/`state.fileSha`, `renderFiles`, `setLayout`, `focusPane`, `getJSON`, `esc`.
- Produces: `openStashDetail(st)`, `updateDiffNav()`, `stepFile(delta)`, `stepChange(delta)`, `diffChangeBlocks()`, `state.diffBlockIdx`, `#diff-title`/`#diff-nav` DOM.

- [ ] **Step 1: index.html — split the diff header**

Replace `<div id="diff-header"></div>` with:

```html
    <div id="diff-header">
      <span id="diff-title"></span>
      <span id="diff-nav">
        <button id="prev-file" title="previous file">‹ file</button>
        <button id="next-file" title="next file">file ›</button>
        <button id="prev-change" title="previous change">‹ change</button>
        <button id="next-change" title="next change">change ›</button>
      </span>
    </div>
```

- [ ] **Step 2: app.js — retarget the title writes**

Change ALL THREE `$("diff-header").textContent = …` sites (in `openFile`, `openStatusDiff`, and the detail-teardown that clears it) to `$("diff-title").textContent = …`. There are exactly three; a leftover `diff-header` write would wipe the toolbar buttons.

- [ ] **Step 3: app.js — openStashDetail + per-file sha**

a. In the `state` initializer, after `lastDiff: null,` add:

```js
  diffBlockIdx: -1,
```

b. After the `openCommitByHash` function, add:

```js
// openStashDetail opens a stash's changes: the stash commit's tracked
// first-parent diff plus, when present, its untracked-files parent
// (stash^3 — a root commit whose file list shows every untracked file as
// added). Untracked rows carry a per-file sha so their diffs read from
// that parent; a failed untracked fetch degrades to the tracked list.
async function openStashDetail(st) {
  const body = await getJSON("/api/commit/" + st.sha);
  let files = body.files || [];
  if (st.untracked_sha) {
    const u = await getJSON("/api/commit/" + st.untracked_sha).catch(() => ({ files: [] }));
    files = files.concat((u.files || []).map((f) => ({ ...f, sha: st.untracked_sha })));
  }
  state.files = files;
  state.fileCursor = 0;
  state.fileSha = st.sha;
  state.pane = "files";
  state.filesMode = "commit";
  setLayout("detail");
  $("files-header").textContent = "≡ " + st.ref;
  renderFiles();
  focusPane();
  if (state.files.length) openFile(0);
}
```

c. Route both stash drill-in sites through it. The stashes-list click handler becomes:

```js
$("stashes-list").addEventListener("click", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.h) return; // a sha-less row ignores left-click
  const st = state.stashes.find((x) => x.ref === li.dataset.r);
  if (st) openStashDetail(st);
});
```

And in `showStashMenu`, the "show changes" item becomes:

```js
  if (st.sha) items.push({ label: "show changes", act: () => openStashDetail(st) });
```

d. In `openFile`, the query line gains the per-file override:

```js
  const q = new URLSearchParams({ sha: f.sha || state.fileSha, path: f.path, status: f.status });
```

- [ ] **Step 4: app.js — the diff-nav block**

a. After the `renderDiff` function, add:

```js
// --- diff-pane navigation (the diff-header toolbar) ---

function activeFileList() {
  return state.filesMode === "status" ? state.statusEntries : state.files;
}

function updateDiffNav() {
  const list = activeFileList();
  $("prev-file").disabled = list.length === 0 || state.fileCursor <= 0;
  $("next-file").disabled = list.length === 0 || state.fileCursor >= list.length - 1;
  const any = diffChangeBlocks().length > 0;
  $("prev-change").disabled = !any;
  $("next-change").disabled = !any;
}

function stepFile(delta) {
  const list = activeFileList();
  const i = state.fileCursor + delta;
  if (i < 0 || i >= list.length) return;
  openFile(i);
}

// diffChangeBlocks returns the first row of each contiguous non-"same" run
// in the rendered diff table (add/del/change rows; a unified changed pair
// renders del+add adjacent — still one run). Derived from the live DOM so
// it survives any render mode (side-by-side, unified, single-column).
function diffChangeBlocks() {
  const rows = $("diff-body").querySelectorAll("table.diff tr");
  const blocks = [];
  let inBlock = false;
  rows.forEach((tr) => {
    const change = !tr.classList.contains("same");
    if (change && !inBlock) blocks.push(tr);
    inBlock = change;
  });
  return blocks;
}

function stepChange(delta) {
  const blocks = diffChangeBlocks();
  if (!blocks.length) return;
  const i = Math.max(0, Math.min(blocks.length - 1, state.diffBlockIdx + delta));
  state.diffBlockIdx = i;
  const tr = blocks[i];
  tr.scrollIntoView({ block: "center" });
  tr.classList.add("flash");
  setTimeout(() => tr.classList.remove("flash"), 600);
}

$("prev-file").addEventListener("click", () => stepFile(-1));
$("next-file").addEventListener("click", () => stepFile(1));
$("prev-change").addEventListener("click", () => stepChange(-1));
$("next-change").addEventListener("click", () => stepChange(1));
```

b. Wire the refresh points in `renderDiff`: after `state.lastDiff = d;` add `state.diffBlockIdx = -1;`; add `updateDiffNav();` immediately before BOTH early `return`s (the binary and too-large notices) and at the very end of the function.

c. In `openFile`, after the `renderFiles();` call, add `updateDiffNav();` (file arrows update synchronously; change arrows refresh when the diff renders).

- [ ] **Step 5: style.css — toolbar styling**

After the existing `#files-header, #diff-header { … }` rule, add:

```css
#diff-header { display: flex; align-items: center; gap: 8px; }
#diff-title { flex: 1 1 auto; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
#diff-nav { flex: 0 0 auto; display: flex; gap: 4px; }
#diff-nav button {
  background: var(--bg); color: var(--fg); border: 1px solid var(--border);
  border-radius: 3px; padding: 0 8px; font: inherit; font-size: 11px; cursor: pointer;
}
#diff-nav button:hover:not(:disabled) { border-color: var(--accent); }
#diff-nav button:disabled { opacity: .45; cursor: default; }
table.diff tr.flash td { background: var(--sel); }
```

(The first rule extends `#diff-header` with flex layout; its existing padding/sticky rule stays.)

- [ ] **Step 6: Build, test, JS syntax check**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-diffnav && go build ./cmd/gg && go test ./internal/web/ && node --check internal/web/static/app.js`
Expected: build OK, tests PASS, no JS syntax error.

- [ ] **Step 7: Update CHANGELOG.md and CLAUDE.md**

CHANGELOG.md — add at the top of the current unreleased section:

```markdown
- `gg web`: diff-pane navigation + stash untracked files. The diff header
  gains ‹/› arrows stepping between files and between change blocks within
  a diff. A stash's untracked files (stored in its `^3` parent, invisible
  to a first-parent diff — an untracked-only stash listed NO files) now
  appear in the stash drill-in, diffed as added content.
```

CLAUDE.md — in the `web` row, after the Ops #8-11 sentence, append (row stays ONE physical line):

```
Stash rows also carry `untracked_sha` (the `^3` untracked-files parent, server-resolved; the client merges its file list into the stash drill-in with a per-file sha override — the same first-parent blind spot exists in the TUI, unfixed); the diff header is `#diff-title` + a `#diff-nav` toolbar (file/change-block arrows over `fileCursor`/DOM-derived blocks).
```

- [ ] **Step 8: Commit**

```bash
git add internal/web/static/index.html internal/web/static/app.js internal/web/static/style.css CHANGELOG.md CLAUDE.md
git commit -m "feat(web): diff-nav arrows + stash untracked files in the drill-in"
```
