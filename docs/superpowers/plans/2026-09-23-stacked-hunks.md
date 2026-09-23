# Working-tree hunk staging inside a stack (plan 4c) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to
> implement this plan task-by-task, INLINE — this repo forbids subagents. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** stage (and unstage) hunks of a working-tree file without leaving the
stacked diff — per-slot on the web, from the cursor's file in the TUI.

**Architecture:** the same move notes (4a) and search (4b) already made. On the web
the module-level `diffHunks` becomes the slot's own `hunks`, and `diffHTML` learns a
third explicit context (`kctx`) beside `nctx`/`hctx` — never a "current slot" global;
`activeDiff()` says whose picks the bar acts on. In the TUI there is no inline lane
to make per-slot: hunk staging is a full-screen `hunkPicker` layer opened from the
Files/Staged panel, so 4c gives the **diff view** its own `H` — single-file and
stacked — which opens that same picker for the file under the cursor. Everything
after the stage is already built: the web's `reconcileStack` and the TUI's
`reconcileStatusStack` both run on every status write.

**Tech Stack:** Go 1.26 + Bubble Tea (TUI), vanilla ES modules (web), node-imported
JS guards, Playwright probes (chromium + firefox), `tui-capture.sh`.

**Spec:** `docs/superpowers/specs/2026-09-22-stacked-diff-view-design.md` — §11 item 3
(this plan), §5.1 (the web slot's `hunks` hook), §5.2 (stackFile), §9 (the working-tree
stack: the cursor's section, conflicts header-only, reconcile slot by slot).

## Global Constraints

- **No subagents.** This session executes every task itself (CLAUDE.md).
- Work happens in the worktree `.claude/worktrees/stacked-hunks` on branch
  `feat/stacked-hunks`; the human merges.
- TDD: a failing test (or a probe red against the **unfixed** build) before every
  change.
- Every user-visible TUI string goes through `i18n.T` with a **literal** key present
  in all four bundles (`ja`, `ko`, `zh`, `ru`), inserted **in place** — never re-sort
  a bundle; remove orphaned keys from all four.
- `internal/tui` never imports `internal/git`; frontends reach git through
  `internal/domain`.
- Web probes run against the unfixed build FIRST and must fail at the first assert;
  every probe curls `/api/repo` to prove the port is its own (orphaned `gg web`
  servers from earlier runs squat ports) and deletes `ui-state.json` before pressing
  `S` (the stacked pref is stored, so `S` toggles across runs).
- `./test.sh race` before each merge; a verify binary (Linux + Windows, plus
  `gg-web-new.exe` when the web half changes) delivered unprompted.

## Premise checks already done (read this before planning any task)

The 4b lesson was "check the premise before planning the work". Three were checked
against `main d2154664`:

1. **The TUI working-tree stack ALREADY reconciles.**
   `diff_stack_keys.go:375 reconcileStatusStack` runs from `viewstate.go:418
   withStatus` — the one place the Files/Staged membership is derived — and drops
   gone files, inserts new ones in list order, marks a re-read file `stackStale`
   (keeping its old rows on screen), keeps the cursor **by path**, and pops the layer
   when the section empties. So nothing has to be built for "what the stack does
   after a stage": it is the shipped behaviour. **Task 1 is a guard test that passes
   on main** and says so out loud.
2. **Slots fetch through the same URL as the single-file view** —
   `stackview.js:423 getJSON(fileDiffURL(s.f))` — so the server's hunk tags
   (`d.hunks = {hash, count}`, only on `/api/diff?wt=unstaged`) already arrive per
   slot. No server work.
3. **`reconcileSlots` (stack.js:158) already re-fetches every kept slot** on a status
   re-read (`o.load = "idle"`, old diff painted meanwhile). Picks are POSITIONAL
   against the hash the server gave, so the refetch must drop them — that is task 11,
   not a new mechanism.

## Decisions (settled with the user at plan review — do not re-open)

- **D1 — staging acts on ONE file: the cursor's.** No cross-slot batch stage.
- **D2 — web picks are per slot; the bar follows the active slot.** Each slot keeps
  its own `hunks.picks`; `#hunk-bar` shows `activeDiff()`'s slot count and
  `stage selected (n)` stages only that file. Clicking a row in another slot moves
  the bar to it; the first slot's picks stay painted and stay pickable.
- **D3 — reconcile in place.** After a stage the 200 body is a fresh status →
  `applyStatus` → `reconcileStatusView` → the existing `reconcileStack`. No stack
  rebuild, the reader's anchor is kept. A slot that re-fetches **drops its picks**
  (they name the old bytes) and re-arms from the new `d.hunks`. In the TUI the
  identical path is `withStatus` → `reconcileStatusStack`.
- **D4 — conflicts unchanged.** A conflicted file stays header-only with the
  resolver door (web `.stk-resolve` button, TUI `enter` on the header). `H` on a
  conflicted file refuses with a notice naming `enter`.
- **D5 — the TUI's `H` lives in the diff view generally** — single-file working-tree
  diffs AND stacks, Files section → stage picker, Staged section → unstage picker,
  judged on the CURSOR's file with the panel's own rules (`canStageHunks` /
  `canUnstageHunks`: never untracked, never unmerged, and unstage never on a staged
  `A`).
- **D6 — the single-file working-tree diff reloads itself after its own staging
  round.** Today nothing reloads an open working-tree diff on a status change; a
  stack survives it (D3) but one file does not. The picker's apply parks a reload
  request, the next status write consumes it, and if the file has left the section
  entirely the diff closes back to the list with a notice. Scoped to the staging
  round on purpose: reloading on EVERY status write would throw the reader to the
  top of the file whenever a watch-driven refresh landed.

## File structure

**TUI (half 1 — merge 1)**

| File | Responsibility |
|---|---|
| `internal/tui/diff_stack_hunks.go` *(new)* | the diff view's `H`: which file it acts on, the refusals, the two loader commands, the parked single-file reload. Keeps `diff_view.go`'s key switch to one line, like `diff_stack_search.go` did for `] [`. |
| `internal/tui/diff_view.go` | one `case "H":` in `updateDiffViewKey`; the reload consumer helper hook. |
| `internal/tui/conflict_picker.go` | the stage/unstage apply arms park the reload (D6). |
| `internal/tui/model.go` | `statusRefreshedMsg` / the full-load handler batch the parked reload command. |
| `internal/tui/footer.go`, `help.go`, `diff_stack_keys.go` (`stackMenuRows`) | the new key advertised in the footer, the `?` help and the `.` menu. |
| `internal/i18n/*.toml` ×4 | the new keys, inserted in place. |
| `internal/tui/diff_stack_hunks_test.go` *(new)* | every behaviour above. |

**Web (half 2 — merge 2)**

| File | Responsibility |
|---|---|
| `internal/web/static/files.js` | `diffHTML(d, paneWidth, notesOn, open, nctx, hctx, kctx)`; `hunkCls`/`hunkAttr` read `kctx`; `renderHunkBar`/`paintHunkPicks`/`stageHunksPicked`/the click listener go through `activeDiff()`; `reconcileStatusView`'s gone-file guard becomes per-slot. |
| `internal/web/static/stackview.js` | the slot arms `s.hunks` on load, `bodyHTML` passes `kctx`, `hunkSlotAt(el)` resolves a clicked row's slot, `stackHunksReconciled()` drops picks on a refetch. |
| `internal/web/static/stack.js` | `buildSlots` gains `hunks: null`; `reconcileSlots` clears it on a kept slot. |
| `internal/web/stackjs_test.go`, `stackviewjs_test.go`, `hunksjs_test.go` *(new)* | node-imported guards. |
| probes `stack-probe/hunks.mjs` + `mkhunks.sh` *(new, scratchpad)* | the browser evidence. |

**Docs (in whichever merge touches them):** `CHANGELOG.md`, `docs/CLAUDE-details.md`,
`docs/web-tui-parity.md`.

---

## Task 1: Guard — the working-tree stack already reconciles (expected PASS)

**Files:**
- Test: `internal/tui/diff_stack_hunks_test.go` (new)

**Interfaces:**
- Consumes: `stackSearchModel`-style helpers from `diff_stack_search_test.go`,
  `testRepo(t, dir)`, `drainRunner`.
- Produces: `wtStackModel(t)` — a Model with a working-tree (unstaged) stack open
  over ≥3 tracked files, used by every later TUI task.

- [ ] **Step 1: Write the test**

```go
// TestWorkingTreeStackReconcilesOnStatusWrite pins the behaviour 4c relies on
// instead of rebuilding: after a status write the stack keeps the reader's file,
// drops a file that left the section, and marks a re-read file stale rather than
// emptying it. This PASSES on main — reconcileStatusStack (diff_stack_keys.go)
// already runs from withStatus. It is here so 4c cannot silently lose it.
func TestWorkingTreeStackReconcilesOnStatusWrite(t *testing.T) {
	t.Parallel()
	m := wtStackModel(t) // files a.txt, b.txt, c.txt — all unstaged M
	v := m.diffLayer()
	// stand on b.txt
	m = m.focusStackFile(v, "b.txt")
	loadedB := v.stk.files[v.curFile()].d
	if loadedB == nil {
		t.Fatal("fixture: b.txt must be loaded before the reconcile")
	}
	// a.txt leaves the section (staged whole)
	st := m.status
	st.Files = dropFile(st.Files, "a.txt")
	m = m.withStatus(st)

	v = m.diffLayer()
	if got := stackPaths(v); got != "b.txt,c.txt" {
		t.Fatalf("stack is %q, want the section without a.txt", got)
	}
	if p := v.stk.files[v.curFile()].path; p != "b.txt" {
		t.Fatalf("the reader moved to %q; the cursor keeps its file BY PATH", p)
	}
	if f := v.stk.files[0]; f.d == nil || f.load != stackStale {
		t.Fatalf("b.txt is %v/%v; a re-read file keeps its rows and goes stale", f.d == nil, f.load)
	}
}
```

- [ ] **Step 2: Run it**

Run: `cd .claude/worktrees/stacked-hunks && go test ./internal/tui -run TestWorkingTreeStackReconciles -count=1`
Expected: **PASS** (the premise check). If it FAILS, stop — the plan's D3 is wrong and
the reconcile is a prerequisite task, not a given.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/diff_stack_hunks_test.go
git commit -m "test(tui): pin the working-tree stack's reconcile before 4c builds on it"
```

---

## Task 2: `H` in a stack opens the stage picker for the cursor's file

**Files:**
- Create: `internal/tui/diff_stack_hunks.go`
- Modify: `internal/tui/diff_view.go` (the key switch)
- Test: `internal/tui/diff_stack_hunks_test.go`

**Interfaces:**
- Consumes: `m.loadStageHunksCmd(path)` / `m.loadUnstageHunksCmd(path)`
  (`internal/tui/op.go:325/349`), `v.curFile()`, `stackFile.fs model.FileStatus`.
- Produces:
  - `func (m Model) hunkFileHere() (model.FileStatus, bool, string)` — the file `H`
    acts on, whether the staged (unstage) lane applies, and a refusal string ("" =
    go ahead).
  - `func (m Model) diffHunkKey() (Model, tea.Cmd, bool)` — the whole `H` handler.

- [ ] **Step 1: Write the failing test**

```go
func TestStackHOpensTheStagePickerForTheCursorFile(t *testing.T) {
	t.Parallel()
	m := wtStackModel(t)
	v := m.diffLayer()
	m = m.focusStackFile(v, "b.txt")

	nm, cmd := keyModel(t, m, "H")
	if cmd == nil {
		t.Fatal("H issued no command; it must load b.txt's two sides")
	}
	msg, ok := cmd().(stageHunksLoadedMsg)
	if !ok {
		t.Fatalf("H sent %T, want stageHunksLoadedMsg", cmd())
	}
	if msg.path != "b.txt" {
		t.Fatalf("H is staging %q; it acts on the CURSOR's file", msg.path)
	}
	nm = nm.Update2(msg) // the loaded handler pushes the picker
	if _, ok := nm.topLayer().(*hunkPicker); !ok {
		t.Fatalf("top layer is %T, want the hunk picker over the stack", nm.topLayer())
	}
}
```

(`keyModel` / `Update2` are the suite's existing key-drive helpers — reuse whatever
`diff_stack_search_test.go` uses rather than inventing names.)

- [ ] **Step 2: Run it**

Run: `go test ./internal/tui -run TestStackHOpensTheStagePicker -count=1`
Expected: FAIL — `H` is an unhandled key in the diff view, `cmd == nil`.

- [ ] **Step 3: Implement**

```go
// diff_stack_hunks.go

// hunkFileHere is the file H acts on: the cursor's file in a stack, the open
// file in a single working-tree diff. staged says which lane (Staged section →
// unstage), and why is a refusal ("" = go ahead) worded for the status bar.
func (m Model) hunkFileHere() (f model.FileStatus, staged bool, why string) {
	v := m.diffLayer()
	if v == nil {
		return f, false, i18n.T("no file here")
	}
	switch m.diffNav {
	case diffNavStatus:
		staged = false
	case diffNavStaged:
		staged = true
	default:
		return f, false, i18n.T("hunks: only a working-tree file can be staged")
	}
	if v.stk != nil {
		sf := v.stk.files[v.curFile()]
		if sf.conflict {
			return f, staged, i18n.T("conflicted — [enter] opens the resolver")
		}
		f = sf.fs
	} else {
		bi, ok := m.statusIndexOf(v.title)
		if !ok {
			return f, staged, i18n.T("hunks: this file is no longer in the working tree")
		}
		f = m.status.Files[bi]
	}
	if !m.opsIdle() {
		return f, staged, i18n.T("an operation is running")
	}
	if f.Kind == model.KindUntracked {
		return f, staged, i18n.T("hunks: an untracked file is staged whole")
	}
	if f.Kind == model.KindUnmerged {
		return f, staged, i18n.T("conflicted — [enter] opens the resolver")
	}
	if staged && f.Staged == 'A' {
		return f, staged, i18n.T("hunks: a newly added file is unstaged whole")
	}
	return f, staged, ""
}

// diffHunkKey is H in the diff view: open the hunk picker over this file. The
// picker is a LAYER — pushLayer returns to the stack (or the single diff) when
// it closes, which is why the stack can keep the reader's place (design §9).
func (m Model) diffHunkKey() (Model, tea.Cmd, bool) {
	f, staged, why := m.hunkFileHere()
	if why != "" {
		m.statusMsg = why
		return m, nil, true
	}
	if staged {
		return m, m.loadUnstageHunksCmd(f.Path), true
	}
	return m, m.loadStageHunksCmd(f.Path), true
}
```

In `diff_view.go`'s key switch, beside the other one-liners:

```go
	case "H":
		nm, cmd, done := m.diffHunkKey()
		if done {
			return nm, cmd
		}
```

(Match the switch's real return shape at the call site; `diff_stack_search.go`'s
`] [` cases are the pattern to copy.)

- [ ] **Step 4: Run the test**

Run: `go test ./internal/tui -run TestStackHOpensTheStagePicker -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/diff_stack_hunks.go internal/tui/diff_stack_hunks_test.go internal/tui/diff_view.go
git commit -m "feat(tui): H stages hunks of the file under the stack's cursor"
```

---

## Task 3: the Staged section's `H` unstages

**Files:**
- Test: `internal/tui/diff_stack_hunks_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestStagedStackHOpensTheUnstagePicker(t *testing.T) {
	t.Parallel()
	m := stagedStackModel(t) // a staged stack: b.txt staged M, n.txt staged A
	v := m.diffLayer()
	m = m.focusStackFile(v, "b.txt")

	_, cmd := keyModel(t, m, "H")
	if _, ok := cmd().(unstageHunksLoadedMsg); !ok {
		t.Fatalf("the Staged section sent %T, want unstageHunksLoadedMsg", cmd())
	}
	// a file not yet in HEAD cannot be unstaged hunk-wise (StageHunks can only
	// set index content, never remove the entry) — the panel's own rule
	m2 := m.focusStackFile(m.diffLayer(), "n.txt")
	nm, cmd2 := keyModel(t, m2, "H")
	if cmd2 != nil {
		t.Fatal("H on a staged-A file must refuse, not open a picker")
	}
	if !strings.Contains(nm.statusMsg, "unstaged whole") {
		t.Fatalf("no notice for the refusal: %q", nm.statusMsg)
	}
}
```

- [ ] **Step 2: Run it** — Expected: FAIL (`stagedStackModel` missing, then the
  staged-A refusal).
- [ ] **Step 3: Implement** the fixture helper; the refusals are already in
  `hunkFileHere`.
- [ ] **Step 4: Run it** — Expected: PASS.
- [ ] **Step 5: Commit**

```bash
git commit -am "feat(tui): H unstages hunks in a staged stack, refusing a staged-A file"
```

---

## Task 4: the refusals — conflicted, untracked, a commit stack

**Files:**
- Test: `internal/tui/diff_stack_hunks_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestHRefusesWhereHunksCannotApply(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, file, want string }{
		{"conflicted", "u.txt", "resolver"},
		{"untracked", "new.txt", "staged whole"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := wtStackModelWith(t, tc.file)
			nm, cmd := keyModel(t, m.focusStackFile(m.diffLayer(), tc.file), "H")
			if cmd != nil {
				t.Fatalf("%s opened a picker; it must refuse", tc.name)
			}
			if !strings.Contains(nm.statusMsg, tc.want) {
				t.Fatalf("notice %q does not say %q", nm.statusMsg, tc.want)
			}
		})
	}
	// a COMMIT stack has no working tree to stage into
	m := commitStackModel(t)
	nm, cmd := keyModel(t, m, "H")
	if cmd != nil || !strings.Contains(nm.statusMsg, "working-tree") {
		t.Fatalf("a commit stack must refuse H: cmd=%v notice=%q", cmd != nil, nm.statusMsg)
	}
}
```

- [ ] **Step 2: Run it** — Expected: FAIL on the missing fixtures.
- [ ] **Step 3: Implement** the fixtures (`commitStackModel` = the existing
  `stackSearchModel` shape over a commit).
- [ ] **Step 4: Run it** — Expected: PASS.
- [ ] **Step 5: Commit**

```bash
git commit -am "test(tui): H refuses on a conflict, an untracked file and a commit stack"
```

---

## Task 5: `H` in the SINGLE-file working-tree diff, and its reload (D5 + D6)

**Files:**
- Modify: `internal/tui/diff_stack_hunks.go`, `internal/tui/conflict_picker.go`,
  `internal/tui/model.go`
- Test: `internal/tui/diff_stack_hunks_test.go`

**Interfaces:**
- Produces: `Model.hunkReload *hunkReload` (`{path string; staged bool}`) and
  `func (m Model) takeHunkReload() (Model, tea.Cmd)` — consumed by the status
  handlers.

- [ ] **Step 1: Write the failing test**

```go
func TestSingleFileDiffReloadsAfterItsOwnStagingRound(t *testing.T) {
	t.Parallel()
	m := wtDiffModel(t, "b.txt") // single-file unstaged diff, NOT a stack
	_, cmd := keyModel(t, m, "H")
	if _, ok := cmd().(stageHunksLoadedMsg); !ok {
		t.Fatalf("H did not open the picker from the single-file diff: %T", cmd())
	}
	m = m.armHunkReload("b.txt", false) // what the picker's apply does

	// the op's status write lands
	nm, rcmd := m.Update2(statusRefreshedMsg{status: statusWithout(m.status, "")})
	if rcmd == nil {
		t.Fatal("the parked reload was never issued")
	}
	if nm.hunkReload != nil {
		t.Fatal("the reload must be consumed once, not re-fire on every refresh")
	}

	// and when the file left the section entirely, the diff closes
	m2 := m.armHunkReload("b.txt", false)
	nm2, _ := m2.Update2(statusRefreshedMsg{status: statusWithout(m2.status, "b.txt")})
	if nm2.diffLayer() != nil {
		t.Fatal("a fully staged file must close its diff, not show a stale one")
	}
	if !strings.Contains(nm2.statusMsg, "fully staged") {
		t.Fatalf("no notice when the diff closed: %q", nm2.statusMsg)
	}
}
```

- [ ] **Step 2: Run it** — Expected: FAIL (`armHunkReload` / `hunkReload` missing).

- [ ] **Step 3: Implement**

```go
// hunkReload is a diff the staging round OWES a re-read: the single-file
// working-tree view has no reconcile of its own (a stack's reconcileStatusStack
// runs from withStatus), and nothing else reloads an open working-tree diff on a
// status change. Parked at apply, consumed by the next status write — NOT by
// every status write, or a watch-driven refresh would throw the reader to the
// top of the file they are reading.
type hunkReload struct {
	path   string
	staged bool
}

