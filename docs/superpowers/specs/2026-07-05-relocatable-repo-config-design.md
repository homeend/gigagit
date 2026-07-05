# Relocatable per-repo settings (private user-dir copy)

**Date:** 2026-07-05
**Status:** Design approved, ready for planning
**Feature branch:** `feat/relocatable-repo-config`

## Problem

gg reads settings in two tiers today (`config.Load(globalPath, repoPath)` in
`internal/config/config.go`):

1. **Defaults** (built in)
2. **Global** — `~/.config/gg/config.toml` (`DefaultGlobalPath()`, honours
   `$XDG_CONFIG_HOME`)
3. **Repo** — `<repo-top>/.gg.toml`, overlaid field-level, repo wins.

The repo tier lives *inside the working tree*, so it is committable and shared
with everyone who clones. On a shared monorepo where different users have very
specific, personal usage patterns (refresh rates, graph on/off, commit sort,
worktree hook, …), there is nowhere to put a **per-repo preference that stays
private to this machine** — the only per-repo file is one that gets committed.

## Goal

Add a third place for per-repo settings that lives in the user's config
directory instead of the working tree, so personal per-repo tweaks never land
in a shared `.gg.toml`. Provide a Settings action to **copy or move** the whole
per-repo config between the committed `.gg.toml` and this private file, in both
directions.

Non-goals (YAGNI): per-setting relocation, a CLI surface, any change to the
global tier or to git config.

## Design

### 1. The private file and its path

A per-repo config file with the **identical TOML schema** as `.gg.toml`, stored
in the user config directory under a Claude-style, human-readable key:

```
$XDG_CONFIG_HOME/gg/projects/<encoded-main-worktree-path>/config.toml
   e.g.  ~/.config/gg/projects/-mnt-t-others-gigagit/config.toml
```

- **Key = the main-worktree absolute path**, encoded by replacing every `/`,
  `\`, and `:` with `-`. So `/mnt/t/others/gigagit` → `-mnt-t-others-gigagit`;
  on Windows `C:\src\repo` → `C--src-repo`.
- **Anchored on the main worktree** (`Worktrees(ctx)[0].Path`, the same anchor
  `TempExportBase`/`ExportDefaultDir` already use), so every *linked* worktree
  of a repo resolves to the **same** private file — matching the "survives
  across worktrees" property the committed `.gg.toml` does not have (a linked
  worktree has no `.gg.toml` of its own).
- Base directory sits next to the existing global config
  (`$XDG_CONFIG_HOME/gg/`), never inside any working tree ⇒ never committed.

New pure helper in `internal/config`:

```go
// EncodeRepoKey turns an absolute repo path into a filesystem-safe, readable
// directory name (/,\,: → -). Empty in ⇒ empty out (caller must guard).
func EncodeRepoKey(mainWorktreePath string) string

