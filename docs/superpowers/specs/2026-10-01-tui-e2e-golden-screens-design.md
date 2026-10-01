# TUI e2e golden screens — design

Date: 2026-10-01 · Status: draft for review · Branch: `feat/tui-e2e-golden-screens`

## 1. Why

A later feature must not silently break an earlier feature's screens. Today
the e2e harness (`e2e/`, TOML scenarios → real repo → in-process CLI runs →
state assertions) never draws the TUI, and TUI unit tests drive hand-built
models, not a repo built from a definition. Bugs of 2026-09-30 (a `◆ 1`
badge counting an AI review, a review stacked as an `@notes/` "file", a
`◆4` glyph overlap) were all visible only on screen, in a repo that mixed
several features.

**Outcome:** a scenario defines a git repo (plus the non-git state gg keeps:
notes, reviews, shelves, config), drives the real TUI with keys, and checks
the rendered screens against committed golden files.

**Who writes them:** the agent, as part of normal work:

- **A bug seen in a real repo** (e.g. test-1): first look for a scenario
  with that repo's shape; extend it or add one; reproduce the bug as a
  failing screen; then fix.
- **A new feature with a TUI surface** ships at least one TUI scenario with
  golden screens of that surface.

**Scenarios mix features the way users do.** A scenario gets its own repo
definition (no shared fixture files for now), but features that meet on one
screen are exercised together in one scenario — the trigger case is a
branch review, a commit review and line notes on the same Commits list.
Shared repo definitions are deferred until one shape is copied a third time.

## 2. Scenario format

TUI scenarios live in `e2e/scenarios/*.toml` beside the CLI ones and use
the same harness. `[input]` and `[[run]]` are unchanged; a `[tui]` block is
new. Order of execution: `[input]` builds the repo → every `[[run]]` runs
(this is how non-git state is created, through the real CLI) → the TUI
starts → `[[tui.step]]`s run in order → `[expect]` (existing, optional)
checks repo state last.

```toml
name = "reviewed commit: branch review + commit review + line notes"

[input]
steps = [
  { write = "a.txt", content = "a\n" }, { write = "b.txt", content = "b\n" },
  { commit = "add a b" },
  { branch = "feature" }, { switch = "feature" },   # `branch` only creates; `switch` checks out
  { rm = "a.txt" }, { rm = "b.txt" },
  { commit = "deleted a b" },
  # the fake review tool, per scenario (as s80_cli_review does); this
  # overwrites the harness's default .gg.toml for this scenario only
  { write = ".gg.toml", content = """
[[tools.command]]
name = "fake"
category = "review"
mode = "capture"
command = "{{ggfake}} review"
""" },
]

[[run]]
cmd = ["note", "add", "--rev", "HEAD~1", "--file", "a.txt", "--new-line", "1", "--summary", "why a?"]
[[run]]
cmd = ["review", "--tool", "fake", "HEAD"]   # the fake tool prints a canned review
[[run]]
cmd = ["review", "--tool", "fake"]           # no positional = the current branch (feature vs main)

[tui]
size = "160x40"                  # cols x rows; default 160x40

[[tui.step]]
name = "commits"                 # checkpoint → <scenario>.screens/01-commits.txt
keys = ["right"]
screen_excludes = ["◆ 1"]

[[tui.step]]
keys = ["down", "enter"]         # no name: moves only, compares nothing

[[tui.step]]
name = "stack"
keys = ["down", "down", "enter"]
wait = true                      # fire parked timers (§3.4) before drawing
screen_contains = ["a.txt"]
screen_excludes = ["@notes/"]
```

`[[tui.step]]` fields:

| field | meaning |
|---|---|
| `keys` | keyscript tokens (`enter`, `down`, `C-t`, `M-a`, a literal like `.` or `foo`), sent one at a time, each followed by a settle; a multi-rune literal is one press per rune, exactly as the recorder writes it (§3.7) |
| `name` | makes the step a **checkpoint**: its whole screen is compared with `<scenario>.screens/NN-<name>.txt` (`NN` = the checkpoint's 1-based ordinal, two digits) |
| `wait` | after the keys settle, fire every parked timer once, then settle again (§3.4) |
| `screen_contains` / `screen_excludes` | substrings the settled screen must / must not contain; allowed on any step, checked on every OS |

`name` must be unique within a scenario and match `[a-z0-9-]+`. A golden
file with no matching checkpoint fails the scenario (a renamed step never
leaves a stale golden behind).

**The fake review tool.** Declared per scenario by writing the scenario's
own `.gg.toml` as an `[input]` step (the pattern `s80_cli_review.toml`
already uses), never in the harness's shared default — a shared `fake`
would change every scenario's review-tool resolution ("exactly one
candidate", `internal/cli/review.go`). `{{ggfake}}` is a new harness
substitution: the path of a small Go helper binary (`e2e/internal/ggfake`,
built once in `TestMain`) whose `review` verb prints
`e2e/fixtures/review.md`. Capture commands always run through a shell
(`engine/capture_runner.go`: `$SHELL <file>` / `%COMSPEC% /C <file>`), so
`TestMain` pins `SHELL=/bin/sh` on Unix, and the substituted path is quoted
for `template.FlattenForCmd` on Windows.

## 3. The driver: `tui.Headless`

New file `internal/tui/headless.go`, exported for the e2e harness only.

```go
h, err := tui.NewHeadless(svc, tui.HeadlessOptions{Width: 160, Height: 40, StatePath: p})
err = h.Press("down")      // one token → one tea.KeyMsg, then settle
err = h.FireTimers()       // the `wait` step field
s  := h.Screen()           // the settled frame as plain text
h.Close()                  // teardown (§3.8); always deferred
```

### 3.1 Why not the real `tea.Program`

Bubble Tea gives no "idle" or "frame flushed" signal and repaints on its own
ticker, so reading the screen of a real program means waiting on a clock —
the failure mode seen driving test-1 under tmux on 2026-09-30. The driver
instead runs its **own message loop over the real `Model`**: gg's `Init`,
`Update` and `View` all run unchanged; only Bubble Tea's scheduler, input
parser and incremental renderer (library code) are replaced. The existing
test helper `drainDeep` (`start_at_test.go`) already flattens batches this
way and seeds the implementation.

### 3.2 Start-up

`NewHeadless` mirrors `tui.Run`'s model set-up (theme from config, branch
filters, tasks config, snapshot target) through one helper extracted from
`Run` (`run.go`'s set-up prefix is cleanly extractable), so the two cannot
drift. It then sets `m.quiet = true` and `m.statePath = opts.StatePath`, runs
`Init()`, delivers `tea.WindowSizeMsg{Width, Height}` (the real program's
order: Init first, the size when the terminal reports it), and settles.
Excluded from the shared helper, by design: `--record`, `--web`/`--at`, the
steering inbox, the operation-log side effect.

### 3.3 Quiet mode

A `quiet bool` on `Model`. It stops everything that waits on the outside
world or re-arms itself forever. Most such work starts **after** `Init()`,
so the flag is checked where each is started, not only in `Init()`:

- in `Init()`: `waitTasksCmd`, `waitSessionsCmd`, `startSteerCmd`,
  `startupWebCmd`, `refreshToolStatusesCmd` (probes installed agents on the
  host; also re-issued from `reRoot`, the notice refresh and the settings
  popups — gated at the constructor, so every caller is covered);
- at `configReadyMsg`: `startWatchCmd` → `watchListenCmd`;
  `steerListenCmd`; at `reRoot` and `onWebStarted` the same starters;
- `docWatchListenCmd` (via `openFilesTick` → `syncDocWatch`),
  `waitWebSwitchCmd`, per-console `waitSessionCmd` (agent consoles are
  never opened in a scenario);
- **inside the `heartbeatMsg` handler**: `refreshTick` (wall-clock
  `dueItems`), `prCommentsTick`, `openFilesTick`, `maybeWriteSnapshot`,
  `touchSteerPresence`, `tendKeptInboxes`, `drainSteer`,
  `expirePendingHint` — gated inside the handler, since `wait = true` does
  deliver the heartbeat.

`kickForgeProbe` stays on (finite: the harness's fake `gh`). One visible
difference from a real session follows: tool statuses are never probed, so
the tool-update notice set is empty — scenarios cannot checkpoint it.

**Pin test:** on an empty repo, `NewHeadless`'s full start-up settle (not
just `Init()`) reaches a fixed point within the §3.5 budget, and so does a
`wait` right after it. A new never-ending command fails this test, naming
the recurring message type.

### 3.4 Timers are virtual

The TUI has six `tea.Tick` sites (sticky-notice expiry, notice blink,
heartbeat, the generate spinner, the worktree-preview settle, the
reviews-follow debounce). All go through one new helper `m.tick(d, fn)`:
normally `tea.Tick(d, fn)`; in quiet mode a command that returns a
`timerMsg{due: d, fire: fn}` **immediately**. The loop **parks** timer
messages instead of updating with them.

- A plain step never fires timers: the screen shows the state right after
  the keys — a sticky notice is still visible, a debounced load has not
  happened.
- `wait = true` fires every timer parked at that moment, in `due` order, each
  followed by a settle. Timers created during this (a re-armed heartbeat or
  blink) stay parked for the next `wait`, so self-re-arming ticks never loop.

Time never passes in a test; a scenario says when "the user paused".

**Guard test** (grep-style, like the `handover()` guard): no `tea.Tick` /
`tea.Every` outside `m.tick`, and no `time.After` / `time.Sleep` in a TUI
command without an entry in an explicit allow-list stating why it is safe
headless (`repo_popup.go`'s 1 s stall is taken out of quiet mode;
`console.go`'s sleep sits in a console waiter, which never starts).

### 3.5 Settling

The loop keeps a FIFO of pending commands and runs them **one at a time**
(deterministic order) — valid only for commands that return without user
input. The one command that waits on user input is the **op waiter**
(`waitForOp`, `op.go`): a running operation's events arrive through it, and
an engine decision (`uiDecider.Decide`) blocks the op goroutine until a key
answers the modal. Run synchronously, that waiter would deadlock the loop.
So, like timers, quiet mode turns it into a descriptor: `waitForOp` returns
`opWaitMsg{ch}` immediately and the loop holds it aside.

One settle:

1. While the queue is not empty: pop a command and call it. `nil` → skip.
   `tea.BatchMsg` → enqueue its commands (flattened). `timerMsg` → park.
   `opWaitMsg` → hold as the op waiter. `sequenceMsg` (unused by the TUI
   today) → fail with "tea.Sequence is not supported headless". Any other
   message → `quitFilter`, then `m.Update(msg)`, enqueue the returned
   command.
2. Queue empty and an op waiter held: if the model is waiting for a decision
   (`m.modal` open on an unanswered `opDecisionMsg`), the op is blocked on
   the user — **settled**. Otherwise the op is working: block on the
   waiter's channel for its next message, deliver it, go to 1.
3. Queue empty, no op waiter: **settled**.

The next `Press` (answering the modal) re-enters the loop, which resumes
the held waiter.

Guards (each an error naming the cause):

- **No fixed point**: more than 500 messages, or 30 s wall time, in one
  settle → fail, listing the message types seen most often.
- **A terminal handover** (`handover()` is `tea.Exec`, so an `execMsg`) →
  fail, naming the command; a headless driver cannot run a child in a
  terminal.
- **Quit** (`tea.QuitMsg` returned or produced) → recorded, the loop ends as
  Bubble Tea's does (before `Update`); a later `Press` fails.

Bubble Tea's internal messages pass through `Update` exactly as they would
in a real program (e.g. `clearScreenMsg` does reach `Update` in v1.3.10);
their terminal side effects (alt-screen, mouse mode, cursor, title) have no
headless meaning and are otherwise ignored.

### 3.6 Drawing

After settling, `Screen()` paints `View()` into a fresh `x/vt` emulator
(`vt.NewEmulator(w, h)`, the package the agent consoles use):

- write `CSI ?7l` first — x/vt starts with autowrap **on**; `tui.Run` turns
  it off;
- write every `\n` of the frame as `\r\n` (x/vt's linefeed only returns the
  carriage under LNM), exactly as Bubble Tea's renderer does;
- read the cells back row by row with `CellAt`, skipping a wide glyph's
  continuation cells; colours dropped, trailing spaces trimmed, one `\n`
  per row.

A line wider than the terminal clips at the right edge exactly as on a real
terminal with autowrap off, so overflow bugs reach the golden file.
Under `go test`, `lipgloss.ColorProfile()` is Ascii, so goldens pin the
no-colour rendering (`content_popup.go` and `diff_images.go` branch on it —
image previews render as the luminance ramp).

### 3.7 Keys

Tokens map to `tea.KeyMsg` through `keyMsgFor(token)`, the inverse of the
recorder's `keyToken(tea.KeyMsg)`, defined next to it:

- `keyToken` is extended to emit `M-<x>` for Alt keys (it drops them today,
  though the TUI binds alt+a / alt+t and `tui-capture.sh` accepts `M-*`),
  so recorder, capture script and driver share one vocabulary;
- `space` → `KeySpace` with `Runes = [' ']`, as Bubble Tea coalesces it;
- a multi-rune literal (`foo`) is one press per rune, as the recorder
  writes it.

A round-trip test over the whole vocabulary pins
`keyToken(keyMsgFor(t)) == t`. Tokens `keyToken` never produces (the
recorder's `<...>` diagnostics) are rejected.

### 3.8 Teardown

`Close()` does what `tui.Run` does after the program ends: cancel a running
op (`opCancel`), `Tasks().KillAll` / `Sessions().KillAll`,
`removeSnapshotFile`, `closeSteerInbox`, `releaseKeptInboxes`, `closeWeb`,
and cancels the model's subscriptions to `domain.Sessions()` /
`domain.Tasks()` (today only a manager change cancels them). The harness
defers it per scenario.

### 3.9 What this does not cover

Bubble Tea's input parser and incremental redraw, mouse input, a real
terminal's font rendering (the `◆4` overlap itself was font-level; the
spacing fix is what a golden pins), terminal handovers, agent consoles.

## 4. Determinism

| Varies | Pinned by |
|---|---|
| **Now** (ages, "today", note/review/shelf dates) | New DAG-leaf package `internal/clock`: `clock.Now()`, and `clock.Freeze(t)` for tests. Only call sites where a time is **stored** (note, review, shelf, bookmark, saved-compare, task-history creation, the CLI's `repos.Touch`) or **drawn** (ages, date lines, relative times, the branch filter's age rule, an op's elapsed time over a running op — `m.opStart` and its `time.Since` draws) switch to it; debounce/double-click/timeout code keeps `time.Now`. The e2e `TestMain` freezes it at the builder's `dateBase` + 1 day. The plan lists each converted call site. |
| **Commit dates made by gg itself** (a `[[run]]` or TUI step that commits, stashes, merges) | `[[run]]` commands and the TUI run in-process, so their git subprocesses inherit the process environment, which parallel scenarios share: a per-scenario date is impossible. The e2e `TestMain` sets `GIT_AUTHOR_DATE`/`GIT_COMMITTER_DATE` once, process-wide, to the frozen clock's instant. Every gg-made commit then carries that one date; SHAs stay stable (they still differ by tree and parents — but two gg-made commits with the same tree, parent and message now share a SHA, and anything ordering gg-made commits by date sees a tie). The repo builder keeps its per-call dates (it passes them explicitly per command). The plan checks the existing 120 scenarios still pass with the fixed date before relying on it — first the version-snapshot ones (`s82_cli_versions`, `s95_version_links`, `version-drift`, `migrate-real-data`); if one depends on distinct dates, the fixed date is set only while TUI scenarios run (they then run serially, see §5). |
| **Sandbox path** (header, worktree rows) | TUI scenarios build under a fixed root `$TMPDIR/gg-tui/<scenario-file-stem>`, removed and recreated per run. Goldens store the root as `{{root}}`; before comparing, the actual root is replaced by `{{root}}` padded or cut to the root's own display width, so column alignment is compared as rendered. |
| **Size, theme, colours, language** | Size from `[tui] size`; theme `terminal`; colours never read; language English (the TUI reads it only from `[ui] language`, whose default is English). Theme and language are **process globals** (`setTheme`, `i18n.SetLanguage`): a scenario may not change either — `Headless` fails a step after which either differs from its start value. |
| **Config and state** | `XDG_*` are read from the environment at call time and scenarios run in parallel in one process, so there is one value for all: per-scenario XDG folders are impossible. Instead: **repo-keyed stores** (notes, reviews, shelves, link history — keyed by the repo's common dir) are isolated per sandbox for free; the **machine-global** ones go through the state path the TUI already carries (`m.statePath`, set by `Run`): `Headless` takes `StatePath` (a per-scenario folder, a field on the model — not a global), which isolates the TUI's repo MRU (`repos.toml`) and prompt memory (`prompts.toml`) together. `cli.RepoStatePath` is a package global, so `[[run]]`s keep writing the shared MRU the harness already uses; the TUI never reads it (it reads its own `StatePath`). Task history (`taskhist`) stays shared: no scenario may checkpoint the AI-tasks surfaces (the rule is in the skill). |
| **Hostname / user / agents on PATH** | Quiet mode skips tool-status probes; the fake review tool is the only configured agent. |

**Windows:** TUI steps run (a panic, a hang, a failed `screen_contains`
still fail), but golden **byte comparison is skipped** — paths carry `\`
and a drive letter; a second golden set would double the upkeep.
`screen_contains`/`screen_excludes` run on every OS.

**Mismatch output:** a unified line diff of golden vs actual (with the
checkpoint name and the keys that led there), and the actual screen written
next to the golden as `NN-<name>.txt.actual` (gitignored).

**Updating:** `go test ./e2e -run <Scenario> -update` writes/replaces the
goldens of the scenarios run and deletes goldens with no checkpoint. Never
run by `./test.sh`.

## 5. Where it runs

The existing e2e stage of `./test.sh` (and `race`) — no new stage. TUI
scenarios run in parallel with the others; each has its own fixed root and
state path (§4). The clock is process-global but frozen once in `TestMain`
before any scenario starts, so parallel scenarios see one value.

## 6. First scenarios

1. `tui_review_mixed.toml` — a branch review, a commit review and line
   notes in one repo. Pins: the Commits row shows `✎` without a `◆ 1`; the
   Files view lists the Reviews row and badges the noted file `◆ 1`; a stack
   opened from a file holds no `@notes/` entry; `N`/`P` never land on the
   review row.
2. `tui_reading_width.toml` — size `200x45`. Pins: View all notes maximized
   (`ctrl+t`) centred in the 120-column reading column with a long note
   wrapped under NOTE; the review overview maximized the same way.

Both must be shown failing against the unfixed behaviour before they count:
each is run once with the fix reverted locally (per the "watch it fail"
rule), and the plan records the failing diff.

## 7. Workflow and docs

- `writing-e2e-scenarios` skill: the `[tui]` format, the `-update` loop, the
  rule "read every new or changed golden before committing", the
  mixed-feature rule, and the bug-from-a-real-repo procedure (§1).
- `adding-features` skill: a checklist item — a feature with a TUI surface
  ships a TUI scenario with golden screens of that surface.
- CLAUDE.md `e2e` row: "+ TUI screens (`[tui]` steps → golden files)";
  `tui` row: mention `Headless` + quiet mode.
- CHANGELOG entry; README only if the contributor section describes e2e.

## 8. Out of scope

A recording tool that writes scenarios (hand-written steps + `-update` for
now), the web frontend, shared repo fixture files, mouse steps.

## 9. Risks

- **A missed never-ending command** makes every settle hit the 500-message
  guard. Mitigation: the guard names the looping message type; the quiet
  mode test on an empty repo catches it at the start.
- **Clock conversion misses a drawn time.** Mitigation: goldens diff
  across a real day boundary would show it; the plan greps `time.Now` in
  render paths and the first scenarios draw dates and ages.
- **Golden churn** from intended UI changes. Mitigation: `-update` plus the
  read-before-commit rule; scenarios stay small and focused.
- **Shared `Run`/`Headless` set-up drifts.** Mitigation: one extracted
  helper both call.
- **Another command blocks on user input** besides the op waiter (a future
  prompt that waits on a channel). Mitigation: it hits the no-fixed-point
  guard by name; the fix is the same descriptor pattern as `opWaitMsg`.
- **Quiet mode hides a real bug** in a watcher or the refresh lane. Accepted:
  those have their own unit tests; golden screens pin rendering and
  navigation, not background refresh.
