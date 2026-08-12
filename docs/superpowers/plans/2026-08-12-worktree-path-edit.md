# Editable Worktree Path in the w/W Popup — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user hand-edit the worktree path in the w/W create-worktree popup (new `E` key), with the template-derived path as the default.

**Architecture:** A new popup state `stEditPath` and a `pathOverride` field mirror the existing branch-edit pattern (`stEdit` / `branchOverride`) in `internal/tui/worktree_popup.go`. A confirmed override bypasses the path template in `recompute` (so its `<seq>` counters are not consumed) and is carried verbatim into the engine op via the existing `previewPath`. No engine/domain/CLI/web changes.

**Tech Stack:** Go 1.26, Bubble Tea TUI, project i18n (four bundles: ja/ko/zh/ru), real-git unit tests in `internal/tui`.

**Spec:** `docs/superpowers/specs/2026-08-12-worktree-path-edit-design.md`

## Global Constraints

- Work in the worktree at `.claude/worktrees/worktree-path-edit` on branch `feat/worktree-path-edit`. Prefix every build/test command with `cd /mnt/t/others/gigagit/.claude/worktrees/worktree-path-edit`. Use Write/Edit with the WORKTREE absolute path.
- Every new user-visible TUI string goes through `i18n.T(...)` with a literal key present in ALL FOUR bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`) — `go test ./internal/tui -run TestI18n` gates this.
- TDD: write the failing test first, watch it fail, then implement.
- Scope is the TUI popup only — do NOT touch `internal/cli`, `internal/web`, or `internal/engine`.
- Verified fact: `template.Resolve("", ...)` returns `("", nil)`, so setting `Templates.Path = ""` cleanly bypasses the path template in `worktree.Resolve`.

---

### Task 1: Popup behavior — `stEditPath`, `pathOverride`, seq exclusion

**Files:**
- Modify: `internal/tui/worktree_popup.go`
- Test: `internal/tui/worktree_popup_test.go` (append)

**Interfaces:**
- Consumes: existing `worktreePopup` (`state`, `editBuf textfield`, `branchOverride`, `recompute`, `consumedSeqNames`), test helpers `modelWithConfig`, `keyMsg`, `layerOf[*worktreePopup]`.
- Produces: `stEditPath popupState` const, `pathOverride string` field, `fixedPath() string` method, `E` key handling in `stAction`, updated `recompute` and `consumedSeqNames`. Task 2 relies on `p.state == stEditPath` and `p.editBuf` for rendering.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/worktree_popup_test.go`:

