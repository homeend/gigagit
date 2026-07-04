# Checkout-As (remote checkout under a different local name) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user materialize a remote branch under a caller-chosen local name — explicitly via new Remotes `.`-menu rows and `gg checkout --as`, and as a recovery prompt when the same-name checkout refuses with "has diverged; cannot fast-forward".

**Architecture:** The engine already supports any `SmartCheckout.Local`; it only gains a typed `CheckoutDivergedError` (same message). The TUI adds a name popup (mirroring `commitNamePopup`), two `.`-menu rows, and an `opFinishedMsg` hook that turns the typed error into a rename-or-cancel modal via a `pendingCheckout` field (the `pendingPushTags` capture-and-clear pattern). The CLI adds `--as`.

**Tech Stack:** Go 1.26, Bubble Tea TUI, real-git `t.TempDir()` tests, declarative e2e TOML scenarios.

**Spec:** `docs/superpowers/specs/2026-07-03-checkout-as-design.md`

## Global Constraints

- Work in the worktree `.claude/worktrees/checkout-as` on branch `feat/checkout-as`. Run all commands from that directory. Verify with `git branch --show-current` before the first commit.
- The diverged error message stays byte-identical: `<local> has diverged from <remoteRef>; cannot fast-forward`.
- Engine decisions stay option-lists only; no free-text Decider changes.
- `internal/tui` and `internal/cli` never import `internal/git` (archtest-guarded); go through `internal/domain` / `internal/engine` types only.
- Recovery modal options (exact copy): `check out as different name…`, `cancel`. Modal ID: `checkout-diverged`.
- CLI hint (exact copy, stderr): `hint: retry with --as <name> to check it out under a different local name`.
- Run `gofmt -l` on touched packages before each commit (test.sh enforces it).

---

### Task 1: Engine — typed `CheckoutDivergedError`

**Files:**
- Modify: `internal/engine/smart_checkout.go` (the `!ff` refusal, currently line 55-57)
- Test: `internal/engine/smart_checkout_test.go`

