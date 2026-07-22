# Web write-pathway, increment 1: working-tree status + stage/unstage — Design

**Date:** 2026-07-22
**Branch:** `feat/web-stage`, based on `feat/web-probe` (tip `2b9793b8`), NOT on main.
The probe stays unmerged; this increment stacks on it. Merge-to-main is a later,
human decision covering both.

## Goal

Prove the web frontend can run a **mutating operation** through `domain.Execute`
(archtest-clean, no `internal/git` import) and reflect the result — using the
smallest real write: whole-file stage/unstage. Adds the missing working-tree
view to the probe's SPA.

## Context / prior decisions

- End ambition (user): **full GUI client** — web becomes a first-class frontend.
- This increment deliberately **defers the streaming transport**: `engine.Stage`
  is synchronous, instant, and never forks, so the WebSocket Event stream +
  browser decision modal + blocking web Decider would be unexercised ceremony
  here (YAGNI). They arrive with the first naturally-forking op (next increment).
- Probe evaluation on linux (1.46M commits): all endpoints correct; commits page
  cost (~1.4s) is the shared domain `CommitFeed` (`--date-order` + `%D` over 937
  tags), not web-specific. Known probe inefficiency (full-feed re-send + full
  `Lay` per page) is out of scope here.

## A. Data flow / UX model

A synthetic **“● Working tree” row pinned at the top of the commit list** (the
TUI's WIP-rows model). It exists whenever the working tree is dirty (any staged,
unstaged, untracked, or conflicted file) and shows a counts badge, e.g.
`● Working tree  3 staged · 2 changed · 1 untracked`.

- Selecting the row loads `GET /api/status` into the **files pane**, grouped
  into three sections: **Staged**, **Changes** (unstaged modifications),
  **Untracked**. Conflicted files render in a fourth **Conflicts** section,
  display-only (no stage control) — resolving conflicts is out of scope.
- Each file row in Staged has an unstage control; rows in Changes/Untracked have
  a stage control. The files-pane header has **Stage all** / **Unstage all**.
- Clicking a file shows its **working-tree diff** in the diff pane (see C).
- A stage/unstage action POSTs, and the response carries **fresh status** — the
  files pane re-renders from it (one round-trip, no second fetch). The
  commit-list badge re-renders from the same payload.
- No auto-refresh/polling. Status is fetched when the row is selected and
  refreshed by each stage response. (External changes require re-selecting the
  row; acceptable for this increment.)

## B. Server endpoints (`internal/web`)

### `GET /api/status` — new file `status.go`

Calls `domain.Status(ctx)` (`model.WorkingTreeStatus`). Response:

```json
{
  "files": [
    {"path": "a/b.go", "orig_path": "", "staged": "M", "unstaged": ".",
     "kind": "tracked"}
  ],
  "counts": {"staged": 1, "unstaged": 0, "untracked": 0, "conflicted": 0}
}
```

- `staged`/`unstaged` are the porcelain XY bytes as 1-char strings
  (`model.FileStatus.Staged/.Unstaged`; `.` = unmodified).
- `kind` ∈ `tracked` | `untracked` | `conflicted` (from `model.FileKind`; the
  ignored kind never appears — status is not run with `--ignored`).
- `counts` from `model.Counts()` — the same classification the TUI shows.
- `orig_path` is set for renames/copies; omitted (`omitempty`) otherwise.

### `POST /api/stage` — new file `stage.go`

Body: `{"paths": ["a.go"], "unstage": false, "all": false}`.