```go
// typeRunes types each string as one rune key into the model.
func typeRunes(t *testing.T, m Model, chars ...string) Model {
	t.Helper()
	for _, ch := range chars {
		updated, _ := m.Update(keyMsg(ch))
		m = updated.(Model)
	}
	return m
}

func TestPopupEditPathModeSticksAcrossBranchEdit(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	tmplPath := layerOf[*worktreePopup](m).previewPath

	updated, _ = m.Update(keyMsg("E"))
	m = updated.(Model)
	p := layerOf[*worktreePopup](m)
	if p.state != stEditPath {
		t.Fatalf("state = %v, want stEditPath", p.state)
	}
	if p.editBuf.Value() != tmplPath {
		t.Fatalf("editBuf = %q, want seeded with the previewed path %q", p.editBuf.Value(), tmplPath)
	}

	// Clear and type a custom path; the live preview follows the buffer.
	for len([]rune(layerOf[*worktreePopup](m).editBuf.Value())) > 0 {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = updated.(Model)
	}
	m = typeRunes(t, m, "m", "y", "/", "w", "t")
	if got := layerOf[*worktreePopup](m).previewPath; got != "my/wt" {
		t.Fatalf("live preview path = %q, want my/wt", got)
	}

	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	p = layerOf[*worktreePopup](m)
	if p.state != stAction {
		t.Fatalf("state = %v, want stAction after enter", p.state)
	}
	if p.pathOverride != "my/wt" || p.previewPath != "my/wt" {
		t.Fatalf("pathOverride/previewPath = %q/%q, want my/wt", p.pathOverride, p.previewPath)
	}

	// Now change the branch name — the edited path must stick verbatim.
	updated, _ = m.Update(keyMsg("e"))
	m = updated.(Model)
	m = typeRunes(t, m, "x")
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	if got := layerOf[*worktreePopup](m).previewPath; got != "my/wt" {
		t.Fatalf("preview path = %q after a branch edit, want the sticky override my/wt", got)
	}
}

func TestPopupEditPathEscDiscards(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	tmplPath := layerOf[*worktreePopup](m).previewPath

	updated, _ = m.Update(keyMsg("E"))
	m = updated.(Model)
	m = typeRunes(t, m, "z", "z")
	updated, _ = m.Update(keyMsg("esc"))
	m = updated.(Model)
	p := layerOf[*worktreePopup](m)
	if p.state != stAction {
		t.Fatalf("state = %v, want stAction after esc", p.state)
	}
	if p.pathOverride != "" || p.previewPath != tmplPath {
		t.Fatalf("override/preview = %q/%q, want discarded edit (\"\"/%q)", p.pathOverride, p.previewPath, tmplPath)
	}
}

func TestPopupEditPathEmptyConfirmReverts(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	tmplPath := layerOf[*worktreePopup](m).previewPath

	// Confirm a custom path first.
	updated, _ = m.Update(keyMsg("E"))
	m = updated.(Model)
	for len([]rune(layerOf[*worktreePopup](m).editBuf.Value())) > 0 {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = updated.(Model)
	}
	m = typeRunes(t, m, "q")
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	if layerOf[*worktreePopup](m).pathOverride != "q" {
		t.Fatalf("pathOverride = %q, want q", layerOf[*worktreePopup](m).pathOverride)
	}

	// Re-edit, clear to empty, confirm: back to the template path.
	updated, _ = m.Update(keyMsg("E"))
	m = updated.(Model)
	for len([]rune(layerOf[*worktreePopup](m).editBuf.Value())) > 0 {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = updated.(Model)
	}
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	p := layerOf[*worktreePopup](m)
	if p.pathOverride != "" || p.previewPath != tmplPath {
		t.Fatalf("override/preview = %q/%q, want reverted to template (\"\"/%q)", p.pathOverride, p.previewPath, tmplPath)
	}
}

func TestCreateOpCarriesPathOverride(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("E"))
	m = updated.(Model)
	for len([]rune(layerOf[*worktreePopup](m).editBuf.Value())) > 0 {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = updated.(Model)
	}
	m = typeRunes(t, m, "o", "v")
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	p := layerOf[*worktreePopup](m)
	op, ok := p.createOp("").(engine.CreateWorktreeForBranch)
	if !ok {
		t.Fatalf("createOp = %T, want CreateWorktreeForBranch (unedited branch)", p.createOp(""))
	}
	if op.Path != "ov" {
		t.Fatalf("op.Path = %q, want the overridden ov", op.Path)
	}
}

func TestConsumedSeqNamesWithPathOverride(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "wt-<seq:wt>/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	p := layerOf[*worktreePopup](m)
	if got := p.consumedSeqNames(); len(got) != 1 || got[0] != "wt" {
		t.Fatalf("consumedSeqNames = %v before override, want [wt]", got)
	}

	updated, _ = m.Update(keyMsg("E"))
	m = updated.(Model)
	for len([]rune(layerOf[*worktreePopup](m).editBuf.Value())) > 0 {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = updated.(Model)
	}
	m = typeRunes(t, m, "z")
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	p = layerOf[*worktreePopup](m)
	if got := p.consumedSeqNames(); len(got) != 0 {
		t.Fatalf("consumedSeqNames = %v with the path template bypassed, want none", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/worktree-path-edit && go test ./internal/tui -run 'TestPopupEditPath|TestCreateOpCarriesPathOverride|TestConsumedSeqNamesWithPathOverride' -count=1`
Expected: compile FAILURE — `undefined: stEditPath`, `p.pathOverride undefined`.

- [ ] **Step 3: Implement the popup behavior**

In `internal/tui/worktree_popup.go`:

3a. Add the state constant:

```go
const (
	stInput    popupState = iota // collecting <user:LABEL> field values
	stAction                     // preview shown; choose create / edit / cancel
	stEdit                       // free-editing the resolved branch name
	stEditPath                   // free-editing the resolved worktree path
)
```

3b. Add the field to `worktreePopup` (right after `branchOverride`):

```go
	pathOverride   string    // a confirmed hand-edited worktree path; "" = use the template
```

