# `gg discard` CLI command

**Date:** 2026-06-18
**Status:** Approved (brainstorm)
**Scope:** CLI frontend only — no engine changes (`engine.Discard` already ships).

## Problem

The TUI can discard unstaged changes (`d`/`D`), but the scriptable CLI cannot.
A `gg discard` command lets scripts/CI throw away unstaged edits and new files
through the same `engine.Discard` operation the TUI uses.

## Command surface

```
gg discard [--yes|-y] <path>...      # discard the named files
gg discard [--yes|-y] --all          # discard ALL unstaged changes
gg discard                            # error: usage (exit 2)
```

- **Bare `gg discard`** (no paths, no `--all`) → usage error, exit 2. A
  destructive, scriptable command must never mass-wipe from a bare invocation.
- **`--all` and explicit paths are mutually exclusive.** Both supplied → error,
  exit 2.
- **`--yes`/`-y` is required to proceed in BOTH the targeted and `--all` cases**
  (uniform — naming a path is not obviously consent to destroy it). Without it:
  - stdin is an interactive TTY → prompt `Discard <summary>? [y/N] ` and proceed
    only on `y`/`yes`; anything else aborts (exit 0, "aborted").
  - stdin is not a TTY (scripts/CI) → refuse: "pass --yes to confirm", exit 2.

## Behaviour

### Classification (mirrors the TUI's `discardTargets`)

For explicit paths, look each up in `svc.Status().Files`:

- `KindUntracked` → `Remove` (→ `git clean -f -d`)
- tracked, non-conflicted → `Restore` (→ `git restore --worktree`)
- `KindUnmerged` → **validate up front: error naming the file, discard nothing**
  (conflicts are resolved with `gg merge`/`rebase` continue/abort, not discard)
- not present in `status.Files` (clean file, directory, or glob) → **error
  naming it, discard nothing**. Directory/glob expansion is out of scope this
  slice; args are individual files matched against status.

All requested paths are validated before anything runs — a single bad path
fails the whole command (exit 2) with nothing discarded, so the user is never
left with a partial, surprising result.

Then run `engine.Discard{Restore, Remove}`.

### `--all`

If any conflict exists (`len(svc.Status().Conflicts()) > 0`), refuse with a
message (exit 2), mirroring the TUI's `D`. Otherwise run
`engine.Discard{All: true}` (whole-tree `git restore --worktree :/` +
`git clean -f -d`).

### Output / exit codes

- Success → `finish()` prints `✓ discarded` (the op's `Result.Summary`), exit 0.
- Usage / precondition failures (bare invocation, mutual exclusion, missing
  `--yes` non-interactive, unmatched path, conflicted path, `--all`+conflict) →
  exit **2** (consistent with the bare-`discard` and other CLI usage errors).
- Underlying op error → `finish()` prints `error: …`, exit 1.

## Architecture

The op emits **no engine `Decision`** (the confirmation was a TUI modal, not a
`DecisionRequest`), so `cmdDiscard` runs it with a plain `cliDecider{}` — there
is no fork to resolve. The confirmation lives entirely in the CLI layer.

### Files

- **Create `internal/cli/discard.go`** — `cmdDiscard(svc, args, stdin, stdout,
  stderr) int`, plus a small `confirmDiscard(prompt, stdin, stderr) bool` TTY
  y/N helper (no existing one; keep it tiny, do not generalise). Classification
  is ~6 inline lines (a shared helper is not worth the indirection).
- **Modify `internal/cli/cli.go`** — add `case "discard": return cmdDiscard(...)`
  to `Run`'s switch **and** `"discard": true` to the `var commands` map.
  **Both are required:** `cmd/gg/main.go` routes via `IsCommand` → the
  `commands` map; if `discard` is missing there, `gg discard` silently launches
  the TUI instead of running the command. The e2e scenario proves the real path.
- **Modify `internal/agentskill/using-gg.md`** — add a `gg discard` entry under
  `## Commands` (near `gg stash`). **Bump `agentskill.Version`** in
  `internal/agentskill/agentskill.go` (9 → 10) — CLAUDE.md mandates this on any
  CLI surface change (the version-marker tests enforce consistency).
- **Create `e2e/scenarios/sNN_discard_all.toml`** — real repo with a tracked
  edit + an untracked file → `gg discard --all -y` → assert a clean working tree.
- **Modify `CHANGELOG.md`, `README.md`** — document the new command.

## Testing

**`internal/cli` unit tests** (real-git via the existing CLI test harness):

- `gg discard --all -y` → clean tree, exit 0.
- `gg discard -y <tracked-edit> <untracked>` → those discarded, exit 0.
- bare `gg discard` → exit 2, nothing changed.
- `gg discard --all` non-interactive (no `-y`) → exit 2, nothing changed.
- `gg discard --all foo.go` (mutual exclusion) → exit 2.
- `gg discard -y <unmatched-path>` → exit 2, nothing changed.
- `gg discard -y <conflicted-path>` → exit 2, nothing changed.
- `gg discard --all -y` with a conflict present → exit 2, nothing changed.

**e2e scenario** — the CLI→engine→git integration test and the guard that the
`commands`-map wiring actually routes to `cmdDiscard` (not the TUI).

## Out of scope

- Directory / glob path expansion (individual files only).
- Discarding staged changes (`gg discard` is unstaged-only, like the TUI).
- Any engine changes — `engine.Discard` is reused as-is.