**Interfaces:**
- Produces: `engine.CheckoutDivergedError{Local, RemoteRef string}` implementing `error` (value receiver). Tasks 4 and 5 detect it with `errors.As(err, &engine.CheckoutDivergedError{})`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/engine/smart_checkout_test.go` (add `"errors"` to its imports):

```go
func TestSmartCheckoutDivergedReturnsTypedError(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "foo")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "main-only")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/foo", "HEAD")
	gitIn(t, dir, "checkout", "foo")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "foo-only")
	gitIn(t, dir, "checkout", "main")
	_, err := SmartCheckout{RemoteRef: "origin/foo", Local: "foo", Intent: CheckoutStay}.
		Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	var div CheckoutDivergedError
	if !errors.As(err, &div) {
		t.Fatalf("err = %v, want CheckoutDivergedError", err)
	}
	if div.Local != "foo" || div.RemoteRef != "origin/foo" {
		t.Fatalf("fields = %+v, want Local=foo RemoteRef=origin/foo", div)
	}
	// The rendered message must stay byte-identical to the legacy fmt.Errorf.
	if got, want := err.Error(), "foo has diverged from origin/foo; cannot fast-forward"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestSmartCheckoutCustomLocalName(t *testing.T) {
	dir, repo := newRepo(t)
	configRemote(t, dir)
	gitIn(t, dir, "update-ref", "refs/remotes/origin/foo", "HEAD")
	res, err := SmartCheckout{RemoteRef: "origin/foo", Local: "foo2", Intent: CheckoutStay}.
		Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("checkout as foo2: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if ok, _ := repo.LocalBranchExists(context.Background(), "foo2"); !ok {
		t.Fatal("local foo2 was not created")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/engine/ -run 'TestSmartCheckoutDivergedReturnsTypedError|TestSmartCheckoutCustomLocalName' -v`
Expected: `TestSmartCheckoutDivergedReturnsTypedError` FAILS (`undefined: CheckoutDivergedError` compile error). Fix nothing else.

- [ ] **Step 3: Implement the typed error**

In `internal/engine/smart_checkout.go`, add above the `SmartCheckout` struct:

```go
// CheckoutDivergedError is the typed refusal returned when an existing local
// branch cannot fast-forward to the remote ref. Frontends detect it with
// errors.As to offer recovery (check the remote out under a different local
// name); the rendered message is byte-identical to the old fmt.Errorf.
type CheckoutDivergedError struct{ Local, RemoteRef string }

func (e CheckoutDivergedError) Error() string {
	return fmt.Sprintf("%s has diverged from %s; cannot fast-forward", e.Local, e.RemoteRef)
}
```

Replace the `!ff` refusal body:

```go
		if !ff {
			return Result{}, CheckoutDivergedError{Local: op.Local, RemoteRef: op.RemoteRef}
		}
```

(`fmt` is already imported.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/engine/ -run 'TestSmartCheckout' -v`
Expected: all `TestSmartCheckout*` PASS, including the pre-existing `TestSmartCheckoutDivergedRefuses`.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/smart_checkout.go internal/engine/smart_checkout_test.go
git commit -m "feat(engine): typed CheckoutDivergedError for the SmartCheckout ff refusal"
```

---

### Task 2: TUI — checkout-as popup, `pendingCheckout` state, name suggestion

**Files:**
- Create: `internal/tui/checkout_as_popup.go`
- Create: `internal/tui/checkout_as_popup_test.go`
- Modify: `internal/tui/model.go` (Model struct, next to `pendingPushTags` at ~line 51)

**Interfaces:**
- Consumes: `engine.CheckoutIntent` / `engine.SmartCheckout` (existing), `newTextField(prefill string) textfield`, `viewField`, `popupContentWidth`, `popupInnerWidth`, `overlayCenter`, `clipToHeight`, `modalStyle`, `m.pushLayer`/`m.popLayer`/`m.startOp` (all existing).
- Produces:
  - `type pendingCheckout struct{ remoteRef, base string; intent engine.CheckoutIntent }` and the Model field `pendingCheckout pendingCheckout` (zero `remoteRef` = none) — Task 4 captures/clears it.
  - `func (m Model) openCheckoutAsPopup(remoteRef, prefill string, intent engine.CheckoutIntent) Model` — Tasks 3 and 4 call it.
  - `func suggestLocalName(branches []model.Branch, base string) string` — Task 4 calls it.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/checkout_as_popup_test.go`:

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

func TestSuggestLocalNameFirstFree(t *testing.T) {
	bs := []model.Branch{{Name: "foo"}, {Name: "main"}}
	if got := suggestLocalName(bs, "foo"); got != "foo-2" {
		t.Fatalf("suggest = %q, want foo-2", got)
	}
}

func TestSuggestLocalNameSkipsTaken(t *testing.T) {
	bs := []model.Branch{{Name: "foo"}, {Name: "foo-2"}, {Name: "foo-3"}}
	if got := suggestLocalName(bs, "foo"); got != "foo-4" {
		t.Fatalf("suggest = %q, want foo-4", got)
	}
}

func TestCheckoutAsPopupEnterDispatchesAndArmsPending(t *testing.T) {
	m := remoteModel()
	m = m.openCheckoutAsPopup("origin/foo", "foo", engine.CheckoutSwitch)
	p, ok := m.topLayer().(*checkoutAsPopup)
	if !ok {
		t.Fatalf("expected checkoutAsPopup on top; got %T", m.topLayer())
	}
	nm, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter must dispatch the checkout op")
	}
	if nm.pendingCheckout.remoteRef != "origin/foo" || nm.pendingCheckout.base != "foo" ||
		nm.pendingCheckout.intent != engine.CheckoutSwitch {
		t.Fatalf("pendingCheckout = %+v, want origin/foo/foo/switch", nm.pendingCheckout)
	}
	if _, still := nm.topLayer().(*checkoutAsPopup); still {
		t.Fatal("popup must close on enter")
	}
}

func TestCheckoutAsPopupEmptyNameRefuses(t *testing.T) {
	m := remoteModel()
	m = m.openCheckoutAsPopup("origin/foo", "", engine.CheckoutStay)
	p := m.topLayer().(*checkoutAsPopup)
	nm, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("empty name must not dispatch")
	}
	if _, still := nm.topLayer().(*checkoutAsPopup); !still {
		t.Fatal("popup must stay open on empty name")
	}
}

func TestCheckoutAsPopupEscCancels(t *testing.T) {
	m := remoteModel()
	m = m.openCheckoutAsPopup("origin/foo", "foo", engine.CheckoutStay)
	p := m.topLayer().(*checkoutAsPopup)
	nm, _ := p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if _, still := nm.topLayer().(*checkoutAsPopup); still {
		t.Fatal("esc must close the popup")
	}
	if nm.pendingCheckout.remoteRef != "" {
		t.Fatal("esc must not arm pendingCheckout")
	}
}
```

(`remoteModel()` already exists in `remote_actions_test.go` — same package.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestSuggestLocalName|TestCheckoutAsPopup' -v`
Expected: compile FAILURE (`undefined: suggestLocalName`, `undefined: checkoutAsPopup`, …).

