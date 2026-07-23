# `gg web` read-only probe — design

Date: 2026-07-21 · Status: approved · Branch: `feat/web-probe`

## Goal

A disposable, read-only web frontend probe that answers three questions
before any larger GUI investment is considered:

1. **Performance** — does a browser render gg's data smoothly at monorepo
   scale (600k-commit feeds, instant diffs on the big test repos)?
2. **Worth** — does the look/density of a browser UI beat the TUI by enough
   to justify a fourth frontend growing beyond a probe?
3. **Fit** — does the engine/domain contract (paged `CommitFeed`, cached
   `Differ`, domain-only frontend) hold cleanly for an HTTP surface?

If the results disappoint, the deliverable is still useful: we delete one
small, isolated package and know the answer cheaply.

## Non-goals (scope guards)

- **No mutations.** No operations, no `Decider` wiring, no config writes.
  Ops-over-WebSocket with decision modals is a stage-2 question this probe
  exists to justify.
- **No auth token.** Read-only on loopback. Any future mutating stage adds
  token auth *first*. (The server does validate the request `Host` header
  against loopback names — a DNS-rebinding defense so a malicious page can't
  reach `127.0.0.1:<port>` and read repo contents; this is not auth.)
- **No node toolchain.** Plain ES modules served from `go:embed`. A JS build
  pipeline is a decision for a real product, not a probe.
- **One browser tab assumed.** The server holds one `CommitFeed` instance;
  concurrent tabs may interleave paging oddly. Acceptable for a probe.
