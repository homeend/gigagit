# TUI Window Framework — Plan 1b (popups onto the primitive) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Retire the last hand-rolled list-popup render paths by routing the
remaining list-bearing popups (content/help, conflict, settings, pairop,
stash-action) through the `renderWindow` primitive, so `z` cycles the display
mode (cutoff/wrap/scroll) and `shift+←/→` pans in every popup — exactly as the
repo switcher popup already does.

**Architecture:** Each popup gets `mode dispMode` + `hscroll int` fields on its
pointer-held struct, its key handler gains the `z` / `shift+←/→` arms copied
from the verified `updateRepoPopupKey` template, and its render function builds
`[]winRow` and calls `renderWindow(wr, winOpts{w, h, mode, anchor: sel,
hscroll})` instead of `strings.Builder` + per-row `truncate`. `modeCutoff` is
the default, so each popup's existing look is unchanged. TUI-only; no
engine/domain/CLI change.

**Tech Stack:** Go 1.26, Bubble Tea (value-receiver `Model`, pointer popup
fields), `internal/tui/window.go` primitive (`renderWindow`, `winRow`,
`winOpts`, `dispMode`), lipgloss styles.

---

## Scope decisions (ratify at the review gate)

This plan implements the deferred remainder of the window-framework spec's
Stage 1 item 5 + item 8 (`docs/superpowers/specs/2026-06-16-tui-window-framework-design.md`).
Two deviations from the spec's literal enumeration — **please confirm before execution**:

1. **Input-form popups excluded (recommend: exclude).** The spec lists
   `branch` and `worktree` under item 5, but its own qualifier is "popups **that
   list rows**". The branch popup is a single editable field; the worktree popup
   is input fields + a live preview; the commit popup is two text fields. None
   has a list of rows where cutoff/wrap/scroll is meaningful, and converting an
   editable field through `renderWindow` adds risk with no display-mode benefit.
   Plan 1b therefore covers only the **list-bearing** popups. (Repo popup was
   already converted in Stage 1a.)

