# Confirm Slow Working-Tree Operations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pop a yes/no confirmation (movable, default **No**, also `y`/`n`/`esc`) before any slow working-tree-rewriting TUI operation, on by default with a config opt-out.

**Architecture:** Reuse the existing `decisionState` modal. A new `confirm bool` field on it enables `y`/`n` accelerators and is gated so engine-driven decisions are untouched. A `confirmOp(op, prompt)` helper either pops the modal (running the op on "Yes") or, when disabled by config, launches the op directly. Slow-op `startOp(...)` call sites are rewritten to `confirmOp(...)`. A new inverted config field `[ui] disable_slow_op_confirm` (default false ⇒ confirm ON) controls it.

**Tech Stack:** Go 1.26, Bubble Tea (Elm-style value-receiver `Model`), TOML config, table tests against a real `git` / `FakeRunner`.

## Global Constraints

- **TUI only.** No CLI change — `cliDecider` resolves forks from flag policy/stdin and must never block on a human.
- **`internal/tui` must not import `internal/git`** (archtest-guarded). Ops are launched via `m.startOp`, reads via domain.
- **`Model` is a value receiver** with pointer fields (`modal`). `m.modal = &decisionState{...}` is the established pattern.
- **Config is read-only at runtime** (except the existing oplog toggle). The new field is read at startup-load into `m.cfg`, like every other `[ui]` field.
- **Config overlay propagates only `true` upward** — hence the inverted field name.
- Commit message footer (every commit):
  ```
  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01K3f8q5u5ztfdgEd5o55F8v
  ```
- Run tests from the worktree root: `.claude/worktrees/confirm-slow-ops`.

---

### Task 1: Config field `[ui] disable_slow_op_confirm`

**Files:**
- Modify: `internal/config/config.go` (`UIConfig` struct ~line 44; `overlayUI` ~line 174)
- Modify: `internal/config/template.go` (`settingDocs` slice, after the `show_eol_only_changes` row ~line 53)
- Test: `internal/config/config_test.go` (overlay), `internal/config/template_test.go` (coverage test already exists — must stay green)

**Interfaces:**
- Produces: `config.UIConfig.DisableSlowOpConfirm bool` (toml `disable_slow_op_confirm`); default `false` ⇒ confirmation ON. A `true` in a higher layer overlays up to disable.

- [ ] **Step 1: Write the failing overlay test**

Add to `internal/config/config_test.go`:

```go
func TestOverlayDisableSlowOpConfirm(t *testing.T) {
	// Default zero value: confirmation enabled (field false).
	var def UIConfig
	if def.DisableSlowOpConfirm {
		t.Fatal("zero UIConfig should leave slow-op confirm enabled (DisableSlowOpConfirm=false)")
	}
	// A true in a higher layer overlays up to disable.
	dst := UIConfig{}
	overlayUI(&dst, UIConfig{DisableSlowOpConfirm: true})
	if !dst.DisableSlowOpConfirm {
		t.Fatal("overlayUI did not propagate DisableSlowOpConfirm=true")
	}
	// A false in a higher layer does NOT clear a true already set (OR-only).
	dst2 := UIConfig{DisableSlowOpConfirm: true}
	overlayUI(&dst2, UIConfig{DisableSlowOpConfirm: false})
	if !dst2.DisableSlowOpConfirm {
		t.Fatal("overlayUI must not clear an existing true (OR-only semantics)")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/config/ -run TestOverlayDisableSlowOpConfirm`
Expected: FAIL — `dst.DisableSlowOpConfirm` undefined / field missing.

- [ ] **Step 3: Add the struct field**

In `internal/config/config.go`, inside `UIConfig`, directly after the `ShowEOLOnlyChanges` field (~line 44):

```go
	// DisableSlowOpConfirm turns OFF the yes/no confirmation shown before slow
	// working-tree operations (switch, checkout, pull, merge, rebase,
	// fast-forward, reset). Inverted polarity: default false ⇒ confirmation ON;
	// only a true in a higher layer overlays (matching the zero-is-unset rule).
	DisableSlowOpConfirm bool `toml:"disable_slow_op_confirm"`
```

- [ ] **Step 4: Add the overlay line**

In `overlayUI` (`internal/config/config.go`), after the `ShowEOLOnlyChanges` overlay block (~line 176):

```go
	if src.DisableSlowOpConfirm {
		dst.DisableSlowOpConfirm = true
	}
```

