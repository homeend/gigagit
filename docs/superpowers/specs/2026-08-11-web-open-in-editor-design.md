# Web: open a file in a local editor (mini-spec, unimplemented)

Date: 2026-08-11 · Status: **proposed** — written while closing the file
ctx-menu parity batch; implementation is its own increment.

## Goal

TUI parity for "Edit in editor" (Files panel) and "Open staged version in
external editor" (Staged panel): a web ctx-menu row that opens the file in
an editor on the machine running `gg web`.

## Why this is NOT a port of the TUI mechanism

The TUI suspends itself and hands the terminal to `$VISUAL`/`$EDITOR`
(`tea.ExecProcess`, edit_actions.go). A web server has no terminal to hand
over, so a terminal editor (vi) started server-side would wedge. The web
variant must launch a **GUI/detached** editor command instead.

## Design

- **Endpoint**: `POST /api/open-editor {path, side?: "working"|"staged"}`.
  `path` resolves against a fresh Status read (the discard pattern: 400
  leading dash, 404 unknown). `working` opens the live file (absolute path
  inside the served worktree); `staged` exports the index blob
  (`svc.ShowFile(ctx, "", path)`, the TUI's stagedOpenExternalRow source)
  to a temp file first and opens that; a staged deletion 422s.
- **Which editor**: a configured command, never `$EDITOR`. Reuse the
  exttool `[[tools.command]]` shape with a new category (e.g.
  `category = "open_editor"`, `mode = "detach"`), `<file>` token quoted by
  the template resolver. The Settings wizard can detect common GUI editors
  (`code -g`, `subl`, …) like it detects agents.
- **Approval**: first run is consent-gated exactly like the AI lanes — the
  client shows the fully resolved command; approval stored in promptstate
  keyed on `CommandHash` (shared with the TUI, both ways).
- **Launch**: detached process (no wait, stdio to /dev/null), worktree as
  cwd. Failure to spawn → 500 with the exec error; the op-line reports it.
  This is NOT an engine Operation — nothing repo-mutating happens and no
  reservation is needed; a plain handler like /api/stage suffices.
- **Client**: ctx-menu rows "open in editor" (changes/untracked rows) and
  "open staged version in editor" (staged rows), hidden when no
  `open_editor` tool is configured (the TUI's hasReviewTool gate pattern —
  a dead row is worse than no row). Chooser when several are configured
  (the conflict-lane chooser precedent).

## Security posture

Loopback + Host/Origin guards already restrict callers to the local user;
the command comes from the user's own config (never the wire — the wire
carries only a tool NAME, the AI-lane rule) and runs only after the same
approval flow every other external command uses.

## Open questions for the implementation session

- Temp-file lifetime for the staged view (TUI deletes on editor exit; a
  detached launch cannot know — likely per-boot temp dir, best-effort
  sweep).
- Whether `history`/`blame`/commit-file surfaces get a read-only variant
  (same export-to-temp lane) in the first cut or later.