2. **Item 8 "over-popup reveal" dropped (recommend: drop — it's status quo).**
   Item 8 generalizes the panel truncation-tooltip to the focused popup. Stage
   1a already shipped the repo popup with **no** reveal, and the current
   behavior (panel tooltip suppressed while any popup owns focus) is coherent
   with no partial state. With `z`→wrap/scroll a user can already read a cut-off
   row without a separate reveal strip. Dropping item 8 keeps today's behavior —
   it is not a regression. If the user wants the reveal, it becomes its own
   follow-up task (and would also be retrofitted to the repo popup).

If the user rejects either cut, add the corresponding tasks before executing.

## Reference template (already on main — copy this exactly)

`internal/tui/repo_popup.go` is the proven pattern. Key handler (`updateRepoPopupKey`,
lines ~59-80), display-mode arms take precedence over query typing:

```go
case "z":
    p.mode = p.mode.next()
    p.hscroll = 0
case "shift+left":
    if p.mode == modeScroll && p.hscroll > 0 {
        if p.hscroll -= m.hscrollStep(); p.hscroll < 0 {
            p.hscroll = 0
        }
    }
case "shift+right":
    if p.mode == modeScroll {
        p.hscroll += m.hscrollStep()
    }
```

Render (`renderRepoPopup`, builds `[]winRow`, caps height, lets the primitive scroll):

```go
wr := make([]winRow, len(vis))
for i, e := range vis {
    var st lipgloss.Style
    if i == p.sel {
        st = selectedRow
    }
    wr[i] = winRow{text: row, style: st}  // row built WITHOUT pre-truncation
}
h := len(vis)
if h > 12 { h = 12 }
bodyLines := renderWindow(wr, winOpts{w: inner, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll})
```

Struct fields: `mode dispMode` + `hscroll int`. Hint string gains `[z] mode`.
`winRow.text` is the **raw** row (prefix `> ` / `  ` included, no `truncate`);
`renderWindow` truncates/wraps then applies `winRow.style`.

**Primitive API (`window.go`, do not change):** `renderWindow(rows []winRow, o winOpts) []string`
returns exactly `o.h` lines each `o.w` wide; `winOpts{w, h, mode, anchor, hscroll}`
(`anchor` = selected logical row, drives keep-visible scrolling in cutoff/wrap);
`winRow{text string, style lipgloss.Style}`.

---

### Task 1: Content/help popup (`?`, commit-files-in-popup) → renderWindow

**Files:**
- Modify: `internal/tui/content_popup.go` (struct `contentPopup`, `renderContentPopup` ~181-226, the key handler `updateContentPopupKey`)
- Test: `internal/tui/content_popup_test.go`

This is the highest-value conversion: real scrolling (currently `windowRows`),
search, and long rows (help text, commit-file paths). Heading rows carry
`titleStyle`; the cursor style must still win on a heading row.

- [ ] **Step 1: Write the failing test — `z` cycles the content popup's mode**

```go
func TestContentPopupZCyclesMode(t *testing.T) {
	m := Model{width: 100, height: 30}
	m.contentPopup = &contentPopup{title: "Help", lines: []contentLine{{text: "alpha"}, {text: "beta"}}}
	if m.contentPopup.mode != modeCutoff {
		t.Fatalf("default mode = %v, want modeCutoff", m.contentPopup.mode)
	}
	u, _ := m.updateContentPopupKey(keyMsg("z"))
	mm := u.(Model)
	if mm.contentPopup.mode != modeWrap {
		t.Fatalf("after z, mode = %v, want modeWrap", mm.contentPopup.mode)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run TestContentPopupZCyclesMode -v`
Expected: FAIL — `contentPopup` has no `mode` field (compile error) or wrong value.

- [ ] **Step 3: Add `mode`/`hscroll` to the struct and the `z`/`shift+←/→` arms**

In `contentPopup`'s struct definition add:
```go
	mode    dispMode // text display mode; z cycles (cutoff default)
	hscroll int      // modeScroll horizontal offset
```
In `updateContentPopupKey`, BEFORE the `typing`/search handling (so the arms
exist), add — but gate `z` on `!p.typing` so the user can still type "z" into a
search query:
```go
	case "z":
		if !p.typing {
			p.mode = p.mode.next()
			p.hscroll = 0
			return m, nil
		}
	case "shift+left":
		if !p.typing && p.mode == modeScroll && p.hscroll > 0 {
			if p.hscroll -= m.hscrollStep(); p.hscroll < 0 {
				p.hscroll = 0
			}
			return m, nil
		}
	case "shift+right":
		if !p.typing && p.mode == modeScroll {
			p.hscroll += m.hscrollStep()
			return m, nil
		}
```
(Match the existing handler's `switch msg.String()` / key-type style; if it
switches on `msg.Type`, place these in the `KeyRunes`/string fallthrough as the
existing code does. The `return m, nil` mirrors other no-reload arms.)

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/tui/ -run TestContentPopupZCyclesMode -v`
Expected: PASS

- [ ] **Step 5: Convert `renderContentPopup` to `renderWindow`, output unchanged in cutoff**

Replace the `rows := make([]string,...)` + `windowRows(...)` block with a
`[]winRow` build (raw text, no `truncate`) + `renderWindow`:
```go
	vis := p.visible()
	wr := make([]winRow, len(vis))
	for i, l := range vis {
		var st lipgloss.Style
		switch {
		case i == p.sel:
			wr[i] = winRow{text: "> " + l.text, style: selectedRow}
			continue
		case l.heading:
			st = titleStyle
		}
		prefix := "  "
		if l.heading {
			prefix = ""
		}
		wr[i] = winRow{text: prefix + l.text, style: st}
	}
	capRows := m.contentPageRows()
	body := renderWindow(wr, winOpts{w: textW, h: capRows, mode: p.mode, anchor: p.sel, hscroll: p.hscroll})
```
Then assemble title + body + hint with `strings.Join` (keep the `(no match)`
empty case, the `%d/%d` position indicator when `len(vis) > capRows`, and the
search title suffix). Add `[z] mode` to the hint: `"[/] search  [z] mode  [q] close"`.

- [ ] **Step 6: Pin the cutoff-default output is unchanged**

Add a test that a short, non-truncating popup renders the same selected-row text
as before (assert via `ansi.Strip` that `> alpha` appears and no wrap occurs):
```go
func TestContentPopupCutoffRendersRows(t *testing.T) {
	m := Model{width: 100, height: 30}
	m.contentPopup = &contentPopup{title: "Help", lines: []contentLine{{text: "alpha"}, {text: "beta"}}}
	out := ansiStrip(m.renderContentPopup())
	if !strings.Contains(out, "> alpha") || !strings.Contains(out, "beta") {
		t.Fatalf("rows missing:\n%s", out)
	}
}
```
(Reuse the existing ansi-strip helper in the test package; if none, use
`ansi.Strip` from `charmbracelet/x/ansi`.)

- [ ] **Step 7: Run content popup tests + the help drift guard**

Run: `go test ./internal/tui/ -run 'ContentPopup|Help' -v`
Expected: PASS (existing help-window scroll/search/`q`-close tests still green).

- [ ] **Step 8: Commit**

```bash
git add internal/tui/content_popup.go internal/tui/content_popup_test.go
git commit -m "feat(tui): content/help popup renders via renderWindow (z modes)"
```

---

### Task 2: Conflict popup (`x`) → renderWindow

**Files:**
- Modify: `internal/tui/conflict_popup.go` (struct `conflictPopup`, `renderConflictPopup` ~34-60, `updateConflictPopupKey`)
- Test: `internal/tui/conflict_popup_test.go`

File paths + conflict labels can be long; today the popup has **no** scroll, so a
repo with many conflicts overflows. `renderWindow` adds scroll for free.

- [ ] **Step 1: Write the failing test — `z` cycles the conflict popup's mode**

```go
func TestConflictPopupZCyclesMode(t *testing.T) {
	m := Model{width: 100, height: 30}
	m.conflictPopup = &conflictPopup{files: []model.FileStatus{{Path: "a.go", Unstaged: 'U', Staged: 'U', Kind: model.KindUnmerged}}}
	u, _ := m.updateConflictPopupKey(keyMsg("z"))
	mm := u.(Model)
	if mm.conflictPopup.mode != modeWrap {
		t.Fatalf("after z, mode = %v, want modeWrap", mm.conflictPopup.mode)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestConflictPopupZCyclesMode -v`
Expected: FAIL — no `mode` field.

- [ ] **Step 3: Add `mode`/`hscroll` + the three arms to `updateConflictPopupKey`**

Add `mode dispMode` + `hscroll int` to `conflictPopup`. In the key handler add
the `z` / `shift+left` / `shift+right` arms verbatim from the repo template
(this popup has no typing mode, so no `!p.typing` guard). Conflict action keys
(`o`/`t`/`m`/`k`/`d`/`b`/`A`/`c`/`a`) are unaffected — `z` is not among them.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run TestConflictPopupZCyclesMode -v`
Expected: PASS

- [ ] **Step 5: Convert `renderConflictPopup` to `renderWindow`**

Replace the per-file `strings.Builder` loop with a `[]winRow` build + a capped
`renderWindow`, keeping the header (`Resolve conflicts` + `Describe()` source),
the `(all resolved)` empty case, `actionHint()`, and `[esc] close`:
```go
	wr := make([]winRow, len(p.files))
	for i, f := range p.files {
		prefix := "  "
		var st lipgloss.Style
		if i == p.sel {
			prefix, st = "> ", selectedRow
		}
		wr[i] = winRow{text: fmt.Sprintf("%s%s  — %s", prefix, f.Path, f.ConflictLabel()), style: st}
	}
	inner := popupInnerWidth(w)
	h := len(p.files)
	if h > 12 { h = 12 }
	body := renderWindow(wr, winOpts{w: inner, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll})
```
Assemble header + (empty `(all resolved)` OR `body`) + `actionHint()` +
`[esc] close  [z] mode` via `strings.Join`, then `modalStyle.Width(inner).Render`.

- [ ] **Step 6: Run conflict tests**

Run: `go test ./internal/tui/ -run Conflict -v`
Expected: PASS (existing conflict-resolution flow tests green).

- [ ] **Step 7: Commit**

```bash
git add internal/tui/conflict_popup.go internal/tui/conflict_popup_test.go
git commit -m "feat(tui): conflict popup renders via renderWindow (z modes + scroll)"
```

---

### Task 3: Pair-op popup (`m` on a 2nd row) → renderWindow

**Files:**
- Modify: `internal/tui/pairop_popup.go` (struct `pairOpPopup`, `updatePairPopupKey`, `renderPairOpPopup`)
- Test: `internal/tui/pairop_popup_test.go` (create if absent)

Small fixed list (Merge/Rebase/Interactive). Conversion is for uniformity — one
render path for every list popup — not truncation value.

- [ ] **Step 1: Write the failing test**

```go
func TestPairOpPopupZCyclesMode(t *testing.T) {
	m := Model{width: 100, height: 30}
	m.pairPopup = &pairOpPopup{marked: "a", selected: "b", ops: branchPairOps()}
	u, _ := m.updatePairPopupKey(keyMsg("z"))
	mm := u.(Model)
	if mm.pairPopup.mode != modeWrap {
		t.Fatalf("after z, mode = %v, want modeWrap", mm.pairPopup.mode)
	}
}
```
(Use whatever constructor populates `ops`; if `branchPairOps()` is not the real
name, build a one-element `ops` slice inline with an enabled stub op.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestPairOpPopupZCyclesMode -v`
Expected: FAIL — no `mode` field.

- [ ] **Step 3: Add `mode`/`hscroll` + the three arms**

Add `mode dispMode` + `hscroll int` to `pairOpPopup`. In `updatePairPopupKey`'s
`switch msg.String()` add `z` / `shift+left` / `shift+right` from the template
(no typing mode). The `enter`/`esc`/`up`/`down` arms are unchanged.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run TestPairOpPopupZCyclesMode -v`
Expected: PASS

- [ ] **Step 5: Convert `renderPairOpPopup` to `renderWindow`**

```go
	wr := make([]winRow, len(p.ops))
	for i, op := range p.ops {
		line := op.label(p.marked, p.selected)
		if !op.enabled {
			line += "  (" + op.note + ")"
		}
		prefix := "  "
		var st lipgloss.Style
		if i == p.sel {
			prefix, st = "> ", selectedRow
		}
		wr[i] = winRow{text: prefix + line, style: st}
	}
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	body := renderWindow(wr, winOpts{w: inner, h: len(p.ops), mode: p.mode, anchor: p.sel, hscroll: p.hscroll})
	parts := []string{p.marked + " + " + p.selected, ""}
	parts = append(parts, body...)
	parts = append(parts, "", "[↑/↓] choose  [enter] run  [z] mode  [esc] cancel")
	return modalStyle.Width(inner).Render(strings.Join(parts, "\n")) + "\n"
```

- [ ] **Step 6: Run pair-op + mark tests**

Run: `go test ./internal/tui/ -run 'Pair|Mark' -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/tui/pairop_popup.go internal/tui/pairop_popup_test.go
git commit -m "feat(tui): pair-op popup renders via renderWindow (z modes)"
```

---

### Task 4: Settings popup (`,`) → renderWindow

**Files:**
- Modify: `internal/tui/settings_popup.go` (its popup struct, its render fn, its key handler)
- Test: `internal/tui/settings_popup_test.go`

Two list states (menu + agent checkbox picker). Both render row lists; convert
both bodies. Read the file first to learn the exact struct/handler/render names
(it has a menu vs. picker state machine).

- [ ] **Step 1: Write the failing test — `z` cycles mode (in whichever state lists rows)**

```go
func TestSettingsPopupZCyclesMode(t *testing.T) {
	m := newSettingsModel(t) // reuse the existing settings-test constructor
	u, _ := m.updateSettingsPopupKey(keyMsg("z"))
	mm := u.(Model)
	if mm.settingsPopup.mode != modeWrap {
		t.Fatalf("after z, mode = %v, want modeWrap", mm.settingsPopup.mode)
	}
}
```
(Use the real popup field name and key-handler/constructor names from the file;
adjust the test to them.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestSettingsPopupZCyclesMode -v`
Expected: FAIL — no `mode` field.

- [ ] **Step 3: Add `mode`/`hscroll` + the three arms**

Add `mode dispMode` + `hscroll int` to the settings popup struct. Add the `z` /
`shift+left` / `shift+right` arms (no typing mode). `space` (toggle) / `enter` /
`esc` / arrows unchanged.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run TestSettingsPopupZCyclesMode -v`
Expected: PASS

- [ ] **Step 5: Convert both render states to `renderWindow`**

For each list state (menu rows and agent-picker rows), build `[]winRow` (raw
text incl. cursor `> `/`  ` prefix and any checkbox glyph, style `selectedRow`
on the sel row) and call `renderWindow(wr, winOpts{w: inner, h: capped, mode:
p.mode, anchor: p.sel, hscroll: p.hscroll})`. Keep headers/hints; append
`[z] mode` to the hint line.

- [ ] **Step 6: Run settings tests**

Run: `go test ./internal/tui/ -run Settings -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/tui/settings_popup.go internal/tui/settings_popup_test.go
git commit -m "feat(tui): settings popup renders via renderWindow (z modes)"
```

---

### Task 5: Stash-action popup (`enter` on a stash) → renderWindow

**Files:**
- Modify: `internal/tui/stash_view.go` (struct `stashActionPopup`, its render fn, its key handler — opened at `stash_view.go:137`)
- Test: `internal/tui/stash_view_test.go`

Tiny fixed list (apply / pop / drop, with drop→confirm). Convert for uniformity.
Read `stash_view.go` to find the action-popup render fn + key handler names.

- [ ] **Step 1: Write the failing test**

```go
func TestStashActionPopupZCyclesMode(t *testing.T) {
	m := Model{width: 100, height: 30}
	m.stashAction = &stashActionPopup{ref: "stash@{0}", subject: "WIP"}
	u, _ := m.updateStashActionKey(keyMsg("z")) // use the real handler name
	mm := u.(Model)
	if mm.stashAction.mode != modeWrap {
		t.Fatalf("after z, mode = %v, want modeWrap", mm.stashAction.mode)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestStashActionPopupZCyclesMode -v`
Expected: FAIL — no `mode` field.

- [ ] **Step 3: Add `mode`/`hscroll` + the three arms**

Add `mode dispMode` + `hscroll int` to `stashActionPopup`. Add the `z` /
`shift+left` / `shift+right` arms to its key handler. The apply/pop/drop +
confirm arms are unchanged.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run TestStashActionPopupZCyclesMode -v`
Expected: PASS

- [ ] **Step 5: Convert the action-popup render to `renderWindow`**

Build `[]winRow` for the action rows (apply/pop/drop, plus the confirm-state row
if shown), style `selectedRow` on the sel row, call `renderWindow` with the
popup's `mode`/`hscroll`/`anchor: sel`. Keep the subject header and hint; add
`[z] mode`.

- [ ] **Step 6: Run stash tests**

Run: `go test ./internal/tui/ -run Stash -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/tui/stash_view.go internal/tui/stash_view_test.go
git commit -m "feat(tui): stash-action popup renders via renderWindow (z modes)"
```

---

### Task 6: Docs — `z` works in every popup

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `internal/tui/help.go`
- Test: `internal/tui/help_test.go` (drift guard runs as part of the suite)

- [ ] **Step 1: CHANGELOG** — under the window-framework section note that `z`
  (display mode) + `shift+←/→` (pan) now work in the content/help, conflict,
  settings, pair-op, and stash-action popups (completing the Stage 1 popup
  migration; repo popup already shipped in Stage 1a).

- [ ] **Step 2: README** — in the `z` key row, broaden "(panels, stash list,
  files tree, history, blame, repo switcher)" to note it now applies to the
  listed popups too (keep it tight; the footer truncates).

- [ ] **Step 3: help.go** — add `[z] mode` to the relevant popup sections
  (Conflicts, Pair-op popup, Settings, Stash window, Help window) so the
  `TestHelpFooterCoverage` drift guard and the help reference stay in sync.
  Per [[advertise-features-in-help-and-footer]] every advertised key lands in
  both the `?` pane and (where shown) the footer.

- [ ] **Step 4: Run the full suite + drift guard**

Run: `go test ./internal/tui/ -run 'Help|Footer' -v && gofmt -l internal/tui/`
Expected: PASS, no gofmt diffs.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md README.md internal/tui/help.go internal/tui/help_test.go
git commit -m "docs(tui): document z display modes in the converted popups"
```

---

## Final verification

- [ ] `./test.sh race` is green (unit + e2e) before merge.
- [ ] Manual smoke (optional): open each popup, press `z` thrice (cutoff→wrap→scroll→cutoff),
      and `shift+→`/`shift+←` in scroll mode; confirm no panic and the body stays
      inside the box.
- [ ] No hand-rolled list-popup body (`strings.Builder` row loop + `truncate`)
      remains: `grep -n 'truncate(' internal/tui/*popup*.go internal/tui/stash_view.go`
      should show only header/hint truncation, not per-row list truncation.

## Self-review notes

- **Spec coverage:** item 5 popups (minus the input forms by decision 1, minus
  already-done repo) = Tasks 1-5; item 8 dropped by decision 2; docs = Task 6.
- **Type consistency:** every popup uses the same field names `mode dispMode` /
  `hscroll int`, the same three key arms, and `winOpts{w, h, mode, anchor,
  hscroll}` — matching the repo template and the `window.go` API verified on main.
- **No engine/domain/CLI/agentskill change** (TUI-only, FR-4 preserved).
