# AI tasks in agent sessions, worktree terminals, instance discovery

Status: agreed in brainstorming 2026-09-24. TUI first; the web keeps its
current headless AI lanes (web attach is its own later spec).

Builds on: agent sessions (`2026-09-24-agent-sessions-design.md`, merged) and
open files (`2026-09-24-open-files-design.md`, merged — it owns the current
shape of the `ctrl+\` popup).

## Goals

1. **Every AI task can run as a visible agent** — commit message, review,
   conflict resolve, resolve & complete, and any later kind (e.g. name
   generation). A launch dialog picks the agent and the mode: interactive in
   the background, interactive in the foreground, or headless (queued, with a
   status list and a result history).
2. **Open terminal** for a worktree: a plain shell in an embedded console.
3. **Instance discovery:** an agent or shell started by gg reaches THAT gg
   with `gg session …`, whichever worktree the TUI currently shows.

## Rulings (user, 2026-09-24 — do not re-ask)

1. **Launch dialog on every AI action**, always shown, with the last-used
   agent and mode for that task kind preselected (`enter` repeats them).
   Modes: *Interactive, background* (session, no console opened) ·
   *Interactive, foreground* (session, console opens focused) · *Headless*
   (queued, no console).
2. **Interactive results:** every write of `$GG_MESSAGE_FILE` is picked up as
   the task's latest result and applied (commit box / review lane / overview)
   with a notice; the agent stays alive so the user can ask for changes; the
   user closes the session.
3. **Headless tasks** are listed in a **Headless** tab of the `ctrl+\` popup:
   queued and running first, then the history. `enter` on a finished task
   shows its result.
4. **History: the last 50 task records**, all repos, persisted on disk with
   their result texts.
5. **Parallelism:** `[tasks] max_parallel` (default **3**, clamped in code to
   **1..10**) counts AI tasks of every mode. User-started agent sessions
   (Start agent…) and terminals are NOT counted and are unlimited.
6. **Task key** `<kind> — <target>` is the task's identity and title. Tasks
   with the **same key run in sequence**; different keys run in parallel
   (within the cap). No per-worktree rule, no warning. Keys:

   | Kind | Key |
   |------|-----|
   | commit message | `commit message — <worktree> @ <HEAD sha7>` |
   | review | `review — <from sha7>..<to sha7>` |
   | conflict resolve | `resolve conflict — <worktree> <op> <sha7>` (+ ` <path>` for a per-file run) |
   | resolve & complete | `resolve & complete — <worktree> <op> <sha7>` |
   | later kinds | `<kind> — <its subject id>` |

   The two conflict kinds keep distinct keys (user confirmed).
7. **Open terminal:** Worktrees `.` → *Open terminal*, next to *Start agent…*.
   Shell: `$SHELL` (fallback `sh`) on Unix; on Windows `pwsh` → `powershell`
   → `cmd`; override `[console] shell`. Opens in the foreground. An ordinary
   session: sub-row, console, `ctrl+\`.
9. **Worktree mismatch asks, never switches silently** ("interactive but not
   intrusive"): a `gg session` command that depends on a worktree (a
   navigate to a file or diff, a highlight) arriving from a worktree other
   than the one gg shows is NOT applied to the wrong checkout. gg raises a
   notice ("Claude (b) wants to show src/x.go:40" — *Switch to b and show* /
   *Ignore*), and the agent gets an immediate exit 1: `gg is showing worktree
   <a>; asked the user to switch to <b>`. Accepting switches (the guarded
   switch path) and then replays the navigate. Worktree-independent commands
   (status, focus, reload, a commit navigate within the same repo) apply as
   usual.
8. **Popup layout** stays as open files built it (sessions group, then open
   files); the Headless tab is added beside it. A layout rethink is a later
   refactor, not this work.

## Architecture

### A. Instance discovery (`GG_INBOX`)

Every session and task gg starts gets `GG_INBOX=<the starting gg's steer
inbox>` (the inbox of the worktree the TUI was on when it started the
child — the one its presence file is in). The `gg session …` verbs use
`$GG_INBOX` before computing the inbox from the current directory; a
`GG_INBOX` whose presence is not live falls back to the cwd inbox, with the
usual "no live gg" error when neither is live. Every command also carries
`Worktree` (the CLI's own checkout, or a link's checkout) so the TUI can
apply ruling 9. Today the TUI keeps presence
only in its current worktree's inbox, so after a `reRoot` a child's
`GG_INBOX` would go stale: the TUI therefore keeps presence (and consumes
commands) in every inbox it handed to a still-running child — a small set —
and drops each inbox when its last child ends. Commands arriving there act on
the TUI as it is (the current worktree), exactly like a command in the
current inbox.

### B. Engine: prepare / collect split

`GenerateMessage`, `ReviewChanges` and `CompleteConflict` each do three
things in one `Run`: prepare inputs (temp diff/context files, env with
`GG_MESSAGE_FILE`), run the capture command, collect the result (file wins
over stdout, CRLF → LF). Split each into:

- `Prepare(ctx, deps) (TaskInputs, error)` — `TaskInputs{Env []string,
  MessageFile string, Cleanup func()}`;
- `Collect(inputs, stdout) (Result, error)`.

`Run` becomes prepare → capture → collect, so the web and the CLI keep
working unchanged. The interactive path is prepare → session → collect on
each write.

### C. `taskhist` (new package, DAG leaf)

Machine-local store of the last 50 task records under XDG state, TOML index +
the shared `filelock` (the `linkhist` pattern), one result file per record
next to the index. Record: `ID, Key, Kind, Agent, Repo, Worktree, Mode,
State, Started, Ended, ExitCode, ResultFile, OutputTail (≤64 KiB)`. Adding
the 51st record prunes the oldest record AND its result file. An unreadable
or locked store degrades to in-memory records with one notice.

### D. `domain.Tasks()` (process-global, like `Sessions()`)

- `Submit(TaskSpec) TaskID` — spec: key, kind, agent, mode, repo, worktree,
  the prepared command, and the kind's collect/apply hooks.
- Scheduler: FIFO per key (same key in sequence), global cap
  `clamp(max_parallel, 1, 10)` over running tasks of every mode.
- **Headless** run: `domain.Execute` of the kind's op (prepare → capture →
  collect) when its slot opens; record in `taskhist`.
- **Interactive** run: prepare, then `StartSession` with the task's
  interactive command and env; a watcher on `MessageFile` (fsnotify + a
  polling fallback — `/mnt` drvfs misses inotify) turns each non-empty
  write into a `ResultReady` event carrying the collected result. The task
  holds its slot until the session ends.
- Events: a coalesced `Changed()` channel (the `Sessions` pattern) for
  frontends.
- `Cancel(id)`: queued → removed; running headless → ctx cancel; running
  interactive → session kill.
- `KillAll` on quit (the quit guard counts running + queued tasks).

States: `queued → running → result-ready* → done | failed | cancelled`
(result-ready is interactive-only and repeatable).

| End | Headless | Interactive |
|-----|----------|-------------|
| done | exit 0 and a non-empty result | session ended after ≥1 result |
| failed | non-zero exit or empty result (output tail kept) | session ended with no result |
| cancelled | removed, killed, or gg quit | killed before any result |

### E. `exttool`: interactive templates

A task category may carry, per agent, a **capture** command (headless, as
today) and an **interactive** command (new `mode = "interactive"`, allowed
for `commit_message`, `review`, `conflict`, `conflict_complete`). An existing
`terminal`-mode command is interactive already: it runs in a session instead
of the terminal handover (the handover is retired for AI tasks; editors keep
it). Interactive prompts carry the same `$GG_MESSAGE_FILE` write contract as
the capture prompts, plus "then wait for further instructions". Templates
ship only for agents verified to start interactively with a prompt (the plan
probes each: claude, codex, junie, agy, kimi); the rest offer Headless only.
`gg`'s first-run detection writes the new rows with the others.

### F. TUI

- **Launch dialog** (`task_launch_popup.go`): title = the task key; agent
  selector (←/→) over the agents configured for the kind; mode radio;
  modes with no command for the chosen agent are greyed; a wait line when
  the task will queue ("1 task with this key is running — this one will
  wait" / "3 tasks running — it will start when one finishes"). Last choice
  per kind is remembered in `promptstate`.
- Routed through it: the commit box's generate message, review, the conflict
  window's resolve (per-file and whole-op) and resolve & complete. The
  existing command-approval step (hash approval) runs before the dialog's run.
- **Worktree sub-rows:** interactive tasks are sessions (● `Claude · commit
  message  running 12s`); headless tasks are ◆ rows (`◆ review 1a2b..9f8e
  queued`). `enter` on ◆ opens the Headless tab on it; its `.` menu has
  Cancel and Show result.
- **`ctrl+\` Headless tab** (`tab` switches): rows `key · agent · state ·
  age`. `enter`: finished → a read-only result viewer (`a` apply, `y` copy);
  running interactive → its console. `k` twice cancels; `x` removes a
  finished record.
- **Result application:** commit message → the commit box (the existing
  ask-before-replace when it has text); review → the review lane; conflict
  kinds → the overview window and a status refresh; per-file conflict → the
  existing mark-resolved offer on session exit. A headless result that lands
  while the user is elsewhere raises a notice (`commit message ready —
  ctrl+\`) instead of pushing; one started from the still-open commit box
  fills it directly.
- **Open terminal** row in the Worktrees `.` menu.
- Footer/help: help rows for the dialog and the Headless tab
  (`TestHelpFooterCoverage`); every string through `i18n.T`, four bundles.

## Error handling

| Case | Result |
|------|--------|
| agent binary missing | dialog marks it "not found", run blocked for it |
| headless non-zero exit / empty result | failed; last 64 KiB of output kept, shown on enter |
| interactive never writes a result | runs on; ends as failed / cancelled |
| repeated result writes | each is the latest result + notice; history keeps the last |
| history store locked / unreadable | tasks run; one notice; records in memory |
| `max_parallel` ≤ 0 or > 10 | clamped to 1..10, config warning |
| quit with tasks queued / running | quit popup counts them; `Q` kills, records cancelled |
| commit box already has text | existing ask-before-replace |
| `GG_INBOX` presence not live | falls back to the cwd inbox |

## Plans (in order)

1. **Terminal + `GG_INBOX`** — shell choice, the menu row, `GG_INBOX` in
   every session env, `gg session` preferring it, presence per given inbox.
2. **Task core** — engine prepare/collect split, `taskhist`,
   `domain.Tasks()` (scheduler, headless run, interactive result watcher),
   exttool interactive templates + config validation. No UI.
3. **TUI** — launch dialog, Headless tab, ◆ rows, result application, the
   four AI actions routed through the dialog, quit guard count.

## Testing

- `taskhist`: cap/prune (records and result files), lock, corrupt index —
  real temp dirs.
- Scheduler: same key sequential, different keys parallel, cap + clamp,
  cancel in each state — fake capture runner + real `sh` children.
- Interactive pickup: a session script writing `$GG_MESSAGE_FILE` twice →
  two results, task still running; session exit → done / failed.
- Engine split: existing op tests stay green; prepare/collect unit tests.
- `GG_INBOX`: present in session env; `gg session` prefers it and falls back
  (CLI tests + an e2e scenario).
- TUI: dialog defaults and memory, greyed modes, wait line, Headless tab keys,
  ◆ rows, result application per kind, quit-guard count, Open terminal.
- Windows: shell choice via injected lookups; a manual check on real Windows.
- i18n gates, `TestHelpFooterCoverage`, `./test.sh race`.

## Docs

CHANGELOG, README (AI tasks, Open terminal, Headless tab), `docs/CLAUDE-details.md`
(scheduler, keys, prepare/collect, GG_INBOX), `using-gg.md` + version (GG_INBOX
discovery), CLAUDE.md package map (`taskhist` row), memory.