- [ ] **Step 3: Implement popup + state + helper**

Create `internal/tui/checkout_as_popup.go`:

```go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// checkoutAsPopup collects the local branch name for materializing a remote
// ref (SmartCheckout with a caller-chosen Local). Enter dispatches directly —
// this popup IS the confirmation; esc cancels. Mirrors commitNamePopup.
type checkoutAsPopup struct {
	remoteRef string // short remote ref, e.g. "origin/foo"
	intent    engine.CheckoutIntent
	name      textfield
}

func (p *checkoutAsPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		name := strings.TrimSpace(p.name.Value())
		if name == "" {
			return m, nil // a local branch needs a name; esc cancels
		}
		remoteRef, intent := p.remoteRef, p.intent
		m = m.popLayer()
		// Arm the diverged-recovery hook for THIS dispatch: base is the typed
		// name, so a re-collision suggests name-2, not the original branch-2.
		m.pendingCheckout = pendingCheckout{remoteRef: remoteRef, base: name, intent: intent}
		return m.startOp(engine.SmartCheckout{RemoteRef: remoteRef, Local: name, Intent: intent})
	default:
		p.name.HandleEditKey(msg)
	}
	return m, nil
}

func (p *checkoutAsPopup) render(m Model, below string) string {
	verb := "check out"
	if p.intent == engine.CheckoutSwitch {
		verb = "switch"
	}
	w, h := m.overlayDims()
	var b strings.Builder
	b.WriteString("Check out " + p.remoteRef + " as\n\n")
	b.WriteString(viewField("name: ", p.name, true, popupContentWidth(w)) + "\n\n")
	b.WriteString("[enter] " + verb + "   [esc] cancel")
	box := modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}

// openCheckoutAsPopup pushes the name popup for materializing remoteRef under
// a user-chosen local name; prefill seeds the text field.
func (m Model) openCheckoutAsPopup(remoteRef, prefill string, intent engine.CheckoutIntent) Model {
	return m.pushLayer(&checkoutAsPopup{remoteRef: remoteRef, intent: intent, name: newTextField(prefill)})
}

// suggestLocalName returns the first of base-2, base-3, … that is not a
// loaded local branch name. base itself is always treated as taken — this is
// only called after base failed to check out.
func suggestLocalName(branches []model.Branch, base string) string {
	taken := make(map[string]bool, len(branches)+1)
	taken[base] = true
	for _, b := range branches {
		taken[b.Name] = true
	}
	for i := 2; ; i++ {
		if cand := fmt.Sprintf("%s-%d", base, i); !taken[cand] {
			return cand
		}
	}
}
```

In `internal/tui/model.go`, next to the `pendingPushTags` field (~line 51), add the field, and add the type near the Model struct:

```go
	pendingCheckout       pendingCheckout     // arms the diverged-checkout recovery modal; zero remoteRef = none
```

```go
// pendingCheckout remembers the SmartCheckout the TUI just dispatched so a
// CheckoutDivergedError at opFinishedMsg can offer "check out as different
// name…". base seeds the -2/-3 suggestion (the name whose ff just failed).
// Captured-and-cleared unconditionally at opFinishedMsg and cleared by reRoot
// (the pendingPushTags pattern). Stale-safe: only SmartCheckout produces the
// typed error, and every checkout dispatch overwrites this field.
type pendingCheckout struct {
	remoteRef string
	base      string
	intent    engine.CheckoutIntent
}
```

Note the tests use `p.update(m, …)` whose first return is `Model` (not `tea.Model`) — matching `commitNamePopup.update`'s signature.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestSuggestLocalName|TestCheckoutAsPopup' -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/checkout_as_popup.go internal/tui/checkout_as_popup_test.go internal/tui/model.go
git commit -m "feat(tui): checkout-as name popup + pendingCheckout state + name suggestion"
```

---

### Task 3: TUI — Remotes `.`-menu rows "Check out as…" / "Switch to as…"

**Files:**
- Modify: `internal/tui/remote_actions.go` (append after `remoteDeleteRow`)
- Modify: `internal/tui/action_menu.go` (row registration, after the `remoteDeleteRow` append at ~line 147-149)
- Test: `internal/tui/remote_actions_test.go`

**Interfaces:**
- Consumes: `m.selectedRemoteForAction()` (focus- and opsIdle-gated), `m.openCheckoutAsPopup` (Task 2).
- Produces: menu row ids `remote-checkout-as` and `remote-switch-as` (help/docs in Task 6 reference these labels).

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/remote_actions_test.go`:

