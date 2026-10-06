# Agent console: mouse pass-through, scroll mode, selection & copy — design

Date: 2026-10-06 · Branch: `feat/console-mouse-scroll` · Re-opens the parked
"console copy/select" candidate (2026-09-28).

## Goal

In the TUI's agent / terminal console the user can **scroll back through what
the program produced** and **select and copy part of it** with the mouse and
the keyboard — for Claude Code in fullscreen and in its normal mode, Junie,
and `mc` (or any other program) in the Open-terminal console. The wheel works
by **hover**: over the console it scrolls the console, whatever panel has the
keyboard focus.

Out of scope: the web console (its per-frame `innerHTML` rebuild and ctrl+c
routing are separate blockers; it can reuse the `agentsession` core later),
hover-only mouse motion (Bubble Tea runs in cell-motion mode), soft-wrap
joining on copy.

## Findings (probes, 2026-10-06)

- gg already runs a real terminal emulator (`charmbracelet/x/vt`, 10k-line
  scrollback) per session — the same model as herdr (the server owns the PTY
  and the parsed screen, the client renders it). The gap is pure UI: the
  console draws only the live screen and `handleMouse` swallows every event
  over it except the focusing left click (`internal/tui/mouse.go`).
- x/vt scrollback fills as lines scroll off the top (100 lines on 10 rows →
  91); Ink-style in-place redraws add nothing; `ESC[2J` **pushes** the screen
  into scrollback (xterm does not); `ESC[3J` wipes it; the alt screen has
  none.
- Claude Code 2.1.292 with `"tui": "fullscreen"` (the user's setting) enters
  the **alt screen** (`?1049h`) and enables mouse tracking `?1000h ?1002h
  ?1003h ?1006h`. It redraws with `ESC[2J` on resize, never `ESC[3J`. Fed SGR
  mouse drags through the PTY, it **selects and copies via OSC 52**
  (`ESC]52;c;<base64>BEL`), one per drag. x/vt ignores OSC 52 and gg's
  `oscFilter` records only titles/progress, so today those copies are lost.
- x/vt's `SendMouse` encodes per the child's active mouse mode (X10 or SGR)
  and is a no-op when no tracking mode is set; `Callbacks.EnableMode` /
  `DisableMode` / `AltScreen` report mode changes.
- Bubble Tea v1 cannot report shift+PgUp (and Windows Terminal takes it for
  its own scrollback); alt+PgUp arrives as `KeyPgUp` with `Alt`.

## Reference: herdr (`~/others/herdr` @ `5da0a01e`)

Same routing order: any mouse tracking mode → report to the app; alt screen
→ arrow keys; else its own scrollback, 3 rows per notch
(`src/pane/terminal.rs:1968`, `src/server/pane_input.rs:128`). Its scrollback
is a live offset (libghostty keeps it stable), left by any key; copy-on-release
by default; double-click word (350 ms), no triple-click; wheel during a drag
extends the selection; child OSC 52 forwarded to the foreground client only
(cap 192 KiB); copied text trims trailing blanks and skips wide-glyph halves
(it also joins soft wraps — x/vt cannot). Deliberate differences here:

- **Alternate scroll** for every alt-screen program without tracking (as
  Windows Terminal does by default), not only after `?1007h` as herdr/xterm.
- **Hover wheel does not focus** the console (user ruling); herdr focuses
  the hovered pane first.
- **Frozen snapshot + cursor row + space/space/enter** instead of herdr's
  separate vi copy mode — gg's existing line-selection grammar.
- Triple-click selects a row.

## Design

### 1. Routing — who gets a mouse event over the console

Every mouse event whose cell lies inside the console box (docked or
maximised), focused or not, is routed by the program's **live** modes:

| Program state | Wheel | Press / drag / release |
|---|---|---|
| Mouse tracking on (fullscreen Claude, mc) | forwarded | forwarded; a press on an unfocused console also focuses it (tmux) |
| Normal screen, no tracking (normal Claude, Junie, a shell) | gg scroll mode (§2) | in scroll mode: gg character selection; at the live view a drag enters scroll mode frozen at the current screen and starts the selection |
| Alt screen, no tracking (less, …) | ↑/↓ keys, 3 per notch (xterm "alternate scroll") | press focuses only |

- Coordinates: the event cell minus the box origin, the border + padding and
  the title row → a 0-based cell in the console's inner area. Events on the
  border or title row are not forwarded (a press there still focuses). A cell
  beyond the emulator's width/height (the PTY can be a different size from
  the box while another viewer holds it) is clamped.
