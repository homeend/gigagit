# Search History — Design

**Date:** 2026-06-22
**Status:** Approved (brainstorm)

## Goal

Remember phrases the user has searched for, and let them recall those phrases
while typing a new search via **Alt+↑ / Alt+↓** (readline-style). Only
Enter-confirmed searches are remembered; an aborted (esc) search is not.

## Scope model — per-window rings

Each "window" keeps its **own** independent history ring. Within a window,
multiple search kinds **share** one ring. This reconciles the two user
decisions: "each window should have its own history" (partition by window) and
"one shared list" (within the main view, `/` filter and `@` highlight are not
split apart).

Worked example:

> Search `fix` in the Commits `/` filter, then Alt+↑ in the `@` highlight → you
> get `fix` back (same window). But Alt+↑ in the bookmark switcher will **not**
> show `fix` — that's a different window with its own ring.

### Scope → input map (exhaustive)

| Scope (ring key) | Inputs that feed/recall it | Mechanism |
|------------------|----------------------------|-----------|
| `panel` | main panel `/` filter **+** `@` commit highlight. The files-view commit-side `/` reuses the same filter machinery, so it joins this ring automatically. | `m.filterTyping`/`m.filterQuery`, `m.highlightTyping`/`m.highlightQuery` |
| `filetree` | files-view file-tree `/` search | `contentPopup.typing`/`.query` when `m.filesView != nil` |
| `bookmark` | the `g` bookmark switcher `/` | bookmark popup typing state |
| `shelf` | the `G` shelf switcher `/` | shelf popup typing state |

**Explicitly excluded:** the `.` action-menu `/` filter (`action_menu.go`
`a.typing`/`a.query`). It is a transient command-palette filter, not a phrase a
user would want to recall, so it neither records nor recalls.

**Note on `contentPopup`:** `contentPopup` also backs the stash file list and
the help/blame content. Only the **files-view** instance (`m.filesView`)
records/recalls under `filetree`. Other `contentPopup` uses do not participate
(they are not free-text searches the user composes for content).

## Recall behavior (scrollable dropdown)

While in a search's typing mode, with a non-empty history ring for that scope,
recall presents a **visible dropdown list** of past phrases anchored at the
search input. It is not a blind inline cycle — the user sees the candidates.

- **Alt+↓ opens** the dropdown (when closed), highlighting the **newest** entry.
  While open, **Alt+↓** moves the highlight down (toward older) and **Alt+↑**
  moves it up (toward newer).
- The dropdown shows **at most 10 rows at once**. When the ring holds more than
  10 entries (up to the 1000 ceiling), the visible window **scrolls** to keep
  the highlight in view — newest-first, oldest scrolling in from the bottom.
- The highlighted entry **previews into the query** (the search filters/highlights
  live against it as the user moves), so navigation is also a preview.
- **Enter** accepts the highlighted entry: the dropdown closes and the search
  commits on that phrase (a normal commit → also records it, harmlessly
  dedup-to-top).
- **Esc** closes the dropdown **without** accepting and **restores the draft**
  (the text typed before the dropdown opened). The search stays in typing mode.
- **Alt+↑ above the newest** (top of the list) closes the dropdown and restores
  the draft — symmetric with Esc, so you can "back out the way you came in".
- **Typing any character** (or backspace/space) closes the dropdown and returns
  to live editing with the current query as the new draft. A later Alt+↓ reopens
  from the newest.
- Recall is **unfiltered** — the dropdown lists the full ring in order, not
  prefix-matched against the draft.

### Recall cursor state (per typing session)

The recall position is transient TUI state, reset every time a typing mode opens:

- `recallScope` — which ring is active (derived from the open search).
- `recallOpen` — whether the dropdown is currently shown.
- `recallIndex` — highlight position into the ring; `0` = newest, `n-1` = oldest.
  Meaningful only while `recallOpen`.
- `recallDraft` — the user's typed text captured the moment the dropdown opens,
  restored on Esc / Alt+↑-past-newest / typing.

The visible window is derived from `recallIndex` and the 10-row cap at render
time (no stored offset needed): show rows `[max(0, idx-9) .. ]` clamped so the
highlight stays visible, capped at 10. These live on the `Model` and are cleared
whenever a typing mode is entered or committed/cancelled.

### Rendering

A single shared helper renders the dropdown — a small bordered box of up to 10
rows (highlighted row inverted, a scroll affordance such as `↑N`/`↓N` when the
window is clipped) — anchored per context:

- `panel` filter / `@` highlight → near the focused panel's filter line.
- `filetree` → within/below the files-view search line.
- `bookmark` / `shelf` → inside the centered switcher popup, below its filter row.

The dropdown is drawn only while `recallOpen` and the active typing scope matches.

## What gets recorded

- **Enter (commit) only.** Esc/cancel records nothing.
- **Non-empty only.** A committed empty query records nothing.
- **Dedup-to-top.** Re-confirming a phrase already present moves it to
  most-recent rather than duplicating.
- Recording happens for the scope the search belongs to (per the map above).

## Persistence

- **Per-repo, on disk**, reusing gg's existing side-store convention
  (`shelf`/`bookmark`): `<state>/gg/search/<repoKey>.toml`, where `<repoKey>` is
  a **hash of the git common dir** (`repoKey` in `internal/domain/shelfstore.go`)
  — so the path is stably per-repo but does not literally contain the repo name.
- **One small TOML file per repo** holding all four named rings. No
  content-addressed blobs, no separate index — this is a tiny record store, not
  the shelf's blob machinery. Shape:

  ```toml
  [rings]
  panel = ["fix login", "TODO", "refactor"]
  filetree = ["handler.go"]
  bookmark = []
  shelf = []
  ```

  (newest-first within each ring).

