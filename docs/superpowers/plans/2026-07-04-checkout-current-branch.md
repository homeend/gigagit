# Checkout-Current-Branch Smart Prompt Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `c`/`s` on the remote counterpart of the CURRENT branch opens a state-aware prompt (pull now / check out as different name… / cancel) instead of dead-ending on "X is the current branch; use pull to update it".

**Architecture:** The TUI detects the case at keypress (`rb.Branch == m.status.Branch` via the existing `remoteCurrentBranch()` dual-guard) and opens a frontend `decisionState` modal instead of dispatching a doomed op — no engine round-trip. The engine refusal becomes a typed `CheckoutCurrentBranchError` (message unchanged) as the stale-status backstop, and the CLI's existing `--as` hint fires for it too.

**Tech Stack:** Go 1.26, Bubble Tea TUI, real-git `t.TempDir()` engine tests.

**Spec:** `docs/superpowers/specs/2026-07-04-checkout-current-branch-design.md`

## Global Constraints

- Work in the worktree `.claude/worktrees/checkout-current` on branch `feat/checkout-current-branch`. Run all commands from that directory; verify with `git branch --show-current` first.
- The current-branch error message stays byte-identical: `<local> is the current branch; use pull to update it`.
- Modal ID (exact): `checkout-current-branch`. Option copy (exact): `pull now`, `check out as different name…`, `cancel` (cancel always last — esc resolves to the last option via `abortOption`).
- `pull now` appears ONLY when `rb.Name == m.status.Upstream && m.status.Behind > 0`, and dispatches `engine.SmartPull{Intent: engine.PullAndStay}` with no extra confirm.
- The modal path must NOT arm `m.pendingCheckout` (nothing is dispatched).
- CLI hint copy (exact, stderr): `hint: retry with --as <name> to check it out under a different local name`.
- No agentskill Version bump (no CLI surface/flag change).
- `gofmt -l` clean on touched packages before each commit.

---

### Task 1: Engine — typed `CheckoutCurrentBranchError`

**Files:**
- Modify: `internal/engine/smart_checkout.go` (the `cur == op.Local` refusal, ~line 54, and a new type next to `CheckoutDivergedError`)
- Test: `internal/engine/smart_checkout_test.go` (extend `TestSmartCheckoutCurrentBranchRefuses`, ~line 100)

**Interfaces:**
- Consumes: existing `CheckoutDivergedError` pattern in the same file.
- Produces: `engine.CheckoutCurrentBranchError{Local, RemoteRef string}` implementing `error` (value receiver), detectable via `errors.As`. Task 3's CLI hint uses it.

- [ ] **Step 1: Extend the test to demand the typed error**

In `internal/engine/smart_checkout_test.go`, replace the body of `TestSmartCheckoutCurrentBranchRefuses` (keep the function name) with:

```go
func TestSmartCheckoutCurrentBranchRefuses(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "checkout", "-b", "foo")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/foo", "HEAD")
	_, err := SmartCheckout{RemoteRef: "origin/foo", Local: "foo", Intent: CheckoutStay}.
		Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	var cur CheckoutCurrentBranchError
	if !errors.As(err, &cur) {
		t.Fatalf("err = %v, want CheckoutCurrentBranchError", err)
	}
	if cur.Local != "foo" || cur.RemoteRef != "origin/foo" {
		t.Fatalf("fields = %+v, want Local=foo RemoteRef=origin/foo", cur)
	}
	// The rendered message must stay byte-identical to the legacy fmt.Errorf.
	if got, want := err.Error(), "foo is the current branch; use pull to update it"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}
```

