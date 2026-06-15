# Interactive rebase — design

**Date:** 2026-06-15
**Status:** draft (brainstorm, approved)
**Roadmap:** `docs/superpowers/2026-06-14-feature-roadmap.md` (F12 / M3 interactive-rebase slice)

## Goal

A GitKraken-style **interactive-rebase editor** in the TUI: a list of the
commits being rebased, each with a per-row action (**Pick / Reword / Squash /
Drop**), reorderable, with **Reset / Cancel / Start Rebase**. "Reword any
commit" (the original F12 ask) becomes simply the **Reword** action inside this
editor. Plus a thin scriptable CLI entry so the operation is scriptable and
e2e-testable.

This is the first concrete slice of M3's interactive-rebase machinery.

## What already exists (and what's missing)

- **Pair-op picker** (`internal/tui/mark.go`, `pairop_popup.go`): mark a branch
  with `m`, mark a second with `m`, get a picker offering
  `pairOp{label(marked,selected), build(marked,selected) engine.Operation}`.
  Today: **Merge {marked} into {selected}** (`engine.SmartMerge`) and
  **Rebase {marked} onto {selected}** (`engine.SmartRebase{Branch, Onto}`).
- **`engine.SmartRebase`** (`git rebase <onto>`, worktree-aware: rebases in the
  worktree that has the branch checked out, else autostash+switch; conflict →
  keep/abort decision). The interactive op reuses this worktree-awareness and
  conflict pattern.
- **View-stack** primitive (`internal/tui`, used by history/blame): a
  full-screen surface that owns the keyboard; `esc` pops. The editor is a new
  consumer.
- **F2 commit popup** (`internal/tui/commit_popup.go`): title+description
  message popup. **Reused verbatim** to capture the Reword message.
- **Stash by path** (`internal/git/stash.go`:
  `StashPush(ctx, msg, paths, includeUntracked)` + pop/apply/drop) from the
  stash-surface work — used by the stash-wrap.
- **Missing — built here:**
  - `internal/gitexec` has **no per-command env** support: `ExecRunner.Run`/
    `Stream` build `exec.CommandContext(ctx, gitPath, argv...)` and inherit the
    process environment; there is no way to set `GIT_SEQUENCE_EDITOR` for one
    invocation.
  - No non-interactive "editor" for `git rebase -i`.
  - No interactive-rebase engine op, TUI editor, or CLI entry.

## Entry & range

- Branches panel: mark `marked`, mark `selected` → the pair-op picker gains a
  third entry **"Interactive rebase {marked} onto {selected}"** (same
  marked/selected direction as the existing Rebase).
- Choosing it opens the editor loaded with the range **`selected..marked`** —
  the marked branch's commits not reachable from `selected`; `selected` is the
  immutable base. Empty range → no-op with a statusMsg.
- **Worktree-aware** exactly like `SmartRebase`: if `marked` is checked out in a
  linked worktree, the rebase runs there; otherwise gg autostashes/switches.

## Architecture — four slices (one design, built in dependency order)

| Slice | Responsibility | Depends on |
|---|---|---|
| 1. `gitexec` per-command env | A git invocation can carry extra environment (`GIT_SEQUENCE_EDITOR`, …). | — |
| 2. gg-as-editor protocol | Hidden `gg` subcommands git invokes as its sequence editor / reword step, applying a gg-written **plan file**. Cross-platform (no `sed`). | 1 |
| 3. engine `InteractiveRebase` op | Plan → driven `git rebase -i <base>` with the stash-wrap and conflict-pause handling. | 1, 2 |
| 4. TUI editor view + CLI entry | The GitKraken-style editor that builds the plan and calls the op; thin scriptable `gg rebase -i <base> --plan <file>`; e2e scenario. | 3 |

Each slice is an independently-mergeable PR; slices 1–2 ship as foundation with
their own unit tests even before the editor is visible.

### Slice 1 — `gitexec` per-command env

- Extend the `Runner` contract so a call can carry extra env. Chosen shape: an
  optional env map threaded through `Run`/`Stream` (e.g. a new
  `RunEnv(ctx, name, argv, env)` / `StreamEnv(...)` pair, or an `env` field on a
  call-options struct), keeping the existing `Run`/`Stream` as the no-env
  fast path so existing call sites are untouched.
- `ExecRunner` sets `cmd.Env = append(os.Environ(), env...)`.
- `FakeRunner` records the env passed, for argv+env assertions.
- `LimitRunner` forwards env unchanged.

### Slice 2 — gg-as-editor protocol

- The plan is serialized to a temp file (JSON): an ordered list of entries
  `{ Sha string; Action "pick"|"reword"|"squash"|"drop"; Message string }`
  (Message only set for reword). Order is git's todo order (**oldest-first**).