Validation (400 on failure):
- `all` is mutually exclusive with `paths` and `unstage` (mirrors
  `engine.Stage`'s contract).
- Without `all`, `paths` must be non-empty.
- Every path must pass the existing `isGitArgSafe` (no leading `-`, no control
  bytes) — untrusted input flows into git argv.

Runs `domain.Execute(ctx, engine.Stage{Paths, All, Unstage})` with a discard
event sink and a fail-loud decider (`Stage` never forks; any decision request is
a bug → 500). On success, responds with the **same JSON shape as
`GET /api/status`**, freshly read after the op.

### `GET /api/diff` — extend existing `diff.go`

New query form: `?wt=unstaged|staged&path=<p>[&old=<orig>]` (mutually exclusive
with `sha`; `sha` form unchanged).

Byte sources:
- `wt=unstaged` → old = index version (`domain.ResolveBytes` on
  `model.FileRef{Source: SourceStaged, Path}`), new = working-tree file
  (`FileRef{Source: SourceUnstaged, Path}`). An untracked file's old side is
  empty (index read fails → treat as empty, rendering an all-added diff).
- `wt=staged` → old = HEAD version (`svc.ShowFile(ctx, "HEAD", oldPath)`; a
  path new in HEAD → empty old side), new = index version
  (`FileRef{Source: SourceStaged, Path}`).
- `old` (rename orig path) feeds the old side when present, like the sha form.

Same `isGitArgSafe` checks on `path`/`old`. **Working-tree diffs must not be
cached** (the working tree mutates without a key change): pass
`domain.Request{Key: ""}` — the empty key disables caching in `cachedDiffer`
(`internal/domain/differ.go`, built for exactly this case). Requirement: a
second request after the file changed on disk reflects the new content.

### `server.go` — routing + write guard

```go
mux.HandleFunc("GET /api/status", s.handleStatus)
mux.HandleFunc("POST /api/stage", s.writeGuard(s.handleStage))
```

## C. Write-endpoint hardening

A write endpoint is more dangerous than the read surface. `writeGuard` (in
`server.go`, beside `hostGuard`) enforces, in order:

1. `Content-Type` must be `application/json` (mime-parsed, parameters ignored)
   → else **415**. A cross-site HTML form cannot send this content type without
   a CORS preflight, which the server never answers.
2. If an `Origin` header is present, its host must be loopback
   (`isLoopbackHost` on the parsed URL's hostname) → else **403**. Browsers
   always send Origin on cross-site POSTs; curl/same-origin requests pass.

The existing `hostGuard` (DNS-rebinding) and loopback-only bind stay in front.
No auth token in this increment — loopback bind + Host guard + Origin guard +
content-type gate is the accepted posture for a localhost single-user tool
(matches the probe's threat model note in the spec on `feat/web-probe`).

## D. SPA changes (`internal/web/static/`, no toolchain)

- `index.html`: files-pane header gains Stage all / Unstage all buttons
  (hidden unless the working-tree row is selected); footer gains
  `s stage · u unstage`.
- `app.js`:
  - `fetchStatus()` + `state.status`; synthetic row injected at index 0 of the
    rendered commit list when dirty (feed data untouched — presentation-layer
    only, mirroring the TUI's "m.commits stays pure feed" rule).
  - `renderStatusFiles()` — the four sections; per-row button + `s`/`u`
    keybinding on the focused row; enter/click → working-tree diff via the
    `wt=` query form.
  - `stage(paths, unstage, all)` → `POST /api/stage` → re-render files pane +
    badge from the response. Errors render in the files-pane header
    (`writeErr` JSON `error` field), never silently.
- `style.css`: section headers, staged/unstaged/untracked/conflict row tints,
  badge styling.

## E. Testing

Go tests (`internal/web/*_test.go`, real git in `t.TempDir()`, probe's existing
`gitRun` helper):

1. **Status classification:** seed one staged, one unstaged-modified, one
   untracked file → JSON has the right `kind`/`staged`/`unstaged`/counts.
   Clean repo → empty files, zero counts.
2. **Stage round-trip:** POST stage of an untracked path → 200, response counts
   show it staged, and `git status --porcelain` (test-side) confirms. Unstage
   it back → confirmed. `all: true` stages everything.
3. **Stage validation:** empty paths without all → 400; `all`+`paths` → 400;
   path `--evil` → 400 (isGitArgSafe).
4. **Write guard:** POST without JSON content type → 415; with
   `Origin: http://evil.example` → 403; with `Origin: http://127.0.0.1:9` →
   200-path (guard passes; handler may still 400 on body).
5. **Working-tree diff:** modify a tracked file → `wt=unstaged` rows show the
   edit; stage it → `wt=staged` shows it and `wt=unstaged` is empty; edit again
   → second `wt=unstaged` request reflects the NEW content (cache-bypass
   requirement).
6. **Archtest:** existing rule already covers `internal/web` never importing
   `internal/git` — no new test needed, CI just keeps passing.

JS remains untested-by-design (probe convention); verified via a curl smoke run
+ human visual pass.

## F. Out of scope (explicit)

- Hunk-level staging (whole-file only).
- Commit, discard, stash from the web.
- Streaming Event transport, decision modals, web Decider (next increment,
  first forking op).
- Conflict resolution (conflicted files are visible, not actionable).
- Auto-refresh / file-watch push.
- The probe's paging inefficiency (separate fast-follow).
