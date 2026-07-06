# External Tools Stage 3: `review` — Design

**Status:** approved for planning (2026-07-07).
**Builds on:** Stage 1 (conflict lane, `2026-07-05-external-tools-design.md`)
and Stage 2 (commit-message capture lane, merged `fb4d5e7`; Junie
`$GG_MESSAGE_FILE` output channel, merged `6f853f5`).

## Goal

Run an external AI agent (Claude Code, Junie) over a branch, a commit/range, or
the uncommitted working changes; capture its **review report**; persist the
report durably; and show it in a read-only in-TUI viewer (with a key to open it
in `$EDITOR`) — plus a scriptable `gg review` CLI verb that prints the report to
stdout.

## Scope — three review targets

One pipeline, three inputs (all confirmed in brainstorming):

| Target | Source (TUI) | Resolves to |
|---|---|---|
| **Branch work** | Branches panel `.` menu → "Review this branch (AI)" | range `<base>..<tip>`, base = `git merge-base main <tip>`, falling back to `@{upstream}` when no `main`; the branch tip when neither exists |
| **Commit / range** | Commits panel `.` menu → "Review this commit (AI)" (focused) / "Review marked range (AI)" (two ◉ marks) | focused = `<sha>^..<sha>` (root commit → just `<sha>`); two marks = `<older>..<newer>` (reuses existing space-marks) |
| **Uncommitted** | Files/Status panel `.` menu → "Review working changes (AI)" | the working-tree **+** staged diff (a diff, not a range) |

Each target produces a **review target** value carrying:
- a **`<range>` label** — a prose token (git rev-range like `main..HEAD`, or the
  literal `(working changes)` for the uncommitted target), and
- a **`model.DiffSpec`** — the diff to materialize (a rev-range spec, or the
  working+staged spec for the uncommitted target).

## Architecture

Dual-channel input, capture output, durable report, viewer — reusing Stage 2's
machinery end to end.

```
 TUI .menu / CLI `gg review`
        │  (build review target: <range> label + DiffSpec)
        ▼
 domain.ReviewReport(ctx, target, tool) ──► engine.ReviewChanges (capture op)
        │  writes report to state dir          │  CaptureRunner seam
        │  returns {path, content}             │  $GG_MESSAGE_FILE channel
        ▼                                       │  $GG_CONTEXT_FILE + $GG_REVIEW_DIFF
 TUI: open review_view.go on {path,content}     ▼
 CLI: print content to stdout             Result.Captured (the report)
```

### Engine — `ReviewChanges` op (sibling of `GenerateMessage`)

`ReviewChanges{Command, Dir, Env, Diff model.DiffSpec, RangeLabel string}` —
`LockMode()` = `Read`. Reuses the Stage-2 primitives verbatim:

- **`CaptureRunner` seam** (`OpDeps.CaptureRunner` / `ShellCaptureRunner`) — the
  headless run; `ErrWaitDelay`-on-clean-exit-0 = success (Stage-2 fix).
- **`$GG_MESSAGE_FILE` output-channel contract** — the op provisions an empty
  temp file, and **non-empty file content wins over stdout** as
  `Result.Captured`. A task-agent (Junie) writes its report there; a stdout tool
  (Claude `.result`) leaves it empty and stdout is used. Identical to the
  Stage-2 Junie fix; this is why that contract was written to generalize.
- **`$GG_CONTEXT_FILE`** — a labeled summary: the `<range>` label, the
  files-changed numstat, and the range's commit subjects (empty for the
  uncommitted target).
- **`$GG_REVIEW_DIFF`** — the full unified diff of `op.Diff` (the Stage-2
  `$GG_STAGED_DIFF` analog), capped at `MaxDiffBytes` with a stat-only note past
  the cap.

It differs from `GenerateMessage` only in (a) which diff it computes (`op.Diff`
— a range or the working+staged spec, not `--cached`), and (b) the captured
output is a **free-form report** returned as-is (no `ParseCaptureMessage`; a
review is not a subject/body). `buildSummary`/`writeTempFile`/`MaxDiffBytes` are
shared (extract the shared bits rather than copy).