- `GIT_SEQUENCE_EDITOR = "<gg-bin> __rebase-seq <planpath>"`. git appends the
  todo-file path; `gg __rebase-seq` rewrites the todo to match the plan:
  - **pick** → `pick <sha>`
  - **reword** → `pick <sha>` followed by
    `exec <gg-bin> __rebase-message <planpath> <index>`
  - **squash group** (a target commit + the commits melded into it) →
    `pick <target>`, then `fixup <each squashed>` (applies their *changes*,
    discards their individual messages), then one
    `exec <gg-bin> __rebase-message <planpath> <groupindex>` that applies the
    composed combined message
  - **drop** → line omitted
  - reorder → todo line order follows the plan order
- `gg __rebase-message <planpath> <index>` runs as a rebase `exec` step with the
  just-applied (and, for a squash group, already-melded) commit at HEAD; it
  amends HEAD's message via `git commit --amend -F <tempfile>` (message written
  from the plan entry). **Only `GIT_SEQUENCE_EDITOR` is needed — no
  `GIT_EDITOR`**, sidestepping the "which commit is git asking about" problem.
  The one mechanism powers both Reword (user-typed message) and Squash
  (gg-composed message).
- Both subcommands (`__rebase-seq`, `__rebase-message`) are routed in
  `cmd/gg/main.go` alongside the existing hidden routes; they are not
  user-facing.
- **Squash combined message:** because gg holds every commit's message in the
  plan, it composes the group's message deterministically — the **target
  (first) commit's subject as the title**, then **each squashed commit's message
  on its own line in the body** — and stores it as the group's plan message. No
  message editor is involved.

### Slice 3 — engine `InteractiveRebase` op

`engine.InteractiveRebase{ Branch, Onto string; Plan []PlanEntry }`. `Run`:

1. **Guards:**
   - Refuse if `Onto..Branch` contains **merge commits**
     (`git rev-list --merges <onto>..<branch>` non-empty) → error
     "interactive rebase of merge commits isn't supported yet" (v1 linear only).
   - If `Branch` has an upstream and the range is already pushed, emit a
     **proceed/abort** `DecisionNeeded` (pushed-history rewrite warning).
2. **Stash-wrap:** read status; if the tree is dirty, record the staged paths
   and `git stash push -u` (git requires a clean tree to rebase).
3. **Drive:** write the plan file; run `git rebase -i <onto>` with
   `GIT_SEQUENCE_EDITOR` set (slice 1), worktree-aware like `SmartRebase`.
4. **Success:** if stashed, `git stash pop` then re-`git add` the
   previously-staged paths (restore the staged/unstaged split). `Result.Summary`
   = e.g. `rebased <branch> (<n> commits)`.
5. **Conflict (rebase pauses):** emit a **keep/abort** `DecisionNeeded`,
   mirroring `SmartRebase`:
   - **keep** — leave the rebase paused for manual `git rebase --continue`,
     return non-zero. If we stashed, the stash **stays** in the list (cannot pop
     onto a conflicted tree); the summary reports
     "changes stashed as stash@{0}; pop after finishing the rebase."
   - **abort** — `git rebase --abort`; if we stashed, pop + re-stage. Tree fully
     restored to the pre-rebase state.

### Slice 4 — TUI editor view + CLI entry

**Editor view** (view-stack surface; owns the keyboard, footer shows its own
keys, like the diff/history views):

- Rows displayed **newest-first** (matching the Commits panel and the
  screenshot; top = newest). Each row: action label + short hash + subject.
  gg reverses to git's oldest-first order when serializing the plan.
- Row model: `{ sha, shortHash, subject, action, newMessage }`; order mutable.
- **Action keys** (focused row): `p` Pick, `r` Reword, `s` Squash, `d` Drop.
  - `r` opens the **F2 commit popup** pre-filled with the message; on confirm it
    stores `newMessage` and sets the row to Reword.
  - `s` (Squash) is **refused on the oldest row** (nothing older to meld into) —
    statusMsg, no change.
- **Reorder:** `ctrl+↑` / `ctrl+↓` Move Up / Move Down (the focused row).
- **Navigate:** `↑`/`↓` or `j`/`k`.
- **Commit/bail:** `enter` = Start Rebase (build the plan, call the op via the
  standard `startOp` path), `esc` = Cancel (close, nothing runs), `R` = Reset
  (every row back to Pick, original order).
- Footer legend mirrors the screenshot:
  `p pick · s squash · r reword · d drop · ctrl+↑/↓ move · enter start · R reset · esc cancel`.
  A help-window section is added (footer/help drift guard, `TestHelpFooterCoverage`).
