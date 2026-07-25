# Web Popup/Layer Infrastructure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One layer stack for all overlay surfaces (decision modal, help, ctx-menu) with byte-identical behavior; the foundation for wave-3 popups.

**Architecture:** Client-only refactor of `internal/web/static/app.js`: a `layers` array + `pushLayer`/`closeLayer(id)`/`topLayer`, one keydown routing rule replacing the three special-cased blocks. No server, HTML, or CSS changes.

**Tech Stack:** vanilla ES JavaScript.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-25-web-popups-design.md` (this worktree).
- Frozen public API (the parallel re-root-UI track builds against it): `showCtxMenu(items, x, y)`, `showLocalConfirm(prompt, options, cb)`, `showModal(ev)`, `hideModal()`, `hideCtxMenu()` keep exact signatures and observable behavior.
- Behavior must be byte-identical: modal owns the whole keyboard (preventDefault on every key; esc answers `"abort"` only when offered, otherwise the modal stays); help owns the keyboard (preventDefault; esc or `?` closes); ctx-menu swallows keys WITHOUT preventDefault (esc closes); outside-click closes the ctx-menu; `hideModal` always clears `modalLocalCb`.
- `closeLayer(id)` must remove a layer from ANY stack position (the transport closes a parked modal even under an open help) and be idempotent.
- Working dir: `/mnt/t/others/gigagit.worktrees/feat-web-popups`, branch `feat/web-popups`. Verify with `git branch --show-current` before any edit.

---

### Task 1: The layer stack + surface conversion + docs

**Files:**
- Modify: `internal/web/static/app.js`
- Modify: `CHANGELOG.md`, `CLAUDE.md`

**Interfaces:**
- Produces: `pushLayer(id, el, opts)`, `closeLayer(id)`, `topLayer()` — wave 3 consumes these; `opts.onKey(e) -> bool` (true = handled; the handler calls `e.preventDefault()` itself when it wants default suppressed).

Every edit below is an exact old→new replacement; surrounding code must not otherwise change.

- [ ] **Step 1: Add the layer core**

Directly after the `lsGet`/`lsSet` helpers block, insert:

```js
// --- overlay layer stack ---
// Every overlay surface (decision modal, help, ctx-menu, future popups)
// registers here. One rule: a non-empty stack owns the keyboard — the top
// layer's onKey sees the event first; an unhandled Escape closes the top
// layer. closeLayer(id) removes a layer WHEREVER it sits in the stack:
// the op transport must be able to close a parked decision modal even
// under an open help overlay.
const layers = [];

function pushLayer(id, el, opts) {
  if (layers.some((l) => l.id === id)) return; // one instance per surface
  el.classList.remove("hidden");
  layers.push({ id, el, onKey: (opts && opts.onKey) || null });
}

function closeLayer(id) {
  const i = layers.findIndex((l) => l.id === id);
  if (i < 0) return; // idempotent
  const [l] = layers.splice(i, 1);
  l.el.classList.add("hidden");
}

function topLayer() {
  return layers[layers.length - 1] || null;
}
```

- [ ] **Step 2: Convert the modal**

Replace:

```js
function showModal(ev) {
  $("modal-prompt").textContent = ev.prompt;
  $("modal-options").innerHTML = (ev.options || [])
    .map((o) => `<button data-o="${esc(o)}"${DANGER_OPTIONS.has(o) ? ' class="danger"' : ""}>${esc(o)}</button>`)
    .join("");
  $("modal").classList.remove("hidden");
  $("modal").dataset.opts = JSON.stringify(ev.options || []);
}
```

with:

```js
function showModal(ev) {
  $("modal-prompt").textContent = ev.prompt;
  $("modal-options").innerHTML = (ev.options || [])
    .map((o) => `<button data-o="${esc(o)}"${DANGER_OPTIONS.has(o) ? ' class="danger"' : ""}>${esc(o)}</button>`)
    .join("");
  $("modal").dataset.opts = JSON.stringify(ev.options || []);
  pushLayer("modal", $("modal"), {
    onKey: (e) => {
      if (e.key === "Escape") {
        const opts = JSON.parse($("modal").dataset.opts || "[]");
        if (opts.includes("abort")) answerModal("abort"); // the TUI's esc rule
      }
      e.preventDefault();
      return true; // the modal owns the keyboard — even over a focused form field
    },
  });
}
```

(Note `dataset.opts` is set BEFORE pushLayer so a replayed decision that re-calls showModal on an already-open modal still refreshes the options; pushLayer dedups.)

Replace:

```js
function hideModal() {
  $("modal").classList.add("hidden");
  modalLocalCb = null; // a done-driven close must not leak the callback to the next modal
}
```

with:

```js
function hideModal() {
  modalLocalCb = null; // a done-driven close must not leak the callback to the next modal
  closeLayer("modal");
}
```

- [ ] **Step 3: Convert the ctx-menu**

In `showCtxMenu`, replace the final line:

```js
  menu.classList.remove("hidden");