### domain — `ReviewReport`

`domain.ReviewReport(ctx, target ReviewTarget, tool config.ToolCommand)
(ReviewResult, error)` owns orchestration + persistence (as it owns the
shelf/bookmark stores):

1. Compute `op.Diff`/`RangeLabel` from `target`.
2. Run `engine.ReviewChanges` via `Execute` (Read reservation) → captured report.
3. Write the report to the durable path (below); return `{Path, Content, Range}`.

`ReviewTarget` is a small value: `{Kind: branch|range|working, Range string,
Diff model.DiffSpec}`. The TUI/CLI build it; domain resolves the actual diff.

### Report storage — durable, per-repo, accumulating

Reports persist under the gg state dir, keyed by git common dir (the shelf/
bookmark keying):

```
<state>/gg/reviews/<EncodeRepoKey(commonDir)>/<YYYYMMDD-HHMM>-<sanitized-range>.md
```

**Keep all** (confirmed): every run writes a new timestamped file; they
accumulate (small text). No history-browser UI in this stage — the files are on
disk and re-openable; a browser can come later (YAGNI). The sanitized-range
segment replaces the bytes that are unsafe *inside a single filename segment* —
`/`, whitespace, and control/`:` bytes → `-` — so `main..HEAD` stays readable
(`..` is safe in a filename), `feature/x..main` → `feature-x..main`, and
`(working changes)` → `-working-changes-`.

### TUI — the viewer (`review_view.go`)

A new **read-only full-screen layer**, built on the `history`/`blame`
full-screen-layer pattern so `ctrl+t` maximize and `isFullScreenLayer` come for
free:

- renders the markdown report scrollable (plain text render — no markdown
  styling engine in this stage; the report is readable as-is),
- `/` search reusing the shared `filterMotion`,
- **`e`** → opens the **same report file** in `$EDITOR` (reuses
  `open_external.go` / the `edit_actions.go` handover precedent, read-only),
- `esc` closes.

Run flow: `.` menu → (chooser if >1 review tool) → first-run approval (shared
`toolCommandApproved`/`approvalBoxView`) → dispatch with the animated spinner →
on success the report lands in the state dir and the viewer opens on it; a
failed/empty run surfaces the error in the status line (no empty viewer), same
as the commit lane. The three `.`-menu entries each build the right
`ReviewTarget` and share one dispatch path.

### CLI — `gg review`

`gg review [<rev>|<A..B>] [--tool <name>] [--working]` (confirmed):

- default target = the current branch's work (`<base>..HEAD`); a positional
  `<rev>`/`<A..B>` overrides; `--working` reviews the uncommitted changes.
- `--tool <name>` selects among configured `review` commands (else: the sole
  one, or an error listing the choices — the non-interactive contract).
- Runs `domain.ReviewReport`, **prints the report to stdout**, and persists it
  to the state dir (same path as the TUI — one code path). Read-only; exits 0 on
  a produced report, 1 on tool failure / empty report, 2 on usage error.
- Routed through `runOne` so `gg batch` can drive it.

## Catalog defaults & tool verification

