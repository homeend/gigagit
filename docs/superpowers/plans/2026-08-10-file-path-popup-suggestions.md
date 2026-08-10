# File-Path Popup Fuzzy Suggestions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When the palette's File history / File blame popup gets a path that isn't an exact tracked file, switch to an inline fuzzy-suggestion list (escape-hatch "open as typed" row + ranked tracked-file matches) instead of dead-ending.

**Architecture:** All changes live in `internal/tui/file_path_popup.go` (+ its test file, `internal/tui/model.go` routing, and the four i18n bundles). The popup gains an async `LsFiles` load (distinct `filePathLsMsg`, collision-free with the F finder's `lsFilesMsg`), an exact-match set, and a `suggesting` mode whose selection index is `0 = open-as-typed escape row, 1..N = fuzzy matches`. Reuses `fuzzy.Rank` and `renderWindow` as-is.

**Tech Stack:** Go 1.26, Bubble Tea (Elm-style value-receiver `Model`), `internal/fuzzy`, `internal/i18n` (English-text-as-key TOML bundles).

**Spec:** `docs/superpowers/specs/2026-08-10-file-path-popup-suggestions-design.md`

## Global Constraints

- **Worktree:** ALL work happens in `/mnt/t/others/gigagit.worktrees/feat-file-path-suggestions` on branch `feat/file-path-suggestions`. Subagents start in the main checkout — every task begins with `cd /mnt/t/others/gigagit.worktrees/feat-file-path-suggestions` and a `git branch --show-current` check; every Write/Edit uses the worktree-absolute path. NEVER edit files under `/mnt/t/others/gigagit/` (the shared checkout).
- **i18n:** every new user-visible TUI string goes through `i18n.T` with a LITERAL key, and that exact key must be added to ALL FOUR bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`) in the same task — the AST-gate tests (`i18n_scan_test.go`) fail otherwise. The loading literal is exactly `"  (loading…)"` (two leading spaces) — it already exists in all four bundles; do not add a variant.
- **TUI `Model` is a value receiver**; the popup is a pointer layer (state persists via the pointer).
- **TDD:** each task writes the failing test first, sees it fail, implements, sees it pass.
- **Fuzzy query is the NORMALIZED path** (`repoRelPath(m.currentWorktree, p.input.Value())`), never the raw input — tracked paths are slash-normalized and `fuzzy.Score` is a byte subsequence matcher, so a `\`-containing raw input would match nothing.
- Run tests with: `go test ./internal/tui/ -run '<Name>' -count=1` (from the worktree root).
- Commit with `gg add <paths>` + `gg commit -m "<msg>"`.

---

### Task 1: Async LsFiles load + popup state

**Files:**
- Modify: `internal/tui/file_path_popup.go` (struct fields, `openFilePathPopup`, new msg + cmd)
- Modify: `internal/tui/model.go` (route `filePathLsMsg`; add the case next to the existing `case lsFilesMsg:` around line 762)
- Test: `internal/tui/file_path_popup_test.go`

**Interfaces:**
- Consumes: `domain.Service.LsFiles(ctx) ([]string, error)`, `layerOf[*filePathPopup]`, `send`/`gotoModel` test helpers (in `goto_commit_popup_test.go`).
- Produces: `filePathLsMsg{paths []string; err error}`; `filePathPopup` fields `all []string`, `set map[string]struct{}`, `loading bool`, `loadErr error`, `suggesting bool`, `matches []fuzzy.Match`, `sel int`; `(Model).loadFilePathLsCmd() tea.Cmd`. Task 2 relies on all of these exact names.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/file_path_popup_test.go` (add `"errors"` to its imports):

```go
func TestFilePathPopupStartsLsFilesLoad(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, cmd := m.openFilePathPopup(filePathHistory)
	p := layerOf[*filePathPopup](m)
	if p == nil || !p.loading {
		t.Fatal("opening the popup must mark it loading")
	}
	if cmd == nil {
		t.Fatal("opening the popup must start the LsFiles load")
	}
}

func TestFilePathPopupLsDeliveryBuildsSet(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m, _ = send(m, filePathLsMsg{paths: []string{"a/b.go", "README.md"}})
	p := layerOf[*filePathPopup](m)
	if p.loading {
		t.Fatal("delivery must clear loading")
	}
	if _, ok := p.set["README.md"]; !ok || len(p.all) != 2 {
		t.Fatalf("delivery must fill all+set; all=%v", p.all)
	}
}

func TestFilePathPopupLsErrorKeepsPopup(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m, _ = send(m, filePathLsMsg{err: errors.New("boom")})
	p := layerOf[*filePathPopup](m)
	if p == nil || p.loadErr == nil || p.loading {
		t.Fatal("an LsFiles error must be recorded and the popup kept open")
	}
}

func TestFilePathPopupLsLateDeliveryIsNoop(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = send(m, filePathLsMsg{paths: []string{"a"}}) // no popup open — must not panic
	_ = m
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-file-path-suggestions && go test ./internal/tui/ -run 'TestFilePathPopupLs|TestFilePathPopupStarts' -count=1`
Expected: COMPILE FAIL — `filePathLsMsg`, `p.loading`, `p.set` undefined.

- [ ] **Step 3: Implement**

In `internal/tui/file_path_popup.go` — add `"github.com/homeend/gigagit/internal/fuzzy"` and `"context"` to imports; extend the struct and add msg + cmd:

```go
// filePathSuggestLimit caps the fuzzy suggestion list, like fileFinderLimit.
const filePathSuggestLimit = 200

type filePathPopup struct {
	popupMax
	kind  filePathKind
	input textfield

	// Fuzzy-suggestion state. The tracked-file list loads async on open
	// (distinct msg from the F finder's lsFilesMsg so the two never cross).
	all        []string            // tracked files from LsFiles
	set        map[string]struct{} // exact-match test over all
	loading    bool                // true until filePathLsMsg lands
	loadErr    error               // LsFiles failure → enter falls back to open-as-typed
	suggesting bool                // suggestion list visible below the input
	matches    []fuzzy.Match       // ranked subset of all
	sel        int                 // 0 = open-as-typed escape row, 1..len(matches) = match rows
}

// filePathLsMsg is the async LsFiles result for the file-path popup.
type filePathLsMsg struct {
	paths []string
	err   error
}

func (m Model) openFilePathPopup(kind filePathKind) (Model, tea.Cmd) {
	m = m.pushLayer(&filePathPopup{kind: kind, input: newTextField(""), loading: true})
	return m, m.loadFilePathLsCmd()
}

// loadFilePathLsCmd calls LsFiles off-thread and delivers filePathLsMsg.
func (m Model) loadFilePathLsCmd() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		paths, err := svc.LsFiles(context.Background())
		return filePathLsMsg{paths: paths, err: err}
	}
}
```

In `internal/tui/model.go`, directly after the existing `case lsFilesMsg:` block:

```go
	case filePathLsMsg:
		p := layerOf[*filePathPopup](m)
		if p == nil {
			return m, nil // user closed before the load returned
		}
		p.loading = false
		if msg.err != nil {
			p.loadErr = msg.err
			return m, nil
		}
		p.all = msg.paths
		p.set = make(map[string]struct{}, len(msg.paths))
		for _, s := range msg.paths {
			p.set[s] = struct{}{}
		}
		return m, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-file-path-suggestions && go test ./internal/tui/ -count=1`
Expected: PASS (whole package — existing popup tests still pass because the enter path is untouched).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-file-path-suggestions
gg add internal/tui/file_path_popup.go internal/tui/file_path_popup_test.go internal/tui/model.go
gg commit -m "feat(tui): file-path popup loads tracked files async for suggestions"
```

---

### Task 2: Enter dispatch + suggestion mode (logic)

**Files:**
- Modify: `internal/tui/file_path_popup.go` (`update`, new `updateSuggesting`, `open`, `rerank`)
- Modify: `internal/tui/model.go` (the `filePathLsMsg` case gains a rerank-on-delivery line)
- Test: `internal/tui/file_path_popup_test.go` (3 existing tests updated + new tests)

**Interfaces:**
- Consumes: Task 1's fields/msg; `repoRelPath`, `fuzzy.Rank`, `newHistoryView`/`newBlameView`, `m.loadHistoryListCmd`/`m.loadBlameCmd`, `popupFilterPage`.
- Produces: `(p *filePathPopup) open(m Model, rel string) (Model, tea.Cmd)`, `(p *filePathPopup) rerank(query string)`, `(p *filePathPopup) updateSuggesting(m Model, msg tea.KeyMsg) (Model, tea.Cmd)`. Task 3 renders off `p.suggesting`, `p.loading`, `p.matches`, `p.sel`.

- [ ] **Step 1: Update the three existing tests that assume unvalidated enter**

`TestFilePathPopupHistoryOpensSurface`, `TestFilePathPopupBlameOpensSurface`, and `TestFilePathPopupSpaceReachesNavContext` type a path and expect a direct open; once the set-membership gate lands they'd route to suggestion mode (set nil, loading true). Add this helper and make each deliver the list first:

```go
// lsReady delivers the popup's tracked-file list, as if LsFiles returned.
func lsReady(t *testing.T, m Model, paths ...string) Model {
	t.Helper()
	nm, _ := send(m, filePathLsMsg{paths: paths})
	return nm
}
```

In `TestFilePathPopupHistoryOpensSurface` and `TestFilePathPopupBlameOpensSurface`, insert `m = lsReady(t, m, "README.md")` immediately after the `palettePick(...)` line. In `TestFilePathPopupSpaceReachesNavContext`, insert `m = lsReady(t, m, "a b.txt")` after the `openFilePathPopup` line. `TestFilePathPopupEmptyKeepsOpen`, `TestFilePathPopupAllowsSpaces`, and `TestFilePathPopupEscRevealsPalette` stay untouched.

- [ ] **Step 2: Write the new failing tests**

Append to `internal/tui/file_path_popup_test.go`:

```go
func TestFilePathPopupNonMatchEntersSuggestionMode(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = lsReady(t, m, "internal/tui/model.go", "internal/tui/view.go", "README.md")
	m = typeRunes(t, m, "model.go")
	m, _ = send(m, keyType(tea.KeyEnter))
	p := layerOf[*filePathPopup](m)
	if p == nil || !p.suggesting {
		t.Fatal("a non-tracked path must switch the popup to suggestion mode")
	}
	if p.sel != 0 {
		t.Fatal("suggestion mode must start on the open-as-typed row")
	}
	if len(p.matches) == 0 || p.matches[0].S != "internal/tui/model.go" {
		t.Fatalf("matches must rank tracked files; got %+v", p.matches)
	}
}

func TestFilePathPopupSuggestionEnterOpensHistory(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "File history")
	m = lsReady(t, m, "internal/tui/model.go", "README.md")
	m = typeRunes(t, m, "model")
	m, _ = send(m, keyType(tea.KeyEnter)) // → suggestion mode
	m, _ = send(m, keyType(tea.KeyDown))  // sel=1: first match
	m, _ = send(m, keyType(tea.KeyEnter))
	hv := layerOf[*historyView](m)
	if hv == nil || hv.ctx.path != "internal/tui/model.go" {
		t.Fatalf("enter on a suggestion must open history for it; hv=%+v", hv)
	}
	if layerOf[*filePathPopup](m) != nil || layerOf[*commandPalette](m) != nil {
		t.Fatal("opening must unwind the popup and the palette beneath")
	}
}

