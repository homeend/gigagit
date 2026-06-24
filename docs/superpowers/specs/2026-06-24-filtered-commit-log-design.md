# Filtered commit log (path / author / message / date) — design

**Status:** approved 2026-06-24
**Branch:** `feat/filtered-commit-log`
**Gap closed:** #2 in `docs/feature-gaps-lazygit-gitkraken.md` (commit/log search &
path-scoped filtering) — the highest effort-to-value monorepo item after
sparse-checkout.

## Goal

Let the Commits panel narrow its walk by **path**, **message text**, **author**,
and **date range** — not just by branch as today. In a 1.46M-commit monorepo,
"commits touching `dir/`" and "where did I fix X" are daily questions that the
unfiltered feed can't answer by scrolling.

## Scope of this pass

- **In:** path scope, message grep (case-insensitive), author filter, date range
  (`--since`/`--until`); a TUI filter popup; a "Commits touching this" seed action
  from the files view and the fuzzy file finder; filter ANDs with the existing
  branch scope (solo).
- **Out (YAGNI):** `gg log` CLI (deferred to a follow-up — falls out for free once
  `LogScope` carries the fields); `--follow` rename tracking (single-path only,
  fights the graph); grep regex-type toggles; saved/named filters.

## Architecture — widen one seam, add no subsystem

The commit feed already flows through a single narrowing carrier, `LogScope`.
Every change below plugs into a spot that already exists:

```
LogScope (internal/git/log.go)  ──widen──►  + Paths/Author/Grep/Since/Until
        │
LogScoped argv builder          ──add───►   --author / --grep -i / --since /
        │                                    --until / trailing `-- <paths>`
CommitFeed.SetScope (domain)    ──unchanged: already carries a LogScope──
        │
startFeedReload (tui)           ──build──►  LogScope from
                                            commitScopeBranches + commitFilter
```

`LogScope` becomes the single carrier of **all** feed narrowing. Branch scope and
filter axes **compose** (you can solo a branch *and* path-scope it). Paging
(`--skip`), supersede-cancel (the feed `gen` bump), and `%D`/`%S` parsing are
untouched: git applies `--skip` *after* filtering, so `LoadMore` just works.

## Components

| Layer | File | Change |
|---|---|---|
| git | `internal/git/log.go` | Widen `LogScope`; `LogScoped` appends the filter flags + a trailing `-- <paths>` (pathspec must come after `--`, after the refs). |
| domain | `internal/domain/commitfeed.go` | No structural change — the new `LogScope` fields flow through `SetScope`/`LoadInitial`/`LoadMore` as-is. (Covered by tests.) |
| tui state | `internal/tui/model.go` | New `commitFilter` field (struct below); status-line render (`model.go:~1694`) appends filter chips; graph predicate gains a "no filter" guard. |
| tui popup | `internal/tui/commit_filter_popup.go` (new) | A `layer` with five `textfield` rows: Path / Author / Message / Since / Until. `\` opens it on the Commits panel. Enter → set `commitFilter` → `popLayer` → `startFeedReload`. |
| tui scope | `internal/tui/commit_scope.go` | `startFeedReload` folds `commitFilter` into the `LogScope`; a clear path (mirroring the branch-scope clear at `commit_scope.go:~640`) drops the filter and reloads. |
| tui seed | `internal/tui/` files-view `.` menu + fuzzy-finder `F` action menu | New row **"Commits touching this"** → `commitFilter.Paths=[path]`, clear other axes, focus Commits, `startFeedReload`. |

### `LogScope` after widening

```go
type LogScope struct {
    Branches []string // empty → all local branches + HEAD (unchanged)
    Paths    []string // → trailing `-- <paths>`
    Author   string   // → --author=<s>
    Grep     string   // → --grep=<s> -i  (case-insensitive)
    Since    string   // → --since=<s>   (git-parsed: "2 weeks ago", "2026-01-01")
    Until    string   // → --until=<s>
}

func (s LogScope) filtered() bool {
    return len(s.Paths) > 0 || s.Author != "" || s.Grep != "" ||
        s.Since != "" || s.Until != ""
}
```

### `commitFilter` (TUI Model field)

```go
type commitFilter struct {
    Paths  []string
    Author string
    Grep   string
    Since  string
    Until  string
}
```

## The one correctness gotcha — graph suppression

Any active filter makes the feed a **non-contiguous subset** of history (path
scope additionally rewrites parent linkage via git's history simplification). The
single-line commit-graph (`internal/commitgraph`) assumes contiguous parent
reachability across the displayed window, so its lanes would be wrong or
disconnected under a filter.

**Rule:** when the active `LogScope` is `filtered()`, the commit-graph is forced
off (`commitGraphOn` / `graphActive()` return false). The graph already only draws
in natural, unfiltered feed order, so this is **one extra guard**, not a rewrite.
The footer's graph hint follows the same predicate.

## Data flow & semantics

- **Apply:** popup enter → set `commitFilter` → `popLayer` → `startFeedReload`
  (gen bump drops stale pages; fresh `git log` with the filter flags).
- **Compose:** branch scope and all filter axes AND together. A single `--grep`
  needs no `--all-match`; `--author` is always AND.
- **Empty fields** contribute no flag (an all-empty popup apply == clear filter).
- **Dates** pass verbatim to git — **no date-parsing code** on our side.
- **Clear:** a clear path mirroring the branch-scope clear drops `commitFilter`
  and reloads the full feed; the status line stops showing chips.

## Error handling

- A filter that matches nothing yields an empty feed (not an error) — the panel
  shows its existing empty state; the chips stay visible so the user sees *why*.
- An invalid `--since`/`--until` makes `git log` exit non-zero; surface it via the
  existing feed-load error path (status message), keep the prior feed, do not
  crash. (`LoadInitial` already returns `(FeedState, error)`.)
- Popup input is swallowed (no key leaks to global handlers); `esc` cancels
  without touching the feed.

## Performance (documented, inherent)

A rarely-touched path/author forces git to walk deep to find `-n limit` matching
commits — bounded by the limit but potentially slower than the unfiltered first
paint. This is inherent to filtered `git log` (same in lazygit/GitKraken) and is
acceptable; the supersede-cancel already protects against a stuck walk being
overtaken by the next one.

## Testing

- **git (`internal/git`, real repo):** `LogScoped` returns only path-touching
  commits; author/grep narrow correctly; `--since/--until` bound the range;
  combined axes AND; pathspec sits after `--`.
- **domain (`internal/domain`):** `SetScope` with a filtered `LogScope` +
  `LoadMore` keeps paging correct (skip applies post-filter); supersede-cancel
  still holds when the scope changes mid-walk.
- **tui:** popup open/type/apply/esc + a key-swallow test; status-line chip
  render; a **graph-suppressed-when-filtered** assertion; "Commits touching this"
  seed from *both* surfaces sets the path scope and focuses Commits; clear
  restores the full feed.
- Finish: `gofmt -l`, `go vet ./...`, `./test.sh race`.

## Staging (becomes the plan's task groups)

1. **git + domain widening** + tests (no UI). Independently testable.
2. **TUI filter popup** (`\`) + status-line chips + clear + graph suppression.
   The main UX.
3. **"Commits touching this"** seed from the files view and the fuzzy finder.

## Open decisions (resolved)

- **Filter popup key:** `\` on the Commits panel (`/` is the in-memory filter,
  `@` is highlight, `f` is fetch, `F` is the fuzzy finder — all taken).
- **All four axes** kept (path/author/message/date), per the brainstorm.
- **TUI-only** this pass; `gg log` CLI is a deferred follow-up.
