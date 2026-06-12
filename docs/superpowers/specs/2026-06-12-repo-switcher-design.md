# Repo Switcher — Design Spec

**Date:** 2026-06-12
**Status:** Approved
**Scope:** The "Workspaces" half of M2, narrowed to the repo-switcher (Plan B).
Named workspace groups and **group sync** are the designated follow-up (see
§Roadmap); multi-repo-open-at-once stays backlog.

## Goal

Jump between known repositories from inside gg — a popup picker in the TUI
(re-rooting in place, shell follows on exit) and a `gg repo` CLI — with a
zero-maintenance repo list that builds itself from usage.

## Motivation

gg already re-roots cleanly (`reRoot(path)` from the worktree work) and the
shell already follows (`--cwd-file` + `gg shell-init`). What's missing is the
list of places to jump to. People who juggle a monorepo plus satellite repos
currently `cd` around; the switcher makes that one keystroke.

## 1. The repo registry: auto-tracked recents

- **No manual registry.** Every time gg opens a repo — process start or
  `reRoot` — it records that repo's **top-level path** with a last-opened
  timestamp. The list is MRU-sorted. Adding a repo = open gg there once.
- **Storage:** a global **state** file (machine-local history, never
  committed): `<XDG_STATE_HOME>/gg/repos.toml`, defaulting to
  `~/.local/state/gg/repos.toml` (Windows: `%LocalAppData%/gg/repos.toml`).
  Written atomically (temp + rename), like the `<seq>` counter state.
- **Schema:**

  ```toml
  [[repos]]
  path = "/abs/top-level"
  last_opened = 2026-06-12T10:00:00Z
  ```

- **Pruning:** on load, entries whose path no longer exists are dropped
  silently (lazily — persisted on the next Touch, not eagerly rewritten).
- **Dedupe:** by exact path after `filepath.Clean`. A linked worktree is a
  valid entry (whatever root gg ran at); intra-repo switching stays the
  Worktrees panel's job — no special-casing here.
- **Corruption tolerance:** an unreadable/unparsable state file behaves as an
  empty list (never blocks startup); the next Touch rewrites it whole.

## 2. New package: `internal/repos`

Shared by both frontends (the `internal/worktree` precedent):

```go
type Entry struct {
    Path       string    // absolute top-level path
    LastOpened time.Time
}

func Name(e Entry) string // display name: filepath.Base(e.Path)

// Load reads the registry, drops entries whose Path no longer exists, and
// returns MRU-first (most recently opened first). Missing/corrupt file = empty.
func Load(statePath string) []Entry

// Touch records repoPath (cleaned, absolute) with now as LastOpened, dedupes,
// prunes dead paths, and writes atomically. Errors are returned but callers
// treat recording as best-effort (a read-only FS must not break gg).
func Touch(statePath, repoPath string, now time.Time) error

// Remove forgets repoPath (no error if absent). Never touches the repo itself.
func Remove(statePath, repoPath string) error

// DefaultStatePath resolves the platform state file location.
func DefaultStatePath() string
```

## 3. TUI: `R` opens the switcher popup

Per the adding-tui-windows taxonomy this is a transient picker → **popup**
(pointer field on Model), not a panel.

- **`R`** (unbound today; mnemonic Repo, pairs with `r` reload) opens a
  centered popup (via `overlayCenter`) listing known repos **MRU-first**:
  name, path, and a coarse relative age ("2h ago", "3d ago"). The current
  repo is listed, marked (e.g. `●`), and `enter` on it is a no-op.
- **Keys inside the popup** (swallows everything; standard popup contract):
  - typed runes / backspace — **filter as you type** (case-insensitive
    substring over name+path; same matching semantics as `/`),
  - `↑`/`↓` — move selection (within the filtered list),
  - **enter** — switch: `m.repoPopup = nil` then `reRoot(path)` (existing
    primitive: new repo, full reload, selections reset, `switchTarget` set so
    `--cwd-file`/shell-init makes the shell follow on exit),
  - **ctrl+d** — remove the selected entry from the registry (forgets the
    *entry only*; never touches the repo on disk),
  - **esc** — cancel; **ctrl+c** — quit gg.