3c. Add `fixedPath` (after `fixedBranch`):

```go
// fixedPath returns the verbatim worktree path when one is fixed: the live
// buffer while editing the path, or a confirmed hand-edit. "" means the path
// template applies — an emptied buffer previews (and, once confirmed,
// reverts to) the template result.
func (p *worktreePopup) fixedPath() string {
	if p.state == stEditPath {
		return p.editBuf.Value()
	}
	return p.pathOverride
}
```

3d. Rework `recompute` so a fixed path bypasses the path template (its `<seq>`s must not resolve — and later not bump):

```go
func (p *worktreePopup) recompute() {
	fixed := p.fixedBranch()
	tm := worktree.Templates{Branch: p.branchTmpl, Path: p.pathTmpl}
	fp := p.fixedPath()
	if fp != "" {
		tm.Path = "" // hand-edited path: the template (and its <seq>s) is bypassed
	}
	vals := make(map[string]string, len(p.inputs))
	for l, f := range p.inputs {
		vals[l] = f.Value()
	}
	p.previewBranch, p.previewPath, p.previewErr = worktree.Resolve(tm, fixed, vals, p.tctx())
	if fp != "" {
		p.previewPath = fp
	}
}
```

3e. In `update`, add the `stEditPath` case (after the `stEdit` case):

```go
	case stEditPath:
		switch msg.Type {
		case tea.KeyEnter:
			p.pathOverride = p.editBuf.Value() // "" reverts to the template path
			p.state = stAction
			p.recompute()
		case tea.KeyEsc:
			p.state = stAction
			p.recompute()
		default:
			if p.editBuf.HandleEditKey(msg) {
				p.recompute()
			}
		}
		return m, nil
```

3f. In the `stAction` case of `update`, add the `E` key (next to `case "e":`):

```go
		case "E": // edit the worktree path; a confirmed edit sticks verbatim
			p.editBuf = newTextField(p.previewPath)
			p.state = stEditPath
			p.recompute()
```

3g. Rework `consumedSeqNames` — each templated side contributes its own `<seq>` names; a bypassed side contributes none:

```go
// consumedSeqNames returns the <seq> counters the created names actually used.
// A bypassed template contributes nothing: in existing mode and after a
// confirmed branch edit the branch template is skipped; after a confirmed
// path edit the path template is skipped. A chosen prefix's own <seq> names
// are always unioned in.
func (p *worktreePopup) consumedSeqNames() []string {
	branchTemplated := !p.existing && p.branchOverride == ""
	pathTemplated := p.pathOverride == ""
	var base []string
	switch {
	case branchTemplated && pathTemplated:
		base = p.seqNames
	case branchTemplated:
		base = worktree.Templates{Branch: p.branchTmpl}.SeqNames()
	case pathTemplated:
		base = worktree.Templates{Path: p.pathTmpl}.SeqNames()
	}
	return appendDistinctAll(base, p.prefixSeqNames)
}
```

- [ ] **Step 4: Run the new tests to verify they pass, then the whole package**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/worktree-path-edit && go test ./internal/tui -run 'TestPopupEditPath|TestCreateOpCarriesPathOverride|TestConsumedSeqNamesWithPathOverride' -count=1 && go test ./internal/tui -count=1`
Expected: new tests PASS; full package PASS except the i18n scan test may NOT fail yet (no new strings were added in this task — rendering strings come in Task 2). If `TestI18n...` fails here, something in this task added a user-visible literal it shouldn't have.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/worktree-path-edit
git add internal/tui/worktree_popup.go internal/tui/worktree_popup_test.go
git commit -m "feat(tui): editable worktree path in the w/W popup (E key)

A confirmed hand-edit becomes pathOverride: it wins over the path
template (whose <seq> counters are then not consumed) and sticks
verbatim across later branch-name changes. Empty confirm reverts to
the template path."
```

---

### Task 2: Rendering, key hints, and i18n bundles

**Files:**
- Modify: `internal/tui/worktree_popup.go` (`box` only)
- Modify: `internal/i18n/lang/ja.toml`, `internal/i18n/lang/ko.toml`, `internal/i18n/lang/zh.toml`, `internal/i18n/lang/ru.toml`
- Test: `internal/tui/worktree_popup_test.go` (append)

