# Command-palette commands: File history, File blame, Find, Open repo, Git config explorer, Set up agent skills

**Date:** 2026-07-08
**Status:** design approved, pending spec review
**Branch:** `feat/palette-commands` (worktree off `main`)

## Summary

Grow the `ctrl+p` command palette from one entry ("Show commit") to seven. Two
new entries open a small "type a file path" popup that leads to the existing
history / blame full-screen surfaces. One opens a "type a repo path" popup that
switches gg to a repository not previously opened. Three are thin launchers over
surfaces that already exist elsewhere (the fuzzy finder, the git-config
explorer, the agent-skills picker) — and two of those three are **moved out of
the Settings `,` menu** into the palette.

## Motivation

The palette (`command_palette.go`) was built to grow but has held a single
command since it shipped. History and blame are today only reachable when a file
row is already selected (`h`/`b` on the Files panel, the diff view, the fuzzy
finder) — there is no way to jump straight to "history of an arbitrary path".
Opening a brand-new repository requires knowing its path is already in the MRU
registry (`R` switcher lists only previously-opened repos). And two setup-style
surfaces (git-config explorer, agent-skills) are buried in Settings when they are
really one-shot launchers, not persistent settings.

## The palette after this change

`paletteCommands()` returns, in display order:

| # | Label | keyHint | Action |
|---|-------|---------|--------|
| 1 | Show commit | `#` | *(existing)* goto-commit popup |
| 2 | File history | — | file-path popup → history surface |
| 3 | File blame | — | file-path popup → blame surface |
| 4 | Find | `F` | fuzzy file finder (identical to `F`) |
| 5 | Open repo | — | repo-path popup → validate → `reRoot` |
| 6 | Git config explorer | — | git-config explorer (**moved out of Settings**) |
| 7 | Set up agent skills | — | agent-skills picker (**moved out of Settings**) |

`keyHint` is shown only for entries whose *global* direct key does the identical
thing: `#` (Show commit) and `F` (Find). History/blame's `h`/`b` are **contextual**
(they require a selected file row), so advertising them from a global palette
would mislead — left blank. "Open repo" is a new path-to-new-repo action, distinct
from the `R` MRU switcher, so no keyHint. Git-config and agent-skills have no
global key (they lived only in Settings), so no keyHint.

## Palette launch semantics (existing contract, reused)

The palette's `enter` handler runs `p.cmds[p.sel].run(m)` and does **not** pop the
palette itself. Each command's `run` decides:

- **Launch a popup that keeps the palette beneath it** (esc from the popup reveals
  the palette): just push the popup. This is what "Show commit" does today via
  `Model.openGotoCommitPopup`. The two new **path popups** (File history, File
  blame, Open repo) follow this — esc returns to the palette.
- **Replace the palette** (the command jumps to another surface; esc from that
  surface returns to the base, matching pressing the direct key): `m.popLayer()`
  first, then open. The three **thin wrappers** (Find, Git config, Agent skills)
  do this.

When a path popup finally opens a full-screen surface or switches repos, it
unwinds **both** itself and the palette beneath it — the `resolvedGotoCommit`
trick (`if _, ok := m.topLayer().(*commandPalette); ok { m = m.popLayer() }`).

## Component 1 — `filePathPopup` (File history + File blame)

A new file `internal/tui/file_path_popup.go`, modeled on `gotoCommitPopup`.

```go
type filePathKind int

const (
    filePathHistory filePathKind = iota
    filePathBlame
)

type filePathPopup struct {
    popupMax
    kind  filePathKind
    input textfield
}
```

- `openFilePathPopup(kind)` pushes a fresh popup (palette stays beneath).
- **Rendering:** title "File history" / "File blame"; one `viewField("path: ", …)`;
  hint `[enter] show  [esc] cancel`. Mirrors `gotoCommitPopup.box`, embeds
  `popupMax` for `ctrl+t`.
- **Keys:**
  - `esc` → `popLayer` (reveals palette).
  - `enter` → `path := strings.TrimSpace(input.Value())`; empty → keep open
    (no-op); else normalize, build `navContext{path: rel, rev: ""}`, unwind
    popup + palette, push the history / blame surface + its load cmd:
    - history: `newHistoryView(ctx)` + `loadHistoryListCmd(ctx, hv.listTag)`
    - blame: `newBlameView(ctx)` + `loadBlameCmd(ctx, bv.tag)`
  - other keys → `input.HandleEditKey(msg)`. **Do not** copy `gotoCommitPopup`'s
    `case tea.KeySpace: return m, nil` swallow — file/repo paths can contain
    spaces, and `HandleEditKey` already inserts a space on `KeySpace`
    (`textfield.go`). Let space fall through to the default arm.