func (m Model) armHunkReload(path string, staged bool) Model {
	if v := m.diffLayer(); v == nil || v.stk != nil {
		return m // a stack reconciles itself
	}
	m.hunkReload = &hunkReload{path: path, staged: staged}
	return m
}

// takeHunkReload consumes a parked reload against the status just written: the
// file is re-read where it is still in its section, and the diff closes when it
// is not (fully staged / gone).
func (m Model) takeHunkReload() (Model, tea.Cmd) {
	r := m.hunkReload
	if r == nil {
		return m, nil
	}
	m.hunkReload = nil
	v := m.diffLayer()
	if v == nil || v.stk != nil {
		return m, nil
	}
	bi, ok := m.statusIndexOf(r.path)
	if !ok || !m.inSection(m.status.Files[bi], r.staged) {
		m = m.popLayer()
		m.diffTag = ""
		m.statusMsg = i18n.T("%s is fully staged", r.path)
		return m, nil
	}
	return m, m.loadStatusDiffCmd(m.status.Files[bi], r.staged)
}
```

`conflict_picker.go`'s two apply arms (`newStagePicker` / `newUnstagePicker`) call
`m = m.armHunkReload(path, staged)` beside `m = m.popLayer()`. The
`statusRefreshedMsg` handler (`model.go:3402`) and the full-load handler
(`model.go:1507`) batch `takeHunkReload`'s command into what they already return.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui -run 'TestSingleFileDiffReloads|TestStackH|TestHRefuses' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git commit -am "feat(tui): the single-file working-tree diff re-reads itself after its staging round"
```