New `review` templates in `exttool.Builtins()`. Exact commands are
**verified against the real binaries during implementation** (the Stage-1/2
lesson — the spec's recorded defaults are a starting point, not gospel):

- **Claude** — the anchor, `capture` mode. Seed:
  `claude -p "/code-review <range>" --output-format json`. Verify `/code-review`
  runs headless under `-p` and that `.result` is the report; adjust flags
  (`--permission-mode`, `--allowedTools "Bash(git *)" "Read"`) as the probe
  shows. For the uncommitted target, `<range>` is absent and the prompt points
  the agent at `$GG_REVIEW_DIFF`.
- **Junie** — probe `junie --review "…" --output-format json` (optionally
  `--json-output-file`). **If it emits a capturable report** → Junie ships as
  `capture` (report → viewer, uniform with Claude). **If not** → Junie ships as
  `terminal` (interactive `--review`, terminal handover like the conflict lane,
  **no** report viewer — the run just hands over the terminal). The design
  supports either; the probe outcome is recorded in the spec/commit and in
  `internal/exttool`'s catalog comment (as the Stage-1 Junie note did).

Templates use generation-time `<bin>`/`<env:…>` tokens only; dynamic content
reaches the agent through `<range>` + the `$GG_*` file channels — never a raw
prose substitution (the Stage-1 injection posture).

## Template & config plumbing

- New **`<range>`** command token — **prose-valued** (a git rev-range has no
  spaces), substituted literally like `<op>`/`<source>`. Added to
  `resolveCommandToken`, `commandTokens`, and `ValidateCommandTokens`. Empty/
  absent for the uncommitted target (the prompt uses `$GG_REVIEW_DIFF` instead);
  a `<range>` in an uncommitted-target command resolves to the literal
  `(working changes)` label so no command is left with a dangling token.
- New **`$GG_REVIEW_DIFF`** env channel (generation token `<env:GG_REVIEW_DIFF>`
  → `${…}`/`%…%`), set by `ReviewChanges` to the diff temp-file path.
- **`toolCommands` un-inerts `review` capture:** Stage 2 allowed `mode =
  "capture"` only for `commit_message`; Stage 3 extends `toolUsable` to also
  allow it for `review`. A `review` `terminal` block stays valid too (Junie
  fallback).
- Settings "External tools" wizard already builds category-generic rows, so the
  new `review` catalog rows appear with **no wizard UI change** — as Stage 1
  designed. (Note the Stage-2 lesson: the append-only wizard won't rewrite a
  user's existing blocks, but `review` blocks are new, so no stale-config issue
  here.)

## Testing

- **engine `ReviewChanges`** — real `ShellCaptureRunner` + scripted `sh -c`
  (never a real agent): file-channel content wins over stdout; empty file →
  stdout; `$GG_REVIEW_DIFF` holds the range diff; over-cap truncation; temp
  cleanup. Mirrors `generate_message_test.go`.
- **domain `ReviewReport`** — real git repo: branch/range/working targets
  resolve the right diff; the report file is written to the state dir with the
  expected name; content round-trips.
- **template** — `<range>` resolves literally; invalid-token guard;
  `GenerateCommandFor` renders `<env:GG_REVIEW_DIFF>` per-OS.
- **exttool** — `review` templates materialize with `<range>`, `${GG_REVIEW_DIFF}`,
  and (capture) `${GG_MESSAGE_FILE}`.
- **tui** — the three `.`-menu targets build the correct `ReviewTarget`; the
  viewer renders, `/` searches, `e` opens `$EDITOR`, `esc` closes; capture mode
  un-inerted for review.
- **cli/e2e** — a `gg review` scenario: a committed range produces a non-empty
  report on stdout (using a fake/echoing tool command, not a real agent) and a
  persisted file; usage/`--tool` errors exit as specified; drivable under
  `gg batch`.

## Docs to update on completion

`CHANGELOG.md` (always), `README.md` (new `gg review` surface), `CLAUDE.md`
(engine `ReviewChanges` + domain `ReviewReport` + the `review` viewer + the
`<range>`/`$GG_REVIEW_DIFF` tokens), and **`internal/agentskill/using-gg.md`**
(document `gg review`, bump `agentskill.Version`, `gg init --update`). Update the
`adding-external-tools` skill if the catalog contract changed.

## Open items to verify during implementation

1. `claude -p "/code-review <range>"` headless behavior + report shape + flags.
2. Junie review capture vs terminal (decides its catalog mode).
3. Whether `/code-review` accepts the uncommitted target sensibly, or the
   uncommitted target should always lean on `$GG_REVIEW_DIFF` in the prompt.
