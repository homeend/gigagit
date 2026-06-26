# Session error log — design

**Date:** 2026-06-27
**Status:** Approved (ready for plan)

## Goal

Record every git operation that does **not** result in success to a durable,
always-on global log file in the user's gg state dir, and add a **Settings menu
option** to view the current session's errors from inside the TUI.

User intent (verbatim): *"Record in memory all git execs that have not resulted
in success and add in settings menu Option to View all errors during current
session"* → refined to *"save all errors to global file, like in user dir"*.

## Acceptance criteria

1. A failed `git pull` / `push` / `merge` / `rebase` / `commit`, or a failed
   read query (status / snapshot / show-file), appears in the error log.
2. A control-flow probe that exits non-zero by design — `git merge-base
   --is-ancestor` (exit 1 = "no"), `git diff --quiet` (exit 1 = "changed") — is
   **not** recorded. These are not failures.
3. A user-initiated cancellation (`context.Canceled` / `DeadlineExceeded`, e.g.
   quitting mid-op) is **not** recorded.
4. Every recorded failure is appended to a durable global file
   (`errors.log`) in the gg state dir, surviving across sessions and repos.
5. A new **Settings** (`,`) menu row opens a read-only, scrollable list of the
   **current session's** failures and shows where the full history file lives.

## Why the domain boundary is the capture point