---

## Task 6: footer, help, `.` menu and the four bundles

**Files:**
- Modify: `internal/tui/footer.go`, `internal/tui/help.go`,
  `internal/tui/diff_stack_keys.go` (`stackMenuRows`), `internal/i18n/*.toml` ×4
- Test: `internal/tui/diff_stack_hunks_test.go` + the existing i18n AST gates

- [ ] **Step 1: Write the failing test**

```go
func TestHIsAdvertisedWhereItWorks(t *testing.T) {
	t.Parallel()
	m := wtStackModel(t)
	if !strings.Contains(footerText(m), "[H]") {
		t.Fatalf("the footer never offers H in a working-tree stack:\n%s", footerText(m))
	}
	if !hasMenuRow(m.stackMenuRows(), "stack-hunks") {
		t.Fatal("the . menu has no row for the hunk picker")
	}
	// and NOT where it refuses
	if strings.Contains(footerText(commitStackModel(t)), "[H]") {
		t.Fatal("a commit stack advertises a key that refuses")
	}
}
```

- [ ] **Step 2: Run it** — Expected: FAIL.
- [ ] **Step 3: Implement.** Add a `diff-hunks` footer chip gated on
  `m.hunkFileHere()` returning no refusal; a `stack-hunks` row in `stackMenuRows`
  (replaying `H` through Update like every other row); a help entry. Keys go into all
  four bundles **in place**.
