# Worktree Rename & Move Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `gg` can rename or relocate a linked worktree directory (TUI `e` key / `.`-menu rows, CLI `gg worktree rename|move`) by wrapping `git worktree move`, with a reactive unlock decision and follow-the-move re-rooting when the current worktree moves.

**Architecture:** One git verb (`Repo.MoveWorktree`) → one engine op (`engine.MoveWorktree`, rename is the same op with `Dest` computed from the old parent + new name) → TUI popup + CLI subcommands. Locked worktrees resolve via the Decider (`move-worktree-locked`); the TUI chains `guardedReRoot` through the existing `pendingSwitch`/`Result.Path` mechanism when the moved worktree is the one gg is rooted in.

**Tech Stack:** Go 1.26, Bubble Tea TUI, real-git tests in `t.TempDir()`.

**Worktree:** all work happens in `/mnt/t/others/gigagit.worktrees/feat-worktree-rename-move` (branch `feat/worktree-rename-move`). Prefix every build/test command with `cd /mnt/t/others/gigagit.worktrees/feat-worktree-rename-move`.

**Spec:** `docs/superpowers/specs/2026-08-11-worktree-rename-move-design.md`

## Global Constraints

- One git verb = one git invocation; argv via `gitcmd`, run via `r.Runner.Run`/`.Stream`. Never shell out directly.
- Operations never block: forks via `deps.decide` with **option lists only**; include `"abort"` before anything happened; `Done` is emitted **on success only**.
- Decision ID + option strings are the cross-frontend API (exact string match): ID `move-worktree-locked`, options `unlock-and-move` / `abort`.
- `internal/tui` and `internal/cli` never import `internal/git` (archtest-guarded); they go through `internal/domain`.
- Every user-visible TUI string goes through `i18n.T` with a literal key present in **all four** bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`) — AST-gate tests fail otherwise. Engine summaries/prompts also need bundle entries (`engine_prose_test.go`). REQUIRED SUB-SKILL for those steps: `adding-translations`.
- Engine summary/prose only via `WithSummary`/`AppendSummary`/`PromptReq` (`msg.go` helpers).
- TUI `Model` is a value receiver; popups are pointer layers pushed via `m.pushLayer`.
- TDD throughout: write the failing test first, watch it fail, implement, watch it pass, commit.
- Finish gates: `gofmt -l internal/ cmd/` (empty), `go vet ./...`, `./test.sh race` before merge.

---

### Task 1: git verb `Repo.MoveWorktree`

**Files:**
- Modify: `internal/git/worktree.go` (after `WorktreeRepair`, ~line 123)
- Modify: `internal/engine/gitops.go` (worktree verb block, ~line 88)
- Test: `internal/git/worktree_verbs_test.go`

**Interfaces:**
- Consumes: `gitcmd.New`, `r.Runner.Stream` (existing).
- Produces: `MoveWorktree(ctx context.Context, fromDir, path, dest string, onLine func(string)) error` on `*git.Repo` AND on the `engine.GitOps` interface. Task 2 calls it via `deps.Repo.MoveWorktree`.

`fromDir` exists because of a Windows constraint: a directory that is any process's cwd cannot be renamed. The verb therefore runs `git -C <fromDir> …` (the caller passes the **main** worktree path) so the git subprocess's cwd is never inside the tree being moved. Same `-C` idiom as `ResetInDir` (`internal/git/worktree.go:142`).

- [ ] **Step 1: Write the failing verb test** in `internal/git/worktree_verbs_test.go` (copy the style of the existing `RemoveWorktree` verb test in that file — `newTestRepo(t)` helper, real git):

```go
func TestMoveWorktree(t *testing.T) {
	r, dir := newTestRepo(t) // adjust to the file's actual helper signature
	// create a linked worktree next to the repo
	wt := filepath.Join(filepath.Dir(dir), "wt-src")
	if err := r.AddWorktree(context.Background(), wt, "wt-branch", "HEAD", nil); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	dest := filepath.Join(filepath.Dir(dir), "wt-dst")
	if err := r.MoveWorktree(context.Background(), dir, wt, dest, nil); err != nil {
		t.Fatalf("MoveWorktree: %v", err)
	}
	wts, err := r.Worktrees(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range wts {
		if w.Path == dest && w.Branch == "wt-branch" {
			found = true
		}
		if w.Path == wt {
			t.Fatalf("old path still listed: %v", wts)
		}
	}
	if !found {
		t.Fatalf("dest not listed with its branch: %v", wts)
	}
}
```

Match the file's existing helper names/signatures exactly (read the neighboring `RemoveWorktree`/`AddWorktree` tests first); the assertions above are the contract.

- [ ] **Step 2: Run it — must fail to compile** (`MoveWorktree` undefined):
`cd /mnt/t/others/gigagit.worktrees/feat-worktree-rename-move && go test ./internal/git/ -run TestMoveWorktree -v`

- [ ] **Step 3: Implement the verb** in `internal/git/worktree.go` after `WorktreeRepair`:

```go
// MoveWorktree relocates the linked worktree at path to dest
// (`git -C <fromDir> worktree move <path> <dest>`). fromDir should be the
// MAIN worktree: the command must never run with its own cwd inside the tree
// being moved (Windows cannot rename a directory any process holds as cwd).
// onLine receives any output lines (nil is allowed). A refusal (locked tree,
// existing dest, submodules) is returned as an error.
func (r *Repo) MoveWorktree(ctx context.Context, fromDir, path, dest string, onLine func(string)) error {
	if onLine == nil {
		onLine = func(string) {}
	}
	argv := gitcmd.New("-C").Arg(fromDir, "worktree", "move", path, dest).ToArgv()
	_, err := r.Runner.Stream(ctx, "git worktree move", argv, onLine)
	return err
}
```

Add the same signature to the `GitOps` interface in `internal/engine/gitops.go` next to `RemoveWorktree` (line ~85).

- [ ] **Step 4: Run the test — PASS:** same command as Step 2.

- [ ] **Step 5: Commit:**
```bash
git add internal/git/worktree.go internal/git/worktree_verbs_test.go internal/engine/gitops.go
git commit -m "feat(git): MoveWorktree verb (git worktree move)"
```

---

### Task 2: engine op `engine.MoveWorktree`

**Files:**
- Create: `internal/engine/move_worktree.go`
- Test: `internal/engine/move_worktree_test.go`

**Interfaces:**
- Consumes: `deps.Repo.MoveWorktree(ctx, fromDir, path, dest, onLine)` (Task 1), `deps.Repo.Worktrees`, `deps.Repo.UnlockWorktree`, `isLockedWorktreeErr` + `samePath` (both already in `remove_worktree.go`), `PromptReq`, `Result.WithSummary`.
- Produces: `engine.MoveWorktree{Path, Dest string}` — an `Operation`; success `Result{Changed: true, Path: Dest}` with summary `"moved worktree %s → %s"`. Decision `move-worktree-locked` with options `unlock-and-move`/`abort`. Tasks 3–5 depend on exactly these strings.

No `LockMode()` method: the default reservation is TreeWrite, which is correct (same as `RepairWorktree`).

- [ ] **Step 1: Write failing engine tests** in `internal/engine/move_worktree_test.go`, copying `remove_worktree_test.go`'s harness (`newRepo(t)`, `MapDecider`, `drain(ch)` — read that file first and reuse its helper idioms):

Cover, as separate test funcs:
1. **Happy move:** create a linked worktree, run `MoveWorktree{Path: wt, Dest: dest}` with an empty `MapDecider{}` → no error, `res.Changed`, `res.Path == dest`, `git worktree list` (via the repo's `Worktrees`) shows dest and not the old path.
2. **Missing fields:** `MoveWorktree{}` → error containing "Path and Dest are required".
3. **Main worktree refused:** `Path` = the main worktree → error containing "main worktree".
4. **Dest exists:** pre-create the dest directory → error containing "already exists".
5. **Dest parent missing:** dest under a nonexistent parent → error naming the parent.
6. **Dest inside source:** dest = `filepath.Join(wt, "sub")` → error containing "inside the worktree".
7. **Locked → unlock-and-move:** `git worktree lock <wt>` (run via the test repo's runner or `exec.Command` in the test, matching how `remove_worktree_test.go` locks a tree — reuse its approach), `MapDecider{"move-worktree-locked": "unlock-and-move"}` → success, dest listed.
8. **Locked → abort:** `MapDecider{"move-worktree-locked": "abort"}` → no error, `res.Changed == false`, old path still listed.
9. **Done on success only:** drain events in test 1 → exactly one `Done`; in test 8 → no `Done`.

- [ ] **Step 2: Run — must fail to compile:**
`go test ./internal/engine/ -run TestMoveWorktree -v`

- [ ] **Step 3: Implement** `internal/engine/move_worktree.go`:

```go
package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MoveWorktree relocates a linked worktree directory to Dest
// (`git worktree move`). Rename is the same op — callers compute Dest from
// the old parent + the new name. A locked worktree resolves reactively via
// the Decider; every other refusal (existing dest, submodules) surfaces as an
// error. Default TreeWrite reservation. Result.Path carries Dest so
// frontends can chain a switch when the current worktree moved.
type MoveWorktree struct {
	Path string // absolute path of the worktree to move
	Dest string // absolute destination path
}

func (op MoveWorktree) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Path == "" || op.Dest == "" {
		return Result{}, fmt.Errorf("move worktree: Path and Dest are required")
	}
	if samePath(op.Path, op.Dest) {
		return Result{Changed: false}.WithSummary("nothing to move: source and destination are the same"), nil
	}
	if rel, err := filepath.Rel(op.Path, op.Dest); err == nil &&
		rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Result{}, fmt.Errorf("move worktree: destination %s is inside the worktree being moved", op.Dest)
	}
	wts, err := deps.Repo.Worktrees(ctx)
	if err != nil {
		return Result{}, err
	}
	// `git worktree list` always lists the main (primary) worktree first.
	if len(wts) == 0 {
		return Result{}, fmt.Errorf("move worktree: no worktrees listed")
	}
	if samePath(op.Path, wts[0].Path) {
		return Result{}, fmt.Errorf("move worktree: cannot move the main worktree (%s)", op.Path)
	}
	if _, err := os.Stat(op.Dest); err == nil {
		return Result{}, fmt.Errorf("move worktree: destination %s already exists", op.Dest)
	}
	if parent := filepath.Dir(op.Dest); parent != "" {
		if _, err := os.Stat(parent); err != nil {
			return Result{}, fmt.Errorf("move worktree: destination parent %s does not exist", parent)
		}
	}

	deps.emit(ctx, Progress{Step: "moving worktree", Detail: op.Dest})
	onLine := func(line string) { deps.emit(ctx, GitLine{Raw: line}) }
	// Run from the MAIN worktree so the git subprocess's cwd is never inside
	// the tree being moved (Windows cannot rename a process's cwd).
	if err := deps.Repo.MoveWorktree(ctx, wts[0].Path, op.Path, op.Dest, onLine); err != nil {
		if !isLockedWorktreeErr(err) {
			return Result{}, err
		}
		choice, derr := deps.decide(ctx, PromptReq("move-worktree-locked", "Worktree %s is locked. Unlock and move?", []string{"unlock-and-move", "abort"}, op.Path))
		if derr != nil {
			return Result{}, derr
		}
		if choice.Option != "unlock-and-move" {
			return Result{Changed: false}.WithSummary("cancelled; worktree not moved"), nil
		}
		if err := deps.Repo.UnlockWorktree(ctx, op.Path); err != nil {
			return Result{}, fmt.Errorf("unlock worktree: %w", err)
		}
		if err := deps.Repo.MoveWorktree(ctx, wts[0].Path, op.Path, op.Dest, onLine); err != nil {
			return Result{}, fmt.Errorf("move worktree (after unlock): %w", err)
		}
	}

	res := Result{Changed: true, Path: op.Dest}.WithSummary("moved worktree %s → %s", op.Path, op.Dest)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = MoveWorktree{}
