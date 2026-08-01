# Web big-repo optimization suggestion — design

Date: 2026-08-01 · Branch: feat/web-bigrepo-suggest (off web-dev)

## Goal

When the web UI serves a big repository, suggest — once, dismissibly — the
settings that make commit browsing fast, mirroring the TUI's two mechanisms:
the commit-graph notice (`commitGraphNotice`, `internal/tui/notify.go`) and
the graph-off ↔ plain-sort coupling (`internal/tui/related_prompts.go`).

## Decisions (user-approved)

1. **Scope**: one suggestion surface carrying BOTH recommendations —
   "turn graph off + plain sort" and "write commit-graph + keep it fresh".
2. **Persistence**: accepting graph-off/plain-sort writes the repo
   `.gg.toml` (`[ui] show_graph = "off"`, `commit_sort = "plain"`) via the
   existing `config.SetShowGraph`/`SetCommitSort` writers — shared with the
   TUI. The web additionally starts honoring `[ui] show_graph` as the
   default for its `g` toggle; a manual per-browser localStorage override
   still wins thereafter.
3. **Suppression**: "never for this repo" persists via
   `promptstate.DismissNotice` keyed by git common dir — the same store the
   TUI and the web review-approval lane already share. The commit-graph
   recommendation reuses the TUI's existing id `commit_graph_recommend`
   (dismissing in either frontend silences both — same advice, same repo);
   the graph-off recommendation gets a new web-only id
   `web_graph_off_suggest` (the TUI has no equivalent auto-notice).
4. **Surface**: a slim dismissible banner under the top bar — non-blocking
   plain DOM, not a layer; it must never eat keys or block work.

## 1. Detection & data flow

New read-only endpoint (no writeGuard; hostGuard applies globally):

```
GET /api/health →
{ "big": true, "pack_mb": 412, "has_commit_graph": false,
  "write_commit_graph_set": false,
  "show_graph": "on", "commit_sort": "date-order",
  "dismissed": { "commit_graph_recommend": false,
                 "web_graph_off_suggest": false } }
```

- `big` = `domain.RepoHealth.PackBytes >= bigRepoPackBytes`, where the web
  defines its own `bigRepoPackBytes = 100 << 20` const mirroring the TUI's
  (`internal/tui/notify.go`). A test seam — a `Server` threshold field
  defaulting to that const — lets tests use small fixtures.