- [ ] **Step 4: Run** `go test ./internal/tui -count=1` — Expected: PASS, including
  `i18n_scan_test.go`, `options_vocab_test.go`, `menu_labels_test.go`.
- [ ] **Step 5: Commit**

```bash
git commit -am "feat(tui): advertise H in the diff footer, the . menu and help"
```

---

## Task 7: TUI evidence + docs, then the race gate and merge 1

**Files:**
- Modify: `CHANGELOG.md`, `docs/CLAUDE-details.md`

- [ ] **Step 1** — `./tui-capture.sh --state <fresh dir>` over a working-tree fixture
  with two multi-hunk files: stack → cursor on the second file → `H` → the picker
  titled with THAT file → `ctrl+s` → back in the stack, reader's file kept, the
  staged file's rows shrunk. **A state dir is single-use** (the previous run's `S`
  pref is inherited) and the capture starts on BRANCHES.
- [ ] **Step 2** — write the "Working-tree hunk staging inside a stack (plan 4c)"
  section of `docs/CLAUDE-details.md` (the premise checks above belong in it) and the
  CHANGELOG entry.
- [ ] **Step 3** — `./test.sh race`; report the counts.
- [ ] **Step 4** — build the Linux + Windows verify binaries, deliver both, and hand
  the merge to the user.

