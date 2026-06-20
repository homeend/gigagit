# Add-to-`.gitignore` Files-panel actions — Design

**Date:** 2026-06-21
**Status:** Approved, ready for planning
**Scope:** TUI-only (no CLI surface)

## Goal

Add two context (`.`-menu) actions to **untracked** files in the TUI Files
panel:

1. **Add to .gitignore** — ignore the exact selected file.
2. **Add extension to .gitignore** — ignore every file with that extension
   (`*.ext`).

Both append a line to the repo-root `.gitignore`, skip if the line is already
present, and leave the `.gitignore` change unstaged. After the op, a full
refresh makes the now-ignored untracked file disappear from the Files panel and
shows `.gitignore` itself as a change.

## Why untracked-only

`.gitignore` only affects **untracked** files — adding a tracked file's path to
`.gitignore` does nothing in git (the file stays tracked). Offering the actions
on tracked files would produce silent no-ops, so the actions are gated to
untracked files only. This mirrors the existing `canStageHunks()` predicate,
inverted.

## What gets written

- **Exact file:** `"/" + escapeIgnorePattern(path)` — the repo-relative path,
  anchored with a leading `/` so it matches only that one file (e.g.
  `/build/out.log`). The path is the porcelain status path; gg runs
  `git status --porcelain=v2 -z`, and `-z` disables git's path quoting, so
  `f.Path` is already the literal, dequoted path (spaces and all) — no
  unquoting needed.
- **Extension:** `"*" + path.Ext(path)` — e.g. `*.log`. The extension action is
  offered only when the file actually has an extension (`path.Ext(p) != ""`).