- [ ] **Step 5: Add the settingDoc row**

In `internal/config/template.go`, in `settingDocs`, immediately after the `show_eol_only_changes` row:

```go
	{"ui", "disable_slow_op_confirm", false, "skip the yes/no confirmation shown before slow working-tree ops (switch, checkout, pull, merge, rebase, fast-forward, reset)"},
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/config/`
Expected: PASS — including `TestOverlayDisableSlowOpConfirm` and the existing `TestSettingDocsCoverAllFields` (which now sees the field documented).

- [ ] **Step 7: Commit**

```bash
git add internal/config/
git commit  # message: "feat(config): [ui] disable_slow_op_confirm (default: confirm on)"
```

---

### Task 2: Confirm machinery — `decisionState.confirm`, `y`/`n` keys, `confirmOp`, `confirmSlowOps`

**Files:**
- Modify: `internal/tui/op.go` (`decisionState` struct ~line 212)
- Modify: `internal/tui/model.go` (modal key handler ~lines 518-545; add `confirmOp` + `confirmSlowOps` helpers; add `resolveModal` helper)
- Test: `internal/tui/confirm_op_test.go` (new)

**Interfaces:**
- Consumes: `config.UIConfig.DisableSlowOpConfirm` (Task 1); `m.startOp(engine.Operation) (Model, tea.Cmd)`; `abortOption([]string) string` (returns last option for `["Yes","No"]` ⇒ `"No"`).
- Produces:
  - `decisionState.confirm bool`
  - `func (m Model) confirmSlowOps() bool` ⇒ `!m.cfg.UI.DisableSlowOpConfirm`
  - `func (m Model) confirmOp(op engine.Operation, prompt string) (tea.Model, tea.Cmd)`
  - `func (m Model) resolveModal(opt string) (tea.Model, tea.Cmd)`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/confirm_op_test.go`. (`newDrivableModel`/drive helpers: mirror the style already used in `modal_test.go` — find the helper that builds a `Model` with a fake/real service and a window size; reuse it verbatim. Below assumes a `newTestModel(t)` helper exists in the package's test files; if it is named differently, use that name.)

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
)

// stubOp is an inert operation used to observe whether confirmOp launched one.
type stubOp struct{ ran *bool }

func (stubOp) Name() string { return "stub" }
func (s stubOp) LockMode() engine.LockMode { return engine.LockTreeWrite }
func (s stubOp) Run(ctx engineRunCtx, d engine.OpDeps) (engine.Result, error) {
	if s.ran != nil {
		*s.ran = true
	}
	return engine.Result{}, nil
}
```

NOTE: the exact `Operation` interface signature lives in
`internal/engine/operation.go:59`. Before writing `stubOp`, open it and copy
the real method set (Name / LockMode / Run signatures) exactly — the stub above
is illustrative. If a test-only operation stub already exists in the `tui` or
`engine` test files, reuse it instead of defining `stubOp`.

Then the behavior test:

```go
func TestConfirmOpPopsModalDefaultNo(t *testing.T) {
	m := newTestModel(t) // build a loaded model; reuse the existing helper
	tm, _ := m.confirmOp(engine.SmartSwitch{Branch: "x"}, "Switch to x?")
	mm := tm.(Model)
	if mm.modal == nil {
		t.Fatal("confirmOp should pop a modal when confirmation is enabled")
	}
	if !mm.modal.confirm {
		t.Fatal("confirm modal must set confirm=true")
	}
	if got := mm.modal.req.Options; len(got) != 2 || got[0] != "Yes" || got[1] != "No" {
		t.Fatalf("options = %v, want [Yes No]", got)
	}
	if mm.modal.req.Options[mm.modal.sel] != "No" {
		t.Fatalf("default selection = %q, want No", mm.modal.req.Options[mm.modal.sel])
	}
}

func TestConfirmOpDisabledRunsDirectly(t *testing.T) {
	m := newTestModel(t)
	m.cfg.UI.DisableSlowOpConfirm = true
	tm, cmd := m.confirmOp(engine.SmartSwitch{Branch: "x"}, "Switch to x?")
	mm := tm.(Model)
	if mm.modal != nil {
		t.Fatal("disabled confirmation must not pop a modal")
	}
	if cmd == nil {
		t.Fatal("disabled confirmation must launch the op (non-nil cmd)")
	}
}
```

And the key-accelerator test (drives the modal handler through `Update`):