```go
func TestRemoteCheckoutAsRowsPresent(t *testing.T) {
	m := remoteModel()
	got := ids(availableActions(m))
	for _, id := range []string{"remote-checkout-as", "remote-switch-as"} {
		if !got[id] {
			t.Fatalf("expected %s in remote menu; got %v", id, got)
		}
	}
}

func TestRemoteCheckoutAsRowsAbsentWhenBranchesTabFocused(t *testing.T) {
	m := remoteModel()
	m.focus = panelBranches // Remotes still holds a stored selection — must not leak
	got := ids(availableActions(m))
	if got["remote-checkout-as"] || got["remote-switch-as"] {
		t.Fatalf("checkout-as rows must be absent off the Remotes tab; got %v", got)
	}
}

func TestRemoteCheckoutAsRowOpensPrefilledPopup(t *testing.T) {
	m := remoteModel()
	row, ok := m.remoteCheckoutAsRow()
	if !ok {
		t.Fatal("remoteCheckoutAsRow not available")
	}
	nm, _ := row.run(m)
	p, isPopup := nm.(Model).topLayer().(*checkoutAsPopup)
	if !isPopup {
		t.Fatalf("expected checkoutAsPopup on top; got %T", nm.(Model).topLayer())
	}
	if p.remoteRef != "origin/foo" || p.name.Value() != "foo" || p.intent != engine.CheckoutStay {
		t.Fatalf("popup = ref %q prefill %q intent %v, want origin/foo foo stay", p.remoteRef, p.name.Value(), p.intent)
	}
}

func TestRemoteSwitchAsRowCarriesSwitchIntent(t *testing.T) {
	m := remoteModel()
	row, ok := m.remoteSwitchAsRow()
	if !ok {
		t.Fatal("remoteSwitchAsRow not available")
	}
	nm, _ := row.run(m)
	p, isPopup := nm.(Model).topLayer().(*checkoutAsPopup)
	if !isPopup {
		t.Fatalf("expected checkoutAsPopup on top; got %T", nm.(Model).topLayer())
	}
	if p.intent != engine.CheckoutSwitch {
		t.Fatalf("intent = %v, want CheckoutSwitch", p.intent)
	}
}
```

Add `"github.com/homeend/gigagit/internal/engine"` to that file's imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestRemoteCheckoutAs|TestRemoteSwitchAs' -v`
Expected: compile FAILURE (`m.remoteCheckoutAsRow undefined`).

- [ ] **Step 3: Implement the rows**

Append to `internal/tui/remote_actions.go`:

```go
// remoteCheckoutAsRow offers "Check out <remote> as…" on the Remotes tab: the
// name popup materializes the remote ref under a user-chosen local name
// (stay on the current branch). Pre-fills the remote's own branch name.
func (m Model) remoteCheckoutAsRow() (actionRow, bool) {
	rb, ok := m.selectedRemoteForAction()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "remote-checkout-as",
		label: "Check out " + rb.Name + " as…",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openCheckoutAsPopup(rb.Name, rb.Branch, engine.CheckoutStay), nil
		},
	}, true
}

// remoteSwitchAsRow is remoteCheckoutAsRow with switch intent: create the
// local branch under the chosen name AND switch to it (SmartSwitch autostash
// semantics, same as s).
func (m Model) remoteSwitchAsRow() (actionRow, bool) {
	rb, ok := m.selectedRemoteForAction()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "remote-switch-as",
		label: "Switch to " + rb.Name + " as…",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openCheckoutAsPopup(rb.Name, rb.Branch, engine.CheckoutSwitch), nil
		},
	}, true
}
```

In `internal/tui/action_menu.go`, after the `remoteDeleteRow` block (~line 147-149), insert:

```go
	if r, ok := m.remoteCheckoutAsRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.remoteSwitchAsRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestRemote' -v`