func TestFilePathPopupSuggestionEnterOpensBlame(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathBlame)
	m = lsReady(t, m, "internal/tui/model.go")
	m = typeRunes(t, m, "model")
	m, _ = send(m, keyType(tea.KeyEnter))
	m, _ = send(m, keyType(tea.KeyDown))
	m, _ = send(m, keyType(tea.KeyEnter))
	bv := layerOf[*blameView](m)
	if bv == nil || bv.ctx.path != "internal/tui/model.go" {
		t.Fatalf("enter on a suggestion must open blame for it; bv=%+v", bv)
	}
}

func TestFilePathPopupEscapeRowOpensAsTyped(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = lsReady(t, m, "internal/tui/model.go")
	m = typeRunes(t, m, "deleted/file.go")
	m, _ = send(m, keyType(tea.KeyEnter)) // suggestion mode; sel=0 = escape row
	m, _ = send(m, keyType(tea.KeyEnter))
	hv := layerOf[*historyView](m)
	if hv == nil || hv.ctx.path != "deleted/file.go" {
		t.Fatalf("the escape row must open the typed path; hv=%+v", hv)
	}
}

func TestFilePathPopupSuggestionTypingReranks(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = lsReady(t, m, "aaa/x.go", "bbb/y.go")
	m = typeRunes(t, m, "zzz")
	m, _ = send(m, keyType(tea.KeyEnter))
	p := layerOf[*filePathPopup](m)
	if !p.suggesting || len(p.matches) != 0 {
		t.Fatalf("zzz matches nothing; got %+v", p.matches)
	}
	for range 3 {
		m, _ = send(m, keyType(tea.KeyBackspace))
	}
	m = typeRunes(t, m, "aaa")
	if p = layerOf[*filePathPopup](m); len(p.matches) != 1 || p.matches[0].S != "aaa/x.go" {
		t.Fatalf("typing must re-rank live; got %+v", p.matches)
	}
	if p.sel != 0 {
		t.Fatal("editing the query must reset the cursor to the escape row")
	}
}

