# Web track B: worktrees + tags read panels — Design

**Date:** 2026-07-23 · **Branch:** `feat/web-panels` off `web-dev` (merges back
into web-dev, never main). Runs in parallel with track A (`feat/web-commit`);
disjoint regions by design (B: new endpoint files + `server.go` routes, the
sidebar HTML/JS region, `fetchBranches`/`loadRepo`; A: `ophttp.go`, the
status-pane region, the keydown top).

## Goal

Grow the read surface toward the full client: worktrees and tags in the
sidebar, with tags linking into commit detail.

## A. Endpoints (new files `worktrees.go`, `tags.go`)

- `GET /api/worktrees` → `domain.Worktrees` →
  `{"worktrees":[{"path","branch","head","detached","bare"}]}` (from
  `model.Worktree{Path, Branch, Head, Detached, Bare}`; `branch` "" when
  detached/bare).
- `GET /api/tags` → `domain.Tags` →
  `{"tags":[{"name","target","annotated","subject"}], "truncated": bool}` —
  **capped at 100 rows** (`maxTagRows = 100`, order as returned by the
  domain); `truncated:true` when the cap cut anything (linux has 937 tags;
  the sidebar is not the place for all of them).
- Both read-only (hostGuard only), both via domain queries (no
  `internal/git` import).

## B. SPA: three-section sidebar + tag → commit detail

- `#branches-pane` gains stacked sections: BRANCHES (unchanged), WORKTREES,
  TAGS, each `<div class="side-header">` + `<ul>`. Sidebar already scrolls.
- Worktrees rows: branch name (or `(detached)` / `(bare)`), dim path
  basename; the CURRENT worktree marked (compare against the repo's
  worktree path — `loadRepo` stores it as `state.worktree`). Display-only,
  no click action (switching the served repo is out of scope).
- Tags rows: name + dim subject (when present). **Click → open that commit's
  detail screen** via a new `openCommitByHash(hash, title)` — fetches
  `/api/commit/{sha}` directly and enters the detail layout without needing
  a feed row (title = tag name + subject). When `truncated`, a dim trailing
  row `… N more (capped)` — display-only.
- Data flow: `fetchBranches()` becomes the one sidebar loader — it fetches
  branches, worktrees, and tags in parallel (worktrees/tags failures degrade
  to empty sections, never block branches) and renders all three sections.
  Its existing call sites (boot, refreshAfterOp) are untouched.

## C. Testing

Go (real git, httptest): worktrees contract (fixture adds a second worktree
via `git worktree add -b w2`; assert both rows, paths non-empty, branch
names, exactly one row for the main worktree path); tags contract (3 tags,
one annotated with subject → fields correct; `truncated:false`); the cap
(105 lightweight tags → exactly 100 rows + `truncated:true`).
JS untested-by-design: node --check + build + curl smoke + Playwright
(sidebar shows all three sections; clicking a tag opens the commit detail).

## D. Out of scope

Worktree switch/create/remove from the web, tag create/delete/push, remote
branches section, tag search/paging beyond the cap, stashes panel.
