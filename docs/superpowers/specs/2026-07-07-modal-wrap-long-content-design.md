# Spec: Wrap decision-modal content so essential dynamic text is never clipped

Date: 2026-07-07
Branch: `fix/modal-wrap-content`

## Problem

Merging a branch with a very long name into `main` shows a confirm popup whose
text is cut off — the branch name (the essential thing you need to see to know
what you're about to do) runs off the right edge and is unreadable. This is
common in repos where branches carry multiple authors / long descriptive names.

## Root cause

Every confirm/decision dialog in gg is a `decisionState` "modal" (`m.modal`),
and they all render through a single function: `renderModal()`
(`internal/tui/view.go`). Unlike every other popup in the TUI, `renderModal`
applies **no width cap and no wrapping**:

```go
func (m Model) renderModal() string {
	var b strings.Builder
	b.WriteString(m.modal.req.Prompt)          // may be arbitrarily wide
	b.WriteString("\n\n")
	for i, opt := range m.modal.req.Options {  // options may be arbitrarily wide
		...
	}
	b.WriteString("\n[↑/↓] choose  [enter] confirm  [esc] abort")
	return modalStyle.Render(b.String()) + "\n"
}
```

`modalStyle` (`Border(DoubleBorder) + Padding(1,2)`) sets no `.Width()`, so the
box grows to the width of its widest line. `overlayCenter` then positions it at
`(termW - fgW)/2`; when `fgW > termW` this is negative, `overlayAt` clamps
`left` to 0, and the over-wide line overflows past `termW` — the terminal clips
the right edge. That is the cut-off.

gg's **other** popups (the `.` action menu, Settings, git-config explorer, diff
view, bookmark/shelf/repo switchers) lay out through width-bounded helpers
(`popupBox` / `renderWindow` at `popupTextWidth`) and truncate-or-scroll rather
than overflow, so they do not exhibit this bug.

## Which popups qualify

All qualifying popups are `decisionState` modals; because they share
`renderModal()`, one fix covers every one. Modals that carry essential dynamic
content today:

- **Frontend confirms** (`confirmOp` / `mustConfirmOp`): Merge, Rebase, Pull,
  Switch, Checkout (branch names — the reported case); Reset, Fast-forward
  (short hashes).
- **Engine-driven forks** (rendered through the same modal via `opDecisionMsg`):
  - SmartMerge / SmartRebase / cherry-pick / interactive-rebase conflict prompts
    (branch names).
  - SmartPull worktree-aware decision tree (branch + worktree names & paths in
    options).
  - Delete branch / remote branch / tag (branch/tag/remote names, "not fully
    merged" warning).
  - Export file / dir "already exists" (full file/dir **paths** — often very
    long).
  - Delete-remote-tag "from which remote?" (remote names **in the option list**).
  - Force-push (branch + remote).
  - Post-create-hook approval (the hook **script**, multi-line).

Non-qualifying (already width-safe, out of scope): `.` action menu, Settings,
git-config explorer, diff view, switchers.

## Design

Rewrite `renderModal()` to wrap its content to a terminal-bounded width. Only
long content wraps; short "Yes/No" confirms stay compact (no fullscreen
takeover for tiny dialogs — the goal is "essential content visible", not
"always maximize").

1. **Width budget.** `maxW := m.width - chrome - margin`, floored at a small
   minimum (~24) so a tiny terminal still renders. `chrome` accounts for the
   double border (2) + `modalStyle`'s horizontal padding (4). A couple of extra
   columns of margin keep the box off the screen edge. On a normal 80-col
   terminal this yields ~70 columns of text.

2. **Prompt.** Split on existing newlines (so a multi-line prompt like the hook
   script keeps its line breaks), then word-wrap each physical line to `maxW`
   via the existing `wrapWords` helper (`internal/tui/settings_tools.go`). That
   helper already hard-chunks a single token wider than `maxW` (via
   `wrapWidth`), covering an unbreakable long branch name.

3. **Options.** Word-wrap each option to `maxW - 2` (room for the `> ` / `  `
   selection marker). The first physical line gets the `> `/`  ` marker;
   continuation lines are indented two spaces so they align under the text. The
   selected option's reverse-video highlight (`selectedRow`) is applied to
   every physical line of that option so the highlight is not split.

4. **Footer.** Unchanged: `\n[↑/↓] choose  [enter] confirm  [esc] abort`.

5. The box now sizes to the wrapped content, whose widest line ≤ `maxW` ≤
   terminal width, so `overlayCenter` centers it cleanly and it never clips.

`modalStyle.Render` is still used to frame the assembled, already-wrapped
content (no `.Width()` needed — the content is pre-wrapped, matching how the
prompt/options are the source of truth for box width).

## Testing (TDD)

Extend `internal/tui/modal_test.go`:

- **Long branch merge prompt** at `width=80`: assert every rendered line's
  display width ≤ terminal width, and that the full branch name appears in the
  stripped output (nothing lost to clipping).
- **Long unbreakable token** (a single word wider than the terminal): assert it
  is hard-wrapped and fully present, no line exceeds terminal width.
- **Long option strings** (e.g. a delete-remote-tag remote list, or a long
  export path): each option wrapped; selected-row highlight intact across
  wrapped lines.
- **Regression**: existing short-prompt tests
  (`TestModalRendersPromptAndOptions`, `TestModalEnterSendsSelectedOption`,
  `TestModalEscAborts`, `TestModalRendersCenteredOverInterface`) still pass —
  short prompts unchanged, centering preserved.

## Scope / non-goals

- No change to the maximizable *layer* popups (`popupMax` / `ctrl+t`) — they are
  already width-safe.
- No auto-fullscreen for modals — wrapping is sufficient and less jarring for
  small confirms.
- No new config; behavior is unconditional (a modal that already fit is
  unaffected).