```

Note the inside-source guard has no `rel != "."` case — `samePath` returned above already handled equality. `isLockedWorktreeErr` matches move's refusal too (git says "cannot move a locked working tree, lock reason: …").

- [ ] **Step 4: Run — all TestMoveWorktree* PASS:** same command as Step 2. Also `go test ./internal/engine/` (full package — no regressions).

- [ ] **Step 5: Commit:**
```bash
git add internal/engine/move_worktree.go internal/engine/move_worktree_test.go
git commit -m "feat(engine): MoveWorktree op with move-worktree-locked decision"
```

---

### Task 3: TUI — popup, `e` key, `.`-menu rows, footer, help, i18n

**Files:**
- Create: `internal/tui/move_worktree_popup.go`
- Modify: `internal/tui/avail.go` (~line 158, after `canDeleteWorktree`)
- Modify: `internal/tui/model.go` (normal-key switch; add `case "e"` near `case "d"` ~line 1553)
- Modify: `internal/tui/footer.go` (`contextBindings()`)
- Modify: `internal/tui/action_menu.go` (menu-only Move row — follow the existing menu-only-row idiom used by the tag-checkout `.`-menu row; grep `"Check out tag"` there)
- Modify: `internal/tui/source.go` (`opAffectedSources`, ~line 257)
- Modify: `internal/tui/help.go` (Worktrees section, near lines 76–77)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Test: `internal/tui/move_worktree_test.go`

**Interfaces:**
- Consumes: `engine.MoveWorktree{Path, Dest}` (Task 2), `textfield`/`newTextField`/`viewField`/`HandleEditKey`, `popupMax`, `m.pushLayer`/`m.popLayer`, `m.startOp`, `m.currentWorktree`, `m.worktrees`, `m.pendingSwitch` + `Result.Path` chain in `opFinishedMsg` (model.go:2211).
- Produces: `canMoveWorktree() bool` on `Model`; `openMoveWorktreePopup(wt model.Worktree, rename bool) Model`; `Model.pendingWorktreeMoveOld string` (consumed in Task 4).

The popup mirrors `checkoutAsPopup` (`internal/tui/checkout_as_popup.go`) exactly: enter dispatches, esc cancels, one textfield.

- [ ] **Step 1: Write failing TUI tests** in `internal/tui/move_worktree_test.go` (copy the harness of `worktree_delete_test.go`: `loadedModel(t)`/`newRepoDir(t)`, `keyMsg`, `driveOp`):

1. `e` on a linked-worktree row opens the popup with the field prefilled with the directory **basename**; typing a new name + enter runs the op; after `driveOp` the worktree list shows the new path.
2. `e` on the **main** worktree row is a no-op (no layer pushed).
3. The `.` menu on a linked-worktree row contains both a "Rename worktree…" and a "Move worktree…" row (see `action_menu_test.go` for how menu rows are asserted); the Move row's popup prefills the **full path**.
4. Rename face rejects a value containing `/`: enter keeps the popup open and sets a status message.
5. Renaming the CURRENT worktree: after the op finishes, the model re-rooted onto the new path (assert like `worktree_w_switch_test.go` asserts a switch) — this half may be written now but will only pass after Task 4; mark it with a `// passes after follow-the-move wiring` comment if you split the commits.