- **Recording:** `Touch` runs on TUI startup and inside `reRoot`, best-effort
  (failure to write state never surfaces as an error).
- Footer hint gains `[R]epo`.

## 4. CLI: `gg repo`

- `gg repo list` — `<name>\t<path>` MRU-first to stdout (scriptable).
- `gg repo switch <query>` — case-insensitive substring match of query against
  each entry's name and path:
  - exactly one match → print the path to stdout and write it to `--cwd-file`
    (a `gg`-wrapped shell `cd`s there); exit 0,
  - zero matches → error to stderr, exit 1,
  - multiple matches → error listing the candidates, exit 1.
- **No engine operation.** Switching repos is frontend state, not a git
  mutation — `internal/engine` is untouched by this feature. (No new decision
  IDs either; the cliDecider is not involved.)
- The CLI also `Touch`es the registry for the repo it runs in (same
  best-effort rule), so heavy CLI users build the same list.
- Register `repo` in the `commands` map + dispatch; update the
  `cmd/gg/main.go` unknown-command help string (per the adding-features
  checklist).

## 5. Files touched (expected shape)

| File | Change |
|------|--------|
| `internal/repos/repos.go` (new) | Registry: Load/Touch/Remove/DefaultStatePath, TOML schema, atomic write, prune, dedupe. |
| `internal/tui/repo_popup.go` (new) | Popup state struct + key handler + renderer (worktree popup is the exemplar). |
| `internal/tui/model.go` | `repoPopup` pointer field; `R` key; popup routing (modal → popups → filter → normal); `Touch` on startup/reRoot. |
| `internal/tui/view.go` | Popup overlay branch; footer `[R]epo`. |
| `internal/cli/repo.go` (new) | `cmdRepo` (list/switch). |
| `internal/cli/cli.go` | `repo` registration + dispatch. |
| `cmd/gg/main.go` | Help-string update. |
| Docs | `CHANGELOG.md`; `README.md` (key + CLI tables); `CLAUDE.md` package map (+`repos`). |

## 6. Testing

- **repos package (pure, temp dirs):** Touch creates/bumps/dedupes; Load is
  MRU-first; dead paths pruned; Remove forgets; corrupt file → empty list;
  atomic write leaves no temp litter; missing parent dirs created.
- **TUI:** `R` opens with MRU entries; typing filters; enter re-roots
  (assert `switchTarget` equals the chosen path — the worktree-switch test
  pattern); enter on current repo is a no-op; ctrl+d removes from the popup
  list AND the state file; esc cancels; popup swallows global keys
  (`p` while open → nothing); overlay fits (existing fit-test pattern);
  startup/reRoot Touch the registry (state file gains the path).
- **CLI:** list order; switch writes cwd-file and prints the path;
  zero/ambiguous matches error with the right exit codes; running any command
  Touches the registry.
- TUI/CLI tests point `DefaultStatePath` at a temp file — the state path is
  injectable (a field/parameter, not a hard-coded global) so tests never touch
  the real user state.

## 7. Out of scope (YAGNI)

- **Group sync** (see Roadmap), named groups/tags, directory scanning,
  manual `gg repo add` (opening gg there does it), multi-repo-open,
  per-repo UI-state persistence, fuzzy matching.

## Roadmap note — group sync (designated follow-up)

A *workspace* = named group of repos; **group sync** = one command applying
background-SmartPull (fetch + fast-forward main without touching the working
tree) to every repo in the group, in parallel, with a per-repo summary.
Deferred because it requires: named groups in the registry schema, concurrent
engine operations in a TUI built around one-op-at-a-time, decision routing
from multiple simultaneous ops through one modal (queueing + "which repo is
asking?"), and partial-failure reporting/exit semantics. That concurrency +
decision-queueing design is shared with the M3 MCP server, so it should be
brainstormed once, deliberately, on top of this feature's registry.