func TestFilePathPopupSuggestionNavClamps(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = lsReady(t, m, "aaa/x.go", "aab/y.go")
	m = typeRunes(t, m, "aa")
	m, _ = send(m, keyType(tea.KeyEnter))
	p := layerOf[*filePathPopup](m)
	m, _ = send(m, keyType(tea.KeyUp)) // above escape row: clamp
	if p.sel != 0 {
		t.Fatal("up on row 0 must clamp")
	}
	m, _ = send(m, keyType(tea.KeyPgDown)) // far past end: clamp
	if p.sel != len(p.matches) {
		t.Fatalf("pgdown must clamp to the last row; sel=%d", p.sel)
	}
}

func TestFilePathPopupSuggestionEscReturnsToInput(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = lsReady(t, m, "aaa/x.go")
	m = typeRunes(t, m, "zzz")
	m, _ = send(m, keyType(tea.KeyEnter))
	m, _ = send(m, keyType(tea.KeyEsc))
	p := layerOf[*filePathPopup](m)
	if p == nil || p.suggesting {
		t.Fatal("esc must drop back to plain input, popup kept")
	}
	if p.input.Value() != "zzz" {
		t.Fatalf("esc must preserve the input; got %q", p.input.Value())
	}
	m, _ = send(m, keyType(tea.KeyEsc))
	if layerOf[*filePathPopup](m) != nil {
		t.Fatal("second esc must close the popup")
	}
}