- [ ] **Step 2: Run — must fail:** `go test ./internal/tui/ -run TestMoveWorktree -v`

- [ ] **Step 3: Implement the popup** `internal/tui/move_worktree_popup.go`:

```go
package tui

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// moveWorktreePopup collects the destination for engine.MoveWorktree. The
// rename face edits just the directory NAME (dest joins the old parent); the
// move face edits the full absolute path. Enter dispatches directly — this
// popup IS the confirmation; esc cancels. Mirrors checkoutAsPopup.
type moveWorktreePopup struct {
	popupMax
	wt     model.Worktree
	rename bool
	field  textfield
}

func (p *moveWorktreePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		val := strings.TrimSpace(p.field.Value())
		if val == "" {
			return m, nil
		}
		dest := val
		if p.rename {
			if strings.ContainsAny(val, `/\`) {
				m.statusMsg = i18n.T("a new name cannot contain a path separator (use Move worktree…)")
				return m, nil
			}
			dest = filepath.Join(filepath.Dir(p.wt.Path), val)
		}
		if filepath.Clean(dest) == filepath.Clean(p.wt.Path) {
			return m.popLayer(), nil // unchanged: no-op
		}
		wt := p.wt
		m = m.popLayer()
		if wt.Path == m.currentWorktree {
			// gg's own cwd must leave the tree before git renames it (Windows
			// cannot rename a directory any process holds as cwd); the chained
			// reRoot below lands us in the new path.
			_ = os.Chdir(filepath.Dir(wt.Path))
			m.pendingSwitch = true // opFinishedMsg chains guardedReRoot(res.Path)
		}
		m.pendingWorktreeMoveOld = wt.Path
		return m.startOp(engine.MoveWorktree{Path: wt.Path, Dest: dest})
	default:
		p.field.HandleEditKey(msg)
	}
	return m, nil
}

func (p *moveWorktreePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	var b strings.Builder
	title := i18n.T("Move worktree %s", p.wt.Path)
	label := i18n.T("path: ")
	verb := i18n.T("[enter] move   [esc] cancel")
	if p.rename {
		title = i18n.T("Rename worktree %s", filepath.Base(p.wt.Path))
		label = i18n.T("name: ")
		verb = i18n.T("[enter] rename   [esc] cancel")
	}
	b.WriteString(title + "\n\n")
	b.WriteString(viewField(label, p.field, true, popupContentWidth(w)) + "\n\n")
	b.WriteString(verb)
	box := modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}

// openMoveWorktreePopup pushes the rename/move popup for wt; rename prefills
// the basename, move the full path.
func (m Model) openMoveWorktreePopup(wt model.Worktree, rename bool) Model {
	prefill := wt.Path
	if rename {
		prefill = filepath.Base(wt.Path)
	}
	return m.pushLayer(&moveWorktreePopup{wt: wt, rename: rename, field: newTextField(prefill)})
}
```