(`"errors"` is already imported in this file since the diverged-error test.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/engine/ -run TestSmartCheckoutCurrentBranchRefuses -v`
Expected: compile FAILURE (`undefined: CheckoutCurrentBranchError`).

- [ ] **Step 3: Implement the typed error**

In `internal/engine/smart_checkout.go`, directly below the `CheckoutDivergedError` type + method, add:

```go
// CheckoutCurrentBranchError is the typed refusal returned when the checkout
// targets the branch that is already checked out — updating it is a pull, not
// a checkout. Frontends detect it with errors.As (the TUI normally pre-empts
// this case at dispatch; this is the stale-status backstop); the rendered
// message is byte-identical to the old fmt.Errorf.
type CheckoutCurrentBranchError struct{ Local, RemoteRef string }

func (e CheckoutCurrentBranchError) Error() string {
	return fmt.Sprintf("%s is the current branch; use pull to update it", e.Local)
}
```

Replace the refusal (`if cur == op.Local { ... }` body):

```go
		if cur == op.Local {
			return Result{}, CheckoutCurrentBranchError{Local: op.Local, RemoteRef: op.RemoteRef}
		}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/engine/ -run 'TestSmartCheckout' -v`
Expected: all `TestSmartCheckout*` PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/smart_checkout.go internal/engine/smart_checkout_test.go
git commit -m "feat(engine): typed CheckoutCurrentBranchError for the current-branch refusal"
```

---

### Task 2: TUI — dispatch-time `checkout-current-branch` modal

**Files:**
- Modify: `internal/tui/checkout_as_popup.go` (new modal builder below `checkoutDivergedModal`)
- Modify: `internal/tui/model.go` (the `case "c":` Remotes branch ~line 1093-1100 and `case "s":` Remotes branch ~line 1117-1122)
- Test: `internal/tui/checkout_as_popup_test.go`

**Interfaces:**
- Consumes: `m.remoteCurrentBranch()` (`internal/tui/remote_actions.go:40` — returns `(branch, attached)`, guarding `""` and `"(detached)"`), `openCheckoutAsPopup`, `suggestLocalName`, `decisionState`/`resolveModal`, `engine.SmartPull{Intent: engine.PullAndStay}` (the exact op the `p` key dispatches for a current-branch pull — see `pullForFocus`, `internal/tui/branch_pull.go:21`).
- Produces: `func (m Model) checkoutCurrentBranchModal(rb model.RemoteBranch, intent engine.CheckoutIntent) *decisionState`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/checkout_as_popup_test.go` (add `"strings"` to its imports):

```go
func TestCurrentBranchCheckoutOpensModalWithPull(t *testing.T) {
	m := remoteModel() // rb = origin/foo
	m.status.Branch = "foo"
	m.status.Upstream = "origin/foo"
	m.status.Behind = 3
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	rm := nm.(Model)
	if cmd != nil {
		t.Fatal("opening the modal must not dispatch an op")
	}
	if rm.modal == nil || rm.modal.req.ID != "checkout-current-branch" {
		t.Fatalf("expected checkout-current-branch modal; modal=%+v", rm.modal)
	}
	if got := rm.modal.req.Options; len(got) != 3 || got[0] != "pull now" || got[2] != "cancel" {
		t.Fatalf("options = %v, want [pull now, check out as different name…, cancel]", got)
	}
	if !strings.Contains(rm.modal.req.Prompt, "behind origin/foo by 3") {
		t.Fatalf("prompt = %q, want behind-by-3 wording", rm.modal.req.Prompt)
	}
	if rm.pendingCheckout.remoteRef != "" {
		t.Fatal("the modal path must not arm pendingCheckout")
	}
	if _, cmd2 := rm.resolveModal("pull now"); cmd2 == nil {
		t.Fatal("pull now must dispatch the pull op")
	}
}

func TestCurrentBranchModalNoPullWhenNotBehind(t *testing.T) {
	m := remoteModel()
	m.status.Branch = "foo"
	m.status.Upstream = "origin/foo"
	m.status.Behind = 0
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	rm := nm.(Model)
	if rm.modal == nil || rm.modal.req.ID != "checkout-current-branch" {
		t.Fatalf("expected checkout-current-branch modal; modal=%+v", rm.modal)
	}
	for _, o := range rm.modal.req.Options {
		if o == "pull now" {
			t.Fatal("pull now must be absent when the branch is not behind")
		}
	}
	if !strings.Contains(rm.modal.req.Prompt, "already contains origin/foo") {
		t.Fatalf("prompt = %q, want already-contains wording", rm.modal.req.Prompt)
	}
}

func TestCurrentBranchModalNoPullOnNonUpstreamRemote(t *testing.T) {
	m := remoteModel() // rb = origin/foo …
	m.status.Branch = "foo"
	m.status.Upstream = "upstream/foo" // …but foo tracks a DIFFERENT remote
	m.status.Behind = 5
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	rm := nm.(Model)
	if rm.modal == nil || rm.modal.req.ID != "checkout-current-branch" {
		t.Fatalf("expected checkout-current-branch modal; modal=%+v", rm.modal)
	}
	for _, o := range rm.modal.req.Options {
		if o == "pull now" {
			t.Fatal("pull now must be absent when the selected remote is not the upstream")
		}
	}
	if strings.Contains(rm.modal.req.Prompt, "already contains") {
		t.Fatalf("prompt = %q must not claim containment for a non-upstream remote", rm.modal.req.Prompt)
	}
}

func TestCurrentBranchModalRenameOpensPopupWithIntent(t *testing.T) {
	m := remoteModel()
	m.branches = []model.Branch{{Name: "foo"}}
	m.status.Branch = "foo"
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}) // s → switch intent
	rm := nm.(Model)
	if rm.modal == nil || rm.modal.req.ID != "checkout-current-branch" {
		t.Fatalf("expected checkout-current-branch modal; modal=%+v", rm.modal)
	}
	nm2, _ := rm.resolveModal("check out as different name…")
	p, ok := nm2.(Model).topLayer().(*checkoutAsPopup)
	if !ok {
		t.Fatalf("expected checkoutAsPopup after rename choice; got %T", nm2.(Model).topLayer())
	}
	if got := p.name.Value(); got != "foo-2" {
		t.Fatalf("prefill = %q, want foo-2", got)
	}
	if p.intent != engine.CheckoutSwitch {
		t.Fatal("s must carry CheckoutSwitch into the popup")
	}
}