func TestFilePathPopupLoadErrorOpensAsTyped(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m, _ = send(m, filePathLsMsg{err: errors.New("boom")})
	m = typeRunes(t, m, "some/file.go")
	m, _ = send(m, keyType(tea.KeyEnter))
	hv := layerOf[*historyView](m)
	if hv == nil || hv.ctx.path != "some/file.go" {
		t.Fatalf("LsFiles failure must fall through to open-as-typed; hv=%+v", hv)
	}
}

func TestFilePathPopupEnterWhileLoadingThenDelivery(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = typeRunes(t, m, "model")
	m, _ = send(m, keyType(tea.KeyEnter)) // list not loaded yet
	p := layerOf[*filePathPopup](m)
	if p == nil || !p.suggesting || !p.loading {
		t.Fatal("enter while loading must enter suggestion mode in the loading state")
	}
	m, _ = send(m, filePathLsMsg{paths: []string{"internal/tui/model.go"}})
	if len(p.matches) != 1 || p.matches[0].S != "internal/tui/model.go" {
		t.Fatalf("delivery while suggesting must fill the list; got %+v", p.matches)
	}
}

func TestFilePathPopupEnterWhileLoadingThenError(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = typeRunes(t, m, "model")
	m, _ = send(m, keyType(tea.KeyEnter))
	m, _ = send(m, filePathLsMsg{err: errors.New("boom")})
	p := layerOf[*filePathPopup](m)
	if p == nil || !p.suggesting || p.loading {
		t.Fatal("a late error must land in suggestion mode with loading cleared")
	}
	// The always-present escape row keeps this from being a dead end.
	m, _ = send(m, keyType(tea.KeyEnter))
	hv := layerOf[*historyView](m)
	if hv == nil || hv.ctx.path != "model" {
		t.Fatalf("escape row must still open as typed after a load error; hv=%+v", hv)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-file-path-suggestions && go test ./internal/tui/ -run 'TestFilePathPopup' -count=1`
Expected: FAIL — suggestion mode does not exist yet (`p.suggesting` never true; direct-open tests now pass via `lsReady`).

- [ ] **Step 4: Implement**

Replace `update`'s enter case and add the three methods in `internal/tui/file_path_popup.go`:

```go
func (p *filePathPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if p.suggesting {
		return p.updateSuggesting(m, msg)
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		rel := repoRelPath(m.currentWorktree, p.input.Value())
		if rel == "" { // nothing to open; keep the popup open
			return m, nil
		}
		if _, ok := p.set[rel]; ok || p.loadErr != nil {
			// Exact tracked file — or no list to validate against: open as before.
			return p.open(m, rel)
		}
		p.suggesting = true
		p.sel = 0
		p.rerank(rel)
		return m, nil
	default:
		p.input.HandleEditKey(msg) // spaces included — do NOT swallow KeySpace
	}
	return m, nil
}

// updateSuggesting handles keys while the suggestion list is visible.
// sel 0 is the open-as-typed escape row; 1..len(matches) are match rows.
func (p *filePathPopup) updateSuggesting(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		p.suggesting = false
		p.matches = nil
		p.sel = 0
		return m, nil
	case tea.KeyUp:
		if p.sel > 0 {
			p.sel--
		}
		return m, nil
	case tea.KeyDown:
		if p.sel < len(p.matches) {
			p.sel++
		}
		return m, nil
	case tea.KeyPgUp:
		if p.sel -= popupFilterPage; p.sel < 0 {
			p.sel = 0
		}
		return m, nil
	case tea.KeyPgDown:
		if p.sel += popupFilterPage; p.sel > len(p.matches) {
			p.sel = len(p.matches)
		}
		return m, nil
	case tea.KeyEnter:
		if p.sel > 0 {
			return p.open(m, p.matches[p.sel-1].S)
		}
		rel := repoRelPath(m.currentWorktree, p.input.Value())
		if rel == "" {
			return m, nil
		}
		return p.open(m, rel)
	default:
		p.input.HandleEditKey(msg)
		p.sel = 0
		p.rerank(repoRelPath(m.currentWorktree, p.input.Value()))
	}
	return m, nil
}

// open unwinds the popup (and the palette beneath, if any) and opens the
// history or blame surface for rel.
func (p *filePathPopup) open(m Model, rel string) (Model, tea.Cmd) {
	m = m.popLayer()
	if _, ok := m.topLayer().(*commandPalette); ok {
		m = m.popLayer()
	}
	ctx := navContext{path: rel, rev: ""}
	if p.kind == filePathBlame {
		bv := newBlameView(ctx)
		m = m.pushLayer(bv)
		return m, m.loadBlameCmd(ctx, bv.tag)
	}
	hv := newHistoryView(ctx)
	m = m.pushLayer(hv)
	return m, m.loadHistoryListCmd(ctx, hv.listTag)
}

// rerank rebuilds the suggestion list for query (the NORMALIZED input) and
// clamps sel to 0..len(matches).
func (p *filePathPopup) rerank(query string) {
	p.matches = fuzzy.Rank(query, p.all, filePathSuggestLimit)
	if p.sel > len(p.matches) {
		p.sel = len(p.matches)
	}
}
```

The old enter body (pop layers + push surface) moves into `open` verbatim — delete it from `update`.

In `internal/tui/model.go`, at the end of the `case filePathLsMsg:` success path (after the `set` loop, before `return m, nil`), add:

```go
		if p.suggesting { // enter landed before the load; fill the list now
			p.rerank(repoRelPath(m.currentWorktree, p.input.Value()))
		}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-file-path-suggestions && go test ./internal/tui/ -count=1`
Expected: PASS (whole package).

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-file-path-suggestions
gg add internal/tui/file_path_popup.go internal/tui/file_path_popup_test.go internal/tui/model.go
gg commit -m "feat(tui): file-path popup falls back to fuzzy suggestions on non-tracked input"
```

---

### Task 3: Suggestion-list rendering + i18n bundles

**Files:**
- Modify: `internal/tui/file_path_popup.go` (`box`)
- Modify: `internal/i18n/lang/ja.toml`, `ko.toml`, `zh.toml`, `ru.toml`
- Test: `internal/tui/file_path_popup_test.go`

**Interfaces:**
- Consumes: Task 2's state; `renderWindow(rows []winRow, o winOpts) []string` (pads rows to `o.w` itself; anchor keeps `p.sel` visible; zero `mode` = `modeCutoff`), `popupResolveRowCap(maximized, termH, normal)`, `popupContentWidth(w)`, `selectedRow` style, existing i18n key `"  (loading…)"` (two leading spaces, verbatim).
- Produces: rendered suggestion list; two new i18n keys `"open as typed: %s"` and `"[enter] open  [↑↓ pgup/pgdn] nav  [esc] back"` in all four bundles.

- [ ] **Step 1: Write the failing tests**

```go
func TestFilePathPopupRendersSuggestions(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = lsReady(t, m, "internal/tui/model.go")
	m = typeRunes(t, m, "model")
	m, _ = send(m, keyType(tea.KeyEnter))
	p := layerOf[*filePathPopup](m)
	out := p.box(m)
	if !strings.Contains(out, "open as typed: model") {
		t.Fatalf("box must render the escape row; out=%s", out)
	}
	if !strings.Contains(out, "internal/tui/model.go") {
		t.Fatalf("box must render the fuzzy matches; out=%s", out)
	}
	if !strings.Contains(out, "[esc] back") {
		t.Fatalf("box must render the suggestion-mode hint; out=%s", out)
	}
}

func TestFilePathPopupRendersLoadingList(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m = typeRunes(t, m, "model")
	m, _ = send(m, keyType(tea.KeyEnter)) // suggesting while loading
	p := layerOf[*filePathPopup](m)
	if out := p.box(m); !strings.Contains(out, "(loading…)") {
		t.Fatalf("box must show the loading placeholder; out=%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-file-path-suggestions && go test ./internal/tui/ -run 'TestFilePathPopupRenders' -count=1`
Expected: FAIL — box renders no list yet.

- [ ] **Step 3: Implement the render**

Replace `box` in `internal/tui/file_path_popup.go`:

```go
func (p *filePathPopup) box(m Model) string {
	w, termH := m.overlayDims()
	cw := popupContentWidth(w)
	var b strings.Builder
	b.WriteString(p.title() + "\n\n")
	b.WriteString(viewField(i18n.T("path: "), p.input, true, cw) + "\n")
	if p.suggesting {
		b.WriteString("\n")
		if p.loading {
			b.WriteString(i18n.T("  (loading…)") + "\n")
		} else {
			rows := make([]winRow, 1+len(p.matches))
			rows[0] = winRow{text: i18n.T("open as typed: %s", strings.TrimSpace(p.input.Value()))}
			for i, mt := range p.matches {
				rows[i+1] = winRow{text: mt.S}
			}
			for i := range rows {
				if i == p.sel {
					rows[i] = winRow{text: "> " + rows[i].text, style: selectedRow}
				} else {
					rows[i].text = "  " + rows[i].text
				}
			}
			visH := len(rows)
			if capRows := popupResolveRowCap(p.maximized, termH, 12); visH > capRows {
				visH = capRows
			}
			for _, ln := range renderWindow(rows, winOpts{w: cw, h: visH, anchor: p.sel}) {
				b.WriteString(ln + "\n")
			}
		}
		b.WriteString("\n" + i18n.T("[enter] open  [↑↓ pgup/pgdn] nav  [esc] back"))
	} else {
		b.WriteString("\n" + i18n.T("[enter] show  [esc] cancel"))
	}
	return modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
}
```

- [ ] **Step 4: Add the two new keys to ALL FOUR bundles**

Each file keeps `[strings]` keys in alphabetical order — insert at the matching sorted position. Exact lines:

`internal/i18n/lang/ja.toml`:
```toml
"[enter] open  [↑↓ pgup/pgdn] nav  [esc] back" = "[enter] 開く  [↑↓ pgup/pgdn] 移動  [esc] 戻る"
"open as typed: %s" = "入力どおりに開く: %s"
```

`internal/i18n/lang/ko.toml`:
```toml
"[enter] open  [↑↓ pgup/pgdn] nav  [esc] back" = "[enter] 열기  [↑↓ pgup/pgdn] 이동  [esc] 뒤로"
"open as typed: %s" = "입력한 대로 열기: %s"
```

`internal/i18n/lang/zh.toml`:
```toml
"[enter] open  [↑↓ pgup/pgdn] nav  [esc] back" = "[enter] 打开  [↑↓ pgup/pgdn] 导航  [esc] 返回"
"open as typed: %s" = "按输入打开: %s"
```

`internal/i18n/lang/ru.toml`:
```toml
"[enter] open  [↑↓ pgup/pgdn] nav  [esc] back" = "[enter] открыть  [↑↓ pgup/pgdn] навигация  [esc] назад"
"open as typed: %s" = "открыть как введено: %s"
```

- [ ] **Step 5: Run tests to verify they pass — including the i18n AST gates**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-file-path-suggestions && go test ./internal/tui/ ./internal/i18n/ -count=1`
Expected: PASS. If `i18n_scan_test` fails naming a key, the bundle line does not byte-match the `i18n.T` literal — fix the bundle, not the code.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-file-path-suggestions
gg add internal/tui/file_path_popup.go internal/tui/file_path_popup_test.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml
gg commit -m "feat(tui): render file-path suggestion list; translate new strings"
```

---

### Task 4: Docs + full gates

**Files:**
- Modify: `CHANGELOG.md` (new entry at the top of the unreleased/current section, matching the file's existing entry style)

**Interfaces:**
- Consumes: everything above, complete and committed.
- Produces: a branch ready for the human to review/merge.

- [ ] **Step 1: Add the CHANGELOG entry**

At the top of the current section in `CHANGELOG.md`, following the file's existing bullet style:

```markdown
- Palette File history / File blame: a path that isn't an exact tracked file
  now opens an inline fuzzy-suggestion list (with an "open as typed" escape
  row) instead of dead-ending on "(no history)"; typing refines the list live.
```

- [ ] **Step 2: Run the full staged gate**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-file-path-suggestions && ./test.sh`
Expected: vet+gofmt clean, unit tests pass, e2e pass. Fix anything it flags before committing.

- [ ] **Step 3: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-file-path-suggestions
gg add CHANGELOG.md
gg commit -m "docs: changelog for file-path popup fuzzy suggestions"
```

- [ ] **Step 4: Report done**

Do NOT merge — the human owns merges (`./test.sh race` runs before that, on a quiet machine). Report the branch, the commits, and the test results.