```go
func TestConfirmModalKeys(t *testing.T) {
	key := func(s string) tea.KeyMsg {
		if len(s) == 1 {
			return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
		}
		switch s {
		case "enter":
			return tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			return tea.KeyMsg{Type: tea.KeyEsc}
		}
		return tea.KeyMsg{}
	}

	// enter on the default selection = No = no op launched.
	t.Run("enter defaults to No", func(t *testing.T) {
		m := newTestModel(t)
		tm, _ := m.confirmOp(engine.SmartSwitch{Branch: "x"}, "p?")
		tm, cmd := tm.(Model).Update(key("enter"))
		if tm.(Model).modal != nil {
			t.Fatal("enter should dismiss the modal")
		}
		if cmd != nil {
			t.Fatal("enter on default (No) must not launch the op")
		}
	})

	// y launches the op and dismisses.
	t.Run("y confirms", func(t *testing.T) {
		m := newTestModel(t)
		tm, _ := m.confirmOp(engine.SmartSwitch{Branch: "x"}, "p?")
		tm, cmd := tm.(Model).Update(key("y"))
		if tm.(Model).modal != nil {
			t.Fatal("y should dismiss the modal")
		}
		if cmd == nil {
			t.Fatal("y must launch the op")
		}
	})

	// n / esc dismiss without launching.
	for _, k := range []string{"n", "esc"} {
		t.Run(k+" cancels", func(t *testing.T) {
			m := newTestModel(t)
			tm, _ := m.confirmOp(engine.SmartSwitch{Branch: "x"}, "p?")
			tm, cmd := tm.(Model).Update(key(k))
			if tm.(Model).modal != nil {
				t.Fatalf("%s should dismiss the modal", k)
			}
			if cmd != nil {
				t.Fatalf("%s must not launch the op", k)
			}
		})
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run 'TestConfirmOp|TestConfirmModalKeys'`
Expected: FAIL — `confirm` field / `confirmOp` undefined. (If `newTestModel` is not the helper name, fix the test to call the real one before proceeding — do not invent a helper.)

- [ ] **Step 3: Add the `confirm` field**

In `internal/tui/op.go`, `decisionState` struct:

```go
type decisionState struct {
	req       engine.DecisionRequest
	reply     chan engine.DecisionResponse
	sel       int
	confirm   bool // yes/no confirm modal: enables y/n accelerators (frontend-only)
	onResolve func(m Model, opt string) (tea.Model, tea.Cmd)
}
```

- [ ] **Step 4: Add `confirmSlowOps`, `confirmOp`, `resolveModal` helpers**

In `internal/tui/model.go` (near the other `Model` helpers, e.g. by `wheelStep`):

```go
// confirmSlowOps reports whether slow working-tree ops should pop a yes/no
// confirmation first. On by default; the inverted [ui] disable_slow_op_confirm
// turns it off. m.cfg is the zero Config only before the first load, where the
// zero value (false) also yields confirm-on — the desired default.
func (m Model) confirmSlowOps() bool { return !m.cfg.UI.DisableSlowOpConfirm }

// confirmOp guards a slow working-tree operation behind a yes/no modal whose
// default (highlighted, and so enter) selection is No. y/Y confirm, n/N/esc
// cancel. When confirmation is disabled it launches the op directly.
func (m Model) confirmOp(op engine.Operation, prompt string) (tea.Model, tea.Cmd) {
	if !m.confirmSlowOps() {
		return m.startOp(op)
	}
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "confirm-slow-op",
			Prompt:  prompt,
			Options: []string{"Yes", "No"},
		},
		sel:     1, // default highlight = No
		confirm: true,
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt == "Yes" {
				return m.startOp(op)
			}
			return m, nil
		},
	}
	return m, nil
}

// resolveModal answers the active modal with opt, clearing it. Frontend-only
// decisions go through onResolve; engine-driven ones reply over the channel.
func (m Model) resolveModal(opt string) (tea.Model, tea.Cmd) {
	if r := m.modal.onResolve; r != nil {
		m.modal = nil
		return r(m, opt)
	}
	m.modal.reply <- engine.DecisionResponse{Option: opt}
	m.modal = nil
	return m, nil
}
```

- [ ] **Step 5: Route the modal key handler through `resolveModal` and add `y`/`n`**

In `internal/tui/model.go`, replace the `enter` / `esc` cases inside the
`if m.modal != nil { switch msg.String() {` block (currently ~lines 528-543)
and add the accelerators. The full updated switch body:

```go
		switch msg.String() {
		case "up", "k":
			if m.modal.sel > 0 {
				m.modal.sel--
			}
		case "down", "j":
			if m.modal.sel < len(m.modal.req.Options)-1 {
				m.modal.sel++
			}
		case "enter":
			return m.resolveModal(m.modal.req.Options[m.modal.sel])
		case "y", "Y":
			if m.modal.confirm {
				return m.resolveModal(m.modal.req.Options[0]) // "Yes"
			}
		case "n", "N":
			if m.modal.confirm {
				return m.resolveModal(abortOption(m.modal.req.Options)) // "No"
			}
		case "esc":
			return m.resolveModal(abortOption(m.modal.req.Options))
		}
		return m, nil
```

(`abortOption(["Yes","No"])` returns the last option, `"No"`. The `y`/`n`
cases are inert unless `confirm` is set, so engine-driven decisions and the
existing `discard` / `switch-to-worktree` modals are unaffected.)

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/tui/ -run 'TestConfirmOp|TestConfirmModalKeys'`
Expected: PASS.

- [ ] **Step 7: Guard against regressions in existing modal tests**

Run: `go test ./internal/tui/ -run 'Modal|Decision|Discard'`
Expected: PASS — enter/esc on non-confirm modals still resolve correctly.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/op.go internal/tui/model.go internal/tui/confirm_op_test.go
git commit  # message: "feat(tui): confirmOp yes/no modal (default No; y/n/esc) for slow ops"
```

---

### Task 3: Wire key-triggered slow ops (`s`, `p`, remote stay/switch)

**Files:**
- Modify: `internal/tui/model.go` (the `case "s":` / `case "p":` handlers ~lines 744-808)
- Test: `internal/tui/confirm_wiring_test.go` (new)

**Interfaces:**
- Consumes: `m.confirmOp(op, prompt) (tea.Model, tea.Cmd)` (Task 2).

- [ ] **Step 1: Write the failing test**

Create `internal/tui/confirm_wiring_test.go`. Drive the loaded model so a
branch is selected and `s` is pressable, then assert a confirm modal appears
(rather than the op firing). Reuse the focus/selection helpers the existing
`switch_to_worktree_test.go` / `nav_test.go` use to land focus on Branches with
a non-current branch selected.

```go
func TestSwitchKeyPopsConfirm(t *testing.T) {
	m := loadedModelOnBranches(t) // helper: model with Branches focused, a
	                              // non-current, non-worktree branch selected.
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	mm := tm.(Model)
	if mm.modal == nil || !mm.modal.confirm {
		t.Fatal("s on a switchable branch should pop the slow-op confirm")
	}
	if mm.modal.req.Options[mm.modal.sel] != "No" {
		t.Fatal("confirm default must be No")
	}
}
```

If no `loadedModelOnBranches` helper exists, build the selection inline using
the same steps `switch_to_worktree_test.go` uses, then call Update with `"s"`.
Do NOT assert against a worktree-resident branch (that path is the
`switch-to-worktree` modal, a carve-out).

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run TestSwitchKeyPopsConfirm`
Expected: FAIL — `s` currently calls `startOp` directly (no modal, or the op
starts).

- [ ] **Step 3: Wire the four key sites**

In `internal/tui/model.go`:

`case "p":` (~line 746):
```go
				return m.confirmOp(m.pullForFocus(), "Pull "+m.pullPromptTarget()+"?")
```
where `pullPromptTarget` is a tiny helper returning the focused branch name or
`"current branch"` — OR, to avoid a new helper, inline a fixed prompt:
`"Pull? This may rewrite the working tree."`. Pick the fixed prompt to keep the
task small.

Final form for `case "p":`:
```go
		case "p":
			if m.canPull() { // keep whatever guard currently wraps the pull
				return m.confirmOp(m.pullForFocus(), "Pull? This may rewrite the working tree.")
			}
```
(Match the existing guard structure around line 744-746 exactly; only the
`startOp` call changes to `confirmOp`.)

Remote stay (~line 759):
```go
				return m.confirmOp(engine.SmartCheckout{RemoteRef: rb.Name, Local: rb.Branch, Intent: engine.CheckoutStay}, "Check out "+rb.Branch+"?")
```

Remote switch (~line 780):
```go
				return m.confirmOp(engine.SmartCheckout{RemoteRef: rb.Name, Local: rb.Branch, Intent: engine.CheckoutSwitch}, "Switch to "+rb.Branch+"?")