**Interfaces:**
- Consumes: `stEditPath`, `p.editBuf` from Task 1; existing `viewField`, `i18n.T`.
- Produces: user-visible path-edit field + `[E] edit path` hints. No API for later tasks.

- [ ] **Step 1: Write the failing render test**

Append to `internal/tui/worktree_popup_test.go`:

```go
func TestRenderWorktreePopupPathEdit(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	if v := layerOf[*worktreePopup](m).box(m); !contains(v, "[E] edit path") {
		t.Fatalf("stAction hint should advertise [E] edit path, got:\n%s", v)
	}
	updated, _ = m.Update(keyMsg("E"))
	m = updated.(Model)
	v := layerOf[*worktreePopup](m).box(m)
	if !contains(v, "[enter] done") || !contains(v, "edit path") {
		t.Fatalf("stEditPath should show its own hint line, got:\n%s", v)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/worktree-path-edit && go test ./internal/tui -run TestRenderWorktreePopupPathEdit -count=1`
Expected: FAIL — the hint does not yet contain `[E] edit path`.

- [ ] **Step 3: Implement rendering + hints in `box`**

In `internal/tui/worktree_popup.go`, `box`:

3a. Replace the fixed `path:` line with a state-aware one:

```go
	if p.state == stEditPath {
		b.WriteString(viewField(i18n.T("path:   "), p.editBuf, true, cw) + "\n")
	} else {
		b.WriteString(i18n.T("path:   ") + p.previewPath + "\n")
	}
```

(The `branch:` line above it is unchanged — in `stEditPath` it renders the preview branch.)

3b. Show the keep-mode line in `stEditPath` too (the `[m] change` hint already stays stAction-only):

```go
	if (p.state == stAction || p.state == stEdit || p.state == stEditPath) && p.keepOffered {
```

Also update the comment above the `[m]` hint to say "In stEdit/stEditPath, "m" types into the edited field".

3c. Add the `stEditPath` hint case to the trailing `switch p.state`:

```go
	case stEditPath:
		b.WriteString(i18n.T("[type] edit path  [enter] done  [esc] discard"))
```

3d. Insert `[E] edit path` into all four stAction hint variants (the English keys change — the bundles are updated in Step 4):

```go
		hint := i18n.T("[w] create  [W] create & switch  [e] edit name  [E] edit path  [p] use a prefix  [esc] cancel")
		if p.switchOnCreate {
			hint = i18n.T("[enter/W] create & switch  [w] create only  [e] edit name  [E] edit path  [p] use a prefix  [esc] cancel")
			if m.cfg.Worktree.PostCreateHook != "" {
				hint = i18n.T("[enter/W] create & switch  [w] create only  [e] edit name  [E] edit path  [p] use a prefix  [h] hook  [esc] cancel")
			}
		} else if m.cfg.Worktree.PostCreateHook != "" {
			hint = i18n.T("[w] create  [W] create & switch  [e] edit name  [E] edit path  [p] use a prefix  [h] hook  [esc] cancel")
		}
```

- [ ] **Step 4: Update the four bundles**

In each of `internal/i18n/lang/{ja,ko,zh,ru}.toml`, EDIT IN PLACE the four existing stAction hint entries (around line 220; the key AND the value gain the `[E]` segment) and ADD one new entry next to the existing `"[type] edit name …"` key (~line 215). Exact entries:

**ja.toml:**
```toml
"[type] edit path  [enter] done  [esc] discard" = "[type] パスを編集  [enter] 完了  [esc] 破棄"
"[w] create  [W] create & switch  [e] edit name  [E] edit path  [p] use a prefix  [esc] cancel" = "[w] 作成  [W] 作成して切替  [e] 名前編集  [E] パス編集  [p] 接頭辞使用  [esc] キャンセル"
"[w] create  [W] create & switch  [e] edit name  [E] edit path  [p] use a prefix  [h] hook  [esc] cancel" = "[w] 作成  [W] 作成して切替  [e] 名前編集  [E] パス編集  [p] 接頭辞使用  [h] フック  [esc] キャンセル"
"[enter/W] create & switch  [w] create only  [e] edit name  [E] edit path  [p] use a prefix  [esc] cancel" = "[enter/W] 作成して切替  [w] 作成のみ  [e] 名前編集  [E] パス編集  [p] 接頭辞使用  [esc] キャンセル"
"[enter/W] create & switch  [w] create only  [e] edit name  [E] edit path  [p] use a prefix  [h] hook  [esc] cancel" = "[enter/W] 作成して切替  [w] 作成のみ  [e] 名前編集  [E] パス編集  [p] 接頭辞使用  [h] フック  [esc] キャンセル"
```

