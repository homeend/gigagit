# Web AI conflict lane — design

Date: 2026-08-01 · Branch: `feat/web-ai-conflict-lane` (off `web-dev`) ·
Status: approved design

## Goal

From the web UI's paused-operation banner, run a headless (capture-mode)
`conflict_complete` agent: it resolves every conflict, stages, runs the
matching `--continue`, and reports an overview — the deferred item from the
2026-07-31 web conflict surface spec, riding the review-run transport.

Two user decisions shape it:

1. **The TUI experience stays terminal-first.** The existing
   `mode = "terminal"` resolve-&-complete rows are untouched — watching and
   steering the agent in a real terminal is the preferred TUI flow. The web
   gets its own headless rows, hidden from the TUI picker via a new
   `frontends` tag, so the TUI menu looks exactly like today.
2. **Headless variants for every agent that can honestly do it**: Claude,
   Codex, Antigravity (each has a verified headless invocation + permission
   bypass flag). Junie gets **no** web row — headless Junie has no bypass
   flag (`--brave` is interactive-only), so it cannot honestly attempt the
   task. Kimi's existing capture row is the one row both frontends share.

Out of scope (deliberately): plain `conflict` (resolve-only) commands in the
web (the picker + Continue already cover manual flow; can ride the same lane
later), per-file mergetool commands (need a local display), `<user:…>` token
fill in the web (rows with user tokens are simply not listed), overview
persistence (display-only, TUI parity), and any TUI behavior change beyond
the tag filter.

## 1. `frontends` tag on tool commands (`internal/config`, `internal/exttool`)

`ToolCommand` gains one optional field:

```toml
[[tools.command]]
category  = "conflict_complete"
name      = "Claude — resolve & complete (yolo, headless)"
mode      = "capture"
frontends = ["web"]            # NEW: where this row is offered
command   = '…'
```

- `Frontends []string` `toml:"frontends"`. Allowed values: `"tui"`, `"web"`,
  `"cli"`. **Empty/absent = visible everywhere** — every existing user
  config keeps behaving exactly as today.
- `ValidateToolCommand` rejects an unknown value (the block goes inert at
  load, the standing rule). `Key()` (overlay collision) ignores the field.
- `AppendToolCommands` writes the field when non-empty (single-line TOML
  array; no new delimiter hazards — values are fixed words).
- A shared helper answers visibility:
  `config.ToolVisibleIn(tc ToolCommand, frontend string) bool` — true when
  `Frontends` is empty or contains `frontend`.
- Filter application:
  - TUI `Model.toolCommands(category)` (`internal/tui/tools.go`, the single
    funnel for `t`/`ctrl+g`/review pickers) additionally requires
    `ToolVisibleIn(tc, "tui")`.
  - Web listing requires `ToolVisibleIn(tc, "web")` (and `mode == "capture"`
    — a browser has no terminal to hand over; the tag expresses intent, the
    mode gate enforces physics).
  - CLI `gg review`'s candidate list requires `ToolVisibleIn(tc, "cli")`.
- `exttool.CommandTemplate` gains the same `Frontends []string`;
  `GenerateCommand`/`GenerateCommandFor` carry it into the materialized
  `ToolCommand`. The Settings wizard still LISTS all templates (config is
  shared; the web has no wizard of its own — installing a web-only row from
  the TUI wizard is the intended path); the tag only affects run-time
  pickers.

## 2. Catalog rows (`internal/exttool`)

`conflict_complete` rows after this change:

| Agent | terminal row (existing) | headless row |
|---|---|---|
| Claude | stays, `Frontends: ["tui"]` | NEW `["web"]`: `<bin> -p <claudeCompletePrompt> --dangerously-skip-permissions`, ModeCapture, OptIn |
| Codex | stays, `["tui"]` | NEW `["web"]`: `<bin> exec <codexCompletePrompt> --dangerously-bypass-approvals-and-sandbox --output-last-message "<env:GG_MESSAGE_FILE>"`, ModeCapture, OptIn |
| Antigravity | stays, `["tui"]` | NEW `["web"]`: `<bin> -p <agyCompletePrompt> --dangerously-skip-permissions`, ModeCapture, OptIn |
| Junie | stays, `["tui"]` | **none** (no headless bypass flag) |
| Kimi | — (none exists) | existing row gains `Frontends: ["tui", "web"]` |