// PrivateRepoPath returns the private per-repo config path for a repo whose
// main worktree is at mainWorktreePath, honouring $XDG_CONFIG_HOME. Returns ""
// if mainWorktreePath is "" (no anchor ⇒ no private path).
func PrivateRepoPath(mainWorktreePath string) string
```

These are pure/string functions — no I/O, unit-testable without a repo. The
main-worktree path is resolved by the caller (TUI/CLI) via the existing
`Worktrees()` query; `config` never imports `git`/`domain`.

**Anchor = main worktree, not current worktree (deliberate).** The committed
`.gg.toml` is anchored on `top` = the *current* worktree today
(`filepath.Join(top, ".gg.toml")` in `load.go`). The private file is instead
anchored on the **main** worktree (`Worktrees()[0].Path`) so every linked
worktree of a repo shares one private config — a user with five worktrees gets
one "this repo's private settings", not five. This asymmetry is intentional:
`.gg.toml` is a tracked working-tree artifact (naturally per-checkout); the
private file is a machine-level per-repo artifact.

**Load-time ordering.** `loadCmd` calls `config.Load` *before* the snapshot so
`commit_initial_count`/`commit_sort` govern the first feed walk — but at that
point only `top` is known, not the main-worktree path. Since a user may set
those keys in the private file, `loadCmd` resolves the main-worktree path
**up front** (one extra cheap `svc.Worktrees(ctx)` gated read; fall back to
`top` if it fails/empty), computes `privateRepoPath`, resolves the **active**
path (`ActiveRepoConfigPath`), and passes that single path to `config.Load` as
`repoPath`. So the active file governs the first paint too — no one-paint
sort/page-size discrepancy.

### 2. Precedence — the repo tier's file relocates (pure replace, not layer)

```
defaults → global → active repo file      (later wins)
```

The per-repo tier is **one file read at one path** — not two layered files.
The active path is the **private file if it exists on disk, else the committed
`.gg.toml`** (`config.ActiveRepoConfigPath`, §3). `config.Load` is **unchanged**
(still `Load(globalPath, repoPath)`); the caller passes the active path as
`repoPath`. Global still sits under the active repo file, so "global overridden
by local" is preserved exactly.

**Why replace, not layer.** An earlier draft layered the private file *over* the
committed one. With whole-file copy/move that is both pointless and buggy:

- *Pointless:* `CopyRepoConfig` duplicates **every** key into the private file,
  so there are no "keys I didn't set privately" left to inherit from the
  committed baseline — layer's only advantage evaporates.
- *Buggy:* the overlay uses zero-is-unset / inverted-polarity rules
  (`overlayRefresh`: `if src.WorktreesWatch { dst = true }`, `if src.Status > 0`).
  With committed `worktrees_watch = true` copied under a private `false`, the
  private `false` reads as *unset* and the committed `true` **shadows** it — so
  toggling that setting off after a copy silently does nothing. Same for every
  interval (`0` = unset) and inverted-polarity flag.

Replace matches the user's own framing — "the importance stays the same, only
the path differs" — and unifies the read path with the write target (both are
`ActiveRepoConfigPath`).

### 3. Active per-repo write target (the subtle part)

Every per-repo Settings **write** currently targets `m.repoConfigPath` =
`<repo>/.gg.toml`:

- `config.SetShowGraph`, `SetCommitSort`, `SetRefreshInterval`,
  `SetRefreshWatch`, `SetWorktreePostCreateHook`.

If a user moves settings to the private file but a later toggle still writes to
`.gg.toml`, gg would **recreate a committed file** and silently defeat the
feature.

**Rule: the active per-repo write target is the private file if it exists, else
`<repo>/.gg.toml`.** Resolved once at load and on every repo switch (`reRoot`),
stored in `m.repoConfigPath` exactly as today. Every existing writer is
unchanged — they keep writing to `m.repoConfigPath`, which now simply points at
whichever file is active. The **read** path (§2) resolves the *same*
`ActiveRepoConfigPath`, so gg reads and writes one active file — never two.

```go
// private if it exists on disk, else committed; "" private ⇒ committed.
func ActiveRepoConfigPath(committedPath, privatePath string) string
```

- `PrivateRepoPath(...)` present on disk ⇒ `m.repoConfigPath` = private path.
- else ⇒ `m.repoConfigPath` = `<repo>/.gg.toml` (today's behaviour).

The existence stat happens **off the UI thread** in `loadCmd` (not in the value-
receiver `Update`, which avoids I/O): `dataLoadedMsg` is extended to carry the
private path alongside the existing `repoTOML`, plus the resolved active target;
the `dataLoadedMsg` handler rebinds `m.repoConfigPath` to that target — the same
seam that already rebinds `repoConfigPath` on repo switch (`model.go`). The model
also stores both raw paths so the Settings popup can show each slot's state
(re-stat on popup open, since state changes after a copy/move).

The copy/move actions (below) update `m.repoConfigPath` in-memory after they run
so writes redirect immediately without needing a reload.

### 4. The relocation operations

Four whole-file operations, all plain file I/O over the identical schema
(atomic temp-file + rename, mirroring `config.atomicWriteFile`):

| Operation | Effect |
|-----------|--------|
| **Copy to private** | read `.gg.toml` → write private file; leave `.gg.toml`. Private now wins and becomes the write target. |
| **Move to private** | copy to private, then delete `.gg.toml`. |
| **Copy to committed** | read private → write `.gg.toml`; leave private. (Private still exists ⇒ still the write target.) |
| **Move to committed** | copy to committed, then delete private file. `.gg.toml` becomes the write target. |

New writers in `internal/config/write.go` (or a new `relocate.go`), each taking
explicit source and destination absolute paths so `config` stays free of
git/domain:

```go
// CopyRepoConfig copies the whole config file src → dst (atomic write, parents
// created). A missing src is an error the caller surfaces ("nothing to move").
func CopyRepoConfig(src, dst string) error

