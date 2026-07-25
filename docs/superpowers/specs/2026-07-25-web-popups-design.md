# Web popup/layer infrastructure — design

Date: 2026-07-25 · Branch: `feat/web-popups` off `web-dev` · Status: awaiting review

## Goal

A small generic layer system in the SPA so overlay surfaces share one
stack, one keyboard-routing rule, and one close discipline — the
foundation wave 3's command palette, global menu, and MRU repo picker
build on. Client-only; no server changes.

## Background (current state)

Three ad-hoc overlay surfaces, each with its own visibility class and its
own special case at the top of the document keydown handler, in a fixed
order: decision modal → ctx-menu → help → form-field guard → key chain.
`modalLocalCb` routes the shared modal to client-side confirms. Close
rules differ per surface (modal: esc = answer "abort" when offered; help:
esc/?/backdrop; ctx-menu: esc or any outside click). The transport closes
the modal from three places (`resolved`, `done`, lost-op give-up).

## Design

### The layer stack

A module-level `layers` array of `{ id, el, onKey, onClose }`:

- `pushLayer(id, el, opts)` — unhides `el`, pushes the record. `opts.onKey(e) -> bool`
  (true = handled) and `opts.onClose()` are optional.
- `closeLayer(id)` — removes the record WHEREVER it sits in the stack
  (not only top), hides `el`, runs `onClose`. Idempotent. This is
  load-bearing: the transport must be able to close the decision modal
  even if help was opened above it.
- `topLayer()` — the routing target.

Keydown routing replaces the three special cases with one rule: if the
stack is non-empty, offer the event to `topLayer().onKey`; if unhandled
and the key is Escape, `closeLayer(top.id)`; either way the event stops
(layers own the keyboard). The form-field guard and the key chain below
stay untouched and run only with an empty stack.

Backdrop clicks: each overlay element keeps its own click-to-close wiring,
now calling `closeLayer(id)`.

### The three existing surfaces become layers

- **Decision modal** (`id: "modal"`): `showModal` pushes; its `onKey`
  keeps the exact esc rule (answer `"abort"` when the option list has it —
  behavior byte-identical, including local confirms via `modalLocalCb`).
  `hideModal()` becomes `closeLayer("modal")` + the existing
  `modalLocalCb = null` in `onClose` — so `resolved`/`done`/lost-op keep
  working unchanged through the one entry point.
- **Help** (`id: "help"`): default close on esc, plus `?` handled in its
  `onKey`.
- **Ctx-menu** (`id: "ctx"`): pushed by `showCtxMenu`, closed by
  `hideCtxMenu` → `closeLayer("ctx")`; the existing outside-click document
  handler stays.

### Stability contract (parallel-track safety)

`showCtxMenu(items, x, y)`, `showLocalConfirm(prompt, options, cb)`,
`showModal(ev)`, `hideModal()`, `hideCtxMenu()` keep their exact
signatures and observable behavior — the re-root UI track builds against
them concurrently and must not need rebasing beyond git-level merges.

### Explicitly out of scope

Focus traps, animations, nested modal styling, any new surface (palette,
menu, picker — wave 3), any change to which keys do what.

## Risk & verification

This refactors the transport's modal path — the most regression-sensitive
client code. The gate is a Playwright sweep re-running every
already-verified overlay behavior: decision modal esc→abort, answer
buttons, danger styling, `resolved` closes the modal, local confirm
(delete-tag) flow, help open/close (`?`/esc/backdrop, form-guard
immunity), ctx-menu open/close, and one stacked case (help opened while a
decision modal is parked: esc closes help first, the modal stays and
still answers). Plus `node --check` and the web Go tests (unchanged
server).

## Docs

CHANGELOG entry; CLAUDE.md web row gains a short layer-stack sentence.