Verify `render` against `checkoutAsPopup.render` for the current helper names (`popupContentWidth`, `popupResolveWidth`, `popupInnerWidth`, `overlayCenter`, `clipToHeight`) and adjust to whatever that file actually uses today.

Add to `internal/tui/avail.go` after `canDeleteWorktree`:

```go
// canMoveWorktree gates e / the rename+move menu rows on Worktrees: any
// linked worktree may move — including the current one (the TUI follows the
// move) — but never the main worktree.
func (m Model) canMoveWorktree() bool {
	wt, ok := m.selectedWorktree()
	return m.opsIdle() && ok && len(m.worktrees) > 0 && wt.Path != m.worktrees[0].Path
}
```

Add the model field next to `pendingRepairSwitch` (model.go:54):

```go
pendingWorktreeMoveOld string // old path of a just-moved worktree; MRU registry cleanup in opFinishedMsg
```

Add the key case in model.go's normal-key switch (near `case "d"`):

```go
case "e":
	if m.focus == panelWorktrees && m.canMoveWorktree() {
		wt, _ := m.selectedWorktree()
		return m.openMoveWorktreePopup(wt, true), nil
	}
```

Footer binding in `contextBindings()` (footer.go) — gives the footer hint AND the `.`-menu rename row:

```go
{"rename-worktree", "e", i18n.T("[e] rename"), func(m Model) bool {
	return m.focus == panelWorktrees && m.canMoveWorktree()
}, scopeRow},
```