```

with:

```js
  pushLayer("ctx", menu, {
    onKey: (e) => {
      if (e.key === "Escape") closeLayer("ctx");
      return true; // swallowed without preventDefault (today's behavior)
    },
  });
```

Replace:

```js
function hideCtxMenu() {
  $("ctx-menu").classList.add("hidden");
}
```

with:

```js
function hideCtxMenu() {
  closeLayer("ctx");
}
```

(The `document` outside-click handler and the `#ctx-menu` button-click handler stay unchanged — both call `hideCtxMenu`.)

- [ ] **Step 4: Convert help**

Add, next to the modal helpers:

```js
function openHelp() {
  pushLayer("help", $("help"), {
    onKey: (e) => {
      if (e.key === "Escape" || e.key === "?") closeLayer("help");
      e.preventDefault();
      return true; // help owns the keyboard until closed
    },
  });
}
```

Replace the two open sites:

```js
  } else if (e.key === "?") {
    $("help").classList.remove("hidden");
  } else if (e.key === "r") {
```

with:

```js
  } else if (e.key === "?") {
    openHelp();
  } else if (e.key === "r") {
```

and in the footer-chip switch:

```js
    case "help": $("help").classList.remove("hidden"); break;
```

with:

```js
    case "help": openHelp(); break;
```

Replace the click-close wiring:

```js
$("help").addEventListener("click", () => $("help").classList.add("hidden"));
```

with:

```js
$("help").addEventListener("click", () => closeLayer("help"));
```

(`$("help-box")`'s stopPropagation line stays.)

- [ ] **Step 5: Replace the keydown special-cases with the router**

Replace the head of the document keydown handler:

```js
document.addEventListener("keydown", (e) => {
  if (!$("modal").classList.contains("hidden")) {
    if (e.key === "Escape") {
      const opts = JSON.parse($("modal").dataset.opts || "[]");
      if (opts.includes("abort")) answerModal("abort"); // the TUI's esc rule
    }
    e.preventDefault();
    return; // the modal owns the keyboard — even over a focused form field
  }
  if (!$("ctx-menu").classList.contains("hidden")) {
    if (e.key === "Escape") hideCtxMenu();
    return; // the context menu owns the keyboard until closed
  }
  if (!$("help").classList.contains("hidden")) {
    if (e.key === "Escape" || e.key === "?") $("help").classList.add("hidden");
    e.preventDefault();
    return; // the help overlay owns the keyboard until closed
  }
```

with:

```js
document.addEventListener("keydown", (e) => {
  const top = topLayer();
  if (top) {
    if (top.onKey && top.onKey(e)) return;
    if (e.key === "Escape") closeLayer(top.id); // default close for layers without onKey
    return; // a non-empty stack owns the keyboard
  }
```

(The form-field guard and the key chain below stay untouched.)

- [ ] **Step 6: Gate**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-popups && node --check internal/web/static/app.js && go build ./... && go test ./internal/web/ -count=1`
Expected: all pass (no server change).

- [ ] **Step 7: Docs**

`CHANGELOG.md` (top of Unreleased, existing convention):

```
- web: overlay surfaces (decision modal, help, context menu) now share one
  layer stack with a single keyboard-routing rule — groundwork for the
  command palette and menus; behavior unchanged.
```

`CLAUDE.md`: append to the END of the `web` package-map row, before its closing ` |`:

```
 Overlays ride a client layer stack (`pushLayer`/`closeLayer(id)`/`topLayer` in app.js): the top layer's onKey sees keys first, an unhandled esc closes the top, and `closeLayer` removes a layer from ANY position (the transport can close a parked modal under an open help); modal/help/ctx-menu are layers with unchanged behavior, and wave-3 popups build on the same stack.
```

- [ ] **Step 8: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-web-popups
git add internal/web/static/app.js CHANGELOG.md CLAUDE.md
git commit -m "refactor(web): overlay layer stack (modal/help/ctx-menu), behavior identical"
```

## Verification note (controller-run, not this task)

The regression gate is a Playwright sweep of every verified overlay behavior, run by the controller before merge: modal esc→abort, answer buttons + danger styling, local-confirm flow, help open/close + form-guard immunity, ctx-menu esc + outside click, and the stacked case (help over a parked modal: esc closes help only; the modal still answers).
