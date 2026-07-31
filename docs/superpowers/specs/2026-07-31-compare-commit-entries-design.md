# Compare two commit entries (bookmarks / shelved commits)

**Date:** 2026-07-31
**Status:** approved design, pre-plan
**Scope:** TUI + CLI (domain core shared; MCP/web adoption possible later, out of scope here)

## Problem

The `g` (bookmarks) and `G` (shelf) switchers can compare *file* entries with
each other — `m` mark-two within a picker, `c` across pickers — but both
mechanics deliberately exclude commit entries (`commitBookmarkNotice` /
`commitShelfNotice`, plus the compare-mode `enter` refusals). The only
commit-level compare today is `enter` on a commit bookmark against the
Commits-panel selection.

Users want to compare two *commit entries* in all three pairings:

1. shelved commit ↔ shelved commit,
2. commit bookmark ↔ commit bookmark,
3. cross: commit bookmark ↔ shelved commit (either direction).

The result should be the existing whole-tree compare files view (changed-file
list, drill into per-file diffs), since both entry kinds carry a commit sha
(`Bookmark.Commit`, `ShelfEntry.Origin.Commit`).

## Decisions (made during brainstorm)

- **Hybrid semantics.** Compare sha-vs-sha (live tree compare) when both
  commits still exist. When a *shelved* side's sha is gone (gc'd / history
  rewritten), fall back to its frozen tar — that survival is the shelf's whole
  point. A gone *bookmark* sha is a hard error (bookmarks store no blobs).
- **Cross fallback scopes to shelf members.** When the shelf side is frozen
  and the other side is a live commit, compare only the shelf's member paths
  against those same paths in the commit's tree. Honest about what's knowable;
  the view labels the frozen side so the semantic switch is visible.
- **Frontends: TUI + CLI.** The comparison logic lands in `internal/domain`
  so other frontends can adopt it later.
- **The CLI surface is the existing `gg compare`**, extended with entry
  specs (`bookmark:<id>` / `shelf:<id>`) and `--patch`; its existing
  commit-ish/`@staged`/`@worktree` tokens are untouched.

## Design

### 1. Model — `EndpointShelf`

`model.Endpoint` (today: worktree / index / commit) gains:

- `EndpointShelf` kind + `ShelfID string` field — "the frozen changed-file
  set of shelved commit entry `ShelfID`".
- `FileRef(path)` → `{Source: SourceShelf, Locator: ShelfID, Path: path}`.
  Per-file diff resolution then needs zero new code: `domain.ResolveBytes`
  already extracts a member from a commit entry's tar (`shelfResolve`).
- `IsLive()` → false. A shelf entry is immutable, so caching is safe.
- `CacheTag()` → `"shelf:" + ShelfID`. The prefix keeps the tag out of the
  sha namespace (no diff-LRU collision; no mutable-ref poisoning — entry IDs
  are stable and their content frozen).
- `Display()` → `shelf #<short> (frozen)` — English protocol, like the
  existing `Working Tree` / `Staged` labels; the compare title shows the
  frozen semantic.

### 2. Domain — hybrid resolution + shelf-aware `CompareFiles`

Two additions in `internal/domain`:

