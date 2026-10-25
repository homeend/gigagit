# gg web — a width cap for single-file views

Asked 2026-10-25 (screenshot: web blame with a "max 200 characters" box over
the text column). Every view of ONE file's content in gg web shows its text
column at most N characters wide; N is configurable, default 200, and can be
unlimited (as much width as there is).

## Scope

The single-file views:

- the file viewer — a file shown alone (`#viewer-box.vhalf`: commit / F →
  View file, overview links, the switcher), file or one-column diff mode;
- the F window's preview (`#wtf-preview`);
- blame (`#blame-box`).

Not capped: the diff pane, the history overlay, the overview split (both
columns), the stack, overview documents. The viewer's existing test for "one
file, alone" — `!split && !view.ov` (the `vhalf` toggle) — decides it, so a
split or an overview doc never takes the cap.

## Config

`[ui] file_width` (int). Default 200. `-1` = unlimited. `0` / missing =
unset (inherits the lower layer), per the overlay rule — which is why
"unlimited" is `-1`, not `0`: a `0` in `.gg.toml` could never override a
global 200. Same shape as `notes.max_entries` ("any nonzero value overlays").
Values 1–19 are floored at 20.

gg web reads it from `/api/health` (`file_width`, already the carrier of the
effective `show_graph` / `commit_sort`), fetched at boot and on every repo
switch. No health (a failed read) = 200.

The TUI does not take it in this change: its code views fill the terminal
and `reading_width.go` rules that code, diffs and blame never take a column
width. A TUI port is a follow-up if wanted. Recorded as a Design difference
in docs/web-tui-parity.md.

## What N counts

The TEXT column only. Blame's commit / author / age gutter and every line
number column come on top of N.

## Layout: the box narrows, never widens

The cap narrows the view's box (centred, as today); the text column inside
is then N characters:

- viewer alone: `min(today's half width, gutter + N ch)`;
- F preview: `min(today's half width, gutter + N ch)`; with `m` (full):
  `min(100%, gutter + N ch)` — the frame lifts the half-width rule, the
  column stays capped (the TUI's `reading_width` + ctrl+t precedent);
- blame: `min(94vw, gutter + N ch)`.

So the half-width rule shipped in fd7322df stays and the cap only narrows
further: at N = 200 on a 1920px screen the half-width viewer (~130 chars) is
already narrower than the cap, blame (94vw, ~200+ chars of text) is the view
that changes. Unlimited = today's rules untouched.

One character = the width of a `0` in the code bodies' font (12px
`ui-monospace, monospace`, not the page's 13px UI font), measured once in the
browser. A classic (non-overlay) vertical scrollbar is added to the box so it
does not eat into N.

Blame's gutter is `clamp(150px, 34%, 300px)` of the box today — circular
once the box is sized from the gutter. It becomes `clamp(150px, 32vw,
300px)`: the same width at today's 94vw box (34% of 94vw), independent of
the box.

## Long lines (w)

Unchanged inside the narrower box, no new rules: wrap wraps at N, cutoff ends
the line at N with `…`, scroll lets the row run past N and the box scrolls
sideways (blame's gutter and line number stay pinned).

## Side effects on screen

- The box is narrower; it stays centred (blame and the viewer are centred
  flex overlays, the F preview has `margin: 0 auto`).
- Titles and hints elide against the narrower box: the viewer title's budget
  (viewer.js paintTitle) already measures the body, so it follows.
- A split, stack or overview doc opened from a capped viewer takes the full
  94vw as today (the `vhalf` class is dropped there).
- Nothing else changes: search, go-to-line, notes, cursor, `d`/`D` recent
  lines.

## Tests

- config: layers table — default 200, global-only, repo-over-global, `-1`
  overlays, `0` ignored; settingDocs entry (`TestSettingDocsCoverAllFields`).
- `/api/health` carries `file_width` (default 200, a repo value, `-1`).
- Source pins over the CSS width rules and the JS that sets the variables.
- Probe (Chromium + Firefox): the text column measured in characters for the
  viewer, the F preview (and `m`), blame — at the default, at a small value
  and at `-1`; screenshots; diff pane / split absent of the cap.