- Forwarded events become `vt.MouseClick` / `MouseRelease` / `MouseMotion` /
  `MouseWheel` with the Bubble Tea modifiers; motion is only ever seen while
  a button is held.
- A wheel over the console never scrolls the hidden Commits list (the current
  "swallow" rule stays for every event the router does not use).
- The router runs only when no layer/popup/action menu is above the console —
  the same precedence the keyboard has (`updateConsoleKey`).

### 2. Scroll mode (normal-screen programs)

**Enter:** wheel up over the console; alt+PgUp on a focused console; PgUp on
an unfocused docked console while the Commits column has the focus; a drag
at the live view. Allowed on an exited session (its last screen and history
stay readable).

**Freeze:** entering takes one `History` snapshot (scrollback + screen). The
session keeps running and its live screen keeps updating underneath; the
view does not move. The title row reads `scroll ↑ <top>/<total>` and adds
`· new output` once the live line count has grown past the snapshot's. A
resize of the console (box or PTY) leaves scroll mode — the snapshot is the
old width.

**Cursor row:** scroll mode shows a cursor row in the preview's cursor style.
↑/↓ move it (the view follows), PgUp/PgDn a page, Home/End and g/G the
top/bottom. The wheel moves the view 3 rows per notch and drags the cursor
only when it would leave the view.

**Keyboard selection:** space/space/enter — the shared `lineSel` over
snapshot row indexes: the first space starts a range following the cursor,
the second freezes its end, enter copies the lines and clears.

**Mouse selection:** press + drag selects a character stream (terminal
style: from the press cell to the drag cell, whole rows in between); release
copies. Double-click selects a word (a run of non-space cells), triple-click
the row. Dragging above the first / below the last visible row scrolls one
row per motion event; the wheel while the button is held scrolls and extends
the selection (herdr). The selection stays highlighted until the next press,
a key, or leaving.

**Copy text:** cell contents only (no styles); the right half of a wide
glyph (empty content) is skipped; trailing blanks trimmed per row; every row
ends with `\n` (x/vt keeps no soft-wrap flag); the final newline dropped.
Copies go through the TUI's existing clipboard command (`m.clipWrite` =
`clipboard.Copy`: native command first, OSC 52 fallback) and report
`copied N lines` in the status bar; a failure uses the existing copy-failure
notice.

**Leave:**
- esc clears a selection if there is one, otherwise leaves; q leaves. Neither
  is ever sent to the agent.
- Scrolling (wheel or keys) past the bottom leaves.
- alt+a / alt+t / the step-out key keep their meaning and leave.
- Any other key leaves **and** is handled as usual: sent to the agent on a
  focused console, a gg key on an unfocused one.
- The session disappearing leaves.

**Discoverability:** the title-row hint while scrolling becomes
`esc leave · space select · enter copy`; help and footer advertise wheel /
alt+PgUp scrolling and drag-to-copy on the console. Every string through
`i18n.T`, present in all four bundles.

### 3. OSC 52 from the program

`oscFilter` also buffers OSC 52 payloads (`52;<selection>;<data>`), capped at
1 MiB of base64; a payload over the cap is dropped and flagged. A `?` data
(clipboard read request) is ignored — gg never answers it. A complete payload
is base64-decoded and stored on the session with a sequence number; the
existing screen signal wakes subscribers. The TUI's console, on each screen
wake, takes any clipboard text newer than the sequence it last saw and writes
it through `m.clipWrite` (status `copied N lines`; over-cap → `copy too large
— dropped`). Only the shown console's session is consumed (a copy can only
come from a user's mouse/keys in that console).