**Escaping the exact-file path (required, not optional).** gitignore treats
`*`, `?`, `[`, and `\` as glob metacharacters **anywhere** in a pattern, not
just line-leading. A file literally named `a[1].log` written raw as `/a[1].log`
becomes a character-class pattern matching `/a1.log` — it does **not** match the
real file, so the file is silently *not* ignored. `escapeIgnorePattern`
therefore backslash-escapes `\`, `*`, `?`, and `[` (escaping `\` first). A
leading `#`/`!` needs no escaping because the line always starts with `/`. The
extension line (`*<ext>`) is intentionally a glob and is **not** escaped;
`path.Ext` cannot return a value containing these metacharacters.

A filename ending in a literal trailing space (git trims unescaped trailing
spaces from `.gitignore` lines) is a documented v1 limitation and is **not**
escaped.

**Dotfile extensions (intentional):** `path.Ext(".env")` returns `.env`, so
"Add extension" on a dotfile like `.env` yields `*.env`, and on `.gitignore`
itself yields `*.gitignore`. This is the deliberate, accepted behavior — the
extension action simply uses Go's `path.Ext` semantics.

## Where it writes

The **repo-root `.gitignore`** of the current worktree, always. The anchored
`/path` is naturally root-relative, matching that mental model. Per-directory
`.gitignore` placement is a possible future nicety and is explicitly **out of
scope** for v1.

The `.gitignore` change is left **unstaged**, consistent with `engine.WriteFile`
and the rest of the working-tree-mutating ops.

## Architecture

A new engine op plus thin TUI wiring. No new git verb — the op reuses the
existing `Repo.ReadWorktreeFile` / `Repo.WriteWorktreeFile` worktree-file
methods (the same pair `engine.WriteFile` uses).

### Engine: `engine.Ignore`

```go
// Ignore appends a single pattern to the repo-root .gitignore as an unstaged
// change. Path is the repo-relative path of the selected file. When Ext is
// true the pattern is "*<ext>" (ignore the whole extension); otherwise it is
// the file anchored at the repo root ("/<path>"). A pattern already present is
// a no-op. Default (TreeWrite) lock — it touches the working tree.
type Ignore struct {
    Path string
    Ext  bool
}
```

`Run` logic:

1. `line := ignoreLine(op.Path, op.Ext)`.
2. `existing, _ := deps.Repo.ReadWorktreeFile(ctx, ".gitignore")` — absent/
   unreadable ⇒ treat as empty.
3. If `alreadyIgnored(existing, line)` ⇒ emit `Done`, return
   `Result{Summary: line + " already in .gitignore"}` (not `Changed`).
4. Else `updated := appendIgnoreLine(existing, line)`;
   `deps.Repo.WriteWorktreeFile(ctx, ".gitignore", updated)`; emit `Done`;
   return `Result{Summary: "ignored " + line, Changed: true}`.

No `Decider` fork — appending is non-destructive.

Four pure helpers (no git, no ctx — directly unit-testable):

- `ignoreLine(path string, ext bool) string`
  - `ext`  → `"*" + path.Ext(path)`
  - exact → `"/" + escapeIgnorePattern(path)`
- `escapeIgnorePattern(path string) string` — backslash-escape `\`, `*`, `?`,
  `[` (escape `\` first so already-present backslashes don't double up the
  escapes of later metachars). Applied only to the exact-file line.
- `alreadyIgnored(content []byte, line string) bool` — true when `line`
  matches an existing line exactly after trimming, skipping blank and
  `#`-comment lines.
- `appendIgnoreLine(content []byte, line string) []byte` — if `content` is
  non-empty and does not end in `\n`, insert one first; then append
  `line + "\n"`. Empty `content` ⇒ `line + "\n"`.

### TUI: `internal/tui/ignore_actions.go`

Two accessors following the `commitResetRow()` shape (menu-only, no key, no
footer):

```go
func (m Model) fileIgnoreRow() (actionRow, bool)
func (m Model) fileIgnoreExtRow() (actionRow, bool)
```

Shared gate (a small helper, e.g. `ignorableFile()`):

- `m.focus == panelFiles && m.opsIdle()`
- `bi, ok := m.backingIndex(panelFiles)`; selected `f := m.status.Files[bi]`
- `f.Kind == model.KindUntracked`

`fileIgnoreRow` returns:

```go
actionRow{
    id:    "ignore-file",
    label: "Add to .gitignore",
    run:   func(m Model) (tea.Model, tea.Cmd) {
        return m.startOp(engine.Ignore{Path: p})
    },
}
```

`fileIgnoreExtRow` additionally requires `path.Ext(p) != ""`, uses
`id: "ignore-ext"`, `label: "Add extension to .gitignore (*<ext>)"`, and
`engine.Ignore{Path: p, Ext: true}`.

Wired into `availableActions` (action_menu.go) alongside the other
`*Row()`-style appenders. Documented in `help.go`.

## After the op

`startOp` runs the op and triggers the standard post-op full refresh. The
now-ignored untracked file drops out of the Files panel (git no longer reports
it), and `.gitignore` appears as a modified (or untracked, if newly created)
working-tree change. No special-casing needed.

## Testing

### Engine

- **Pure-helper unit tests** (no git):
  - `ignoreLine`: exact (`/a/b.log`), ext (`*.log`), and the no-extension case
    (caller won't pass `ext` then, but assert `path.Ext` is empty so the TUI
    gate is correct).
  - `escapeIgnorePattern`: `a[1].log` → `a\[1].log`; `a*b` → `a\*b`;
    `a?b` → `a\?b`; `a\b` → `a\\b`; a plain path is unchanged.
  - `alreadyIgnored`: exact match present ⇒ true; absent ⇒ false; ignores
    blank and `#`-comment lines; not fooled by a substring/superstring line.
  - `appendIgnoreLine`: empty content; content without trailing newline (newline
    inserted); content with trailing newline (no double blank line).
- **Real-git op test** (`newRepo`/real `git` in `t.TempDir`): create an
  untracked file, run `engine.Ignore{Path}`, assert (a) `.gitignore` contains
  the anchored line and (b) `git status --porcelain` no longer lists the file.
  Repeat the run to assert idempotence (no duplicate line, `Changed == false`).
  A second test covers the `Ext: true` path the same way.
- **Real-git metacharacter test** (the discriminating case): create a file
  literally named `a[1].log`, run `engine.Ignore{Path: "a[1].log"}`, and assert
  the file actually leaves `git status --porcelain` — this fails if the `[` is
  written unescaped. A filename containing a space (e.g. `a b.log`) in the same
  or a sibling test confirms `-z` dequoting end-to-end (the raw `.Path` is
  written and the file disappears from status).

### TUI

Pure predicate tests (no `startOp` goroutine — avoids the nil-service panic):

- `fileIgnoreRow` present when the Files-panel selection is untracked.
- `fileIgnoreExtRow` present for an untracked file **with** an extension; absent
  for an untracked file **without** an extension.
- Both absent: on the Staged panel; on a tracked (modified) file; while an op is
  running (`m.running`).
- The produced line/`engine.Ignore` payload is correct (anchored path for
  ignore-file; `*.ext` for ignore-ext).

No e2e scenario: the e2e harness drives the CLI, and this feature is TUI-only.
The real-git engine test is the end-to-end proof.

## Files

- **Create:** `internal/engine/ignore.go`, `internal/engine/ignore_test.go`
- **Create:** `internal/tui/ignore_actions.go`, `internal/tui/ignore_actions_test.go`
- **Modify:** `internal/tui/action_menu.go` (wire the two rows into `availableActions`)
- **Modify:** `internal/tui/help.go` (document the two menu actions)
- **Modify:** `CHANGELOG.md`

## Out of scope (v1)

- CLI command (`gg ignore`) — TUI-only for now.
- Per-directory `.gitignore` placement (always repo-root).
- Escaping filenames with a literal *trailing* space (glob metacharacters
  `\ * ? [` ARE escaped; trailing-space trimming is the one unhandled edge).
- Staging the `.gitignore` change automatically.
