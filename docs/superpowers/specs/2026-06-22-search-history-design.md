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

## Recall behavior (readline-style inline fill)

While in a search's typing mode, with a non-empty history ring for that scope:

- **Alt+↑** = older, **Alt+↓** = newer.
- The first **Alt+↑** fills the **newest** ring entry into the query, replacing
  whatever was there. Subsequent Alt+↑ walk to older entries; Alt+↓ walk back
  toward newer.
- **Alt+↓ past the newest** restores the in-progress **draft** (the text the
  user had typed before they started cycling).
- Recall is **unfiltered** — it walks the full ring in order, not prefix-matched.
- **Typing any character** (or backspace/space) exits recall: the cursor resets,
  and the now-edited query becomes the new draft. A subsequent Alt+↑ starts
  cycling from the newest again.
- At the **boundaries** (oldest reached via Alt+↑, or already on the draft via
  Alt+↓) the keystroke is a no-op.

### Recall cursor state (per typing session)

The recall position is transient TUI state, reset every time a typing mode opens:

- `recallScope` — which ring is active (derived from the open search).
- `recallIndex` — `-1` = on the draft (not cycling); `0` = newest; `n-1` = oldest.
- `recallDraft` — the user's typed text captured the moment cycling begins, so
  Alt+↓ past newest can restore it.

These live on the `Model` and are cleared whenever a typing mode is entered or
committed/cancelled.

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
- Recall cursor fields (`recallScope`/`recallIndex`/`recallDraft`) as above.
- In each search typing loop (`filterTyping`, `highlightTyping`,
  `contentPopup` files-view, bookmark popup, shelf popup — five loops feeding
  four rings):
  - intercept **Alt+↑ / Alt+↓** (`msg.Alt && msg.Type == tea.KeyUp/KeyDown`)
    **before** the plain Up/Down cases, and apply recall to that scope.
  - on **Enter** (commit) with a non-empty query: append to the local ring
    (dedup-to-top, trim) **and** fire an async `RecordSearch` command
    (fire-and-forget, like other side-store writes).
  - on entering/exiting typing mode: reset the recall cursor.

No footer/help change is strictly required, but a one-line `?` cheat-sheet /
help note ("alt+↑/↓ — search history") is in scope as polish.

## Testing (TDD, real files)

- **`searchhist` store:** temp-dir `FileStore` — record/dedup-to-top/trim-to-size,
  newest-first ordering, empty-phrase no-op, concurrent read-merge does not lose a
  sibling's entry, round-trips through TOML.
- **domain:** `SearchStatePath` → temp dir; `RecordSearch`/`SearchHistory`
  round-trip; size clamp (config 5 → 5; config 5000 → 1000; unset → 20);
  per-repo keying isolates two repos.
- **TUI:** for each scope — Enter records (esc does not); Alt+↑/↓ cycles and
  fills the query inline; draft restore on Alt+↓ past newest; typing exits recall;
  `panel` ring is shared by `/` and `@` but `bookmark` is not. Use
  `loadedModel`/`newRepoDir` with `SetSearchStore` injection.

## Out of scope

- The `.` action-menu filter (excluded above).
- Any CLI surface (`gg ...`) — purely interactive.
- Cross-repo / global history (rings are per-repo by design).
- Prefix-matched / fuzzy recall (recall is unfiltered).