### 4. Code layout

**`internal/agentsession`**
- `session.go`: `EnableMode` / `DisableMode` / `AltScreen` callbacks keep
  atomic flags; `Session.Input() InputModes{Mouse, AltScreen bool}`.
- `io.go`: `SendMouse(ev vt.Mouse)` under `ioMu` (no-op unless running);
  `History() History` under `ioMu`.
- `history.go` (new): `History{lines []uv.Line; screenStart, width int;
  liveTotal int}`. Scrollback lines are cloned by x/vt on push and never
  mutated afterwards, so the snapshot copies only the line-header slice;
  screen rows are cloned. `Len()`, `Row(i) string` (ANSI, rendered lazily per
  visible row), `Text(r0, c0, r1, c1) string` (copy rules above),
  `Cells(i)` for hit-testing (wide-glyph aware). `Session.LineTotal()` =
  scrollback + screen rows for the "new output" check.
- `oscfilter.go`: OSC 52 buffering + `clip []byte, clipSeq int, clipOver
  bool`; `Session.Clipboard(after int) (text string, seq int, over bool)`.

**`internal/domain`**: aliases/forwarders like `SessionKey` —
`SessionMouse`, `SessionHistory`, `SessionInputModes`, and the accessors on
`AgentSession`.

**`internal/tui`**
- `console_mouse.go` (new): the routing table, box→cell translation, click
  counting (double/triple within 400 ms on the same cell), alternate scroll.
- `console_scroll.go` (new): `consoleScroll{hist, top, cursor, sel lineSel,
  drag charSel}` on `consoleState`; enter/leave; keys; render (selected cells
  re-drawn with `selectionStyle`, cursor row with the preview cursor style).
- `console.go`: `renderConsole` draws from the snapshot when scrolling; the
  screen wake consumes OSC 52; `updateConsoleKey` hands keys to scroll mode
  first.
- `mouse.go`: the console branch calls the router instead of swallowing.

### 5. Errors & edge cases

- Clipboard failure → existing copy-failure notice; OSC 52 over cap →
  `copy too large — dropped`.
- A mode switch while scrolling (the program enters the alt screen or turns
  tracking on) leaves scroll mode on the next wheel/press, which then routes
  by the new mode.
- A console replaced (alt+a cycle, repo switch, close) drops its scroll
  state with it.
- Events while a popup/menu is above the console are not routed.

### 6. Testing (TDD)

- `agentsession` with real `sh -c printf` children: mode flags follow
  `?1000h/l`, `?1049h/l`; `SendMouse` bytes the child receives (via `od`) in
  SGR and X10; no bytes with tracking off; OSC 52 capture, split across reads,
  over-cap, `?` ignored, non-ASCII stripped payload irrelevant (base64 is
  ASCII); `History` stays frozen while output continues; `Text` rules (wide
  glyph, trailing blanks, rows).
- `tui`: routing table (tracking / normal / alt-no-tracking × focused /
  unfocused-hover); coordinate translation incl. border/title and clamping;
  scroll keys; esc never reaches the agent; a typed key leaves and forwards;
  line and character selection copy through a recording `clipWrite`;
  double/triple click; OSC 52 → `clipWrite`; i18n gates.
- e2e: one golden screen of scroll mode with a selection.
- Manual before merge: real Claude Code in fullscreen and normal mode,
  Junie, mc in Open terminal — wheel, drag-copy, keyboard copy, paste the
  clipboard elsewhere.

### 7. Stages

1. **Pass-through + OSC 52** (§1 rows 1 and 3, §3) — fullscreen Claude and mc
   fully usable. ≈ 1–1.5 days.
2. **Scroll mode + selection** (§1 row 2, §2) — normal Claude, Junie,
   shells. ≈ 3–4 days.

Both stages on this branch, committed separately; the human merges.
