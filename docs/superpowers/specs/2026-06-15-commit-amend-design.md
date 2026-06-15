# F2 — Commit & amend — design

**Date:** 2026-06-15
**Status:** draft (brainstorm)
**Roadmap:** `docs/superpowers/2026-06-14-feature-roadmap.md` (F2)

## Goal

Commit the staged index from the TUI with a **title + description** message,
and amend the last commit (edit its message and/or fold newly-staged changes
into it). Builds directly on F1 (which fills the index via `space`). Adds CLI
`gg commit --amend` parity.

## What already exists

- `engine.Commit{Message, All}` (`internal/engine/ops_basic.go`) and the
  `git commit` verb (`internal/git/mutate.go`: `Commit(ctx, message, all)`),
  wired only to the CLI (`gg commit -m`). There is **no TUI commit** and **no
  `--amend`** anywhere.
- `c` and `C` are **free** keys in the TUI main dispatch.
- `internal/tui/worktree_popup.go` is the multi-field popup exemplar (state
  struct behind a pointer field, hand-rolled field input, `esc` cancels,
  key-swallowing routing) — the commit popup mirrors its conventions.
- After F1, the Status panel reflects the staged index that this feature commits.

## Surface: the commit popup (two fields)

A new `commitPopup` (pointer field `m.commitPopup *commitPopup`), modeled on
`worktreePopup`:

```go
type commitPopup struct {
	title string // subject line (single line)
	desc  string // body (multi-line, optional)
	field int    // 0 = title, 1 = description
	amend bool    // true → git commit --amend (C); false → fresh commit (c)
}
```

**Field navigation & submit:**
- Opens focused on **title**.
- `tab` / `shift+tab` switch between title and description.
- In **title**: `enter` moves focus to description (titles are single-line).
- In **description**: `enter` inserts a newline (bodies are multi-line).
- **`ctrl+s` commits** (works from either field).
- `esc` cancels (`m.commitPopup = nil`); `ctrl+c` quits. The handler swallows
  every key (no fallthrough), per the popup checklist.

**Message assembly:** `title` when description is empty; otherwise
`title + "\n\n" + desc` (git's subject/blank-line/body convention).

**Guards:**
- `c` (fresh commit) when nothing is staged → `statusMsg = "nothing staged"`,
  popup does not open.
- Submit with an empty (whitespace-only) title → refuse, inline popup hint
  "title required"; do not close.

## Amend (`C`)

`C` opens the same popup with `amend = true`, **pre-filled from HEAD's message**:
the first line → `title`, the remainder (after a blank line) → `desc`. On submit
it runs `git commit --amend`, which folds whatever is currently staged into the
last commit and rewrites its message to the assembled value. "Add to last
commit" is therefore: stage with `space` (F1) → `C` → `ctrl+s` (message
unchanged or edited).

- Pre-fill needs a read: **`LastCommitMessage`** (below).
- `C` with no commits yet (unborn HEAD) → `statusMsg = "no commit to amend"`,
  no popup.
- **Pushed-amend warning is deferred** (not built in F2): amend is already a
  deliberate two-step action, and since `gg push` is not a force-push,
  amending a pushed commit simply makes the next push fail visibly. A dedicated
  warning can come with the rebase work (F12/M3).

## Engine & verbs

- **Extend the `git commit` verb** to
  `Commit(ctx context.Context, message string, all, amend bool) error` —
  `gitcmd.New("commit").ArgIf(all, "-a").ArgIf(amend, "--amend").Arg("-m", message)`.
  Update the `engine.GitOps` interface signature and the two call sites
  (`engine.Commit`, and the new `LastCommitMessage` is a separate verb).
- **Extend `engine.Commit`** with an `Amend bool` field; `Run` passes it to the
  verb. `Result.Summary` = e.g. `committed: <title>` / `amended: <title>`.
- **New read verb** `LastCommitMessage(ctx) (string, error)` =
  `git log -1 --pretty=%B` (empty/err on unborn HEAD), added to `GitOps`, and a
  gated **`domain.LastCommitMessage`** query (mirrors `domain.CurrentBranch`).