- Names: `"<Agent> — resolve & complete (yolo, headless)"`. Prompts reuse
  the existing `*CompletePrompt` constants verbatim (same contract: resolve,
  `git add`, matching `--continue` with `GIT_EDITOR=true`, repeat, never
  abort/push, overview to `GG_MESSAGE_FILE`).
- OptIn invariant holds: OptIn ⇔ the command carries a permission-bypass
  flag. The three new rows are OptIn; Kimi stays non-OptIn (`-p` has no
  bypass flag to carry). `defaultToolChecked` already shows every
  `conflict_complete` row unchecked in the wizard — no change needed.
- **Real-binary verification before merge** (adding-external-tools skill):
  - `claude --help`: `-p` and `--dangerously-skip-permissions` exist
    together (both already used in catalog, but re-verify the combination
    at the current installed version).
  - `codex exec --help`: `--dangerously-bypass-approvals-and-sandbox` is
    accepted by the `exec` subcommand (it is documented on the interactive
    CLI; the exec form must be confirmed) alongside `--output-last-message`.
  - `agy --help`: `-p` + `--dangerously-skip-permissions` (probe-verified
    2026-07-20 for the commit lane; re-confirm at current version).
  - Junie: one live probe — can `junie --task` headless edit a repo file?
    If yes, a Junie web row may be added with the same conditional comment
    discipline as Kimi's; expected answer is no, and then no row ships.
  - The end-to-end behavioral check for the lane itself is the browser
    check (§7), run with a scripted fake agent; one additional live run
    with real Claude against a fixture conflict validates a true agent
    end-to-end.

## 3. Engine op: `CompleteConflict` (`internal/engine`)

A sibling of `ReviewChanges` — the third capture-lane op:

```go
type CompleteConflict struct {
    Command         string   // the command TEMPLATE text (config), resolved by the op
    Dir             string   // worktree root (repo dir for the run)
    Env             []string // caller extras (may be nil)
    Op              string   // paused op: merge|rebase|cherry-pick|revert
    Source, Target  string   // the op's parties (display/context values)
    ConflictedFiles []string // repo-relative conflicted paths
}
```

Behavior (`Run`):

1. Write the **context file**: identical format to the TUI's
   `toolContextFile` — `op:`/`source:`/`target:` header lines then the
   conflicted paths one per line, every value C-quoted when it carries a
   control byte. The format + quoting move to a shared home so the two
   writers cannot drift: `template.ConflictContextDoc(op, source, target,
   files []string) string` and `template.CQuotePath(p string) string` (the
   TUI's `toolContextFile`/`cQuotePath` delegate to them; behavior
   byte-identical, existing TUI tests keep passing).
2. Create an empty **message file** (`gg-overview-*.md`).
3. Resolve the command **inside the op** via `template.ResolveCommand`
   with the full `CmdCtx{Op, Source, Target, ConflictedFiles, Repo: Dir,
   ContextFile: <real temp path>}` and nil inputs — resolution must happen
   after the context file exists so a custom `<context-file>` token gets
   the real path (catalog rows only use `${GG_*}` env references, but the
   token must work). A resolve error fails the op before anything runs.
   This adds an `engine → template` import; `template` is a pure leaf
   (`archtest` DAG addition).
4. Run via the existing `CaptureRunner` seam (`OpDeps.CaptureRunner`,
   `ShellCaptureRunner` real impl: temp script + shell, stdin `/dev/null`,
   stdout captured, stderr streamed as `GitLine`), env = the eleven `GG_*`
   vars (`GG_OP`, `GG_SOURCE`, `GG_TARGET`, `GG_CONFLICTED_FILES`
   (space-joined), `GG_REPO=Dir`, `GG_FILE`/`GG_LOCAL`/`GG_BASE`/
   `GG_REMOTE`/`GG_MERGED` empty, `GG_CONTEXT_FILE`) plus
   `GG_MESSAGE_FILE=<path>`, `GG_TASK=conflict_complete`, plus `op.Env`.
