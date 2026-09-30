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
  { branch = "feature" }, { rm = "a.txt" }, { rm = "b.txt" },
  { commit = "deleted a b" },
]

[[run]]
cmd = ["note", "add", "--rev", "HEAD~1", "--file", "a.txt", "--new-line", "1", "--summary", "why a?"]
[[run]]
cmd = ["review", "--tool", "fake", "HEAD"]   # the fake tool prints a canned review
[[run]]
cmd = ["review", "--tool", "fake"]           # no positional = the current branch

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
| `keys` | keyscript tokens (`enter`, `down`, `C-t`, `M-x`, a literal like `.` or `foo`), sent one at a time, each followed by a settle |
| `name` | makes the step a **checkpoint**: its whole screen is compared with `<scenario>.screens/NN-<name>.txt` (`NN` = the checkpoint's 1-based ordinal, two digits) |
| `wait` | after the keys settle, fire every parked timer once, then settle again (§3.4) |
| `screen_contains` / `screen_excludes` | substrings the settled screen must / must not contain; allowed on any step, checked on every OS |

`name` must be unique within a scenario and match `[a-z0-9-]+`. A golden
file with no matching checkpoint fails the scenario (a renamed step never
leaves a stale golden behind).

**The fake review tool.** The harness's `.gg.toml` (already written by
`writeGGToml`) gains a `review` command named `fake` whose command prints a
fixed review document from `e2e/fixtures/review.md` (a tiny Go helper in the
e2e module prints the file, the way the harness's fake `gh` answers from
`.git/fakegh`; no shell tool is assumed, so Windows runs it too).

## 3. The driver: `tui.Headless`

New file `internal/tui/headless.go`, exported for the e2e harness only.

```go
h, err := tui.NewHeadless(svc, tui.HeadlessOptions{Width: 160, Height: 40})
err = h.Press("down")      // one token → one tea.KeyMsg, then settle
err = h.FireTimers()       // the `wait` step field
s  := h.Screen()           // the settled frame as plain text
```

### 3.1 Why not the real `tea.Program`

Bubble Tea gives no "idle" or "frame flushed" signal and repaints on its own
ticker, so reading the screen of a real program means waiting on a clock —
the failure mode seen driving test-1 under tmux on 2026-09-30. The driver
instead runs its **own message loop over the real `Model`**: gg's `Init`,
`Update` and `View` all run unchanged; only Bubble Tea's scheduler, input
parser and incremental renderer (library code) are replaced.

### 3.2 Start-up

`NewHeadless` mirrors `tui.Run`'s model set-up (theme from config, branch
filters, tasks config, snapshot target) through one shared helper extracted
from `Run`, so the two cannot drift, then sets `m.quiet = true`, delivers a
`tea.WindowSizeMsg{Width, Height}`, runs `Init()`, and settles. Excluded
from the shared helper, by design: `--record`, `--web`/`--at`, the steering
inbox, the operation log side effect.

### 3.3 Quiet mode

A `quiet bool` on `Model`. When set, commands that wait on the outside world
and never finish are **not started**; each site checks the flag where the
command is created:

- `waitTasksCmd`, `waitSessionsCmd` (channel waits),
- `startSteerCmd` and the steering inbox,
- git/file watching (`gitwatch`, `filewatch`) and the open-file stat poll,
- the background auto-refresh / ff-pull lane,
- `startupWebCmd` (the hosted web page),
- `refreshToolStatusesCmd` (probes installed agent versions on the host —
  host-dependent output).

The plan enumerates every `Init()` command and every long-lived command
constructor, and a test pins the list: in quiet mode, `Init()`'s commands
must all return within the settle budget on an empty repo.

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

Time never passes in a test; a scenario says when "the user paused". A test
pins that no `tea.Tick`/`tea.Every` call remains outside `m.tick`
(grep-style, like the `handover()` guard).

### 3.5 Settling

A FIFO of pending commands, run **one at a time** (deterministic order):