```bash
git commit -am "docs: working-tree hunk staging inside a stack (TUI half)"
```

---

## Task 8: web — `diffHTML` takes an explicit hunk context

**Files:**
- Modify: `internal/web/static/files.js`
- Test: `internal/web/hunksjs_test.go` (new)

**Interfaces:**
- Produces: `diffHTML(d, paneWidth, notesOn, open, nctx, hctx, kctx)` where
  `kctx = {picks: Set<int>, armed: bool}` or null; `hunkCls(r, kctx)` /
  `hunkAttr(r, kctx)`.

- [ ] **Step 1: Write the failing test** — a node-imported guard asserting that
  `hunkCls`/`hunkAttr` take their picks from the argument and that no
  `hunkCls(`/`hunkAttr(` call site reads `diffHunks` (the 4a rule: never a
  module-level "current slot").

```go
const hunkCtxHarness = `
import { readFileSync } from "node:fs";
const src = readFileSync(process.argv[2], "utf8");
const out = {};
out.clsTakesCtx = /function hunkCls\(r, kctx\)/.test(src);
out.attrTakesCtx = /function hunkAttr\(r, kctx\)/.test(src);
// neither may fall back to the module-level global
const body = src.slice(src.indexOf("function hunkCls"), src.indexOf("// --- diff collapse"));
out.noGlobal = !/diffHunks/.test(body);
out.diffHTMLTakesKctx = /function diffHTML\(d, paneWidth, notesOn, open, nctx, hctx, kctx\)/.test(src);
console.log(JSON.stringify(out));
`
```