**`ResolveCommitEntryEndpoint(ctx, side)`** — `side` carries a sha, an
optional shelf entry ID, and a display label. It probes the sha via the
existing `CommitLookup` (quiet query, built for possibly-gc'd revs):

- sha resolves → `EndpointCommit{Hash}` with the full sha the entry already
  stores (`CommitLookup` returns a short sha — it serves only as the
  existence probe).
- sha gone, shelf ID present → `EndpointShelf{ShelfID}` (frozen fallback).
- sha gone, no shelf ID (a bookmark) → a typed error naming the side's short
  sha, so frontends can show a precise notice.

Resolution is strictly per-side, so mixed states compose without special
cases: a shelf↔shelf pair where only one sha is gone resolves to
frozen ↔ live-commit and lands in the shelf↔commit lane below (scoped to the
frozen side's members).

**`CompareFiles(ctx, left, right)`** branches when either endpoint is
`EndpointShelf`, *before* reaching `repo.DiffTreeFiles` (git cannot see the
tar). Conventions mirror tree-diff output (left = older, right = newer):

- *shelf ↔ shelf*: union of the two member sets. Only-in-left → `D`,
  only-in-right → `A`, both present but bytes differ → `M`, identical →
  omitted. Bytes come from the tars (via the shelf store / `ResolveBytes`
  path).
- *shelf ↔ commit* (either order): scoped to the shelf's member paths. Each
  member is byte-compared against the same path in the commit's tree
  (`ShowFile`); missing on the commit side → `A`/`D` per direction.

The singleflight/caching contract is unchanged: shelf endpoints are not live,
their `CacheTag` participates in the compare-files key as-is.

### 3. TUI — lift the commit-entry guards in both switchers

**Within a picker (`m` mark-two).** A commit entry becomes markable in both
`g` and `G`. When the second `m` lands on another commit entry:

1. Build both side descriptors (sha + optional shelf ID + label).
2. Resolve both endpoints off the UI thread via
   `ResolveCommitEntryEndpoint`, gen-guarded (the `pickGen` pattern — a
   stale resolve after switcher close / reRoot is dropped).
3. `openCompareFiles(left, right)` — first mark = left/base. The existing
   compare files view handles any endpoint mix; drilling into a file diffs
   via `Endpoint.FileRef` on both sides.

A mixed pair (file mark then commit second, or vice versa) → status notice,
no compare. Marking behavior for file entries is unchanged.

**Across pickers (`c`).** `pendingCompare` gains a commit flavor (sha +
optional shelf ID + label) alongside today's `FileRef` flavor. `c` on a
commit entry sets it and opens the other picker in compare mode. `enter`
there:

- on a commit entry → complete the compare (resolve both sides as above;
  focused/first entry = left);
- on a file entry → notice "cannot compare a commit against a file" (the
  mirror of today's file-vs-commit refusals, which stay).

**Shared behavior.**

- Self-compare (both sides resolve to the same sha) → notice, mirroring
  `compareCommitBookmark`'s "select a different commit".
- A gone bookmark sha → notice naming the short sha; `[t]` export etc.
  remain available.
- Both switchers' `?` cheat sheets and compare-mode footer hints advertise
  the new ability (help AND footer). Every new string lands in all four
  i18n bundles; the AST gates (`i18n_scan_test`, `menu_labels_test`)
  enforce coverage.

### 4. CLI — extend the existing `gg compare`

`gg compare <left> [<right>]` already exists (`internal/cli/compare.go`):
endpoint tokens are a commit-ish, `@staged`, or `@worktree`, output is one
`<status>\t<path>` line per differing file. This feature extends it rather
than adding a command:

```
gg compare [--patch] <spec> [<spec>]    # adds: bookmark:<id> | shelf:<id>
```

- Flags precede positionals (the `gg review` convention).
- Spec resolution: `bookmark:<id>` via `BookmarkGet` (must be a commit
  bookmark), `shelf:<id>` via `ShelfFind` (must be a commit entry); a file
  entry or unknown ID is a usage-level error with a clear message. Entry
  specs resolve through `ResolveCommitEntryEndpoint` (hybrid live/frozen),
  then flow into the same `CompareFiles` call as the existing tokens.
- `validComparePair` learns shelf endpoints (a frozen side pairs with a
  commit or another shelf endpoint; never with `@staged`/`@worktree`).
- Default output: unchanged `<status>\t<path>` lines.
- `--patch`: live lane = one `git diff <A> <B>` (`DiffPatch` with a range
  spec); frozen lane = per-member temp-file `git diff --no-index` with
  relabelled headers (the proven MCP `gg_compare_file` machinery).
- When a side falls back to frozen, a note goes to **stderr**
  (`# frozen compare: commit <short-sha> no longer exists`); stdout stays
  parseable.
- Exit codes: 0 = comparison produced (including an empty diff), 1 =
  failure (gone bookmark sha, unreadable shelf blob, git error), 2 = usage.
- CLI surface change ⇒ update `internal/agentskill/using-gg.md`, bump
  `agentskill.Version`, and refresh installed copies via `gg init --update`.

### 5. Error handling & edge cases

- **Gone bookmark sha** → typed error → precise notice/CLI error naming the
  short sha. Never a silent empty compare.
- **Unreadable/missing shelf blob** → the store's error surfaces as-is.
- **Mixed-kind picks** (commit vs file) refused symmetrically in both
  directions, both pickers.
- **Renames:** the frozen tar carries no rename info — a renamed file shows
  as `D` + `A` in a frozen compare. Documented gap (same standing as
  `PatchPaths`' rename gap), not fixed here.
- **Same-sha self-compare** → notice (TUI) / empty output with exit 0 and a
  stderr note (CLI: comparing an entry against itself is not an error, just
  empty).
- **Untracked-file augmentation** in `CompareFiles` only applies to a
  live working-tree right side; shelf lanes never touch it.

### 6. Testing

- `internal/model`: unit tests for the new kind — `FileRef`, `CacheTag`
  (prefix, no sha collision), `IsLive`, `Display`.
- `internal/domain`: real-git tests (`newTestRepo` + shelf store):
  - shelf↔shelf covering `A`/`D`/`M`/identical-omitted;
  - cross fallback scoped to members (incl. missing-at-commit paths);
  - `ResolveCommitEntryEndpoint`'s three outcomes (live / frozen / typed
    error), exercising a genuinely deleted commit (rewrite + prune).
- `internal/tui`: switcher key tests mirroring `shelf_popup_test` /
  `bookmark_test`: mark-two on commit entries, mixed-pair refusal, cross `c`
  flow both directions, compare-mode enter refusals, gen-guard staleness
  drop, self-compare notice.
- `internal/cli`: in-process `Run(...)` tests covering the full story —
  shelve a commit, rewrite history + `git gc --prune=now`, then assert
  `gg compare` list output, `--patch` output shape, the stderr frozen note,
  and exit codes (0/1/2 paths). A declarative e2e TOML scenario is NOT used
  here: a commit entry's ID embeds the commit sha and blob hash
  (`commit-<shortsha>-<blob8>`), which the TOML harness cannot capture at
  runtime; the Go tests parse the ID from `gg shelf list` output instead.

## Out of scope

- MCP / web surfaces (domain core is ready for them later).
- Changing `gg compare`'s existing raw-rev/`@staged`/`@worktree` tokens
  (they stay as-is; entry specs are additive).
- Rename detection in frozen compares.
- Comparing file entries against commit entries (stays refused).