1. Pop a command and call it.
2. `nil` → skip. `tea.BatchMsg` → enqueue its commands (flattened). A
   `timerMsg` → park it. Any other message → `m.Update(msg)`, enqueue the
   returned command.
3. Stop when the queue is empty. That is the settled state.

Guards (each an error naming the cause):

- **No fixed point**: more than 500 messages, or 30 s wall time, in one
  settle → fail, listing the message types seen most often (a missed
  never-ending command shows up here).
- **A terminal handover** (`handover()` → an editor, a merge tool) → fail,
  naming the command; the headless driver cannot run a child in a terminal.
- **Quit** (`tea.QuitMsg`, or `tea.Quit` returned) → recorded; a later
  `Press` fails.
- Bubble Tea's internal messages the loop cannot act on (screen clears,
  cursor or mouse-mode toggles, window title) are dropped.

`quitFilter` (the `tea.WithFilter` quit guard) runs on every message
exactly as the real program applies it.

### 3.6 Drawing

After settling, `Screen()` paints `View()` into a fresh `x/vt` emulator at
the scenario's size (the emulator the agent consoles already use) with
autowrap off, as `tui.Run` sets it, and reads the cell text back row by
row: colours dropped, trailing spaces trimmed, one `\n` per row. A line
wider than the terminal clips at the right edge exactly as on a real
terminal, so overflow bugs reach the golden file.

### 3.7 Keys

Tokens map to `tea.KeyMsg` through `keyMsgFor(token)`, the inverse of the
recorder's `keyToken(tea.KeyMsg)`, defined next to it. A round-trip test
over the whole token vocabulary pins `keyToken(keyMsgFor(t)) == t`, so what
`gg --record` writes is exactly what `Press` accepts. Tokens `keyToken`
never produces (the recorder's `<...>` diagnostics) are rejected.

### 3.8 What this does not cover

Bubble Tea's input parser and incremental redraw, mouse input, a real
terminal's font rendering (the `◆4` overlap itself was font-level; the
spacing fix is what a golden pins), and terminal handovers.

## 4. Determinism

| Varies | Pinned by |
|---|---|
| **Now** (ages, "today", note/review/shelf dates) | New DAG-leaf package `internal/clock`: `clock.Now()`, and `clock.Freeze(t)` for tests. Only call sites where a time is **stored** (note, review, shelf, bookmark, saved-compare, task-history creation) or **drawn** (ages, date lines, relative times) switch to it; debounce/double-click/timeout code keeps `time.Now`. The e2e `TestMain` freezes it at the builder's `dateBase` + 1 day. The plan lists each converted call site. |
| **Commit dates made by gg itself** (a `[[run]]` or TUI step that commits, stashes, merges) | `[[run]]` commands and the TUI run in-process, so their git subprocesses inherit the process environment, which parallel scenarios share: a per-scenario date is impossible. The e2e `TestMain` sets `GIT_AUTHOR_DATE`/`GIT_COMMITTER_DATE` once, process-wide, to the frozen clock's instant. Every gg-made commit then carries that one date; SHAs stay stable (they still differ by tree and parents). The repo builder keeps its per-call dates (it passes them explicitly per command). The plan checks the existing 120 scenarios still pass with the fixed date before relying on it. |
| **Sandbox path** (header, worktree rows) | TUI scenarios build under a fixed root `$TMPDIR/gg-tui/<scenario-file-stem>`, removed and recreated per run. Goldens store the root as `{{root}}`; before comparing, the actual root is replaced by `{{root}}` padded or cut to the root's own display width, so column alignment is compared as rendered. |
| **Size, theme, colours, language** | Size from `[tui] size`; theme `terminal`; colours never read; language English (`i18n` default, and the harness pins `LANG`/`LC_ALL=C`). |
| **Config and state** | e2e `TestMain` already isolates `XDG_CONFIG_HOME`/`XDG_STATE_HOME`; each TUI scenario gets its own subfolder of both, so notes, reviews and prompt memory never leak across scenarios. |
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
XDG subfolders. The clock is process-global but frozen once in `TestMain`
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