- **Concurrent-safe.** With worktrees, several `gg` processes may run on one
  repo. Each record does **read → merge (dedup-to-top) → write** under an atomic
  rewrite, so a sibling process's entries are not clobbered. Last writer still
  wins on a true simultaneous race, but the read-merge keeps the common
  interleaved case lossless.
- **Best-effort.** If no state dir resolves (no home), history is disabled
  silently (mirrors `repos.toml` / shelf posture). Recall simply shows nothing.

## Size & config

- `[ui] search_history_size` — `int`, `toml:"search_history_size"`.
  - `<= 0` (unset) → default **20**.
  - Clamped to a **hard ceiling of 1000** (a `searchhist.MaxSize = 1000`
    constant), mirroring how `CommitGraphMaxLanes` clamps to
    `commitgraph.MaxLanes`. Values above 1000 clamp down to 1000.
- The limit applies **per ring** (each window keeps up to `size` entries).
- On record, the ring is trimmed to the effective size (oldest dropped).

## Layering — domain-owned side store (no git surface)

This is **not** a git operation. It follows the `shelf`/`bookmark` precedent,
**not** the engine-`Operation` + CLI feature checklist. There is **no engine
`Operation` and no CLI command**.

### New package: `internal/searchhist`

A pure store behind a fixed interface, default impl = atomic-rewrite TOML.

```go
package searchhist

const MaxSize = 1000 // hard ceiling on per-ring entries

// Store persists per-scope history rings for one repo.
type Store interface {
    // List returns the ring for scope, newest-first (nil if none/disabled).
    List(scope string) []string
    // Record prepends phrase to scope's ring (dedup-to-top), trims to size,
    // and persists. No-op on empty phrase. size is the already-clamped limit.
    Record(scope string, phrase string, size int) error
}
```

Default impl `FileStore` reads/writes `<root>/search.toml` (root = the per-repo
dir), read-merge-prepend-write on each `Record`.

### Domain wiring (`internal/domain`)

Mirror `shelfstore.go`:

- `SearchStatePath` override var (tests point it at a temp dir).
- `searchStore(ctx)` — lazily resolves the per-repo `FileStore` keyed by
  `repoKey(commonDir)` under `<state>/gg/search/<key>`, cached on the `Service`.
  Returns nil when disabled.
- `SetSearchStore(st)` injector for tests.
- `func (s *Service) RecordSearch(ctx, scope, phrase string)` — clamps size from
  config, calls `store.Record`. Best-effort (errors swallowed/logged like other
  side stores).
- `func (s *Service) SearchHistory(ctx, scope string) []string` — returns the
  ring (for startup load).
- Snapshot may carry the rings (`Snapshot.SearchHistory map[string][]string`) so
  the TUI gets them on the initial load without an extra round-trip.

Archtest guard: `internal/tui` and `internal/cli` must **not** import
`internal/searchhist` (same guard shelf/bookmark have).

### TUI wiring (`internal/tui`)

- Hold the rings in memory: `m.searchHist map[string][]string`, populated from
  the Snapshot at startup and updated locally on each record (so Alt-cycling is
  instant and never blocks).
- Recall cursor fields (`recallScope`/`recallOpen`/`recallIndex`/`recallDraft`)
  as above.
- A shared dropdown render helper (max 10 visible rows, highlight, scroll
  affordance) anchored per context (see **Rendering** above).
- In each search typing loop (`filterTyping`, `highlightTyping`,
  `contentPopup` files-view, bookmark popup, shelf popup — five loops feeding
  four rings):
  - intercept **Alt+↑ / Alt+↓** (`msg.Alt && msg.Type == tea.KeyUp/KeyDown`)
    **before** the plain Up/Down cases:
    - Alt+↓: open the dropdown (capturing the draft) if closed, else move the
      highlight one older; preview the highlighted phrase into the query.
    - Alt+↑: move the highlight one newer; above the newest, close + restore draft.
  - while `recallOpen`, **Esc** closes + restores draft (does not cancel the
    search); **Enter** commits on the highlighted phrase; any text key closes the
    dropdown and resumes live editing.
  - on **Enter** (commit) with a non-empty query: append to the local ring
    (dedup-to-top, trim) **and** fire an async `RecordSearch` command
    (fire-and-forget, like other side-store writes).
  - on entering/exiting typing mode: reset the recall cursor (closed, draft cleared).

No footer/help change is strictly required, but a one-line `?` cheat-sheet /
help note ("alt+↑/↓ — search history") is in scope as polish.

## Testing (TDD, real files)

- **`searchhist` store:** temp-dir `FileStore` — record/dedup-to-top/trim-to-size,
  newest-first ordering, empty-phrase no-op, concurrent read-merge does not lose a
  sibling's entry, round-trips through TOML.
- **domain:** `SearchStatePath` → temp dir; `RecordSearch`/`SearchHistory`
  round-trip; size clamp (config 5 → 5; config 5000 → 1000; unset → 20);
  per-repo keying isolates two repos.
- **TUI:** for each scope — Enter records (esc does not); Alt+↓ opens the
  dropdown on the newest and previews into the query; Alt+↓/↑ move the highlight;
  a 12-entry ring shows 10 rows and scrolls to reveal the oldest; Esc / Alt+↑
  above newest close and restore the draft; typing closes the dropdown; Enter on
  a highlighted entry commits that phrase; `panel` ring is shared by `/` and `@`
  but `bookmark` is not. Use `loadedModel`/`newRepoDir` with `SetSearchStore`
  injection.

## Out of scope

- The `.` action-menu filter (excluded above).
- Any CLI surface (`gg ...`) — purely interactive.
- Cross-repo / global history (rings are per-repo by design).
- Prefix-matched / fuzzy recall (recall is unfiltered).
