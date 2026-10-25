# Web walk landing marker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans (inline, this session).

**Goal:** the web marks, in the Branches list, the row the last walk landed on (the TUI's Branches cursor after alt+a/t/A/T/q/w).

**Architecture:** `state.landing` ({sid} | {branch} | null) set by the walks and Branches sub-row opens; `renderBranches` paints `.landed` from it; two pure helpers in walk.js.

**Tech Stack:** ES-module SPA in internal/web/static; Go tests (`jsFunc` + `nodeRun`, `readStaticSrc` pins); Playwright probe.

**Spec:** docs/superpowers/specs/2026-10-25-web-walk-landing-marker-design.md

## Global Constraints

- Client only: no Go outside tests.
- Every hideable/stylable element gets its own rule (`#branches-list li.landed`); no global classes.
- A static import of a name a module does not export breaks the page: export every new name.

## Review Focus

- The mark survives a sessions-event repaint (renderBranches re-run) — probe.
- alt+w refused (op running) sets nothing; a refused switch (doReroot false) leaves the old mark.
- Two agents in one worktree: alt+A alternates the mark between the two sub-rows — probe.
- A Worktrees-row open of another session clears a session mark — unit (landingAfterOpen) + probe.
- A folded branches section unfolds on a landing — probe.

---

### Task 1: pure helpers (walk.js)

- Test `TestWalkLandingJS` (walkmarker_test.go): `landingAfterOpen(null,"s1",true)` → `{sid:"s1"}`; `({sid:"s2"},"s1",false)` → null; `({sid:"s1"},"s1",false)` → same; `({branch:"wta"},"s1",false)` → same; `(null,"s1",false)` → null. `worktreeBranch([{path:"/r/wt-a",branch:"wta"},{path:"/r/wt-b",branch:"",detached:true}], "/r/wt-a/")` → "wta"; detached → ""; unlisted → "".
- Implement both in walk.js, export.

### Task 2: state + paint + reveal (core.js, sidebar.js, style.css)

- Pins `TestWalkLandingPainted`: core.js state has `landing: null`; sidebar.js renderBranches calls `landedClass(`; sidebar exports `land`; style.css has `#branches-list li.landed`.
- Pure `landedClass(landing, kind, key)` (sidebar model): " landed" when kind matches and key equals; tested in node.
- `land(target, reveal = true)` in sidebar.js: sets state.landing, renderBranches(); when reveal and target not null: unfold branches if collapsed, scroll the `.landed` li into view (nearest).
- Branches list click: a click on an li that is not `.landed` clears the landing (before its own action); a sub-row click sets `{sid}`.

### Task 3: wire the walks (console.js, sidebar.js)

- Pins `TestWalkLandingWired`: console.js `showStop` lands `{sid}`; page stop lands `worktreeBranch(`; `cycleWorktrees` lands after `doReroot(` resolves; openConsole applies `landingAfterOpen(` with `opts.fromBranches`; sidebar's Branches sub-row click / Open session pass `fromBranches: true`.
- Implement.

### Task 4: probe + docs

- Probe probe85.mjs (fx21-like fixture with two Fake agents in wt-a, one in wt-b), Chromium + Firefox: mark after alt+a ×2 (same worktree alternation), after a sessions repaint, alt+q page stop → branch row, alt+w → target branch row, Worktrees-row open of another session clears, folded section unfolds, click on another branch clears.
- CHANGELOG, parity doc (close the row, design differences), help text line.
