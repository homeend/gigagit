# Web conflict surface: control + block picker — design

**Date:** 2026-07-31
**Status:** approved (brainstorm complete)
**Branch:** `feat/web-conflict-surface` (off `web-dev`)

## What

Today a conflicted file in the web working-tree screen is a dead end: the row
renders "conflicted — resolve in the TUI". This increment gives the web a real
conflict story in two parts:

1. **Sequencer control** — see the paused op (merge / rebase / cherry-pick /
   revert), who it involves, and drive it: continue when everything is
   resolved, abort to roll back.
2. **In-browser block picker** — resolve a conflicted file by choosing
   ours/theirs per conflict block, reusing the proven inline hunk-staging
   pattern and the existing engine op `ResolveConflictHunks`.

Out of scope (deferred, deliberately): the AI conflict lane (most `conflict`
agents are terminal-handover, impossible from a browser; the capture-mode
`conflict_complete` lane can ride the review-run transport in a later
increment), diff3 base display (`hunkpick.ParseConflict` skips base by
design), and editing text inside the picker (pick-only; hand edits go through
the user's editor + mark-resolved).

## Server (`internal/web`, new `conflict.go`)

### Conflict state on `/api/status`

`handleStatus` already reads a full status; it now also calls
`svc.Conflict(ctx, st)` — built precisely to derive conflict state from a
status the caller already read, no second round-trip — and the response gains:

```json
"conflict": { "op": "merge", "source": "feat/x", "target": "main", "conflicted": 2 }
```

Omitted (or `op: ""`) when nothing is paused. `domain.Conflict` also reports a
paused op with **zero** conflicted files (resolved outside, never continued),
so the web gets resume-paused-op parity for free.

### Ops #22–23: continue / abort

`POST /api/op` gains `op:"continue"` → `engine.ContinueOp{}` and `op:"abort"`
→ `engine.AbortOp{}`. Both are plain transport ops — no new arguments, any
mid-op decision parks in the existing modal. The engine owns refusals (nothing
in progress, unresolved files); the client gates buttons but the server never
pre-checks.

### Block-picker endpoints (the `/api/hunks` twins)

- `GET /api/conflict-hunks?path=` — `path` resolved against a **fresh status**;
  only a conflicted row is eligible (the discard precedent: unknown → 404,
  not conflicted → 422). Reads the working-tree bytes via
  `domain.ResolveBytes` (unstaged FileRef), parses with
  `hunkpick.ParseConflict` → response: blocks (ours/theirs text + the context
  runs between them) + a sha256 freshness hash of the file content. A
  malformed file (no/broken markers) or binary content returns a typed
  refusal (422 with a reason) instead of blocks — the client shows it plus the
  mark-resolved affordance.
- `POST /api/resolve-hunks {path, picks, hash}` (writeGuard) — re-resolves
  eligibility and reparses fresh; 409 on hash drift (picks are positional);
  every block must be picked (`ours`/`theirs`, no partial resolve); assembles
  the file with `Doc.Resolved()` and runs
  `engine.ResolveConflictHunks{Path, Content}` (write + stage in one op)
  through `runOp`. EOL fidelity comes from `Doc.EOL` as in stage-hunks.

### Mark resolved

No new server code: staging a conflicted path as-is IS mark-resolved, and
`POST /api/stage {paths}` already exists.

## Client (`app.js` / `index.html` / `style.css`)

- **Banner** — whenever `status.conflict.op` is set, a persistent strip
  renders (status screen and list screen alike):
  `⏸ merge paused (feat/x → main) — 2 conflicted`, with **Continue**
  (enabled only at zero conflicted, the TUI's `canContinue` rule) and
  **Abort** (red; client-side confirm, the delete-tag convention). With zero
  conflicted files the banner still shows and Continue lights up — never trap
  the user in a paused op they resolved by hand.
- **Picker** — clicking a conflicted row opens the picker where the diff goes
  (staged layout: file list right, picker left; the dead notice dies). Each
  block renders ours/theirs stacked with context between blocks; clicking a
  side picks it (visual language borrowed from hunk staging: highlight +
  click, `getSelection().isCollapsed` guard). A header bar shows
  `n/N picked`, **take all ours** / **take all theirs** (pure client — set
  every pick one way), and **Resolve**, enabled at full coverage. On success:
  status reload; the row leaves the Conflicts section; last file → Continue
  enables. 409 → reload the picker (the stage-hunks rule).
- **Right-click** — a conflicted row's ctx-menu gains
  "mark resolved (stage as-is)" for hand-edited files, calling `/api/stage`.
- **Refusals** — an unparseable/binary file renders the server's reason in
  the picker area plus the mark-resolved action.

## Tests

- **Go** (real git fixture: two branches editing the same lines, `merge`
  keep-conflicts): status carries the conflict object; conflict-hunks
  eligibility (unknown 404, clean file 422, malformed 422); hash-drift 409;
  resolve round-trip leaves the file staged, conflicts at zero; continue and
  abort through the transport (abort restores the pre-merge tip; continue
  after full resolution produces the merge commit); paused-with-zero-conflicts
  status still reports `op`.
- **Browser** (headless CDP, visibility asserted via `elementFromPoint`; the
  dead-row replacement check runs against the unfixed build first): banner
  appears with a live conflict; full E2E — merge with conflict → open picker
  → pick blocks → resolve → Continue → merged; take-all-theirs path; abort
  path.

## Docs (after implementation)

CHANGELOG (always), README (web feature list), CLAUDE.md `web` package-map
row, memory `web-ui-next-direction.md`. No CLI surface change → no
`using-gg`/agentskill bump.