```

Branch switch (~line 808):
```go
			return m.confirmOp(engine.SmartSwitch{Branch: b.Name}, "Switch to "+b.Name+"?")
```

Leave the `switch-to-worktree` modal branch (model.go:793-806) and the
`chainSwitch` site (model.go:1343) untouched — carve-outs.

- [ ] **Step 4: Run the test**

Run: `go test ./internal/tui/ -run TestSwitchKeyPopsConfirm`
Expected: PASS.

- [ ] **Step 5: Run the broader switch/checkout tests for regressions**

Run: `go test ./internal/tui/ -run 'Switch|Checkout|Pull|Worktree'`
Expected: PASS — existing tests that drive `s` then expect an op may now need to
answer the confirm. If a pre-existing test breaks because it expected the op to
fire immediately, update it to press `y` after `s` (the new, correct flow), or
set `m.cfg.UI.DisableSlowOpConfirm = true` in that test's setup if it is testing
the op mechanics, not the key. Document which you chose in the commit.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/confirm_wiring_test.go
git commit  # message: "feat(tui): confirm before s/p/remote-checkout slow ops"
```

---

### Task 4: Wire menu-triggered slow ops (merge, rebase, fast-forward, reset)

**Files:**
- Modify: `internal/tui/branch_actions.go:27,46`; `internal/tui/remote_actions.go:82,100`; `internal/tui/tags_actions.go:94,115`; `internal/tui/commit_scope.go:766,787`; `internal/tui/reflog_view.go:26`
- Test: extend `internal/tui/confirm_wiring_test.go`

**Interfaces:**
- Consumes: `m.confirmOp` (Task 2). Each `.`-menu `run` handler returns
  `(tea.Model, tea.Cmd)`; `confirmOp` returns the same, so it is a drop-in for
  `return m.startOp(...)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/confirm_wiring_test.go` — invoke one menu run handler
directly and assert it pops the confirm. Use the action-row accessor the
existing `branch_actions_test.go` uses (e.g. `m.branchMergeRow()` / whatever it
is named) to get the row, then call `row.run(m)`:

```go
func TestMergeMenuPopsConfirm(t *testing.T) {
	m := loadedModelOnBranches(t)
	row, ok := m.branchMergeRow() // use the real accessor name from branch_actions.go
	if !ok {
		t.Skip("no mergeable branch selected in fixture")
	}
	tm, _ := row.run(m)
	mm := tm.(Model)
	if mm.modal == nil || !mm.modal.confirm {
		t.Fatal("merge menu action should pop the slow-op confirm")
	}
}
```

Before writing, open `branch_actions.go` to get the exact row-constructor name
and how a test obtains it (`branch_actions_test.go` shows the pattern). Mirror
it; do not invent names.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run TestMergeMenuPopsConfirm`
Expected: FAIL — merge runs `startOp` directly.

- [ ] **Step 3: Rewrite each menu `run` handler**

For each site, change `return m.startOp(<op>)` to `return m.confirmOp(<op>, <prompt>)`:

`branch_actions.go:27` (merge):
```go
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.confirmOp(engine.SmartMerge{Source: b.Name}, "Merge "+b.Name+" into current branch?") },
```
`branch_actions.go:46` (rebase):
```go
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.confirmOp(engine.SmartRebase{Onto: b.Name}, "Rebase current branch onto "+b.Name+"?") },
```
`remote_actions.go:82` (merge):
```go
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.confirmOp(engine.SmartMerge{Source: rb.Name}, "Merge "+rb.Name+" into current branch?") },
```
`remote_actions.go:100` (rebase):
```go
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.confirmOp(engine.SmartRebase{Onto: rb.Name}, "Rebase current branch onto "+rb.Name+"?") },
```
`tags_actions.go:94` (merge):
```go
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.confirmOp(engine.SmartMerge{Source: name}, "Merge "+name+" into current branch?") },
```
`tags_actions.go:115` (rebase):
```go
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.confirmOp(engine.SmartRebase{Onto: name}, "Rebase current branch onto "+name+"?") },
```
`commit_scope.go:766` (fast-forward):
```go
			return m.confirmOp(engine.FastForward{Commit: selHash}, "Fast-forward to this commit?")
```
`commit_scope.go:787` (commit reset — destructive):
```go
			return m.confirmOp(engine.Reset{Commit: hash}, "Reset to "+shortHash(hash)+"? This moves the current branch ref.")