Menu-only Move row: follow the same idiom the tag-checkout `.`-menu row uses in `action_menu.go` (a row with a `run` func and no key — grep `"Check out tag"`); label `i18n.T("Move worktree…")`, gated on `m.canMoveWorktree()`, running `return m.openMoveWorktreePopup(wt, false), nil`. Give the rename menu row the same treatment ONLY if the footer-binding row does not already appear in the menu (check `availableActions` — scopeRow bindings become rows automatically).

`opAffectedSources` (source.go): add

```go
case engine.MoveWorktree:
	// The worktree list changed; Branches shows per-branch worktree markers.
	// (A current-worktree move chains a full reRoot before this is consulted.)
	return []sourceKey{srcBranches, srcWorktrees}
```

Help rows (help.go, Worktrees section next to lines 76–77):

```go
r("e", i18n.T("rename the selected worktree directory (git worktree move; the current worktree follows the move)")),
r(".", i18n.T("Move worktree (.-menu): relocate the selected worktree to any path")),
```

- [ ] **Step 4: i18n bundles.** REQUIRED SUB-SKILL: `adding-translations`. Add every new literal key to all four bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`). New keys from this task + Task 2's engine prose:
  - `"Rename worktree %s"`, `"Move worktree %s"`, `"name: "` (exists? check — reuse if present), `"path: "`, `"[enter] rename   [esc] cancel"`, `"[enter] move   [esc] cancel"`, `"[e] rename"`, `"Move worktree…"`, `"Rename worktree…"` (if a menu row label is added), `"a new name cannot contain a path separator (use Move worktree…)"`, both help-row strings
  - engine: `"moved worktree %s → %s"`, `"cancelled; worktree not moved"`, `"nothing to move: source and destination are the same"`, `"moving worktree"` (check how Progress steps are gated), `"Worktree %s is locked. Unlock and move?"`, decision options `unlock-and-move`/`abort` (options stay English as VALUES; check `options_vocab_test.go` for whether option RENDERING needs vocab entries — follow what `worktree-locked` does)
  Then run the gates: `go test ./internal/tui/ -run 'TestI18n|TestOptionsVocab|TestMenuLabels|TestEngineProse' -v` — they name any key still missing; fix until green.

- [ ] **Step 5: Run the TUI tests — PASS** (except the follow-the-move test if split): `go test ./internal/tui/ -run TestMoveWorktree -v`, then the full package `go test ./internal/tui/`.

- [ ] **Step 6: Commit:**
```bash
git add internal/tui/ internal/i18n/lang/
git commit -m "feat(tui): worktree rename/move popup, e key, menu rows, i18n"
```

---

### Task 4: TUI — follow the move (reRoot chain + MRU registry cleanup)

**Files:**
- Modify: `internal/tui/model.go` (`opFinishedMsg` handler ~lines 2205–2240; `reRoot` pending-reset block ~line 3233)
- Test: `internal/tui/move_worktree_test.go` (the current-worktree test from Task 3 Step 1.5)

**Interfaces:**
- Consumes: `m.pendingWorktreeMoveOld` + `m.pendingSwitch` (set in Task 3), `repos.Remove(statePath, repoPath)`, `repos.DefaultStatePath()`, existing `switchTo = msg.res.Path` chain.
- Produces: nothing new — behavior only.

The `pendingSwitch && msg.res.Path != ""` → `guardedReRoot` chain already exists (model.go:2211-2213, 2233-2235); Task 3 armed it. This task adds only the registry cleanup and the reset hygiene.

- [ ] **Step 1: Run the current-worktree test from Task 3 — confirm what fails.** If it already passes (the pendingSwitch chain is sufficient), this task reduces to the registry cleanup + resets; still add the assertions below.

- [ ] **Step 2: Extend the test:** seed the MRU registry (`repos.Touch(statePath, oldPath, time.Now())` with a `t.TempDir()` statePath — check how `repo_switcher` tests inject a statePath; if the model reads `repos.DefaultStatePath()` directly, route it the same way those tests do) and assert that after a successful move the old path is gone from `repos.Load(statePath)`.

- [ ] **Step 3: Implement.** In the `opFinishedMsg` success branch (inside `if msg.res.Changed { … }`, next to `repairSwitch = m.pendingRepairSwitch`):

```go
if m.pendingWorktreeMoveOld != "" {
	// The moved-from path must not linger in the repo switcher's MRU;
	// the destination registers itself on next open (load.go Touches
	// the current worktree), incl. immediately via the chained reRoot.
	_ = repos.Remove(repos.DefaultStatePath(), m.pendingWorktreeMoveOld)
}
```

Unconditional reset next to the other pendings (after the success/error branch, with the `// unconditional` comments):