## Refresh

Standard full reload. Unlike staging (F1's status-only refresh), a commit
changes **both** the Status panel (index cleared) and the Commits panel (new/
rewritten commit), so the existing `startOp → opFinishedMsg → loadCmd()` path
is correct. Commit is a one-shot action, so the full reload cost is fine.

## TUI wiring

- `c` dispatch: gated by a new `canCommit` predicate (`m.opsIdle()` and the
  index has at least one staged file — derive from `m.status` counts). Opens
  `commitPopup{amend:false}`.
- `C` dispatch: gated by `canAmend` (`m.opsIdle()` and HEAD has ≥1 commit).
  Issues `LastCommitMessage` (a tiny cmd) to pre-fill, then opens
  `commitPopup{amend:true}`.
- Popup key routing added before the normal-key switch (modal → popups →
  normal), per `adding-tui-windows`.
- Submit (`ctrl+s`) → assemble message → `m.commitPopup = nil` →
  `m.startOp(engine.Commit{Message: msg, Amend: p.amend})`.
- Footer: `[c] commit` (gated `canCommit`) and `[C] amend` (gated `canAmend`)
  in `footer.go`; help rows for both in `help.go` (`TestHelpFooterCoverage`).

## CLI parity

Add `--amend` to `gg commit`: `gg commit [--amend] [-m <msg>] [-a]`. With
`--amend` and no `-m`, reuse the existing message (`git commit --amend
--no-edit` semantics) — or require `-m` for simplicity in v1 (decide at review).
Bump `agentskill.Version`; document in `using-gg.md`; `gg init --update`.

## Out of scope (later features)

- **Reword arbitrary (non-HEAD) commits** → F12 (reuses this popup; needs the
  interactive-rebase driver).
- **Hunk-level** staging before commit → F3.
- **Pushed-history amend warning** → with F12/M3.
- **Commit-message templates / hooks / sign-off** → not now (YAGNI).

## Tests

- `internal/git`: argv for `Commit` with `all`/`amend` combinations; a
  real-repo amend (message changes, staged file folds in, parent unchanged);
  `LastCommitMessage` returns HEAD's full message.
- `internal/engine`: `Commit{Amend:true}` amends; `Commit{}` commits the index;
  Summary text.
- `internal/tui`: `c` opens the popup; typing title/description; `tab` switches
  fields; `enter` in description adds a newline; `ctrl+s` commits and the
  Commits panel gains the commit; empty-title refusal; `c` with nothing staged
  is a no-op with statusMsg; `C` pre-fills from HEAD and amends; key-swallow
  test (a global key while the popup is open does nothing).
- `internal/cli`: `gg commit --amend -m` rewrites the last commit.

## Files touched

| File | Change |
|------|--------|
| `internal/git/mutate.go` (+test) | extend `Commit` with `amend`; add `LastCommitMessage` |
| `internal/engine/gitops.go` | update `Commit` sig; add `LastCommitMessage` |
| `internal/engine/ops_basic.go` (+test) | `engine.Commit` gains `Amend` |
| `internal/domain/query.go` | `LastCommitMessage` gated query |
| `internal/tui/commit_popup.go` (+test) | the two-field popup + routing |
| `internal/tui/avail.go` | `canCommit`, `canAmend` |
| `internal/tui/model.go` | `c`/`C` dispatch + popup routing + pre-fill cmd |
| `internal/tui/footer.go`, `help.go` | hints + help rows |
| `internal/cli/cli.go` | `--amend` flag on `gg commit` |
| `internal/agentskill/using-gg.md` + `agentskill.go` | document `--amend`; bump Version |
| `CHANGELOG.md`, `README.md` | entries |

## Open decisions to confirm at review

1. **Submit key** — `ctrl+s` (recommended; reliable in raw-mode terminals), vs
   a worktree-style "tab past description → action line, then `enter`".
2. **Keys** — `c` commit / `C` amend (recommended).
3. **CLI `--amend` without `-m`** — reuse existing message (`--no-edit`) or
   require `-m`.
4. **Pushed-amend warning** — confirm deferring it to F12/M3.