Expected: all PASS (including the pre-existing remote-row leak/gating tests).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/remote_actions.go internal/tui/action_menu.go internal/tui/remote_actions_test.go
git commit -m "feat(tui): Remotes .-menu rows to check out / switch to a remote branch under a chosen name"
```

---

### Task 4: TUI — diverged-checkout recovery modal

**Files:**
- Modify: `internal/tui/model.go` — the `c` and `s` Remotes dispatch sites (~lines 1068-1072, 1089-1093), the `opFinishedMsg` handler (~lines 1798-1824), the `reRoot` pending-clears block (~line 2724)
- Modify: `internal/tui/checkout_as_popup.go` (add `checkoutDivergedModal`)
- Test: `internal/tui/checkout_as_popup_test.go`

**Interfaces:**
- Consumes: `engine.CheckoutDivergedError` (Task 1), `pendingCheckout` + `openCheckoutAsPopup` + `suggestLocalName` (Task 2), `decisionState`/`resolveModal` (existing).
- Produces: modal ID `checkout-diverged` with options `check out as different name…` / `cancel`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/checkout_as_popup_test.go` (add `"errors"` to its imports):

```go
func TestDivergedCheckoutOpensRecoveryModal(t *testing.T) {
	m := remoteModel()
	m.branches = []model.Branch{{Name: "foo"}, {Name: "foo-2"}}
	m.pendingCheckout = pendingCheckout{remoteRef: "origin/foo", base: "foo", intent: engine.CheckoutSwitch}
	nm, _ := m.Update(opFinishedMsg{err: engine.CheckoutDivergedError{Local: "foo", RemoteRef: "origin/foo"}})
	rm := nm.(Model)
	if rm.modal == nil || rm.modal.req.ID != "checkout-diverged" {
		t.Fatalf("expected checkout-diverged modal; modal=%+v", rm.modal)
	}
	if rm.pendingCheckout.remoteRef != "" {
		t.Fatal("pendingCheckout must be cleared unconditionally at opFinishedMsg")
	}
	nm2, _ := rm.resolveModal("check out as different name…")
	rm2 := nm2.(Model)
	p, ok := rm2.topLayer().(*checkoutAsPopup)
	if !ok {
		t.Fatalf("expected checkoutAsPopup after rename choice; got %T", rm2.topLayer())
	}
	if got := p.name.Value(); got != "foo-3" {
		t.Fatalf("prefill = %q, want foo-3 (foo and foo-2 taken)", got)
	}
	if p.intent != engine.CheckoutSwitch {
		t.Fatal("recovery must keep the original intent")
	}
}

func TestDivergedRecoveryCancelDoesNothing(t *testing.T) {
	m := remoteModel()
	m.pendingCheckout = pendingCheckout{remoteRef: "origin/foo", base: "foo", intent: engine.CheckoutStay}
	nm, _ := m.Update(opFinishedMsg{err: engine.CheckoutDivergedError{Local: "foo", RemoteRef: "origin/foo"}})
	rm := nm.(Model)
	nm2, cmd := rm.resolveModal("cancel")
	if cmd != nil {
		t.Fatal("cancel must not start an op")
	}
	if _, isPopup := nm2.(Model).topLayer().(*checkoutAsPopup); isPopup {
		t.Fatal("cancel must not open the popup")
	}
}

func TestNonDivergedErrorSkipsRecoveryModal(t *testing.T) {
	m := remoteModel()
	m.pendingCheckout = pendingCheckout{remoteRef: "origin/foo", base: "foo", intent: engine.CheckoutStay}
	nm, _ := m.Update(opFinishedMsg{err: errors.New("boom")})
	rm := nm.(Model)
	if rm.modal != nil {
		t.Fatalf("plain error must not open the recovery modal; modal=%+v", rm.modal)
	}
	if rm.pendingCheckout.remoteRef != "" {
		t.Fatal("pendingCheckout must still be cleared")
	}
}

func TestDivergedErrorWithoutPendingSkipsModal(t *testing.T) {
	m := remoteModel() // pendingCheckout zero — e.g. error surfaced by a CLI-driven repo
	nm, _ := m.Update(opFinishedMsg{err: engine.CheckoutDivergedError{Local: "foo", RemoteRef: "origin/foo"}})
	if rm := nm.(Model); rm.modal != nil {
		t.Fatalf("no pending checkout → no modal; modal=%+v", rm.modal)
	}
}

func TestReRootClearsPendingCheckout(t *testing.T) {
	m := footerModel()
	m.pendingCheckout = pendingCheckout{remoteRef: "origin/foo", base: "foo", intent: engine.CheckoutStay}
	updated, _ := m.reRoot(t.TempDir())
	if got := updated.(Model); got.pendingCheckout.remoteRef != "" {
		t.Fatalf("pendingCheckout = %+v after reRoot, want zero", got.pendingCheckout)
	}
}

func TestRemoteCheckoutKeyArmsPending(t *testing.T) {
	m := remoteModel()
	m.cfg.UI.DisableSlowOpConfirm = true // dispatch directly; pending arming is what's under test
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	rm := nm.(Model)
	if rm.pendingCheckout.remoteRef != "origin/foo" || rm.pendingCheckout.base != "foo" ||
		rm.pendingCheckout.intent != engine.CheckoutStay {
		t.Fatalf("pendingCheckout = %+v, want origin/foo/foo/stay", rm.pendingCheckout)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestDiverged|TestNonDivergedError|TestReRootClearsPendingCheckout|TestRemoteCheckoutKeyArmsPending' -v`