```go
m.pendingWorktreeMoveOld = "" // unconditional; covers both error and success paths
```

And in `reRoot`'s stale-pending reset block (~line 3233, next to `m.pendingRepairSwitch = ""`):

```go
m.pendingWorktreeMoveOld = "" // a repo switch must not fire a stale move cleanup
```

- [ ] **Step 4: Run — PASS:** `go test ./internal/tui/ -run TestMoveWorktree -v` then `go test ./internal/tui/`.

- [ ] **Step 5: Commit:**
```bash
git add internal/tui/
git commit -m "feat(tui): follow a current-worktree move with reRoot + MRU cleanup"
```

---

### Task 5: CLI — `gg worktree rename` / `gg worktree move`

**Files:**
- Modify: `internal/cli/worktree.go` (dispatch at line 26–42; new funcs at the end)
- Test: `internal/cli/worktree_test.go`

**Interfaces:**
- Consumes: `engine.MoveWorktree` (Task 2), `runOperation`, `finish`, `cliDecider`, `stdinIsTerminal`, the worktree-target resolution loop from `cmdWorktreeRemove` (worktree.go:279–305).
- Produces: `gg worktree rename [--force] <worktree> <new-name>` and `gg worktree move [--force] <worktree> <new-path>`; `--force` maps `move-worktree-locked → unlock-and-move`. `<worktree>` resolves by path (abs, cwd-relative, or main-top-relative — exactly like `remove`) **or by branch name**.

- [ ] **Step 1: Write failing CLI tests** in `internal/cli/worktree_test.go` (copy the harness of the existing worktree remove tests: `newCLIRepo(t)` + `Run(dir, args, stdin, stdout, stderr, cwdFile)`):