```
`reflog_view.go:26` (reflog reset — destructive):
```go
			return m.confirmOp(engine.Reset{Commit: hash}, "Reset to "+shortHash(hash)+"? This moves the current branch ref.")
```

Leave `reflog_view.go:56` and `tags_actions.go:58` (the `engine.Checkout` rows
already behind a decision modal) and `worktree_popup.go` CreateWorktree
untouched — carve-outs.

- [ ] **Step 4: Run the test**

Run: `go test ./internal/tui/ -run TestMergeMenuPopsConfirm`
Expected: PASS.

- [ ] **Step 5: Run the menu/action regression suite**

Run: `go test ./internal/tui/ -run 'Merge|Rebase|FastForward|Reset|Reflog|Tags|Remote|Menu'`
Expected: PASS. As in Task 3 Step 5, fix any pre-existing test that assumed the
op fired immediately (press `y`, or disable confirm in setup).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/branch_actions.go internal/tui/remote_actions.go internal/tui/tags_actions.go internal/tui/commit_scope.go internal/tui/reflog_view.go internal/tui/confirm_wiring_test.go
git commit  # message: "feat(tui): confirm before menu merge/rebase/fast-forward/reset"
```

---

### Task 5: Help text, full suite, docs

**Files:**
- Modify: `internal/tui/help.go` (note that slow ops now confirm; `y`/`n` in the modal)
- Modify: `CHANGELOG.md`
- Modify: `README.md` (if a user-facing surface line for switch/pull exists)
- Test: full `./test.sh`

**Interfaces:** none new.

- [ ] **Step 1: Add a help line**

In `internal/tui/help.go`, add a short entry (match the existing `r(...)`
helper style) under the general/global section:

```go
		r("", "slow working-tree ops (switch, pull, merge, rebase, fast-forward, reset) ask y/n first — disable with [ui] disable_slow_op_confirm"),
```
(Use the real signature of the `r` helper as seen elsewhere in `help.go`; if it
requires a key in the first arg, attach this note to the existing `s`/`p`
lines instead of a keyless row.)

- [ ] **Step 2: Update CHANGELOG**

Add an entry under the current unreleased/top section of `CHANGELOG.md`:

```markdown
- **Confirm slow operations:** the TUI now asks a yes/no confirmation (default
  No; `y`/`n`/`esc`) before slow working-tree operations — switch, remote
  checkout, pull, merge, rebase, fast-forward, and reset. On by default; opt out
  with `[ui] disable_slow_op_confirm = true`.
```

- [ ] **Step 3: Update README if needed**

Grep `README.md` for the switch/pull keybinding docs; if present, add a clause
that these confirm by default and cite the config key. If no such surface line
exists, skip (note "no README change needed" in the commit body).

- [ ] **Step 4: Run gofmt + vet + the full suite**

Run: `./test.sh`
Expected: PASS (vet+gofmt → unit → e2e). Note: this feature adds **no** CLI
surface, so `internal/agentskill/using-gg.md` and `agentskill.Version` do **not**
change.

- [ ] **Step 5: Run the race suite (pre-merge gate)**

Run: `./test.sh race`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/help.go CHANGELOG.md README.md
git commit  # message: "docs: confirm-slow-ops help, changelog, readme"
```

---

## Self-Review notes

- **Spec coverage:** confirm dialog (Task 2) · default-No + y/n/esc (Task 2) ·
  config opt-out inverted field (Task 1) · every wrapped call site (Tasks 3–4) ·
  carve-outs left untouched and asserted (Tasks 3–4 leave them; Task 4 lists
  them explicitly) · TUI-only / no CLI (Global Constraints, Task 5 Step 4) ·
  tests at each layer (every task). No gaps.
- **Type consistency:** `confirmOp(engine.Operation, string) (tea.Model, tea.Cmd)`,
  `confirmSlowOps() bool`, `resolveModal(string) (tea.Model, tea.Cmd)`,
  `decisionState.confirm bool`, `UIConfig.DisableSlowOpConfirm bool` — used
  identically across all tasks.
- **Known fuzzy spots (resolve by reading the real code, not guessing):** test
  helper names (`newTestModel`, `loadedModelOnBranches`, `branchMergeRow`), the
  exact `p`-key guard, the `engine.Operation` method set for any stub, and the
  `help.go` `r(...)` signature. Each step flags this and says to mirror the
  existing pattern rather than invent.
