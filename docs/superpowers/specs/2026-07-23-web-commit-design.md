# Web track A: commit from the status pane — Design

**Date:** 2026-07-23 · **Branch:** `feat/web-commit` off `web-dev` (merges back
into web-dev, never main). Runs in parallel with track B (`feat/web-panels`);
the two deliberately touch disjoint file regions (A: `ophttp.go`, the status
pane's JS/HTML region, the keydown top; B: new endpoint files, the sidebar
region, `fetchBranches`/`loadRepo`).

## Goal

Complete the web's daily loop — stage → **commit** — as the op transport's
second operation, proving the transport generalizes (op-specific params).

## A. Server: `op:"commit"` (in `internal/web/ophttp.go` only)

`opStartRequest` gains `Message string `json:"message"``. New switch case:

- `op:"commit"`: `strings.TrimSpace(Message)` must be non-empty → else 400
  (`message required`); on success `engine.Commit{Message}` (no `All`, no
  `Amend` — out of scope). Everything else (202/409/SSE/decide) is the
  existing transport unchanged. `engine.Commit` never forks; its
  `committed <short-sha> <subject>` summary reaches the op line via `done`.
  Committing with nothing staged fails inside git → `done{ok:false}` with the
  error — no special-casing.

## B. SPA: commit box on the working-tree screen

- A `#commit-box` (textarea + `commit` button) rendered in the files pane,
  visible ONLY in status mode. Button disabled while `counts.staged == 0` or
  an op is live. **Ctrl+Enter** (or Cmd+Enter) in the textarea commits.
- `startSwitch` is refactored into a generic `startOp(body, label)` (the
  transport client is op-agnostic; switch and commit both call it).
  `state.op` carries `kind` (the op name) so `done{ok:true}` of a COMMIT
  clears the textarea (a successful switch must not eat a draft message).
- **Form-field keyboard guard (the critical fix):** the global keydown
  handler early-returns when `e.target.closest("input,textarea")` — today,
  typing a commit message would trigger j/k/s/u navigation/staging. The
  guard also implements Ctrl+Enter-commits inside the textarea. It sits at
  the VERY TOP of the listener, before the modal block.
- After a successful commit the standard `done{changed}` refresh runs; a
  now-clean tree drops back to the full-width list (existing behavior).

## C. Testing

Go (real git, httptest, existing helpers incl. `readSSE`): commit round-trip
(stage a file → POST op commit with a subject+body message → SSE to
`done{ok,changed}` → git log verifies subject, summary contains
`committed`); whitespace-only message → 400; nothing staged → `done{ok:false}`.
JS untested-by-design: node --check + build + curl smoke + Playwright pass
(scratch repo: stage via `s`, type message, commit, list returns clean).

## D. Out of scope

Amend, `All`, message generation (ctrl+g equivalent), commit template/hook
UI, multi-line subject validation beyond trim.