- On Start, the editor builds `[]engine.PlanEntry` (display order reversed to
  oldest-first) and runs `engine.InteractiveRebase{Branch, Onto, Plan}` through
  the standard `startOp → opFinishedMsg → loadCmd()` full reload (history and
  current branch both change).

**CLI entry** (scriptability + e2e): `gg rebase -i <base> --plan <file>` consumes
the same serialized plan format the TUI builds, builds the same
`engine.InteractiveRebase`, and runs it via the CLI's `cliDecider` (so the
pushed-history and conflict decisions are answered by `--on-conflict=keep|abort`
and a pushed-history flag). The TUI editor and CLI share one plan format and one
engine op.

## Refresh

Standard full reload after the op (history + current branch change), via the
existing `startOp → opFinishedMsg → loadCmd()` path.

## Out of scope (later)

- **Editing the composed squash message** before running — v1 auto-composes
  (target subject + squashed messages line-by-line); tweaking it is a later
  Reword on the squashed result.
- **Merge commits** in the range — refused in v1 (linear only;
  `--rebase-merges` is a later concern).
- **Edit / break / exec** todo actions beyond pick/reword/squash/drop.
- **Mid-conflict resolution UI** — v1 hands off to manual
  `git rebase --continue` (or abort); a conflict editor is separate roadmap work
  (F4).
- Avatars / commit graph in the editor (TUI shows hash + subject).

## Tests

- **`internal/gitexec`:** `FakeRunner` records env; an `ExecRunner` real test
  asserts a per-command env var reaches the subprocess (e.g. `git var
  GIT_EDITOR` reflects a set value).
- **gg-as-editor (slice 2):** given a plan + a sample git todo file,
  `__rebase-seq` produces the expected rewritten todo (pick/fixup/exec lines,
  drop omission, reorder); `__rebase-message` amends HEAD's message from the plan
  (real repo).
- **`internal/engine`:** `FakeRunner` argv+env for the rebase invocation;
  real-repo integration — a 3-commit branch with a plan that rewords one, drops
  one, and reorders → assert resulting subjects/order, commit count, base
  unchanged; stash-wrap (dirty tree restored, index split preserved); conflict
  keep/abort; merge-commit guard; pushed-history decision.
- **`internal/tui`:** pair-op picker shows "Interactive rebase…" and opens the
  editor; action keys set the row action; reorder; Reset; plan-build translates
  newest-first display → oldest-first plan; Reword opens the F2 popup and stores
  the message; squash-on-oldest refused; `enter` starts / `esc` cancels;
  footer/help drift guard.
- **e2e:** a `scenarios/*.toml` driving `gg rebase -i <base> --plan <file>` with
  a reword + drop + reorder, asserting the resulting history (subjects, order,
  count) and that the base is unchanged.

## Files touched (by slice)

| Slice | File | Change |
|---|---|---|
| 1 | `internal/gitexec/runner.go`, `exec.go`, `fake.go`, `limit.go` (+tests) | per-command env on `Run`/`Stream` |
| 2 | `internal/rebaseplan/` (new: plan type + serialize + todo rewrite + squash-message compose), `cmd/gg/main.go` (hidden routes) (+tests) | plan format + `__rebase-seq` / `__rebase-message` |
| 3 | `internal/git/rebase.go` (interactive verb + `RebaseContinue`/range helpers), `internal/engine/interactive_rebase.go`, `internal/engine/gitops.go` (+tests) | the op + verbs |
| 4 | `internal/tui/irebase_view.go` (+test), `internal/tui/mark.go` (3rd pair-op), `internal/tui/model.go`/`view.go`/`footer.go`/`help.go`, `internal/cli/cli.go` (`rebase -i --plan`), `e2e/scenarios/interactive-rebase.toml`, `internal/agentskill/using-gg.md` + `agentskill.go` (bump), `CHANGELOG.md`, `README.md` | editor + CLI + docs |

## Open decisions (settled at brainstorm)

1. Entry via the existing mark-two-branches pair-op picker (3rd option),
   range `selected..marked`. ✓
2. Editor display newest-first; `p/s/r/d` actions; `ctrl+↑/↓` reorder;
   `enter`/`esc`/`R`. ✓
3. Execution via `GIT_SEQUENCE_EDITOR`-only + `exec` message lines (one
   mechanism for Reword and Squash). Squash composes a combined message:
   target subject as title, each squashed commit's message line-by-line in the
   body. ✓
4. Auto stash-wrap; paused-conflict leaves the stash for manual pop. ✓
5. Scriptable CLI entry + e2e scenario included. ✓