// RemoveRepoConfig deletes path (absent is not an error — move = copy+remove).
func RemoveRepoConfig(path string) error
```

Move = `CopyRepoConfig` then `RemoveRepoConfig(src)`. Keeping copy and remove
separate keeps each trivially testable and lets the TUI sequence them.

Whole-file copy is deliberately dumb: because both files share the exact schema,
there is no merge — the destination is overwritten wholesale. If the user wants
to preserve a hand-edited destination, that is out of scope (whole-file model,
as agreed). The action confirms before overwriting a non-empty destination.

**Tracked-`.gg.toml` caveat.** In the target use case the repo is *shared*, so
`.gg.toml` is git-tracked. `RemoveRepoConfig` is a plain `os.Remove`, so **Move
to private** leaves a pending git *deletion* in the working tree rather than
cleanly "removing it from the shared repo" (and a later checkout can restore it).
For the shared-repo case **Copy to private is the primary flow** — the committed
file stays as the team baseline, the private file takes effect (replace), and the
tree stays clean. The popup footer notes that move deletes the source; documenting
this (README) is in scope, a git-tracked check in the popup is a deferred nicety
(it needs a `git ls-files` domain probe the popup does not currently make). In
*this* repo `.gg.toml` is untracked (the session's `?? .gg.toml`), so the smoke
test won't surface it — the user's shared-repo scenario will.

### 5. Settings surface (`,` menu → one new row)

A new Settings row, e.g. **"Repo settings location"**, opens a small popup
(`adding-tui-windows` popup taxonomy) showing both slots and their live state:

```
Committed   <repo>/.gg.toml                       [present / absent]
Private     ~/.config/gg/projects/…/config.toml   [present / absent]

Actions (only the applicable ones shown):
  → Copy to private
  → Move to private
  → Copy to committed
  → Move to committed
```

- Which actions are offered depends on what exists: e.g. with only `.gg.toml`
  present, offer Copy/Move to private; with only the private file present, offer
  Copy/Move to committed; with both present, offer all four; with neither, the
  row is informational (shows both as absent) and offers nothing.
- Overwriting a non-empty destination asks a yes/no confirmation first
  (reuse the existing confirm pattern).
- After the action runs, refresh in-memory config (re-`Load`) and re-resolve
  `m.repoConfigPath` so subsequent Settings edits and the next paint reflect the
  new layout.

### 6. Repo switch

`reRoot` already rebinds `m.repoConfigPath` to the new repo's `.gg.toml`
(`model.go`). Extend that path to: compute the new repo's private path, prefer
it if present, and feed both `repoPath` and `privateRepoPath` into `Load`. No
new lifecycle — it rides the existing repo-switch config-rebind seam (the same
one fixed by the show_graph repo-switch fix).

## Testing

- `internal/config` unit tests:
  - `EncodeRepoKey` — POSIX and Windows-style inputs, empty input.
  - `PrivateRepoPath` — honours `$XDG_CONFIG_HOME`, empty anchor ⇒ "".
  - `ActiveRepoConfigPath` — private-absent ⇒ committed; private-present ⇒
    private; empty private ⇒ committed.
  - `CopyRepoConfig`/`RemoveRepoConfig` — round-trip bytes identical, parent dir
    created, missing-src error, absent-remove is a no-op.
- TUI: pure-logic tests for `repoConfigActions` (which actions given which files
  exist) and `repoCfgEndpoints` (src/dst/isMove per action). The off-thread
  write-target redirection is covered by `ActiveRepoConfigPath` + the live smoke
  test (driving the real popup) — an isolated TUI test of the `svc.Worktrees`
  stat path is impractical.
- No e2e scenario required for MVP (TUI-only surface); revisit if a CLI surface
  is added later.

## Docs to update on completion

- `CHANGELOG.md` (always).
- `README.md` — document the config precedence (global under the active repo
  file) and the private per-repo
  file location.
- `CLAUDE.md` — the `config` package row: new tier, `PrivateRepoPath`, the
  active-write-target rule.
- The `adding-config-entries` skill mentions the two-tier overlay; update to
  three tiers.
- No `agentskill` bump (no CLI surface change).

## Open items / deferred

- **CLI parity** (`gg config move --to-private|--to-repo [--copy]`) —
  intentionally deferred. If added later, factor the relocation into a shared
  domain/helper both frontends call.
- **Orphan cleanup** — if a repo directory is renamed/moved, its private config
  is orphaned under the old encoded key. Out of scope; the private file is
  cheap and inert. A future `gg config prune-private` could sweep unreadable
  keys.
- **"Discard private / revert to committed"** — with replace semantics, going
  back to the committed file while both exist means deleting the private file
  (not "move to committed", which overwrites the team file with your private
  version). The four copy/move actions match the user's request; a dedicated
  discard action is a deferred nicety.
- **Git-tracked warning in the popup** — warn when "Move to private" would delete
  a *tracked* `.gg.toml` (needs a `git ls-files` domain probe). Deferred;
  documented in README for MVP.
