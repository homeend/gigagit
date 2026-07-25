# Temporary two-line status bar for truncated errors — design

**Date:** 2026-07-25 · **Status:** approved · **Branch:** `feat/error-bar-expand`

## Problem

Operation failures render as a single red status-bar line
(`opFinishedMsg` → `m.statusMsg = friendlyOpError(msg.err)`; the line is
`truncate(oneLine(...), g.w)`'d in `renderInterface`). A transport error like

```
error: git push failed (exit 128): ssh: Could not resolve hostname github-homeend:
Temporary failure in name resolution fatal: Could not read from remote repository. …
```

has no friendly rewrite (only credentials / host-key / push-rejection classes
do), so the raw flattened git stderr is cut off at the terminal edge — the
user cannot read what actually failed. The full text lands in `errors.log`
and the Settings `,` → Session errors viewer, but nothing points there.

## Decision summary (user-approved)

1. **Expand, don't rewrite.** When an *error* status message does not fit one
   line, the bar temporarily becomes two lines. No new friendly rewrite for
   the hostname-resolution class — the raw text is the point.
2. **Collapse: 30 s or newer message.** The expansion lasts 30 seconds, or
   ends the moment any newer status message replaces the current one.
   Keypresses alone never collapse it.
3. **The second line covers the footer.** While expanded, the error's second
   line renders in place of the footer key-hint line. Panels keep their full
   height; `layout()` is untouched.
4. **Hint to the full text.** The tail of line 2 always carries a dim hint —
   `· full: , → Session errors` — with its width reserved so truncation can
   never eat it.

## Mechanism

### Central stamp, render-time decision

No changes at the ~110 `statusMsg` call sites. The top-level `Model.Update`
wrapper compares `m.statusMsg` before and after inner dispatch; when it
changed, it stamps `m.statusMsgAt = time.Now()` on the returned model. A
newer message therefore restarts the 30 s window automatically, and a
non-error replacement simply renders one line again (its `statusIsError`
classification fails) — which is exactly the chosen collapse behavior.

`renderInterface` expands only when **all** hold:

- `statusIsError(m.statusMsg)` (existing render-time classifier — includes
  the translated-prefix derivation),
- the assembled status line's display width exceeds `g.w`,
- `time.Since(m.statusMsgAt) < 30s` (constant, not configurable).

### Rendering while expanded

- Row order top-to-bottom: `header, body, <error upper row>, <error lower
  row>` — the footer string is replaced by the first wrapped half of the
  message, and the status row (still the bottom row) holds the second half,
  so the message reads naturally top-to-bottom. The wrap is at display width
  (multi-byte-safe, the `padCell`/`lipgloss.Width` family — not byte slicing).
- The bottom row's tail: dim hint `· full: , → Session errors` (one new i18n key,
  present in all four bundles; the `,` and `Session errors` naming matches
  the Settings menu). The message truncates *before* the hint if even two
  lines are not enough.
- Both rows styled with `statusErrStyle` **after** truncation (existing rule:
  truncation slices runes and would corrupt ANSI).
- Error mode already puts the message first in the segment order, so the
  wrap applies to the same assembled `statusLine` string as today.

### Collapse

No dedicated timer plumbing if avoidable: the TUI re-renders on the
perpetual 1 s session-snapshot heartbeat, so the render condition collapses
the bar within ~1 s of expiry and the footer returns. **Implementation must
verify** the heartbeat ticks while idle with no op running; if it does not,
stamp-time schedules a one-shot `tea.Tick(30s)` collapse message instead.

## Out of scope

- No friendly rewrite for `could not resolve hostname` (decided against).
- No config key for the 30 s duration (YAGNI).
- Popups, Session-errors viewer, `errors.log` unchanged.
- CLI/engine untouched — this is TUI render-layer only.

## Testing

Table-style render tests beside `status_error_test.go`:

- long error + narrow width → two rows, footer hints absent, hint present,
  both rows error-styled;
- short error → one row, footer intact;
- long **non-error** message → one row (no expansion);
- stamp older than 30 s → collapsed, footer back;
- extreme narrow width → hint still fully visible, message truncated first;
- newer non-error status replacing an expanded error → immediate collapse.

Plus a `tui-capture.sh` visual sanity pass against a repo with an
unresolvable remote.

## Files touched (expected)

- `internal/tui/model.go` — `statusMsgAt` field + central stamp in `Update`.
- `internal/tui/view.go` — expansion condition + two-row rendering in
  `renderInterface`.
- `internal/tui/status_error_test.go` (or sibling) — the render tests.
- `internal/tui/lang/*.toml` ×4 — the hint key.