`rev: ""` means the working-tree / HEAD context (same as the fuzzy-finder path).

**No pre-validation.** A bogus/untracked path opens the surface anyway: the
history surface already renders `(no history)` for an empty result; the blame
surface already renders `error: …` in its error state. This matches how `h`/`b`
open today (they never pre-validate) and keeps the popup free of an extra git
round-trip.

### Lenient path normalization — `repoRelPath`

A new helper (in `file_path_popup.go` or a small util) turns user-typed input into
the repo-relative, forward-slashed path git wants:

```
repoRelPath(m, p):
    p = strings.TrimSpace(p)
    if p == "" { return "" }
    // Expand a path that is absolute or ./-relative and lands inside the repo.
    root = m.currentWorktree
    if root != "" and filepath.IsAbs(p):
        if rel, err = filepath.Rel(root, p); err == nil and not escaping(rel):
            return filepath.ToSlash(rel)
    // ./x, x/y — clean and slash; already repo-relative.
    return filepath.ToSlash(filepath.Clean(p))
```

- `escaping(rel)` = `rel == ".." || strings.HasPrefix(rel, ".."+sep)`. An absolute
  path outside the repo falls through to the cleaned input (git then reports no
  history / a blame error — acceptable, no hard failure).
- `filepath.Clean("./internal/x") == "internal/x"`, so `./`-prefixed input
  normalizes for free.

## Component 2 — `repoPathPopup` (Open repo)

A new file `internal/tui/repo_path_popup.go`, structurally identical to
`gotoCommitPopup` (async validate-then-act), distinct from the existing MRU
`repoPopup` (`R`).

```go
type repoPathPopup struct {
    popupMax
    input     textfield
    err       string // inline error from the last failed validation; "" = none
    resolving bool
}
```

- `openRepoPathPopup()` pushes a fresh popup (palette stays beneath).
- **Keys:** `esc` cancels; `enter` with non-empty input sets `resolving`, clears
  `err`, dispatches `resolveRepoCmd(path)`; empty enter keeps it open; editing
  clears the stale `err`. Space falls through to `HandleEditKey` (repo paths can
  contain spaces) — same non-swallow rule as `filePathPopup`.
- **`resolveRepoCmd(path)`** (off the UI thread), where `path` is first expanded
  (trim + leading `~`/`~/` → `$HOME`):

  ```go
  svc := domain.OpenTUI(expanded)          // throwaway service rooted at the typed path
  top, err := svc.TopLevel(context.Background())
  return repoResolvedMsg{path: expanded, top: top, err: err}
  ```

  `TopLevel` runs `git rev-parse --show-toplevel` with the typed path as the git
  working directory. It **validates** (errors when the path is not inside a git
  repo) and **normalizes** (returns the repo root, so any subdirectory of a repo
  resolves to its top-level).

- **`resolvedRepoPath(p, msg)`** (dispatched from `Model.Update`, tag-gated: acts
  only when this popup is on top and its input still equals `msg.path`):
  - `err != nil` → `p.err = "not a git repository: " + msg.path`, `p.resolving =
    false`, keep open.
  - success → unwind popup + palette, `return m.reRoot(msg.top)`.

`reRoot` blanks to a loading state and reloads; its load path calls
`repos.Touch(statePath, CurrentWorktree, now)`, so the newly opened repo is
recorded in the MRU registry and appears in the `R` switcher next session. No
extra registration code is needed.

**Layer hygiene:** `reRoot` closes the files view and any diff layer but does not
clear arbitrary popups, so the popup **and** the palette must be popped *before*
calling it (both are, per the unwind step above).

## Component 3 — thin wrappers (Find, Git config, Agent skills)

Each palette entry's `run` pops the palette, then calls the existing opener:

```go
// Find
run: func(m Model) (Model, tea.Cmd) { m = m.popLayer(); return m.openFileFinder() }
// Git config explorer
run: func(m Model) (Model, tea.Cmd) { m = m.popLayer(); return m.openGitConfigExplorer() }
// Set up agent skills  (openAgentPicker returns Model only)
run: func(m Model) (Model, tea.Cmd) { m = m.popLayer(); return m.openAgentPicker(), nil }
```

No behavior of the underlying surfaces changes; only a second entry point is
added.

## Component 4 — Settings menu removals

In `internal/tui/settings_popup.go`:

- Drop `settingsMenuGitConfig` and `settingsMenuAgents` from the `settingsMenu`
  slice.
- Remove their two `case` arms in the `enter` handler
  (`case settingsMenuGitConfig:` / `case settingsMenuAgents:`).
- Keep the `settingsMenu*` string consts and the `openGitConfigExplorer` /
  `openAgentPicker` methods (now called only from the palette). If a const
  becomes entirely unused after removal, delete it too (go vet / unused-const
  check will tell us).

The two surfaces are unchanged; they simply no longer appear under `,`.

## Error handling summary

| Situation | Behavior |
|-----------|----------|
| Empty file/repo path + enter | Popup stays open (no-op), like goto-commit |
| Bogus file path (history) | Surface opens, renders `(no history)` |
| Bogus file path (blame) | Surface opens, renders git's `error: …` |
| Non-repo path (Open repo) | Inline `not a git repository: <path>`, popup stays open |
| Open repo `TopLevel` succeeds | `reRoot` to the resolved top-level |

## Testing

New/updated tests in `internal/tui`:

- **Registry** (`command_palette_test.go`): `paletteCommands()` returns the seven
  entries in order with the expected labels and keyHints.
- **`filePathPopup`**: enter-with-path pushes a `*historyView` / `*blameView`
  whose `navContext` has the normalized path and `rev == ""`; empty enter keeps
  the popup; esc reveals the palette beneath.
- **`repoRelPath`**: table — absolute-inside-repo → repo-relative; `./x` → `x`;
  already-relative unchanged; absolute-outside-repo → cleaned fallback; empty → empty.
- **`repoPathPopup`**: success path (a temp git repo) resolves via `TopLevel` and
  `reRoot`s to its top-level (and unwinds popup+palette); a non-repo temp dir sets
  the inline error and keeps the popup open; a subdirectory of a repo resolves to
  the repo root. `~` expansion covered by a unit test on the expansion helper.
- **Wrappers**: each of Find / Git config / Agent skills, launched from the
  palette, pops the palette and pushes the correct layer (`*fileFinderPopup`,
  `*gitConfigPopup`, agent picker).
- **Settings**: `settingsMenu` no longer contains `settingsMenuGitConfig` /
  `settingsMenuAgents`.

Follow TDD (tests first) per repo convention. Tests use the `FakeRunner` for argv
assertions or a real `git` in a `t.TempDir()` (the `repoPathPopup` `TopLevel`
tests need a real repo).

## Files touched

- `internal/tui/command_palette.go` — extend `paletteCommands()`.
- `internal/tui/file_path_popup.go` — **new**: `filePathPopup` + `repoRelPath`.
- `internal/tui/repo_path_popup.go` — **new**: `repoPathPopup` + `resolveRepoCmd`
  + the resolved-msg handler.
- `internal/tui/model.go` — dispatch `repoResolvedMsg` to `resolvedRepoPath`
  (tag-gated), mirroring `gotoCommitResolvedMsg`.
- `internal/tui/settings_popup.go` — remove the two moved rows + their cases.
- Tests: `command_palette_test.go`, new `file_path_popup_test.go`,
  `repo_path_popup_test.go`, `settings_popup_test.go` (menu assertion).

## Out of scope / non-goals

- No change to the `R` MRU switcher, the history/blame/finder/git-config/agent
  surfaces themselves, or the CLI.
- No fuzzy path autocompletion in the file-path popup (plain text field, per the
  brainstorm decision).
- No new global direct keybindings (history/blame stay contextual; the palette is
  the new global entry point).

## Docs to update after implementation

- `CHANGELOG.md` (always).
- `README.md` if the palette's user-facing surface is documented there.
- `CLAUDE.md` package map — note the palette's growth and the two Settings→palette
  moves if the Settings/`tui` description warrants it.
- Palette entries should also be reflected wherever the palette is advertised in
  help/footer (per the "advertise features in help AND footer" convention) — verify
  during implementation.
