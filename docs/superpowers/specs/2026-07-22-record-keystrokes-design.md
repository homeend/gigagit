# `gg --record <file>` — keystroke recorder — design

Date: 2026-07-22 · Status: approved · Branch: `feat/record-keystrokes`

## Goal

Let a human record a TUI session's keystrokes to a file, in the **exact
keyscript format `tui-capture.sh` replays**, so a recorded scenario
round-trips: drive the real `gg` TUI once → gg writes the keyscript →
`tui-capture.sh <file>` replays it headlessly and captures a snapshot of
every screen the human walked through. The recorder is the capture harness's
missing input side — the human authors scenarios, Claude checks them.

## Fidelity model (decided)

**One step per keystroke.** Each key becomes its own keyscript step, so replay
settles and snapshots after *every* keystroke — the full film strip of
screens, which is what makes a scenario checkable. No timing/pause-grouping.

## CLI surface

`gg --record <file>` (and `--record=<file>`).

- Pulled off `os.Args` by an `extractRecord(args) (string, []string)` helper
  in `cmd/gg/main.go`, added directly after `extractTimeTrack`
  (`main.go:29`), mirroring its two accepted forms and its "trailing
  `--record` with no value is an error" behavior.
- Applies **only to the TUI launch** (no subcommand). If the remaining args
  route to a CLI subcommand, the record path is ignored (recording is a TUI
  concept). A file that cannot be created is a fatal, clearly-messaged error
  before the TUI starts (the `--time-track` error convention).
- The path threads into `tui.Run`.

## Components

### 1. `tui.Run` gains the record path

`internal/tui/run.go:20` — `Run(svc *domain.Service)` becomes
`Run(svc *domain.Service, recordPath string)` (its sole caller is
`cmd/gg/main.go:110`). When `recordPath != ""`, `Run` constructs a recorder
(passing the repo path from the `svc.TopLevel` it already computes at
`run.go:31`) and stores it on the initial `Model` before
`tea.NewProgram(m, …)` (`run.go:42`). After `p.Run()` returns, the recorder
is closed on the `final.(Model)` branch (`run.go:44-49`), alongside the
existing snapshot-file cleanup.

### 2. `internal/tui/recorder.go` — the recorder (TUI-only)

A new file; imports only `os`/`time`/`fmt`/bubbletea — no engine/domain.

- `recorder` struct: an open `*os.File`, the repo path, and a **one-key
  pending buffer** (see quit handling). Held as a `*recorder` pointer field
  on the value-receiver `Model` (the `modal`/`popup` pointer-field pattern),
  so it survives the value copy.
- `newRecorder(path, repo string) (*recorder, error)` — creates/truncates the
  file and writes a `#` comment header: a title line, the repo path, and the
  recording date (`time.Now()`; real runtime, allowed in gg code). The header
  self-documents which repo to replay the scenario against.
- `(*recorder) note(msg tea.KeyMsg)` — translates the key to a token via
  `keyToken`; writes the *previously buffered* token as its own line and
  buffers the new one (lag-by-one, see below); flushes.
- `(*recorder) close()` — closes the file **without** writing the buffered
  token (drops the terminating quit); safe on a nil recorder.
- `keyToken(msg tea.KeyMsg) string` — pure translation from a bubbletea key to
  the **tui-capture token vocabulary**: `enter`, `esc`, `space`, `tab`,
  `up`/`down`/`left`/`right`, `bspace`; `ctrl+X` → `C-X`; `alt/meta` → `M-X`;
  any rune → the literal rune. Every output MUST be a token
  `tui-capture.sh`'s `send_tokens` accepts (this is the round-trip contract,
  asserted by a test).

### 3. The tap in `Update`

One guarded line at `internal/tui/model.go:969` (the `case tea.KeyMsg:`
entry), before the key is handled:
`if m.recorder != nil { m.recorder.note(msg) }`.

### 4. `tui-capture.sh` — skip `#` comment lines

A one-line addition to the keyscript walk (the `while IFS= read -r step`
loop): after trimming, `[[ "$step" == \#* ]] && continue` so the recorder's
`#` header round-trips cleanly (the walk currently would send a `#`-line as
literal keystrokes). No other capture-side change.

