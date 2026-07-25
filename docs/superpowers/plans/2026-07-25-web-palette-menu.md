# Web command palette + global ☰ menu — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A keyboard command palette (Ctrl+K / Ctrl+P) with an MRU repo-picker mode, and a mouse ☰ menu in the top bar, both on the wave-2 layer stack.

**Architecture:** Client-only. The ☰ menu reuses the generic `showCtxMenu`. The palette is a new layer (`id "palette"`) whose `onKey` handles nav keys and lets everything else fall through to its focused input. Repo mode is the first consumer of `GET /api/repos`, feeding the existing `doReroot`.

**Tech Stack:** Vanilla JS (`internal/web/static/app.js`), HTML, CSS. No Go changes.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-25-web-palette-menu-design.md` (committed in this worktree) — binding.
- Work ONLY in `/mnt/t/others/gigagit.worktrees/feat-web-palette` on branch `feat/web-palette`.
- Do NOT touch: `renderDiff`, `openFile`, `openStatusDiff`, `updateDiffNav`, `drillOut`, the `#files-list` contextmenu handler, `#diff-nav` markup, any `.go` file (other wave-3 tracks own those).
- Every close path of the palette goes through `closePalette()`; its `input.blur()` is load-bearing.
- Option/label strings are English protocol values (no i18n in the web client).
- Gate: `node --check internal/web/static/app.js` must pass.
- Commit messages end with:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01HJ4EsSe6QUvrEADAwdC9HG`

---

### Task 1: palette + menu

**Files:**
- Modify: `internal/web/static/index.html` (top bar, palette markup, footer chip, help rows)
- Modify: `internal/web/static/style.css` (append `#palette*` rules)
- Modify: `internal/web/static/app.js` (one router insert, one footer case, one `loadRepo` line, one new region at end of file)

**Interfaces:**
- Consumes (all existing, do not change): `pushLayer/closeLayer/topLayer`, `showCtxMenu(items, x, y)`, `hideCtxMenu()`, `doPull/doPush/refreshAfterOp/openWorkingTree/toggleSidebar/toggleGraphMode/openHelp/doReroot`, `getJSON/postJSON/esc`, `opLine`, `state.op`.
- Produces: `openPalette(mode, fromCmd)`, `closePalette()`, `openGlobalMenu()` — used only within this feature.

- [ ] **Step 1: index.html edits**

In `#top`, insert as the FIRST child (before `<span id="repo-name">`):

```html
<button id="menu-btn" title="menu">☰</button>
```

Immediately after the closing `</div>` of the `#modal` overlay block (before `#help`), insert:

```html
<div id="palette" class="hidden">
  <div id="palette-box">
    <input id="palette-input" type="text" autocomplete="off" spellcheck="false" placeholder="type a command…">
    <ul id="palette-list"></ul>
  </div>
</div>
```

In `#foot`, after `<button data-act="help">? help</button>`, add:

```html
<button data-act="palette">ctrl+k palette</button>
```

In `#help-box`, add one row at the end of the Keys section and one at the end of the Mouse section (match the surrounding `.hrow` markup exactly):

```html
<div class="hrow"><span class="hkey">ctrl+k / ctrl+p</span><span>command palette (incl. switch repo)</span></div>
```
```html
<div class="hrow"><span class="hkey">☰</span><span>global menu (top-left)</span></div>
```

- [ ] **Step 2: style.css — append at end of file**

```css
/* command palette (z-index 21: above modal 10 / help 11 / ctx-menu 20) */
#palette { position: fixed; inset: 0; background: rgba(0,0,0,.55); display: flex; align-items: flex-start; justify-content: center; z-index: 21; }
#palette.hidden { display: none; }
#palette-box { margin-top: 18vh; background: var(--bg-alt); border: 1px solid var(--accent); border-radius: 6px; width: 90vw; max-width: 560px; display: flex; flex-direction: column; }
#palette-input { background: var(--bg); color: var(--fg); border: none; border-bottom: 1px solid var(--border); border-radius: 6px 6px 0 0; padding: 10px 14px; font: inherit; outline: none; }
#palette-list { list-style: none; margin: 0; padding: 6px 0; max-height: 50vh; overflow-y: auto; }
#palette-list li { display: flex; justify-content: space-between; gap: 14px; padding: 5px 14px; cursor: pointer; }
#palette-list li.sel { background: var(--sel); }
#palette-list li .detail { color: var(--dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 60%; }
#palette-list li.empty { color: var(--dim); cursor: default; }
#menu-btn { background: none; border: 1px solid var(--border); border-radius: 4px; color: var(--fg); font: inherit; padding: 1px 8px; margin-right: 10px; cursor: pointer; }
#menu-btn:hover { border-color: var(--accent); }
```