- [ ] **Step 2: Run it** — `go test ./internal/web -run TestHunkContextIsExplicit -count=1`
  Expected: FAIL (`hunkCls(r)` reads the global today).
- [ ] **Step 3: Implement**

```js
function hunkCls(r, kctx) {
  if (r.hunk == null || !kctx) return "";
  return " hk" + (kctx.picks.has(r.hunk) ? " picked" : "");
}

function hunkAttr(r, kctx) {
  return r.hunk == null || !kctx ? "" : ` data-hunk="${r.hunk}"`;
}
```

`diffHTML` gains the `kctx` parameter and threads it into its four row builders
(files.js:1575/1600/1606/1628). The single-file caller (`renderDiff`) passes
`diffHunks ? {picks: diffHunks.picks} : null`, so nothing changes for it.

- [ ] **Step 4: Run it** — Expected: PASS. Also run the whole `./internal/web` suite:
  `TestStackNoteContextIsShared` and the 4b guards pin `diffHTML`'s signature and
  must be updated in the same commit.
- [ ] **Step 5: Commit**

```bash
git commit -am "refactor(web): diffHTML paints hunk picks from an explicit context"
```

---

## Task 9: each slot arms its own hunks

**Files:**
- Modify: `internal/web/static/stackview.js`, `internal/web/static/stack.js`
- Test: `internal/web/stackjs_test.go`, `internal/web/stackviewjs_test.go`

- [ ] **Step 1: Write the failing test** — in the `stackjs` harness:

```js
// 8. a slot carries its own hunks hook, and a refetch drops it (picks are
//    POSITIONAL against the hash the server gave)
const hs = S.buildSlots(rows(2));
out.hunksHook = hs[0].hunks === null;
hs[0].hunks = { hash: "h1", count: 3, picks: new Set([0, 2]) };
const after = S.reconcileSlots(hs, [{ f: { path: "p0", status: "M" }, idx: 0 }, { f: { path: "p1", status: "M" }, idx: 1 }]);
out.picksDropped = after[0].hunks === null;
```