Expected: FAIL — modal never opens (`rm.modal == nil`), pending never armed.

- [ ] **Step 3: Implement the recovery wiring**

3a. Append to `internal/tui/checkout_as_popup.go`:

```go
// checkoutDivergedModal offers recovery after a CheckoutDivergedError: check
// the remote ref out under a fresh local name (popup pre-filled with the
// first free base-2/-3… suggestion, so enter never re-collides) or cancel.
// The intent (stay vs switch) of the failed dispatch carries over.
func (m Model) checkoutDivergedModal(pc pendingCheckout) *decisionState {
	return &decisionState{
		req: engine.DecisionRequest{
			ID:      "checkout-diverged",
			Prompt:  pc.base + " has diverged from " + pc.remoteRef + " and cannot fast-forward.",
			Options: []string{"check out as different name…", "cancel"},
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt == "check out as different name…" {
				return m.openCheckoutAsPopup(pc.remoteRef, suggestLocalName(m.branches, pc.base), pc.intent), nil
			}
			return m, nil
		},
	}
}
```

3b. In `internal/tui/model.go`, `opFinishedMsg` handler: capture the pending value just before the error/success fork, clear it in the unconditional-clears block, and open the modal on the typed error. The surrounding code stays as-is:

```go
		switchTo := ""
		chainSwitch := ""
		var pushTags []string
		var noticeCfg *engine.SetGitConfig
		pendingCo := m.pendingCheckout // captured; cleared below whatever happened
		if msg.err != nil {
			m.statusMsg = friendlyOpError(msg.err)
			var div engine.CheckoutDivergedError
			if pendingCo.remoteRef != "" && errors.As(msg.err, &div) {
				m.modal = m.checkoutDivergedModal(pendingCo)
			}
			m.pendingRemoteTagSet = ""
```

and in the clears block after the fork (next to `m.pendingPushTags = nil`):

```go
		m.pendingCheckout = pendingCheckout{} // unconditional; only a fresh checkout dispatch re-arms it
```

Add `"errors"` to `model.go`'s imports if not already present.

3c. Arm the pending at the two key dispatch sites (`case "c":` and `case "s":` under `panelRemotes`):

```go
		case "c":
			if m.focus == panelRemotes && m.canCheckoutRemote() {
				rb, _ := m.selectedRemote()
				// Arm the diverged-recovery hook. Stale-safe if the confirm is
				// declined: only SmartCheckout yields the typed error, every
				// checkout dispatch overwrites this, and opFinishedMsg/reRoot clear it.
				m.pendingCheckout = pendingCheckout{remoteRef: rb.Name, base: rb.Branch, intent: engine.CheckoutStay}
				return m.confirmOp(engine.SmartCheckout{RemoteRef: rb.Name, Local: rb.Branch, Intent: engine.CheckoutStay}, "Check out "+rb.Branch+"?")
			}
```

```go
		case "s":
			if m.focus == panelRemotes && m.canCheckoutRemote() {
				rb, _ := m.selectedRemote()
				m.pendingCheckout = pendingCheckout{remoteRef: rb.Name, base: rb.Branch, intent: engine.CheckoutSwitch}
				return m.confirmOp(engine.SmartCheckout{RemoteRef: rb.Name, Local: rb.Branch, Intent: engine.CheckoutSwitch}, "Switch to "+rb.Branch+"?")
			}
```

3d. In `reRoot`, next to `m.pendingPushTags = nil` (~line 2724):