`gitexec.ExecRunner.record()` records a span for **every** git invocation,
including tolerated non-zero probes — so filtering spans on `ExitCode != 0`
would capture the harmless probes (violates AC #2).

The runner returns a non-nil error for *any* non-zero exit (`runFailure`), and
"tolerant" verbs (`IsAncestor`, `CommitExists`, `merge`, `config`,
`symbolic-ref`, …) swallow it by checking `res.ExitCode == 1` and returning a
normal result. So "genuine failure" is **only** unambiguous one level up, at the
**domain boundary**, where an error is actually returned to the frontend:

- `Service.Execute(...)` returns the op's `opErr` (service.go:181). Verified:
  `SmartPull.Run` returns `Result{}, fmt.Errorf("smart pull: %s: %w", …)` on a
  failed pull (smart_pull.go:213). `engine.Result` has no status/error field
  (only `Summary`/`Changed`/`Path`), so op failure can travel *only* through the
  returned error.
- Read queries (`Status`, `Snapshot`, `Worktrees`, `ShowFile`, `CommitFiles`, …)
  return their error directly.

The failure's error string already embeds the failed git command and stderr
(the runner formats `git pull failed (exit 1): <stderr>`), so capturing at this
altitude still records exactly the failed git exec, labeled by the operation
that issued it. No runner/span changes are needed.

## Architecture

```
domain.Execute (opErr != nil)   ─┐
domain.Status/Snapshot/… (err)  ─┼─► observ.NoteFailure(source, err)
                                 │        │
                                 │        ├─► process-global ring (session)  ──► observ.SessionFailures()  ──► TUI viewer
                                 │        └─► failure file sink (if set)      ──► errors.log (durable)
cmd/gg TUI startup: open errors.log (append), observ.SetFailureSink(f)
```

### `internal/observ` — new failure seam (mirrors the span-sink seam)

- `type FailureEntry struct { Time time.Time; Source, Detail string }`
- `func NoteFailure(source string, err error)`:
  - Returns immediately if `err == nil` or `errors.Is(err, context.Canceled)` or
    `errors.Is(err, context.DeadlineExceeded)`.
  - Builds `FailureEntry{Time: now, Source: source, Detail: oneLine(err.Error())}`
    (collapse newlines/whitespace so each entry is one line).
  - Appends to a bounded process-global ring (cap 500, evict oldest), under a
    package mutex.
  - If a failure file writer is registered, writes one line:
    `<RFC3339 time>\t<source>\t<detail>\n`.
- `func SetFailureSink(w io.Writer)` — register/replace/clear (nil) the durable
  file writer. nil (the default) means ring-only: the CLI and library/tests get
  no file side effect unless they opt in (same seam discipline as `SetSpanSink`).
- `func SessionFailures() []FailureEntry` — newest-first copy of the ring.
- `func ResetFailures()` — clears ring + sink; for test isolation.

Rationale for process-global (not Service-owned): the durable file is global,
and the in-memory session list should survive a repo switch (the repo switcher
builds a fresh `Service`; a session is one process). This mirrors the existing
process-global `SetSpanSink` pattern and keeps the TUI from plumbing a buffer
through `Model` or repo-switch.

### `internal/domain` — wire the hook

Call `observ.NoteFailure(<source>, err)` in the error-return paths:

- `Execute`: when `opErr != nil`, `observ.NoteFailure("op "+engine.OpName(op), opErr)`
  (use the same `label` already computed for the span).
- Each public read query that returns an error: `observ.NoteFailure("<QueryName>", err)`.
  Prefer a single shared internal helper if the queries already funnel through
  one (to be confirmed during planning); otherwise instrument each query's error
  return. Keep `source` names short and stable (`Status`, `Snapshot`,
  `Worktrees`, `ShowFile`, `CommitFiles`, `CommitFeed`).

No deduplication: one returned error = one entry. (A failed `Snapshot` that fans
out several reads surfaces one error → one entry, which is the desired
granularity.)

### `internal/tui` — durable file + Settings viewer

**Durable file (always-on):**

- Resolve `errors.log` beside `operations.log`:
  `filepath.Join(filepath.Dir(repos.DefaultStatePath()), "errors.log")`
  (reuse / factor the `defaultOpLogPath` resolution).
- At TUI startup, `MkdirAll` the dir, `OpenFile(..., O_CREATE|O_WRONLY|O_APPEND)`,
  and `observ.SetFailureSink(f)`. Always on — no toggle, no config. Close the
  handle (and `SetFailureSink(nil)`) on TUI exit.
- Wiring lives next to the existing `opLog` lifecycle (a sibling always-on
  handle on `Model`, or opened in `cmd/gg` for the TUI path). Planning picks the
  exact home; it must not interfere with the opt-in `operations.log` toggle.

**Settings menu row:**

- Add `settingsMenuErrors = "Session errors"` to `settingsMenu`.
- Dynamic label (like the op-log row): `Session errors: N — <errors.log path>`
  where `N = len(observ.SessionFailures())`; show `none` when `N == 0`.
- `enter` opens a read-only viewer:
  - A new popup screen (extend the existing `settingsPopup` with a third screen,
    or a dedicated small popup) listing `observ.SessionFailures()` newest-first.
  - Each row: `HH:MM:SS  <source> — <detail>`.
  - Reuse `renderWindow` + `dispMode` (the `z` wrap/scroll cycle already in the
    settings popup) so long details don't break layout.
  - Footer: `[z] mode  [esc] back`. Show the `errors.log` path so the user can
    find the full cross-session history.
  - Empty state: `no errors this session`.

## Testing

**`internal/observ`** (`NoteFailure` / sink / ring):
- `NoteFailure` with a real error + a registered `bytes.Buffer` sink → one line
  written, one `FailureEntry` in `SessionFailures()`.
- `nil` error, `context.Canceled`, `context.DeadlineExceeded` → no entry, no write.
- Ring eviction at cap (oldest dropped, newest-first order preserved).
- `SetFailureSink(nil)` → ring still collects, no panic, no write.

**`internal/domain`** (capture correctness — the acceptance test):
- Fake runner that fails `pull` → `Execute` returns error → entry recorded with
  source `op SmartPull` (or actual `OpName`).
- `IsAncestor` against a fake runner returning exit 1 → verb returns `(false,
  nil)` → **no** entry (AC #2).
- A query (`Status`) whose runner fails → entry recorded.
- Reset failures between tests via `observ.ResetFailures()`.

**`internal/tui`** (surface):
- Settings menu label reflects the session count.
- Opening the viewer renders the entries; `esc` returns to the menu.
- Empty state renders `no errors this session`.

## Docs to update on completion

- `CHANGELOG.md` (always).
- `README.md` if the Settings surface is user-documented there.
- `CLAUDE.md` package map: note `observ` gained the failure seam (`NoteFailure`
  / `SetFailureSink` / `SessionFailures`) and that `domain` feeds it.
- CLI surface unchanged → no `using-gg.md` / `agentskill.Version` bump.

## Out of scope (YAGNI)

- Toggling the durable file off, or a config knob for it (it is always-on).
- Log rotation / size capping of `errors.log`.
- A CLI subcommand to view errors (`gg` errors live on stderr already).
- Per-error copy / jump-to-op actions in the viewer.
- Recording internally-tolerated or recovered failures (only errors that reach
  a frontend count).
- Redacting the detail string beyond newline-collapsing (error strings carry
  stderr/command names, not argv; argv redaction already happened at the span
  layer and does not apply here).