- **No watch/auto-refresh, no i18n, no e2e scenario** (a serve loop doesn't
  fit the e2e harness's run-and-assert shape).

## CLI surface

```
gg web [--addr 127.0.0.1:0] [--open]
```

- Routed in `cmd/gg/main.go` beside `mcp` (pre-TUI branch).
- Binds **loopback only** by default; refuses a non-loopback `--addr` with a
  clear error (the read-only surface still exposes repo contents).
- Prints the resolved URL to stderr; `--open` additionally launches the
  system browser (best-effort: `xdg-open`/`open`/`cmd.exe /c start`,
  WSL-aware like `internal/clipboard`'s platform detection).
- Serves until the process is interrupted (Ctrl+C); context-cancels
  in-flight reads on shutdown.

## Package & architecture

New package `internal/web`:

- A **domain-only frontend** like `cli`/`mcp` — never imports
  `internal/git`, `internal/tui`, `internal/cli`. Archtest gains the
  matching rule.
- Owns: the `http.Server`, route handlers, JSON encoding of domain/model
  types, the embedded static assets (`//go:embed static/*`).
- Depends on: `internal/domain` (queries, `CommitFeed`, `Differ`),
  `internal/commitgraph` (lane layout, already a TUI-consumed leaf),
  `internal/model`.
- Static assets: `static/index.html`, `static/app.js` (+ small modules),
  `static/style.css`. Hand-written, no framework, no build step.

## API

Four JSON GET endpoints. All requests are context-scoped (client disconnect
cancels the domain read). Errors return `{"error": "..."}` with a 4xx/5xx
status. Responses are `application/json; charset=utf-8`.

### `GET /api/repo`

Repo identity for the header bar:

```json
{"name": "gigagit", "worktree": "/abs/path", "branch": "main"}
```

Backed by `TopLevel`/`CurrentBranch` domain queries.

### `GET /api/commits` and `GET /api/commits?more=1`

Drives the server's single `CommitFeed`. Without `more`, calls
`LoadInitial` (idempotent restart of the feed); with `more=1`, `LoadMore`.

```json
{
  "rows": [
    {"hash": "…", "short": "…", "subject": "…", "author": "…",
     "time": 1753113600,
     "refs": [{"name": "main", "kind": "local", "head": true}],
     "lane": 0, "cells": "● "}
  ],
  "can_load_more": true
}
```

- `cells` is the row's `commitgraph.Row.Cells` glyph string (two display
  columns per lane); the column-pair index is the color key. `lane` is the
  node's lane. Both come from a server-side `commitgraph.Lay` over the
  loaded feed. The layout is recomputed per response from the feed
  snapshot —
  correctness first; incremental layout is a later optimization if profiling
  demands it.
- Sort mode follows the repo's `[ui] commit_sort` config, same as the TUI.

### `GET /api/commit/{sha}`

The commit's changed files (backed by `CommitFiles`; the pane header
reuses the feed row's subject/author/time the frontend already holds):

```json
{"sha": "…",
 "files": [{"path": "a/b.go", "status": "M", "old_path": ""}]}
```

`status` is git's single letter (`A M D R C T`); `old_path` is set only
for renames/copies.

### `GET /api/diff?sha=<sha>&path=<path>[&status=M&old=<oldPath>]`

The `Differ`'s aligned side-by-side rows for one file of one commit,
served through the shared cached differ (`svc.Differ()` — the same LRU
the TUI reads). `status`/`old` come from the `/api/commit` response and
steer the old/new sides (`A` → no old side, `D` → no new side, rename →
old side read at `old`):

```json
{
  "rows": [
    {"kind": "change",
     "left": "old line", "right": "new line",
     "left_no": 41, "right_no": 41,
     "left_spans": [[4, 9]], "right_spans": [[4, 9]]}
  ],
  "binary": false, "too_large": false, "truncated": false
}
```

`kind` ∈ `same|add|del|change` (from `textdiff.Kind`); spans are the
enhanced differ's intraline half-open **rune** ranges. `binary`/`too_large`
mirror `domain.Diff`, `truncated` mirrors `textdiff.Result.Truncated`.

## Frontend

One page, three panes, keyboard-first:

- **Commits pane** (left, dominant): virtualized list — only visible rows
  exist in the DOM (~100-line hand-rolled scroller). Scrolling near the end
  fires `?more=1`; `j`/`k` move the cursor, `enter` loads the commit's
  files.
- **Files pane**: the selected commit's changed files; `j`/`k`/`enter`.
- **Diff pane**: side-by-side aligned rows with intraline span highlights.

**Graph rendering, two cuts of the same payload:**

1. First cut: render `cells` as colored monospace glyphs (zero new layout
   code; proves scale honestly).
2. Upgrade: a small glyph→stroke map (~a dozen shapes: `│ ─ ╮ ╯ ╭ ╰ ● ◉ …`)
   draws the same cells as canvas/SVG lines for GitKraken-style smoothness.
   Frontend-only change; no API impact.

## Concurrency & state

- One process, own `domain.Service` (the `gg mcp` pattern); reads run under
  the usual Read reservation with singleflight coalescing.
- Server-side mutable state is exactly: the `CommitFeed` instance (guarded
  by its own locking) and nothing else. Handlers are otherwise stateless.
- Diff responses come from the shared diff LRU; repeated opens are cache
  hits.

## Testing

- **TDD, Go side**: `httptest.Server` over the real handler mux against a
  real repo built with the `newRepo` helper pattern. Assert JSON shapes,
  paging behavior (`more=1` extends, restart reloads), diff row alignment
  on a known change, loopback-refusal of a public `--addr`, and 404/400
  paths.
- **Frontend**: judged by eyeball on the big test repos (linux, babel) —
  it is the experiment itself.

## Evaluation protocol (the "results" the probe exists to produce)

Run `gg web` on linux and babel and judge:

1. Scroll the full feed — smoothness, memory, paging latency vs the TUI.
2. Open diffs while scrolling — perceived latency (cache-warm and cold).
3. Side-by-side density vs the TUI diff view — is it *better*, not just
   prettier?

Outcome decides: grow (stage 2: ops + decisions over WebSocket, then maybe
a Wails wrap), or archive the findings and delete the package.

## Future doors deliberately left open (not built now)

- **Wails wrap**: the identical HTML/JS app in a native window — shares
  ~95% of this code.
- **WASM modules**: `textdiff`/`commitgraph`/`fuzzy` are dependency-free
  and compile to WASM; client-side re-layout/filter matters only for
  remote (non-loopback) sessions, which are out of scope here.
- **Ops + decisions**: `Progress`/`GitLine`/`DecisionNeeded` over a
  WebSocket, decision modals POSTing the chosen option — the MCP
  `MapDecider` shape.