1. `worktree move <old> <new>`: worktree listed at the new path afterward, exit 0, ✓ line on stdout.
2. `worktree rename <branch-name> <new-name>`: target resolved by BRANCH, directory renamed in place (same parent).
3. `worktree rename x new/name` → exit 2, stderr says a new name cannot contain a path separator.
4. `worktree move` with an unknown target → exit 1, "no worktree at".
5. Locked worktree, non-interactive, no `--force` → non-zero exit (missing-decision error from `cliDecider`).
6. Locked worktree with `--force` → moved.
7. `worktree move` of the worktree containing the process cwd writes the moved-equivalent path into `cwdFile` (mirror how the add test asserts cwdFile).

- [ ] **Step 2: Run — must fail:** `go test ./internal/cli/ -run 'TestWorktreeMove|TestWorktreeRename' -v`

- [ ] **Step 3: Implement.** In `cmdWorktree`'s switch add:

```go
case "move":
	return cmdWorktreeMove(svc, args[1:], stdin, stdout, stderr, cwdFile, false)
case "rename":
	return cmdWorktreeMove(svc, args[1:], stdin, stdout, stderr, cwdFile, true)
```

Update the two usage strings in `cmdWorktree` to `<list|add|remove|move|rename|prune>`, and pass `cwdFile` through (it is already a parameter of `cmdWorktree`).

New function at the end of the file:

```go
// cmdWorktreeMove implements `gg worktree move [--force] <worktree> <new-path>`
// and, with rename=true, `gg worktree rename [--force] <worktree> <new-name>`
// (same-parent move). The target resolves by path — absolute, cwd-relative,
// or main-worktree-relative, exactly like `worktree remove` — or by branch
// name. --force answers the move-worktree-locked fork with unlock-and-move.
func cmdWorktreeMove(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer, cwdFile string, rename bool) int {
	verb := "move"
	if rename {
		verb = "rename"
	}
	fs := flag.NewFlagSet("worktree "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "unlock a locked worktree and move it")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 || fs.Arg(0) == "" || fs.Arg(1) == "" {
		if rename {
			fmt.Fprintln(stderr, "worktree rename: usage: gg worktree rename [--force] <worktree> <new-name>")
		} else {
			fmt.Fprintln(stderr, "worktree move: usage: gg worktree move [--force] <worktree> <new-path>")
		}
		return 2
	}
	target, dest := fs.Arg(0), fs.Arg(1)

	ctxBg := context.Background()
	wts, err := svc.Worktrees(ctxBg)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	absTarget, _ := filepath.Abs(target)
	fromTop := ""
	if !filepath.IsAbs(target) && len(wts) > 0 && wts[0].Path != "" {
		fromTop = filepath.Clean(filepath.Join(wts[0].Path, target))
	}
	var match *model.Worktree
	for i := range wts {
		if wts[i].Path == target || wts[i].Path == absTarget ||
			(fromTop != "" && wts[i].Path == fromTop) ||
			(wts[i].Branch != "" && wts[i].Branch == target) {
			match = &wts[i]
			break
		}
	}
	if match == nil {
		fmt.Fprintf(stderr, "worktree %s: no worktree at %q\n", verb, target)
		return 1
	}

	if rename {
		if strings.ContainsAny(dest, `/\`) {
			fmt.Fprintf(stderr, "worktree rename: a new name cannot contain a path separator (use `gg worktree move`)\n")
			return 2
		}
		dest = filepath.Join(filepath.Dir(match.Path), dest)
	} else if !filepath.IsAbs(dest) {
		dest, _ = filepath.Abs(dest)
	}

	policy := map[string]string{}
	if *force {
		policy["move-worktree-locked"] = "unlock-and-move"
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}

	// If the process cwd sits inside the tree being moved, leave it first
	// (Windows cannot rename a directory any process holds as cwd) and hand
	// the shell wrapper the equivalent new path afterward.
	movedCwdRel := ""
	if cwd, err := os.Getwd(); err == nil {
		if rel, rerr := filepath.Rel(match.Path, cwd); rerr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			movedCwdRel = rel
			_ = os.Chdir(filepath.Dir(match.Path))
		}
	}

	res, err := runOperation(ctxBg, svc, engine.MoveWorktree{Path: match.Path, Dest: dest}, dec, stderr)
	if err == nil && res.Changed && movedCwdRel != "" && cwdFile != "" {
		_ = os.WriteFile(cwdFile, []byte(filepath.Join(dest, movedCwdRel)), 0o644)
	}
	return finish(res, err, stdout, stderr)
}
```