plus a `stackviewjs_test.go` guard that `bodyHTML` passes a `kctx` built from
`s.hunks` and that `load()` arms `s.hunks` from `d.hunks` through `hunkEligible`.

- [ ] **Step 2: Run** — Expected: FAIL.
- [ ] **Step 3: Implement**

```js
// stack.js — buildSlots
    hunks: null, // this file's inline staging: {hash, count, picks:Set} while eligible
// stack.js — reconcileSlots, on a kept slot
    o.hunks = null; // the refetch re-arms from the fresh tags; old picks name old bytes
```

```js
// stackview.js — load(), right after s.diff = d
    s.hunks = d.hunks && hunkEligible(s.f)
      ? { hash: d.hunks.hash, count: d.hunks.count, picks: new Set() }
      : null;
// stackview.js — bodyHTML
    const kctx = s.hunks ? { picks: s.hunks.picks } : null;
    return diffHTML(s.diff, $("diff-pane").clientWidth, notesArmed(nc.ctx), s.folds, nc, hctx, kctx);
```

- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit**

```bash
git commit -am "feat(web): a stacked working-tree file carries its own hunk picks"
```

---

## Task 10: clicking a row picks in ITS slot; the bar follows the active slot (D2)

**Files:**
- Modify: `internal/web/static/files.js`, `internal/web/static/stackview.js`
- Test: `internal/web/stackviewjs_test.go`

**Interfaces:**
- Produces: `hunkSlotAt(el)` in `stackview.js` (the slot a clicked row belongs to,
  null outside a stack) and `activeHunks()` in `files.js` (the bar's slot:
  `activeDiff().slot.hunks` stacked, `diffHunks` single-file).

- [ ] **Step 1: Write the failing test** — guard that the `#diff-body` click listener
  resolves the slot from `closest(".stk-file")` rather than returning early on
  `!diffHunks`; that `paintHunkPicks` scopes its query to that slot's section; and
  that `renderHunkBar` / `hunk-all` / `hunk-none` / `stageHunksPicked` go through
  `activeHunks()`.
- [ ] **Step 2: Run** — Expected: FAIL (`if (!diffHunks) return;` is the first line of
  the listener today, so a stack never toggles anything).
- [ ] **Step 3: Implement.** The listener becomes:

```js
$("diff-body").addEventListener("click", (e) => {
  const tr = e.target.closest("tr[data-hunk]");
  if (!tr || !getSelection().isCollapsed) return; // don't toggle mid text-selection
  const h = hunkSlotAt(tr) || (diffHunks ? { hunks: diffHunks, el: $("diff-body") } : null);
  if (!h) return;
  const i = Number(tr.dataset.hunk);
  if (h.hunks.picks.has(i)) h.hunks.picks.delete(i);
  else h.hunks.picks.add(i);
  // a click also makes that file the active one: the bar acts on ONE file (D1)
  if (h.k != null) setStackAnchor(h.k);
  paintHunkPicks(h);
});
```

- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit**

```bash
git commit -am "feat(web): pick hunks inside a stacked file; the bar follows that file"
```

---

## Task 11: staging a slot, and what the stack does next (D3)

**Files:**
- Modify: `internal/web/static/files.js`
- Test: `internal/web/stackviewjs_test.go`

- [ ] **Step 1: Write the failing test** — guard that `stageHunksPicked` posts the
  ACTIVE slot's `{path, picks, hash}`, and that in a stack it does **not** call
  `reopenAfterHunkStage` (which tears the stack down through `openStatusDiff`) but
  falls through to `applyStatus` → `reconcileStatusView` → `reconcileStack`; and that
  `reconcileStatusView`'s gone-file guard (files.js:36) no longer clears a stack's
  picks wholesale.
- [ ] **Step 2: Run** — Expected: FAIL.
- [ ] **Step 3: Implement**

```js
async function stageHunksPicked() {
  const h = activeHunks();
  if (!h || !h.hunks.picks.size) return;
  const v = h.hunks;
  …
  applyStatus(resp);
  reconcileStatusView(); // reconcileStack keeps the reader's file and re-fetches this slot
  renderFiles();
  if (!state.stack) reopenAfterHunkStage(h.path);
}
```

and the guard at files.js:36:

```js
  // a status re-read can invalidate an open hunk view (file fully staged or
  // gone): exit rather than offer stale positional picks. In a stack each slot
  // answers for itself inside reconcileSlots.
  if (!state.stack && diffHunks && !state.statusEntries.some(…)) clearDiffHunks();
```

- [ ] **Step 4: Run** the whole `./internal/web` suite — Expected: PASS.
- [ ] **Step 5: Commit**

```bash
git commit -am "feat(web): staging a stacked file reconciles in place, keeping the reader's spot"
```

---

## Task 12: the probe — against the UNFIXED build first

**Files:**
- Create (scratchpad): `stack-probe/mkhunks.sh`, `stack-probe/hunks.mjs`