**ko.toml:**
```toml
"[type] edit path  [enter] done  [esc] discard" = "[type] 경로 편집  [enter] 완료  [esc] 취소"
"[w] create  [W] create & switch  [e] edit name  [E] edit path  [p] use a prefix  [esc] cancel" = "[w] 생성  [W] 생성 후 전환  [e] 이름 편집  [E] 경로 편집  [p] 접두사 사용  [esc] 취소"
"[w] create  [W] create & switch  [e] edit name  [E] edit path  [p] use a prefix  [h] hook  [esc] cancel" = "[w] 생성  [W] 생성 후 전환  [e] 이름 편집  [E] 경로 편집  [p] 접두사 사용  [h] 훅  [esc] 취소"
"[enter/W] create & switch  [w] create only  [e] edit name  [E] edit path  [p] use a prefix  [esc] cancel" = "[enter/W] 생성 후 전환  [w] 생성만  [e] 이름 편집  [E] 경로 편집  [p] 접두사 사용  [esc] 취소"
"[enter/W] create & switch  [w] create only  [e] edit name  [E] edit path  [p] use a prefix  [h] hook  [esc] cancel" = "[enter/W] 생성 후 전환  [w] 생성만  [e] 이름 편집  [E] 경로 편집  [p] 접두사 사용  [h] 훅  [esc] 취소"
```

**zh.toml:**
```toml
"[type] edit path  [enter] done  [esc] discard" = "[type] 编辑路径  [enter] 完成  [esc] 丢弃"
"[w] create  [W] create & switch  [e] edit name  [E] edit path  [p] use a prefix  [esc] cancel" = "[w] 创建  [W] 创建并切换  [e] 编辑名称  [E] 编辑路径  [p] 使用前缀  [esc] 取消"
"[w] create  [W] create & switch  [e] edit name  [E] edit path  [p] use a prefix  [h] hook  [esc] cancel" = "[w] 创建  [W] 创建并切换  [e] 编辑名称  [E] 编辑路径  [p] 使用前缀  [h] 钩子  [esc] 取消"
"[enter/W] create & switch  [w] create only  [e] edit name  [E] edit path  [p] use a prefix  [esc] cancel" = "[enter/W] 创建并切换  [w] 仅创建  [e] 编辑名称  [E] 编辑路径  [p] 使用前缀  [esc] 取消"
"[enter/W] create & switch  [w] create only  [e] edit name  [E] edit path  [p] use a prefix  [h] hook  [esc] cancel" = "[enter/W] 创建并切换  [w] 仅创建  [e] 编辑名称  [E] 编辑路径  [p] 使用前缀  [h] 钩子  [esc] 取消"
```

**ru.toml:**
```toml
"[type] edit path  [enter] done  [esc] discard" = "[type] изменить путь  [enter] готово  [esc] отменить"
"[w] create  [W] create & switch  [e] edit name  [E] edit path  [p] use a prefix  [esc] cancel" = "[w] создать  [W] создать и переключить  [e] изменить имя  [E] изменить путь  [p] использовать префикс  [esc] отмена"
"[w] create  [W] create & switch  [e] edit name  [E] edit path  [p] use a prefix  [h] hook  [esc] cancel" = "[w] создать  [W] создать и переключить  [e] изменить имя  [E] изменить путь  [p] использовать префикс  [h] хук  [esc] отмена"
"[enter/W] create & switch  [w] create only  [e] edit name  [E] edit path  [p] use a prefix  [esc] cancel" = "[enter/W] создать и переключить  [w] только создать  [e] изменить имя  [E] изменить путь  [p] использовать префикс  [esc] отмена"
"[enter/W] create & switch  [w] create only  [e] edit name  [E] edit path  [p] use a prefix  [h] hook  [esc] cancel" = "[enter/W] создать и переключить  [w] только создать  [e] изменить имя  [E] изменить путь  [p] использовать префикс  [h] хук  [esc] отмена"
```