5. Read back the overview with the standing **output-channel contract**:
   non-empty `GG_MESSAGE_FILE` content wins over stdout. An empty overview
   is NOT an error (the TUI's "reported no overview" stance) —
   `Result.Captured` is simply empty. A non-zero exit from the runner IS an
   error (the run failed).
6. Remove both temp files (best-effort) before returning.

`LockMode()` is **`Read`** — deliberate and documented at the op: the AGENT
mutates the tree and runs the sequencer, not gg; gg itself only reads. Read
keeps web reads (status, commits, diffs) alive during a minutes-long run —
the parked-review rationale — while still excluding gg's own
`TreeWrite`/`RefWrite` ops for the duration. The TUI precedent for this
category is no reservation at all (`tea.ExecProcess`, `$EDITOR` standing);
Read is strictly safer. The op performs no paused-op probe of its own —
validation is the domain wrapper's job (the `ReviewChanges` split).

## 4. Domain wrapper (`internal/domain`)

```go
type CompleteConflictResult struct {
    Overview string // captured overview, may be ""
    Op       string // the op that was paused when the run started
}

func (s *Service) CompleteConflictReport(ctx context.Context,
    commandTemplate string, env []string) (CompleteConflictResult, error)
```

- Reads `Status` + `Conflict(ctx, st)`; `cs.Op == ""` → error
  `"no paused operation"` (the run is meaningless; the web start handler
  surfaces this as 409).
- Builds `ConflictedFiles` from the status' unmerged paths, `Dir` from
  `TopLevel`, and dispatches `engine.CompleteConflict` through `Execute`
  (which injects the CaptureRunner and the repo gate).
- Returns the trimmed `Result.Captured` + the op name. No persistence —
  the overview is display-only in both frontends today.

## 5. Web server (`internal/web`, new `conflictai.go`)

Mirrors `review.go` — same two load-bearing properties: **the command text
never comes off the wire** (the client names a tool; the server looks it up
in the effective config and resolves it), and **nothing runs unapproved**
(shared `promptstate` store, keyed by `CommandHash(template text)` under the
git common dir — approvals interchangeable with the TUI).

- `GET /api/conflict/tools` — 409 when nothing is paused. Otherwise lists
  every effective-config command with `category == "conflict_complete"`,
  `mode == "capture"`, `ToolVisibleIn(tc, "web")`, structurally valid
  (`ValidateToolCommand` + `ValidateCommandTokens`), and `when_op` empty or
  equal to the paused op. Each row: `{name, command, approved}` with
  `command` resolved for display using the real `Op`/`Source`/`Target`/
  `ConflictedFiles`/`Repo` and the literal placeholder `$GG_CONTEXT_FILE`
  for `<context-file>` (the real path exists only at run time; catalog rows
  are unaffected — they reference env vars). A row whose template does not
  resolve (e.g. `<user:…>` tokens with no fill UI) is omitted — the
  unresolvable-template-is-inert precedent. Response also carries the
  banner facts: `{op, source, target, conflicted}`.
- `POST /api/conflict/complete {tool, approve}` (writeGuard) — re-reads
  conflict state server-side (409 when nothing is paused), picks the
  command by name from the same filtered set (400 unknown), then the
  approval gate verbatim from `handleReviewStart`: unapproved + no
  `approve` → 403 `{needs_approval, tool, command}`; `approve:true` records
  the approval (best-effort). Starts via the generalized run lane:
  `startRun("conflict_complete", fn)` where fn calls
  `svc.CompleteConflictReport(ctx, tc.Command, []string{})` and returns
  `extra = {overview, op, tool}` merged into the terminal `done` event.
  202 `{op_id, tool}`. Single-lane rules unchanged — a running op 409s.
- `POST /api/op/{id}/cancel` — the review-only restriction widens to agent
  runs: allowed kinds `{"review", "conflict_complete"}` (both can hang for
  minutes holding the lane; interrupting a GIT op stays a separate
  question). Cancellation detection stays the `run.cancelled` flag, never
  error text.

## 6. Client (`static/index.html`, `app.js`, `style.css`)

- **Entry:** an `AI resolve…` button in `#conflict-bar`, beside
  Continue/abort, visible whenever the banner is (no config probe per
  render — the overlay shows the empty state, the branch-versions rule).
  Guarded by `opBusy()` like every op entry point (reports, never silent).
- **Overlay:** reuse the `#review` overlay machinery in a conflict mode —
  chooser (rows from `GET /api/conflict/tools`; empty →
  "no headless conflict agent configured (mode = \"capture\", frontends
  [\"web\"])" and a dismiss) → approval step (shows the resolved command,
  re-POSTs with `approve:true`) → progress. It is one decision in steps,
  not three surfaces — the review lane's shape.
- **Park/cancel:** identical to review — esc/backdrop PARK the live run to
  the top-bar task chip (destroying minutes of agent work on the reflex key
  is a trap); Cancel stays the labelled button; the parked-run status line
  is a click-handle. The review park state generalizes minimally (the chip
  and `state.task` learn a kind; `followOp` already takes one).
- **Done:** `onDone` refreshes status first — that is the truth about
  whether the agent finished: banner clears when the op completed; if the
  agent stopped early the banner (and the block picker path) remain, and
  the status line says the agent left the operation paused. A non-empty
  overview opens in the `#report` viewer titled
  `Resolution overview — <tool>` (plain text, the TUI's framing); an empty
  one reports `<tool> reported no overview` on the status line. A failed
  run surfaces the error and refreshes status (the tree may be
  half-mutated; reality wins).
- **DANGER_OPTIONS / red styling:** not applicable — no engine decision is
  involved; the approval step is the consent surface.

## 7. Testing

- `internal/config`: `frontends` round-trip (parse + `AppendToolCommands`
  write-back), unknown value → `ValidateToolCommand` error, `ToolVisibleIn`
  truth table (empty = everywhere), overlay collision unaffected.
- `internal/exttool`: catalog invariants extended — every `terminal`
  `conflict_complete` row tagged `["tui"]`; every `capture` one visible in
  web; new rows OptIn ⇔ bypass-flag (Kimi documented exception); all new
  templates pass `ValidateCommandTokens` (the existing all-builtins loop
  covers this automatically); Kimi row tagged `["tui","web"]`.
- `internal/engine`: `CompleteConflict` against a fake CaptureRunner —
  env set (all `GG_*` + `GG_TASK`), context-file bytes exact (incl. a
  C-quoted control-byte path), file-wins-over-stdout, both-empty → ok with
  empty Captured, non-zero exit → error, temp files removed, resolve error
  → nothing runs, `LockMode() == Read`.
- `internal/template`: `ConflictContextDoc`/`CQuotePath` unit tests; TUI's
  delegating helpers keep their existing tests green.
- `internal/tui`: `toolCommands` drops a `frontends=["web"]` row; keeps
  untagged and `["tui",…]` rows.
- `internal/web` (real git, temp repo with a paused merge): tools listing
  (409 no-pause; terminal row hidden; web row shown; `when_op` mismatch
  hidden; `<user:…>` row omitted); approval 403 → approve → 202; a
  scripted fake agent (a shell command that resolves, `git add`s, runs
  `git merge --continue` with `GIT_EDITOR=true`, writes `GG_MESSAGE_FILE`)
  completes the merge — done carries the overview and a follow-up
  `/api/status` shows no conflict; a stop-early script leaves it paused;
  cancel is accepted for the new kind and still refused for git ops.
- Browser check (headless CDP, per the standing rules — old build first,
  twice; visibility via `elementFromPoint`): full flow with the scripted
  fake agent — banner button → chooser → approval box shows the command →
  run → banner clears → report opens. Plus one live real-Claude run against
  a fixture conflict as the behavioral end-to-end.

## 8. Docs

`CHANGELOG.md` (one bullet); `CLAUDE.md` package-map rows for `engine`
(new op), `exttool` (headless rows + `Frontends`), `config` (`frontends`
field), `template` (shared context-doc helpers), `web` (the lane);
`README.md` web section bullet. `using-gg` is untouched — the CLI surface
and the agentic-task contracts are unchanged (the new rows reuse the
existing `conflict_complete` prompt verbatim); if implementation reveals
the skill documents per-frontend visibility, bump then, not now.