## Quit handling (the one subtlety)

The keystroke that ends the session (`q` at top level, or `ctrl+c`) must not
land in the file, or replay would quit `gg` before the final capture. The
recorder **lags by one key**: `note(keyN)` writes `key(N-1)` and buffers
`keyN`; `close()` drops the buffered key. So the terminating quit is never
written, and every meaningful key is. A quit not driven by a keypress (rare)
at worst drops one trailing real key — acceptable. Because the lag is a
single key, a crash loses at most that one buffered key (the quit anyway),
consistent with the "flush as you go" intent.

## File format

```
# gg keystroke recording
# repo: /abs/path/to/repo
# recorded: 2026-07-22T21:30:00Z
.
down
down
enter
esc
```

- `#` header lines: title, repo path, date.
- Body: one tui-capture token per line (unlabeled → snapshots come out
  `snap-NN-stepNN.txt`). Steps are newline-separated (tui-capture also accepts
  `;`). No labels in v1 — the snapshot order carries the sequence; a human can
  add `label:` prefixes by hand later.
- Overwrite (truncate), not append — each recording is a fresh scenario.

## No-ambiguity note

One keystroke is one token, so a rune never collides with a keyword: typing
the letters of "enter" into a filter field records five literal rune steps
(`e n t e r`), each sent by `tui-capture` via `send-keys -l`; the keyword
`enter` only ever comes from `tea.KeyEnter`. No escaping needed.

## Scope guards (YAGNI)

- **Keyboard only.** Mouse events (`tea.MouseMsg`, gg's clickable tabs) are not
  recorded — a scenario should be keyboard-driven, matching the keyboard-first
  TUI. Documented, not enforced.
- **No timing / pause-grouping** (that was the rejected option B).
- **No labels, no editing UI** — the file is plain text the human can edit.
- **Inert outside the TUI path** — `--record` with a subcommand is ignored.

## Error handling

- Un-creatable `--record` file → fatal message before the TUI launches
  (`--time-track` convention), exit non-zero.
- A mid-session write error is non-fatal: recording stops silently (best
  effort — a broken recorder must never take down the user's live session).
  The recorder marks itself disabled on first write error.

## Testing

- **`keyToken` table test** — every vocabulary case (named keys, `ctrl+X`,
  `alt+X`, runes incl. `.`/`/`/`?`/digits) maps to the expected token.
- **Round-trip contract test** — every token `keyToken` can emit is in the set
  `tui-capture.sh`'s `send_tokens` accepts (a small explicit allow-list in the
  test, so a future keyToken change that emits an unsupported token fails
  here).
- **`recorder` file test** — attach a recorder to a temp file, feed a
  `KeyMsg` sequence (the existing `keyMsg(...)` test helper / `tea.KeyMsg`
  literals), `close()`, read the file: assert the header is present, the body
  lines match the expected tokens, and the **final (quit) key was dropped**.
- **Model-tap test** — drive `Model.Update` with a recorder attached over a
  few `KeyMsg`s and assert the file captured them (verifies the `model.go`
  tap fires).
- **`tui-capture.sh` comment-skip** — run the harness with a `#`-headed
  keyscript and confirm it replays the body and ignores the header.
- `./test.sh` stays green.

## Deliverables

1. `extractRecord` + threading in `cmd/gg/main.go`; `tui.Run` signature.
2. `internal/tui/recorder.go` (+ tests) and the `model.go` tap.
3. `tui-capture.sh` `#`-comment skip.
4. Docs: a "Recording scenarios" section in the `driving-tui-headless` skill
   (record → hand Claude the file → replay+capture), a `CLAUDE.md` line, and a
   `CHANGELOG.md` entry.

## Workflow

Worktree `feat/record-keystrokes` off `main` (already created). Spec committed
there; the human merges after review.

## Future doors (not built now)

- Timing capture / pause-grouping (option B) as a `--record-timed` variant.
- Auto-labeling steps from the action taken (would need semantic hooks).
- A replay-and-diff regression harness (record once, replay in CI, diff
  snapshots) — the natural next layer once scenarios exist.