```go
	m.pendingCheckout = pendingCheckout{} // a diverged checkout from the old repo must not prompt in the new one
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestDiverged|TestNonDivergedError|TestReRootClearsPendingCheckout|TestRemoteCheckoutKeyArmsPending|TestCheckoutAsPopup' -v`
Expected: all PASS. Then the full package: `go test ./internal/tui/` — PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/checkout_as_popup.go internal/tui/checkout_as_popup_test.go
git commit -m "feat(tui): diverged remote checkout offers check-out-as-different-name recovery"
```

---

### Task 5: CLI — `gg checkout --as <local>` + diverged hint + e2e

**Files:**
- Modify: `internal/cli/ops.go` (`cmdCheckout`, lines 151-188)
- Create: `e2e/scenarios/s80_checkout_as.toml`

**Interfaces:**
- Consumes: `engine.CheckoutDivergedError` (Task 1), existing `runOperation`/`finish`.
- Produces: the `--as`/`--as=` flag; stderr hint line `hint: retry with --as <name> to check it out under a different local name`.

- [ ] **Step 1: Write the failing e2e scenario**

Create `e2e/scenarios/s80_checkout_as.toml` (same divergence fixture as `s47_checkout_diverged_refused.toml`; read the `writing-e2e-scenarios` skill if the schema is unfamiliar):

```toml
name = "checkout --as: materialize a remote branch under a different local name"

[input]
steps = [
  { branch = "foo" },
  { switch = "foo" },
  { write = "h.txt", content = "local\n" },
  { commit = "local-divergent" },
  { switch = "main" },
]

[input.origin]
steps = [
  { write = "f.txt", content = "v1\n" },
  { commit = "c1" },
  { branch = "foo" },
  { switch = "foo" },
  { write = "g.txt", content = "foo\n" },
  { commit = "foo-c2" },
  { switch = "main" },
]

# Same-name checkout still refuses (diverged local foo)…
[[run]]
cmd  = ["checkout", "origin/foo"]
exit = 1

# …but --as materializes the remote state under a fresh name.
[[run]]
cmd  = ["checkout", "origin/foo", "--as", "foo2"]
exit = 0

[expect]
branch   = "main"
branches = ["foo", "foo2", "main"]

[[expect.log]]
branch   = "foo2"
subjects = ["foo-c2", "c1"]

[[expect.log]]
branch   = "foo"
subjects = ["local-divergent", "c1"]
```

- [ ] **Step 2: Run the scenario to verify it fails**

Run: `go test ./e2e/ -run 'TestScenarios/s80' -v` (if the harness names differ, `go test ./e2e/ -v 2>&1 | grep -i s80`)
Expected: FAIL — run 2 exits 2 (`checkout: unknown flag "--as"`).

- [ ] **Step 3: Implement the flag + hint**

Replace `cmdCheckout`'s parse loop and dispatch in `internal/cli/ops.go` (add `"errors"` to the file's imports if absent):

```go
func cmdCheckout(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	// Order-independent parse: -s/--switch, an optional --as <local> (or
	// --as=<local>), and the remote ref, in any order.
	doSwitch := false
	ref, asName := "", ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-s" || a == "--switch":
			doSwitch = true
		case a == "--as":
			i++
			if i >= len(args) {
				fmt.Fprintln(stderr, "checkout: --as requires a branch name")
				return 2
			}
			asName = args[i]
		case strings.HasPrefix(a, "--as="):
			asName = strings.TrimPrefix(a, "--as=")
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(stderr, "checkout: unknown flag %q\n", a)
			return 2
		default:
			if ref != "" {
				fmt.Fprintln(stderr, "checkout: too many arguments (expected one <remote>/<branch>)")
				return 2
			}
			ref = a
		}
	}
	if ref == "" {
		fmt.Fprintln(stderr, "checkout: a remote branch (e.g. origin/foo) is required")
		return 2
	}
	remote, local, ok := strings.Cut(ref, "/")
	if !ok || remote == "" || local == "" {
		fmt.Fprintln(stderr, "checkout: expected <remote>/<branch>, e.g. origin/foo")
		return 2
	}
	if asName != "" {
		local = asName
	}
	intent := engine.CheckoutStay
	if doSwitch {
		intent = engine.CheckoutSwitch
	}
	res, err := runOperation(context.Background(), svc,
		engine.SmartCheckout{RemoteRef: ref, Local: local, Intent: intent}, cliDecider{}, stderr)
	code := finish(res, err, stdout, stderr)
	var div engine.CheckoutDivergedError
	if asName == "" && errors.As(err, &div) {
		fmt.Fprintln(stderr, "hint: retry with --as <name> to check it out under a different local name")
	}
	return code
}
```

(The hint is suppressed when `--as` was given: the caller already knows the flag; `--as` targeting a name that itself diverges just fails plain, per spec.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./e2e/ -run 'TestScenarios' -v 2>&1 | grep -E 's44|s45|s46|s47|s80'`
Expected: s44-s47 still PASS; s80 PASSES. Manually verify the hint: from a scratch clone with a diverged branch, `gg checkout origin/<b>` prints `error: … cannot fast-forward` then the `hint:` line. (Or check `go test ./internal/cli/` still passes and rely on s47+code review for the hint line.)