- [ ] **Step 3: app.js — router insert**

In the global `keydown` listener, AFTER the layer-routing block (`if (top) { … return; }`) and BEFORE the form-field guard comment ("Form fields own the keyboard"), insert:

```js
  // Palette shortcut: after layer routing (an open layer keeps the keyboard),
  // before the form-field guard (ctrl+k must work from the commit box).
  if ((e.ctrlKey || e.metaKey) && (e.key === "k" || e.key === "p")) {
    e.preventDefault(); // ctrl+p would open the browser print dialog
    openPalette("cmd");
    return;
  }
```

- [ ] **Step 4: app.js — footer case + loadRepo retention**

In the footer dispatch switch, after `case "help": openHelp(); break;`, add:

```js
    case "palette": openPalette("cmd"); break;
```

In `loadRepo`, after the `/api/repo` payload is parsed (adapt the local variable name), retain it:

```js
  state.repo = j; // {name, worktree, branch} — the palette repo-picker filters out the served root
```

(`state.repo` is a dynamic property; do not edit the `state` object literal.)

- [ ] **Step 5: app.js — new region appended at END of file** (after the `boot().catch(...)` line)

```js
// ---- command palette + global ☰ menu (wave 3) ----------------------------
// The palette is a layer with an input INSIDE it: onKey consumes nav keys
// (returns true) and returns false for everything else, so the browser
// delivers the keystroke to the focused input while the router's
// non-empty-stack short-circuit keeps global keys off. Every close path MUST
// go through closePalette() — the input.blur() is load-bearing: a focused
// input after close would trap all global keys in the form-field guard.

let pal = null; // {mode: "cmd"|"repo", fromCmd, rows, filtered, sel}

function paletteCommands() {
  return [
    { label: "pull", detail: "p", run: () => doPull() },
    { label: "push", detail: "P", run: () => doPush() },
    { label: "refresh", detail: "r", run: () => { if (!state.op) refreshAfterOp(); } },
    { label: "switch repo…", detail: "", run: null }, // drills into repo mode (runPaletteRow)
    { label: "open working tree", detail: "", run: () => openWorkingTree() },
    { label: "toggle sidebar", detail: "b", run: () => toggleSidebar() },
    { label: "toggle graph", detail: "g", run: () => toggleGraphMode() },
    { label: "help", detail: "?", run: () => openHelp() },
  ];
}

function openPalette(mode, fromCmd) {
  const already = !!pal;
  pal = { mode, fromCmd: !!fromCmd, rows: [], filtered: [], sel: 0 };
  if (!already) pushLayer("palette", $("palette"), { onKey: paletteKey });
  $("palette-input").value = "";
  if (mode === "cmd") {
    pal.rows = paletteCommands();
    filterPalette();
  } else {
    renderPalette([{ label: "loading…", empty: true }]);
    getJSON("/api/repos")
      .then((j) => {
        if (!pal || pal.mode !== "repo") return; // closed or switched meanwhile
        const cur = state.repo && state.repo.worktree;
        pal.rows = (j.repos || [])
          .filter((r) => r.path !== cur)
          .map((r) => ({ label: r.name, detail: r.path, path: r.path }));
        filterPalette();
      })
      .catch((e) => {
        closePalette();
        opLine("error: " + (e.message || e), true);
      });
  }
  $("palette-input").focus();
}

function closePalette() {
  closeLayer("palette");
  $("palette-input").blur();
  pal = null;
}

function filterPalette() {
  if (!pal) return;
  const q = $("palette-input").value.trim().toLowerCase();
  pal.filtered = pal.rows.filter(
    (r) => !q || r.label.toLowerCase().includes(q) || (r.detail || "").toLowerCase().includes(q)
  );
  pal.sel = 0;
  renderPalette(pal.filtered.length ? pal.filtered : [{ label: pal.mode === "repo" ? "no other repos" : "no match", empty: true }]);
}

function renderPalette(rows) {
  $("palette-list").innerHTML = rows
    .map((r, i) =>
      r.empty
        ? `<li class="empty">${esc(r.label)}</li>`
        : `<li data-i="${i}"${i === pal.sel ? ' class="sel"' : ""}><span>${esc(r.label)}</span><span class="detail">${esc(r.detail || "")}</span></li>`
    )
    .join("");
}

function runPaletteRow(row) {
  if (!row) return;
  if (pal.mode === "repo") {
    const path = row.path;
    closePalette();
    doReroot(path);
    return;
  }
  if (row.label === "switch repo…") {
    openPalette("repo", true);
    return;
  }
  const run = row.run;
  closePalette();
  run();
}

function paletteKey(e) {
  if (!pal) return false;
  if (e.key === "ArrowDown" || e.key === "ArrowUp") {
    const n = pal.filtered.length;
    if (n) {
      pal.sel = Math.min(n - 1, Math.max(0, pal.sel + (e.key === "ArrowDown" ? 1 : -1)));
      renderPalette(pal.filtered);
    }
    e.preventDefault();
    return true;
  }
  if (e.key === "Enter") {
    runPaletteRow(pal.filtered[pal.sel]);
    e.preventDefault();
    return true;
  }
  if (e.key === "Escape") {
    if (pal.mode === "repo" && pal.fromCmd) openPalette("cmd");
    else closePalette();
    e.preventDefault();
    return true;
  }
  if (e.key === "Tab") {
    e.preventDefault();
    return true;
  }
  return false; // typing lands in the focused input; its input event re-filters
}

$("palette-input").addEventListener("input", filterPalette);
$("palette").addEventListener("click", closePalette); // backdrop
$("palette-box").addEventListener("click", (e) => e.stopPropagation());
$("palette-list").addEventListener("click", (e) => {
  const li = e.target.closest("li[data-i]");
  if (li && pal) runPaletteRow(pal.filtered[Number(li.dataset.i)]);
});

function openGlobalMenu() {
  const r = $("menu-btn").getBoundingClientRect();
  showCtxMenu(
    [
      { label: "pull", act: () => doPull() },
      { label: "push", act: () => doPush() },
      { label: "refresh", act: () => { if (!state.op) refreshAfterOp(); } },
      { label: "switch repo…", act: () => openPalette("repo") },
      { label: "command palette…", act: () => openPalette("cmd") },
      { label: "toggle sidebar", act: () => toggleSidebar() },
      { label: "toggle graph", act: () => toggleGraphMode() },
      { label: "help", act: () => openHelp() },
    ],
    r.left,
    r.bottom + 4
  );
}

$("menu-btn").addEventListener("click", (e) => {
  // stopPropagation: the document-level outside-click closer would otherwise
  // see this same click and close the menu the moment it opens.
  e.stopPropagation();
  const t = topLayer();
  if (t && t.id === "ctx") { hideCtxMenu(); return; } // second click toggles closed
  openGlobalMenu();
});
```

- [ ] **Step 6: verify**

Run: `node --check internal/web/static/app.js` — expect no output (pass).
Then `go build ./cmd/gg` (embedded assets must still build).

- [ ] **Step 7: CHANGELOG + commit**

Add to `CHANGELOG.md` under `## [Unreleased]` (top of the list):

```markdown
- web: command palette (`ctrl+k` / `ctrl+p`) — pull/push/refresh, sidebar and
  graph toggles, help, and a **switch repo…** mode listing previously-opened
  repos (the MRU registry) that re-roots the server in place. A global ☰
  menu in the top bar offers the same actions by mouse.
```

```bash
git add -A && git commit -m "feat(web): command palette (ctrl+k) + global ☰ menu, with MRU repo switching"
```
(with the Global Constraints trailers)

## Self-review notes

- The `menu-btn` click toggle checks `topLayer().id === "ctx"` — if a branch RMB menu is open, the first ☰ click closes it, the second opens the menu (accepted).
- `openPalette` while already open re-uses the existing layer (pushLayer dedups); `paletteKey` is stateless (reads module `pal`), so the discarded-onKey gotcha cannot bite.
- Verified against spec: all 8 registry commands, repo-mode esc-back rule, footer chip, help rows, blur-on-close.