- [ ] **Step 4: Run — PASS:** `go test ./internal/cli/ -run 'TestWorktreeMove|TestWorktreeRename' -v`, then `go test ./internal/cli/`.

- [ ] **Step 5: Check `cmd/gg/main.go`'s unknown-command help string** (~line 37 region): `worktree` is already listed as a top-level command — verify the line doesn't enumerate subcommands; if it does, add `move|rename`. Also grep `internal/cli/cli.go` for a `worktree` usage/help line and update it the same way.

- [ ] **Step 6: Commit:**
```bash
git add internal/cli/ cmd/gg/
git commit -m "feat(cli): gg worktree rename + move"
```

---

### Task 6: e2e scenario

**Files:**
- Create: `e2e/scenarios/s87_worktree_move.toml`

REQUIRED SUB-SKILL: `writing-e2e-scenarios` (schema + operation contracts; verify the run/expect keys below against it before writing).

**Interfaces:**
- Consumes: the CLI surface from Task 5; `expect.worktrees` is **sandbox-root-relative** (`e2e/scenario.go:217`).

- [ ] **Step 1: Write the scenario** (numbering: next free is s87 — re-check `ls e2e/scenarios | tail` first):

```toml
name = "worktree rename + move: directory relocated, branch checkout preserved"

[input]
steps = [
  { write = "README.md", content = "hello\n" },
  { commit = "initial" },
]

[[run]]
cmd  = ["worktree", "add", "main"]
exit = 0

[[run]]
cmd  = ["worktree", "rename", "../wt/wt-main", "wt-renamed"]
exit = 0

[[run]]
cmd  = ["worktree", "move", "../wt/wt-renamed", "../wt-moved"]
exit = 0

[expect]
branch    = "main"
worktrees = ["wt-moved"]
```

The two relative run-targets rely on the main-top-relative resolution (same as `s17_worktree_remove.toml`'s `../wt/wt-main`). Adjust the `worktrees` expectation to the harness's sandbox-relative form if `wt-moved` lands elsewhere — run the test and read the assert diff.

- [ ] **Step 2: Run — PASS:** `cd /mnt/t/others/gigagit.worktrees/feat-worktree-rename-move && ./test.sh e2e`

- [ ] **Step 3: Commit:**
```bash
git add e2e/scenarios/s87_worktree_move.toml
git commit -m "test(e2e): worktree rename + move scenario"
```

---

### Task 7: docs, agent skill sync, final gates

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go` (or wherever `Version` lives — grep `agentskill.Version`), `.claude/skills/using-gg/SKILL.md` (regenerated)

- [ ] **Step 1: CHANGELOG.md** — add an entry under the current unreleased/top section following the file's existing style: worktree rename & move (TUI `e` + `.`-menu, CLI `gg worktree rename|move`, `move-worktree-locked` decision, follow-the-move re-root).

- [ ] **Step 2: README.md** — Worktrees key table gains `e` (rename); the CLI command list gains `worktree rename|move`. Match the file's existing table/list formatting.

- [ ] **Step 3: using-gg sync** (REQUIRED SUB-SKILL: `defining-agentic-tasks` names the full rule): document both subcommands + `--force` + the `move-worktree-locked`/`unlock-and-move` policy in `internal/agentskill/using-gg.md`, bump `agentskill.Version`, rebuild (`go build ./cmd/gg`), run `gg init --update` **from the main checkout, not the worktree** (memory: `gg init --to` from a worktree writes a dead target), and commit the regenerated `.claude/skills/using-gg/SKILL.md`.

- [ ] **Step 4: Final gates** (quiet machine for race — memory: `race-gate-needs-a-quiet-machine`):
```bash
cd /mnt/t/others/gigagit.worktrees/feat-worktree-rename-move
gofmt -l internal/ cmd/        # must print nothing
go vet ./...
./test.sh                      # staged: vet+gofmt → unit → e2e
./test.sh race                 # before merge
```

- [ ] **Step 5: Commit:**
```bash
git add CHANGELOG.md README.md internal/agentskill/ .claude/skills/using-gg/
git commit -m "docs: worktree rename/move — changelog, README, using-gg v+1"
```

- [ ] **Step 6: STOP — do not merge.** Ask the user to review; the human merges (memory: `worktree-and-merge-workflow`). Offer a built binary for testing via SendUserFile with the absolute worktree path (memory: `build-gg-in-worktree-for-testing`).