- [ ] **Step 5: Commit**

```bash
git add internal/cli/ops.go e2e/scenarios/s80_checkout_as.toml
git commit -m "feat(cli): gg checkout --as <local> + diverged-checkout hint"
```

---

### Task 6: Docs, help, agent skill, full gate

**Files:**
- Modify: `internal/tui/help.go` (Remotes panel section, lines 62-67)
- Modify: `internal/agentskill/using-gg.md` (the `gg checkout` bullet, lines 90-93)
- Modify: `internal/agentskill/agentskill.go` (`Version` 43 → 44)
- Modify: `CHANGELOG.md` (Unreleased → Added)
- Modify: `README.md` (Remotes `.`-menu + CLI checkout mentions — find with `grep -n "checkout" README.md`)
- Modify: `CLAUDE.md` (engine + cli package-map rows)

**Interfaces:**
- Consumes: everything shipped in Tasks 1-5; exact labels `Check out <remote> as…` / `Switch to <remote> as…`, flag `--as`.

- [ ] **Step 1: help.go**

In the Remotes panel section, extend the `s` row's clause and add a `.`-menu row after line 64's `s` entry:

```go
		r("s", "checkout and switch to it — fast-forward-safe; a diverged local branch offers check-out-as-a-different-name"),
		r(".", "Check out <remote> as… / Switch to <remote> as… (.-menu): materialize the remote branch under a local name you choose"),
```

(The `c` row at line 63 keeps its text.)

- [ ] **Step 2: using-gg.md + version bump**

Extend the checkout bullet (lines 90-93) to:

```markdown
- `gg checkout <remote>/<branch> [-s|--switch] [--as <local>]` — check out a
  remote-tracking branch as a local tracking branch (fast-forward-safe: reuses
  an existing local branch only if it fast-forwards to the remote ref, and
  refuses a diverged one — retry with `--as <name>` to materialize it under a
  different local name). `-s` also switches to it.
```

In `internal/agentskill/agentskill.go`: `const Version = 44`.

Run: `go test ./internal/agentskill/` — Expected: PASS.

- [ ] **Step 3: CHANGELOG, README, CLAUDE.md**

CHANGELOG (top of `### Added` under `## [Unreleased]`):

```markdown
- **Check out a remote branch under a different local name.** The Remotes
  `.`-menu gains "Check out <remote> as…" and "Switch to <remote> as…" (a name
  popup pre-filled with the branch name), and `gg checkout` gains `--as
  <local>`. When a same-name checkout refuses because the local branch has
  diverged, the TUI now offers "check out as different name…" with a free
  `-2/-3` suggestion instead of a dead-end error; the CLI prints a `--as` hint.
```

README: extend the Remotes-panel `.`-menu list and the `gg checkout` CLI line with the same content (locate with `grep -n -e "checkout" -e "Remotes" README.md`; match the surrounding row format).

CLAUDE.md package map: in the `engine` row, after the SmartPull/SmartSwitch listing sentence, add: `SmartCheckout's diverged refusal is the typed `CheckoutDivergedError{Local, RemoteRef}` (message unchanged) so frontends can offer check-out-as-a-different-name recovery.` In the `cli` row's checkout mention (if present) note `--as <local>`.

- [ ] **Step 4: Full gate**

Run: `./test.sh race`
Expected: vet+gofmt clean, unit tests PASS, e2e PASS. Fix anything it flags before committing.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/help.go internal/agentskill/using-gg.md internal/agentskill/agentskill.go CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: checkout-as help, changelog, readme, using-gg (agentskill v44)"
```

- [ ] **Step 6: Refresh installed agent-skill copies (post-merge, main checkout)**

After the human merges the branch: `gg init --update` from the main checkout (refreshes installed using-gg.md copies to v44). Not part of the branch's commits.

---

## Verification checklist (before requesting merge)

- [ ] `./test.sh race` green in the worktree
- [ ] Manual TUI smoke test (`go build -o ./gg ./cmd/gg` in the worktree, run in a repo with a diverged branch): `c` on the diverged remote → recovery modal → rename → branch appears in Branches panel
- [ ] `gg checkout origin/<diverged> ` prints error + hint; `--as` succeeds
- [ ] Spec cross-check: every section of `2026-07-03-checkout-as-design.md` implemented (typed error / popup / two menu rows / recovery modal / suggestion / CLI flag+hint / tests / docs)