- [ ] **Step 1: Fixture.** `mkhunks.sh` builds a repo whose unstaged section holds
  ≥3 tracked files with ≥2 separated hunks each (a 20-line file, edits at lines 2 and
  18), plus one untracked file and one file whose whole content is edited (so it
  empties out of the section when staged).
- [ ] **Step 2: Write `hunks.mjs`** asserting, in order:
  1. in a stack, the rows of slot 2 carry `data-hunk` (**this is where the unfixed
     build must fail**);
  2. clicking a row in slot 2 paints `.picked` **only inside slot 2**;
  3. `#hunk-bar` shows `stage selected (1)` and names slot 2's file;
  4. clicking a row in slot 1 moves the bar to slot 1 and slot 2's picks stay
     painted (D2);
  5. staging slot 1 leaves the reader's scroll anchor on the same file, re-fetches
     slot 1 (its picks gone, its `+/−` counts smaller) and leaves slots 2/3
     untouched;
  6. a file staged whole drops out of the stack and the stack does not jump;
  7. a conflicted row still shows `open resolver` and no `data-hunk`.
- [ ] **Step 3: Run it against the INSTALLED build** (`~/go/bin/gg`, v0.3.0-66):
  Expected: FAIL at assert 1. Record the output.
- [ ] **Step 4: Run it against the worktree build**, chromium AND firefox: Expected:
  `PROBE OK` twice.
- [ ] **Step 5: Re-run the plan 1/2/4a/4b probes** (`probe.mjs`, `wt.mjs`, `sym.mjs`,
  `mixed.mjs`, `notes.mjs`, `land.mjs`, `search.mjs`, `searchback.mjs`) against the
  worktree build. Every one green before the merge — plan 2's lesson: only the
  previous plan's probe caught the working-tree regression.
- [ ] **Step 6: Commit** nothing (the probes live in the scratchpad); report the
  evidence in the merge message.

---

## Task 13: web docs + the stale 4b help string

**Files:**
- Modify: `CHANGELOG.md`, `docs/CLAUDE-details.md`, `docs/web-tui-parity.md`,
  `internal/web/static/stackview.js`

- [ ] **Step 1** — `stackview.js`'s `S · stacked diff` help still ends "Search (/)
  works in the single-file view", which 4b made false. Fix it to say the search spans
  the whole stack and steps into unread files. (Guard: a `stackviewjs_test.go` assert
  that the string is gone.)
- [ ] **Step 2** — the web-half subsection of "Working-tree hunk staging inside a
  stack (plan 4c)" in `docs/CLAUDE-details.md`, the CHANGELOG entry, and the parity
  doc row. Note the **known gap**: the web's staged stack has no inline *unstage*
  (the single-file lane never had one either), while the TUI's `H` covers both — a
  pre-existing asymmetry, recorded, not fixed here.
- [ ] **Step 3: Commit**

```bash
git commit -am "docs: working-tree hunk staging inside a stack (web half)"
```

---

## Task 14: race gate, verify binaries, merge 2

- [ ] **Step 1** — `git status` in the main checkout FIRST (another session works
  there; a staged revert once blocked a merge — never stash it, wait).
- [ ] **Step 2** — `./test.sh race`; report tui / web / e2e counts.
- [ ] **Step 3** — `./build.sh` Linux + Windows + `./build.sh web` for
  `gg-web-new.exe`; deliver all three unprompted.
- [ ] **Step 4** — hand the merge to the user; after they merge, `./build.sh install`
  and confirm `gg --version` matches the merge sha.

---

## Self-review

- **Spec coverage.** §11 item 3 (per-slot `hunks`, inline pick/stage in the unstaged
  stack) = tasks 8–11; §9's reconcile = already shipped on both frontends, pinned by
  task 1 and exercised by task 12 assert 5; §9's conflict rule = D4, guarded by task 4
  (TUI) and task 12 assert 7. §5.1's `hunks` field lands in task 9.
- **The TUI is NOT symmetrical with the web here, by design**: the web picks inline,
  the TUI opens its existing full-screen picker over the stack. Said out loud in the
  architecture note and in the docs, because a reader will otherwise expect inline
  picking in `v.lines`.
- **Types.** `kctx = {picks: Set<int>}`; `s.hunks = {hash, count, picks}`;
  `hunkReload{path, staged}`; `hunkFileHere() (model.FileStatus, bool, string)`. Used
  with those shapes in every task that mentions them.
- **Known risk.** `activeHunks()` follows `activeDiff()`, whose anchor moves on
  SCROLL (`syncCursor`). Scrolling with picks selected therefore moves the bar off
  the picked file. Task 10 pins the anchor on a pick click; task 12 assert 4 is what
  proves the picks themselves survive. If the probe shows the bar wandering while the
  reader scrolls, the fix is to keep the bar on the last PICKED slot until its picks
  are cleared or staged — decide there, with the evidence, not now.