- `show_graph` / `commit_sort` come from the effective config
  (`s.effectiveConfig`, the review lane's resolution), with the config
  defaults applied (`show_graph` missing → `"on"`, `commit_sort` missing →
  `"date-order"`).
- `dismissed` reads `promptstate` per notice id, keyed by the git common
  dir. A missing/unreadable prompts store reads as nothing-dismissed
  (fail-open: worst case the banner shows again).
- The client fetches `/api/health` once after load and after a re-root.

## 2. Banner (client)

Rendered when `big` AND at least one recommendation applies and is not
dismissed (persisted or session). Content: one explanation line naming the
pack size, then up to two action groups plus the dismiss controls:

- **"Turn graph off + plain sort"** — shown when NOT dismissed
  (`web_graph_off_suggest`) AND the effective graph state is on (config
  `show_graph != "off"` and no `localStorage gg.graph == "off"` override)
  OR `commit_sort != "plain"`. (Either misalignment keeps the offer alive;
  accepting always writes both keys.)
- **"Write commit-graph + keep it fresh"** — shown when NOT dismissed
  (`commit_graph_recommend`) AND `!has_commit_graph` AND
  `!write_commit_graph_set` — exactly the TUI notice's conditions.
- **Not now** — hides the banner for this browser session only
  (`sessionStorage`), re-evaluated next visit. Matches the TUI's
  session-dismissal semantics.
- **Never for this repo** — `POST /api/notice-dismiss` for every id the
  banner is currently showing, then hides it.

The banner is plain DOM above `#panes` (a sibling, so the panes grid is
untouched); it has no keyboard handling and no layer-stack membership. It
must not reuse the parked-run status line or any dismiss-only surface (the
"don't put a live handle on a dismiss-only surface" rule) — it is its own
element with explicit buttons.

## 3. Accepting "graph off + plain sort"

```
POST /api/ui-config  {"show_graph": "off", "commit_sort": "plain"}
```

- writeGuard (JSON content type + loopback Origin).
- Values are allowlisted to exactly the enum vocabulary: `show_graph` ∈
  {"on","off"}, `commit_sort` ∈ {"date-order","plain"}; each key optional,
  at least one required; anything else → 400. Free config text never
  crosses the wire (the commit-edit "wire carries a verb" rule).
- Server resolves the repo `.gg.toml` path (`TopLevel` + `.gg.toml`, the
  `feedFor` probe's file) and calls `config.SetShowGraph` /
  `config.SetCommitSort`.
- After a successful `commit_sort` write, the feed is dropped under the
  same `s.mu` section `resetFeed` uses — `feedFor` re-reads commit_sort at
  next build, so the new sort takes effect on the next `/api/commits`.
- Response `{ "ok": true }`; the write is not an engine op (no git, no
  repogate) — it edits a TOML file, same standing as the TUI Settings rows.
- Client on success: `state.graphMode = "off"`, `lsSet("gg.graph","off")`
  (this browser matches immediately and keeps matching), reload commits,
  re-fetch `/api/health`, hide the action group.

## 4. Accepting "write commit-graph"

New op-transport verb:

```
POST /api/op  {"op": "commit-graph"}
```

- Runs via the generalized run lane (`startRun`): the runFunc Executes
  `engine.WriteCommitGraph{}` and, only on its success, executes
  `engine.SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true"}` —
  both inside the ONE server-side run (the TUI's
  `startCommitGraphWriteAndEnable` chain, server-side). The key/value pair
  is hardcoded server-side; `SetGitConfig` is never wire-constructible, so
  a client cannot write arbitrary git config.
- A failure of the config write after a successful graph write surfaces as
  the run's error but the graph file legitimately remains (same partial
  outcome the TUI chain can produce; the banner's re-fetch then shows only
  the still-missing half).
- Progress streams over the existing SSE lane; `done` triggers a client
  health re-fetch which naturally retires the action group
  (`has_commit_graph` flips true).
- No decisions fork from either op — no parking modal involved.

## 5. Dismissal endpoint

```
POST /api/notice-dismiss  {"id": "commit_graph_recommend"}
```

- writeGuard. `id` allowlisted server-side to exactly
  {`commit_graph_recommend`, `web_graph_off_suggest`}; unknown → 400 —
  a frontend bug can never pollute `prompts.toml` with garbage ids.
- Calls `promptstate.DismissNotice(commonDir, id)` on the default store
  (`repos.DefaultStatePath`-rooted, the shared file).
- Response `{ "ok": true }`.

## 6. `[ui] show_graph` honored at load

Parity fix that falls out: at startup, when localStorage has no `gg.graph`
value, the client initializes `state.graphMode` from `/api/health`'s
`show_graph` ("off" → `"off"`, anything else → `"svg"`). A manual `g`
toggle still writes localStorage and wins in that browser thereafter. The
`/api/health` fetch therefore happens early enough to set the mode before
the first commits render, or the client re-renders the pane once when the
answer arrives (implementation may choose; the visible requirement is that
a config-off repo ends up rendering flat without a manual toggle).

## 7. Error handling

- `/api/health` failure: no banner, no error surfaced (best-effort, the
  TUI's "health never surfaces errors" rule); graph mode falls back to the
  existing localStorage-or-default behavior.
- `/api/ui-config` or `/api/notice-dismiss` failure: `opLine` error report,
  banner stays (the action can be retried).
- `op:"commit-graph"` failure: the standard op error path (status line);
  banner stays.

## 8. Testing

Go (`internal/web`):
- `/api/health`: big/small threshold (via the test seam), flag projection,
  effective-config values with and without `.gg.toml` keys, dismissed map
  reflecting a pre-seeded prompts store.
- `/api/ui-config`: writes land in `.gg.toml` (read back via config.Load);
  feed rebuilt with the new sort (next `/api/commits` reflects it); enum
  refusal (unknown value, empty body, non-JSON → 400/415); GET → 405.
- `op:"commit-graph"`: after done, the commit-graph file exists AND
  `fetch.writeCommitGraph` is `true` in local config; health then reports
  both flags flipped.
- `/api/notice-dismiss`: known id lands in prompts.toml (visible to a
  `promptstate` read); unknown id → 400 and the store is untouched.

CDP (browser checks, DOM-driven — app.js is a module):
- Big fixture: the fixture repo commits ~110 MB of incompressible random
  bytes (random data doesn't pack smaller), so the real binary's 100 MB
  floor trips with no production flag — the Go-test seam stays
  test-only. Banner visible (elementFromPoint at its center), both groups
  present when both apply.
- Accept graph-off: banner group retires, commits re-render flat (one-dot
  rows), `.gg.toml` contains both keys.
- Accept commit-graph: group retires after done; second load shows no
  commit-graph group.
- "Never": banner absent after a full reload; prompts.toml carries the ids.
- Small repo: no banner.
- Config `show_graph = "off"` + empty localStorage: first render is flat.