The four OLD keys (without `[E] edit path`) must be REMOVED from each bundle (they are edited in place, so nothing should remain matching `"[w] create  [W] create & switch  [e] edit name  [p]"`).

- [ ] **Step 5: Run the render test + the i18n gates**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/worktree-path-edit && go test ./internal/tui -run 'TestRenderWorktreePopupPathEdit|TestI18n' -count=1 && go test ./internal/i18n/... -count=1`
Expected: PASS. If an i18n scan test fails, its message names the missing key/bundle — fix the named bundle, don't guess.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/worktree-path-edit
git add internal/tui/worktree_popup.go internal/tui/worktree_popup_test.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml
git commit -m "feat(tui): render path editing in the worktree popup + i18n (4 bundles)"
```

---

### Task 3: Help window row, docs, full gates

**Files:**
- Modify: `internal/tui/help.go` (the "Worktree popup (w/W)" section, ~line 174)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (one new entry each)
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: the `r(key, text)` / `h(title)` row helpers already used throughout `help.go`.
- Produces: nothing downstream — final task.

- [ ] **Step 1: Add the help row**

In `internal/tui/help.go`, in the `Worktree popup (w/W)` section, directly after the existing `r("e", ...)` row, add:

```go
		r("E", i18n.T("edit the worktree path — a confirmed edit is used verbatim (empty reverts to the template path); relative paths anchor at the main worktree root")),
```

- [ ] **Step 2: Add the help-row key to all four bundles**

Next to the other help-text entries for the worktree popup in each bundle (search for `"edit the branch name` to find the spot):

**ja.toml:**
```toml
"edit the worktree path — a confirmed edit is used verbatim (empty reverts to the template path); relative paths anchor at the main worktree root" = "ワークツリーのパスを編集 — 確定した編集はそのまま使われる（空にするとテンプレートのパスに戻る）。相対パスはメインワークツリーのルート基準"
```

**ko.toml:**
```toml
"edit the worktree path — a confirmed edit is used verbatim (empty reverts to the template path); relative paths anchor at the main worktree root" = "워크트리 경로 편집 — 확정한 편집은 그대로 사용됨(비우면 템플릿 경로로 복귀). 상대 경로는 메인 워크트리 루트 기준"
```

**zh.toml:**
```toml
"edit the worktree path — a confirmed edit is used verbatim (empty reverts to the template path); relative paths anchor at the main worktree root" = "编辑工作树路径 — 确认后的编辑将原样使用（留空则恢复为模板路径）。相对路径以主工作树根目录为基准"
```

**ru.toml:**
```toml
"edit the worktree path — a confirmed edit is used verbatim (empty reverts to the template path); relative paths anchor at the main worktree root" = "изменить путь рабочего дерева — подтверждённый ввод используется как есть (пустое значение возвращает путь из шаблона); относительные пути отсчитываются от корня основного рабочего дерева"
```

- [ ] **Step 3: CHANGELOG entry**

Add under the Unreleased/newest section of `CHANGELOG.md`, matching the file's existing entry style:

```markdown
- Worktree popup (w/W): `E` edits the worktree path. A confirmed edit is used
  verbatim (relative paths anchor at the main worktree root, absolute paths
  as-is) and sticks across later branch-name changes; confirming an empty
  field reverts to the template-derived path. Hand-edited paths do not bump
  the path template's `<seq>` counters.
```

Do NOT touch `README.md` unless it enumerates worktree-popup keys (check with `grep -n "edit name" README.md`; if no hit, skip). Do NOT touch `internal/agentskill/using-gg.md` — the CLI surface did not change.

- [ ] **Step 4: Run the full staged gate**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/worktree-path-edit && ./test.sh`
Expected: vet+gofmt clean, unit tests PASS, e2e PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/worktree-path-edit
git add internal/tui/help.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml CHANGELOG.md
git commit -m "docs(tui): help row + changelog for worktree-path editing"
```

---

## Final verification (after all tasks)

- `cd /mnt/t/others/gigagit/.claude/worktrees/worktree-path-edit && ./test.sh race` on a quiet machine before merge (per repo convention).
- Optional visual check: `./tui-capture.sh` keyscript driving `w` → `E` → typed path → preview shows it (driving-tui-headless skill).
- The human merges (`feat/worktree-path-edit` → `main`); do not merge unprompted.