func TestCurrentBranchModalCancelInert(t *testing.T) {
	m := remoteModel()
	m.status.Branch = "foo"
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	rm := nm.(Model)
	nm2, cmd := rm.resolveModal("cancel")
	if cmd != nil {
		t.Fatal("cancel must not start an op")
	}
	if _, isPopup := nm2.(Model).topLayer().(*checkoutAsPopup); isPopup {
		t.Fatal("cancel must not open the popup")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui/ -run 'TestCurrentBranch' -v`
Expected: FAIL — no modal opens (the keys dispatch the old confirmOp path; `rm.modal` is either nil or the `confirm-slow-op` modal).

- [ ] **Step 3: Implement modal builder + key-handler branches**

3a. Append to `internal/tui/checkout_as_popup.go` (below `checkoutDivergedModal`; `fmt`, `engine`, `model` are already imported):

```go
// checkoutCurrentBranchModal replaces the doomed dispatch when c/s targets
// the remote counterpart of the checked-out branch — updating it is a pull,
// not a checkout. State-aware: "pull now" is offered only when the selected
// ref IS the branch's upstream and the branch is behind it (pulling would
// otherwise be a no-op, or fetch from a different remote than the one
// selected); "already contains" is only claimed when provable (upstream,
// not behind). The rename option reuses the checkout-as popup with a free
// suggestion, keeping the pressed key's stay/switch intent.
func (m Model) checkoutCurrentBranchModal(rb model.RemoteBranch, intent engine.CheckoutIntent) *decisionState {
	const rename = "check out as different name…"
	prompt := m.status.Branch + " is the current branch."
	opts := []string{rename, "cancel"}
	if rb.Name == m.status.Upstream {
		if m.status.Behind > 0 {
			prompt = fmt.Sprintf("%s is the current branch (behind %s by %d).", m.status.Branch, rb.Name, m.status.Behind)
			opts = []string{"pull now", rename, "cancel"}
		} else {
			prompt = m.status.Branch + " is the current branch and already contains " + rb.Name + " (nothing to pull)."
		}
	}
	return &decisionState{
		req: engine.DecisionRequest{
			ID:      "checkout-current-branch",
			Prompt:  prompt,
			Options: opts,
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			switch opt {
			case "pull now":
				// The same op the p key runs for the current branch; SmartPull's
				// own decision ladder handles merge/rebase/dirty trees.
				return m.startOp(engine.SmartPull{Intent: engine.PullAndStay})
			case rename:
				return m.openCheckoutAsPopup(rb.Name, suggestLocalName(m.branches, rb.Branch), intent), nil
			}
			return m, nil
		},
	}
}
```

3b. In `internal/tui/model.go`, insert the current-branch branch at the TOP of both Remotes checkout handlers, before the pending arming. `case "c":` becomes:

```go
		case "c":
			if m.focus == panelRemotes && m.canCheckoutRemote() {
				rb, _ := m.selectedRemote()
				if cur, attached := m.remoteCurrentBranch(); attached && rb.Branch == cur {
					m.modal = m.checkoutCurrentBranchModal(rb, engine.CheckoutStay)
					return m, nil
				}
				// Arm the diverged-recovery hook. Stale-safe if the confirm is
				// declined: only SmartCheckout yields the typed error, every
				// checkout dispatch overwrites this, and opFinishedMsg/reRoot clear it.
				m.pendingCheckout = pendingCheckout{remoteRef: rb.Name, base: rb.Branch, intent: engine.CheckoutStay}
				return m.confirmOp(engine.SmartCheckout{RemoteRef: rb.Name, Local: rb.Branch, Intent: engine.CheckoutStay}, "Check out "+rb.Branch+"?")
			}
```

and the Remotes block of `case "s":` becomes:

```go
		case "s":
			if m.focus == panelRemotes && m.canCheckoutRemote() {
				rb, _ := m.selectedRemote()
				if cur, attached := m.remoteCurrentBranch(); attached && rb.Branch == cur {
					m.modal = m.checkoutCurrentBranchModal(rb, engine.CheckoutSwitch)
					return m, nil
				}
				m.pendingCheckout = pendingCheckout{remoteRef: rb.Name, base: rb.Branch, intent: engine.CheckoutSwitch}
				return m.confirmOp(engine.SmartCheckout{RemoteRef: rb.Name, Local: rb.Branch, Intent: engine.CheckoutSwitch}, "Switch to "+rb.Branch+"?")
			}
```

(Only the three inserted lines per handler are new; the surrounding lines already exist — do not change them.)

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/tui/ -run 'TestCurrentBranch|TestRemoteCheckout|TestRemoteSwitch|TestDiverged|TestCheckoutAsPopup' -v`
Expected: all PASS (the existing arming tests still pass — their fixture's `m.status.Branch` is `"main"`, not the selected `foo`). Then full package: `go test ./internal/tui/` — PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/checkout_as_popup.go internal/tui/model.go internal/tui/checkout_as_popup_test.go
git commit -m "feat(tui): c/s on the current branch's remote opens a pull-or-checkout-as prompt"
```

---

### Task 3: CLI hint, docs, full gate

**Files:**
- Modify: `internal/cli/ops.go` (the hint block after `finish`, ~lines 205-208)
- Modify: `internal/tui/help.go` (the Remotes `c` row, ~line 65)
- Modify: `CHANGELOG.md` (top of `### Added` under `## [Unreleased]`)
- Modify: `README.md` (the Remotes `.`-menu / checkout clauses — locate with `grep -n "Check out" README.md`)
- Modify: `CLAUDE.md` (engine package-map row, next to the `CheckoutDivergedError` mention)

**Interfaces:**
- Consumes: `engine.CheckoutCurrentBranchError` (Task 1); the existing hint block with `engine.CheckoutDivergedError`.

- [ ] **Step 1: Extend the CLI hint condition**

In `internal/cli/ops.go`, replace the hint block:

```go
	var div engine.CheckoutDivergedError
	if asName == "" && errors.As(err, &div) {
		fmt.Fprintln(stderr, "hint: retry with --as <name> to check it out under a different local name")
	}
```

with:

```go
	// Both refusals (diverged, current-branch) are recoverable the same way
	// non-interactively: materialize the remote under a different local name.
	var div engine.CheckoutDivergedError
	var curb engine.CheckoutCurrentBranchError
	if asName == "" && (errors.As(err, &div) || errors.As(err, &curb)) {
		fmt.Fprintln(stderr, "hint: retry with --as <name> to check it out under a different local name")
	}
```

Run: `go test ./internal/cli/ && go vet ./internal/cli/` — Expected: PASS (the hint line has no automated stderr assertion — accepted, the e2e harness has no stderr matcher).

- [ ] **Step 2: help.go**

Replace the Remotes `c` row (line ~65):

```go
		r("c", "checkout: create or fast-forward a local tracking branch (stay on the current branch)"),
```

with:

```go
		r("c", "checkout: create or fast-forward a local tracking branch (stay on the current branch); on the current branch's own remote it prompts instead: pull now (when behind) / check out under a different name"),
```

Run: `go test ./internal/tui/ -run TestHelp -v` — Expected: PASS.

- [ ] **Step 3: CHANGELOG, README, CLAUDE.md**

CHANGELOG — insert at the top of `### Added` under `## [Unreleased]`:

```markdown
- **Smart prompt when checking out the current branch's remote.** `c`/`s` on
  the remote counterpart of the checked-out branch no longer dead-ends with
  "use pull to update it": a state-aware prompt offers "pull now" (only when
  the branch is actually behind its upstream), "check out as different
  name…" (the checkout-as popup with a free `-2/-3` suggestion), or cancel.
  The CLI's diverged `--as` hint now also fires for the current-branch
  refusal.
```

README — extend the Remotes checkout-as clause (find with `grep -n "Check out" README.md`) with a sentence in the same voice: `c`/`s` on the current branch's own remote prompts pull-now / checkout-as instead of erroring.

CLAUDE.md — in the `engine` row, extend the existing `CheckoutDivergedError` sentence to: `SmartCheckout's diverged and current-branch refusals are typed errors (`CheckoutDivergedError`, `CheckoutCurrentBranchError`; messages unchanged) so frontends can offer recovery (check-out-as-a-different-name; the TUI pre-empts the current-branch case at dispatch with a pull-now/checkout-as prompt).`

- [ ] **Step 4: Full gate**

Run: `./test.sh race`
Expected: vet+gofmt clean, unit + e2e all green.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/ops.go internal/tui/help.go CHANGELOG.md README.md CLAUDE.md
git commit -m "feat(cli)+docs: --as hint on current-branch refusal; checkout-current-branch prompt docs"
```

---

## Verification checklist (before requesting merge)

- [ ] `./test.sh race` green in the worktree
- [ ] Manual TUI smoke (build `go build -o ./gg ./cmd/gg` in the worktree): on a repo where main is ahead-only (the user's ↑143 ↓0 case), `c` on `origin/main` → "already contains … (nothing to pull)" modal without a pull option; rename opens the prefilled popup
- [ ] Spec cross-check: every spec section implemented (modal + gating / typed error / CLI hint / tests / docs; no agentskill bump)
