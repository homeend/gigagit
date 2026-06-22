# Reflog Recovery Actions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add *Reset to this entry* and *Check out this entry…* (detached / create-branch-and-switch) actions to a reflog entry's `.` menu, reusing existing engine ops.

**Architecture:** Frontend wiring of tested engine ops onto reflog-cursor-anchored `.`-menu rows. First a pure rename (`engine.CheckoutTag{Name,Branch}` → generic `engine.Checkout{Ref,Branch}`) so tag-checkout and reflog-checkout share one commit-ish op; then two reflog-anchored row helpers + a name popup mirroring the tag-checkout flow.

**Tech Stack:** Go 1.26, Bubble Tea TUI, shells out to system `git`.

## Global Constraints

- Module `github.com/gigagit/gg`, Go 1.26.
- A git verb is ONE invocation via `gitcmd`/`r.Runner.Run`; frontends never import `internal/git` (reach git through `engine`/`domain`).
- TUI `Model` is a value receiver; reflog rows must read the cursor entry via `m.reflog[backingIndex(panelReflog)]` and gate on `m.focus == panelReflog && m.opsIdle()` — never the `panelCommits`-gated `commitX` helpers (display-vs-backing trap).
- Engine ops reused as-is: `engine.Reset{Commit}` (soft/mixed/hard modal + non-ancestor `reset-confirm`), the renamed `engine.Checkout{Ref, Branch}` (`Branch==""` ⇒ detached; else create+switch).
- `gg tag checkout` CLI command name is UNCHANGED by the rename (only the engine struct changes).
- Tests use a real `git` in `t.TempDir()` (`newRepo` in engine; `newTestRepo`/fixtures in git) or synthetic Model data in tui. TDD.
- `main` is the trunk; branch off `main`. Run `./test.sh race` before merge.
- Every commit ends with these two trailers verbatim:
  ```
  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro
  ```

---

### Task 1: Rename `engine.CheckoutTag` → generic `engine.Checkout{Ref, Branch}`

**Files:**
- Rename: `internal/engine/checkout_tag.go` → `internal/engine/checkout.go`
- Rename: `internal/engine/checkout_tag_test.go` → `internal/engine/checkout_test.go`
- Modify: `internal/cli/tag.go:63`, `internal/tui/tags_actions.go:35`, `internal/tui/tag_checkout_popup.go:28`

**Interfaces:**
- Produces: `type Checkout struct { Ref string; Branch string }` with `Run(ctx, OpDeps) (Result, error)` — `Branch==""` ⇒ `SwitchDetach(Ref)`; else `SwitchCreate(Branch, Ref)`.

- [ ] **Step 1: Migrate the engine test (rename + add a commit-SHA case)**

`git mv internal/engine/checkout_tag_test.go internal/engine/checkout_test.go`, then replace its contents:

```go
package engine

import (
	"context"
	"testing"
)

func TestCheckoutDetached(t *testing.T) {
	dir, repo := newRepo(t)
	if err := repo.CreateTag(context.Background(), "v1.0.0", "", ""); err != nil {
		t.Fatal(err)
	}
	ch := make(chan Event, 16)
	res, err := Checkout{Ref: "v1.0.0"}.Run(context.Background(), OpDeps{Repo: repo, Events: ch})
	close(ch)
	if err != nil || !res.Changed {
		t.Fatalf("detached checkout: res=%+v err=%v", res, err)
	}
	if b := gitOut(t, dir, "branch", "--show-current"); b != "" {
		t.Fatalf("expected detached HEAD, on %q", b)
	}
}

func TestCheckoutCreatesBranch(t *testing.T) {
	dir, repo := newRepo(t)
	if err := repo.CreateTag(context.Background(), "v1.0.0", "", ""); err != nil {
		t.Fatal(err)
	}
	ch := make(chan Event, 16)
	if _, err := (Checkout{Ref: "v1.0.0", Branch: "rel"}).Run(context.Background(), OpDeps{Repo: repo, Events: ch}); err != nil {
		t.Fatalf("branch checkout: %v", err)
	}
	close(ch)
	if b := gitOut(t, dir, "branch", "--show-current"); b != "rel" {
		t.Fatalf("on branch %q, want rel", b)
	}
}

// A plain commit SHA (not a tag) proves Checkout is commit-ish-agnostic — the
// reflog recovery actions pass a reflog entry's SHA.
func TestCheckoutBySHACreatesBranch(t *testing.T) {
	dir, repo := newRepo(t)
	sha := gitOut(t, dir, "rev-parse", "HEAD")
	ch := make(chan Event, 16)
	if _, err := (Checkout{Ref: sha, Branch: "recovered"}).Run(context.Background(), OpDeps{Repo: repo, Events: ch}); err != nil {
		t.Fatalf("checkout by SHA: %v", err)
	}
	close(ch)
	if b := gitOut(t, dir, "branch", "--show-current"); b != "recovered" {
		t.Fatalf("on branch %q, want recovered", b)
	}
}

func TestCheckoutRequiresRef(t *testing.T) {
	_, repo := newRepo(t)
	ch := make(chan Event, 4)
	if _, err := (Checkout{}).Run(context.Background(), OpDeps{Repo: repo, Events: ch}); err == nil {
		t.Fatal("empty ref must error")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/engine/ -run TestCheckout -v`
Expected: FAIL — `Checkout` undefined.

- [ ] **Step 3: Rename the op**

`git mv internal/engine/checkout_tag.go internal/engine/checkout.go`, then replace its contents:

```go
package engine

import (
	"context"
	"fmt"
)

// Checkout checks out a commit-ish (tag, branch, or raw SHA): detached at Ref
// (Branch == "") or by creating Branch at Ref and switching to it. Decision-free;
// the frontend resolves the detached-vs-branch fork (and any branch name) before
// calling, per the option-list-only decision contract (a branch name is free text).
type Checkout struct {
	Ref    string // commit-ish (required)
	Branch string // "" = detached HEAD; else new branch created at Ref and switched to
}

func (op Checkout) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Ref == "" {
		return Result{}, fmt.Errorf("checkout: Ref is required")
	}
	if op.Branch != "" {
		deps.emit(ctx, Progress{Step: "creating branch", Detail: op.Branch + " at " + op.Ref})
		// One atomic invocation: on failure no branch is left behind.
		if err := deps.Repo.SwitchCreate(ctx, op.Branch, op.Ref); err != nil {
			return Result{}, fmt.Errorf("checkout: %w", err)
		}
		res := Result{Summary: "created branch " + op.Branch + " at " + op.Ref + " and switched", Changed: true}
		deps.emit(ctx, Done{Result: res})
		return res, nil
	}
	deps.emit(ctx, Progress{Step: "checking out", Detail: op.Ref})
	if err := deps.Repo.SwitchDetach(ctx, op.Ref); err != nil {
		return Result{}, fmt.Errorf("checkout: %w", err)
	}
	res := Result{Summary: "checked out " + op.Ref + " (detached HEAD)", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = Checkout{}
```

- [ ] **Step 4: Update the 3 call sites**

`internal/cli/tag.go:63` — `engine.CheckoutTag{Name: rest[0], Branch: *branch}` → `engine.Checkout{Ref: rest[0], Branch: *branch}`.

`internal/tui/tags_actions.go:35` — `engine.CheckoutTag{Name: name}` → `engine.Checkout{Ref: name}`.

`internal/tui/tag_checkout_popup.go:28` — `engine.CheckoutTag{Name: p.tag, Branch: p.name.Value()}` → `engine.Checkout{Ref: p.tag, Branch: p.name.Value()}`.

- [ ] **Step 5: Run the engine test + the dependent suites**

Run: `go test ./internal/engine/ -run TestCheckout -v && go build ./... && go test ./internal/cli/ ./internal/tui/ -run "Tag|Checkout"`
Expected: PASS; clean build (no remaining `CheckoutTag` references).

- [ ] **Step 6: Verify no stragglers**

Run: `grep -rn "CheckoutTag" internal/ cmd/`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add internal/engine/checkout.go internal/engine/checkout_test.go internal/cli/tag.go internal/tui/tags_actions.go internal/tui/tag_checkout_popup.go
git commit -m "refactor(engine): CheckoutTag -> generic Checkout{Ref,Branch}

Checkout was already commit-ish-agnostic (switch --detach / switch -c <ref>);
only the name was tag-specific. Rename so tag-checkout and the upcoming reflog
recovery actions share one op. gg tag checkout CLI command name unchanged.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 2: `reflogResetRow` — *Reset to this entry*

**Files:**
- Modify: `internal/tui/reflog_view.go` (add `reflogResetRow`)
- Modify: `internal/tui/action_menu.go` (append in `availableActions`)
- Test: `internal/tui/reflog_view_test.go`

**Interfaces:**
- Consumes: `m.reflog`, `backingIndex(panelReflog)`, `m.startOp`, `engine.Reset{Commit}`, `actionRow`.
- Produces: `func (m Model) reflogResetRow() (actionRow, bool)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/reflog_view_test.go`:

```go
func TestReflogResetRowAnchorsOnCursor(t *testing.T) {
	m := reflogTestModel()
	m.focus = panelReflog
	m.sel[panelReflog] = 1 // second entry
	r, ok := m.reflogResetRow()
	if !ok {
		t.Fatal("reflog . menu must offer Reset to this entry")
	}
	if r.id != "reflog-reset" {
		t.Fatalf("row id = %q, want reflog-reset", r.id)
	}
	// Not offered off the reflog panel.
	m.focus = panelCommits
	if _, ok := m.reflogResetRow(); ok {
		t.Fatal("reset row must not appear off the reflog panel")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run TestReflogResetRow`
Expected: FAIL — `reflogResetRow` undefined.

- [ ] **Step 3: Implement the row**

In `internal/tui/reflog_view.go`, add (the file already imports `tea` and `model`; add `"github.com/gigagit/gg/internal/engine"` to its import block):

```go
// reflogResetRow offers "Reset to this entry" on the reflog panel: moves the
// current branch to the entry's commit via engine.Reset (soft/mixed/hard modal +
// non-ancestor confirm). Anchored on the panelReflog cursor.
func (m Model) reflogResetRow() (actionRow, bool) {
	if m.focus != panelReflog || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelReflog)
	if !ok {
		return actionRow{}, false
	}
	hash := m.reflog[bi].Hash // full SHA → unambiguous
	return actionRow{
		id:    "reflog-reset",
		label: "Reset to this entry",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startOp(engine.Reset{Commit: hash})
		},
	}, true
}
```

- [ ] **Step 4: Append it in `availableActions`**

In `internal/tui/action_menu.go`, next to the `reflogBookmarkRow` append in the base `out` assembly:

```go
	if r, ok := m.reflogBookmarkRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.reflogResetRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 5: Run it to verify it passes**

Run: `go test ./internal/tui/ -run TestReflogResetRow`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/reflog_view.go internal/tui/action_menu.go internal/tui/reflog_view_test.go
git commit -m "feat(tui): reflog . menu — Reset to this entry

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 3: `reflogCheckoutRow` + `reflogCheckoutPopup` — *Check out this entry…*

**Files:**
- Modify: `internal/tui/reflog_view.go` (add `reflogCheckoutRow`)
- Create: `internal/tui/reflog_checkout_popup.go` (`reflogCheckoutPopup`)
- Modify: `internal/tui/action_menu.go` (append in `availableActions`)
- Test: `internal/tui/reflog_view_test.go`

**Interfaces:**
- Consumes: `m.reflog`, `backingIndex(panelReflog)`, `decisionState`, `engine.DecisionRequest`, `engine.Checkout`, `m.pushLayer`, `m.startOp`, `newTextField`, `textfield`, `m.popLayer`, `m.overlayDims`, `overlayCenter`, `clipToHeight`, `modalStyle`, `popupInnerWidth`, `shortHash`.
- Produces: `func (m Model) reflogCheckoutRow() (actionRow, bool)`; `type reflogCheckoutPopup struct { ref string; name textfield }` with `update`/`render`/`box`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/reflog_view_test.go`:

```go
func TestReflogCheckoutRowOpensModal(t *testing.T) {
	m := reflogTestModel()
	m.focus = panelReflog
	m.sel[panelReflog] = 1
	r, ok := m.reflogCheckoutRow()
	if !ok || r.id != "reflog-checkout" {
		t.Fatalf("reflog . menu must offer Check out this entry…, got ok=%v id=%q", ok, r.id)
	}
	nm, _ := r.run(m)
	m = nm.(Model)
	if m.modal == nil {
		t.Fatal("Check out must open a decision modal")
	}
	opts := m.modal.req.Options
	if len(opts) == 0 || opts[len(opts)-1] != "Cancel" {
		t.Fatalf("modal must end with Cancel (never-trap), got %v", opts)
	}
	if opts[0] == "Detached" {
		// good: leads with a non-mutating-to-a-branch choice
	}
}

func TestReflogCheckoutDetachedStartsOp(t *testing.T) {
	m := reflogTestModel()
	m.focus = panelReflog
	m.sel[panelReflog] = 1
	r, _ := m.reflogCheckoutRow()
	nm, _ := r.run(m)
	m = nm.(Model)
	// Resolve the modal's "Detached" branch.
	nm, cmd := m.modal.onResolve(m, "Detached")
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("Detached must start the checkout op")
	}
}

func TestReflogCheckoutCreateBranchOpensPopup(t *testing.T) {
	m := reflogTestModel()
	m.focus = panelReflog
	m.sel[panelReflog] = 1
	r, _ := m.reflogCheckoutRow()
	nm, _ := r.run(m)
	m = nm.(Model)
	nm, _ = m.modal.onResolve(m, "Create branch…")
	m = nm.(Model)
	if layerOf[*reflogCheckoutPopup](m) == nil {
		t.Fatal("Create branch… must push the reflog checkout popup")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run TestReflogCheckout`
Expected: FAIL — `reflogCheckoutRow` / `reflogCheckoutPopup` undefined.

- [ ] **Step 3: Create the popup**

Create `internal/tui/reflog_checkout_popup.go`:

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// reflogCheckoutPopup collects a new branch name to create at a reflog entry's
// commit and switch to. Mirrors tagCheckoutPopup.
type reflogCheckoutPopup struct {
	ref  string // full SHA of the reflog entry
	name textfield
}

func (p *reflogCheckoutPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		if p.name.Value() == "" {
			return m, nil
		}
		op := engine.Checkout{Ref: p.ref, Branch: p.name.Value()}
		m = m.popLayer()
		return m.startOp(op)
	case tea.KeySpace:
		// Branch names cannot contain spaces — drop it.
	default:
		p.name.HandleEditKey(msg)
	}
	return m, nil
}

func (p *reflogCheckoutPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *reflogCheckoutPopup) box(m Model) string {
	var b strings.Builder
	b.WriteString("New branch at " + shortHash(p.ref) + "\n\n")
	b.WriteString("name: " + p.name.View(true) + "\n\n")
	b.WriteString("[type] name  [enter] checkout  [esc] cancel")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
```

- [ ] **Step 4: Add the row**

In `internal/tui/reflog_view.go`, add:

```go
// reflogCheckoutRow offers "Check out this entry…" on the reflog panel: a modal
// (Detached / Create branch… / Cancel) mirroring the tag-checkout flow, on the
// panelReflog cursor entry.
func (m Model) reflogCheckoutRow() (actionRow, bool) {
	if m.focus != panelReflog || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelReflog)
	if !ok {
		return actionRow{}, false
	}
	ref := m.reflog[bi].Hash // full SHA
	return actionRow{
		id:    "reflog-checkout",
		label: "Check out this entry…",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.modal = &decisionState{
				req: engine.DecisionRequest{
					ID:      "reflog-checkout",
					Prompt:  "Check out " + shortHash(ref) + ":",
					Options: []string{"Detached", "Create branch…", "Cancel"},
				},
				onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
					switch opt {
					case "Detached":
						return m.startOp(engine.Checkout{Ref: ref})
					case "Create branch…":
						return m.pushLayer(&reflogCheckoutPopup{ref: ref, name: newTextField("")}), nil
					}
					return m, nil
				},
			}
			return m, nil
		},
	}, true
}
```

- [ ] **Step 5: Append it in `availableActions`**

In `internal/tui/action_menu.go`, after the `reflogResetRow` append:

```go
	if r, ok := m.reflogResetRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.reflogCheckoutRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run TestReflogCheckout -v`
Expected: PASS (all three).

- [ ] **Step 7: gofmt, vet, full TUI suite**

Run: `gofmt -l internal/tui/ && go vet ./internal/tui/ && go test ./internal/tui/`
Expected: clean, all green.

- [ ] **Step 8: Empirically confirm checkout/reset on a dangling SHA**

```bash
cd "$(mktemp -d)" && git init -q && git -c user.email=a@b.c -c user.name=a commit -q --allow-empty -m a && \
  git -c user.email=a@b.c -c user.name=a commit -q --allow-empty -m b && git reset -q --hard HEAD~1 && \
  D=$(git reflog --format=%H | head -1) && git switch -q -c rescued "$D" && git branch --show-current
```
Expected: prints `rescued` (proves `Checkout{Ref: danglingSHA, Branch}` resolves). Note in the commit body.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/reflog_view.go internal/tui/reflog_checkout_popup.go internal/tui/action_menu.go internal/tui/reflog_view_test.go
git commit -m "feat(tui): reflog . menu — Check out this entry (detached / new branch)

Verified Checkout resolves a dangling reflog SHA (the rescue case).

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 4: Help text + docs

**Files:**
- Modify: `internal/tui/help.go` (Reflog panel section)
- Modify: `CHANGELOG.md`, `README.md`

**Interfaces:** none (docs only).

- [ ] **Step 1: Extend the Reflog help section**

In `internal/tui/help.go`, the Reflog panel section currently has a `.` row for Copy SHA / Bookmark. Add after it:

```go
		r(".", "Reset to this entry (.-menu): move the current branch here (soft/mixed/hard) / Check out this entry (detached or as a new branch you switch to)"),
```

- [ ] **Step 2: Update CHANGELOG**

In `CHANGELOG.md` under `## [Unreleased]` → `### Added`, add:

```markdown
- **Reflog recovery actions.** The Reflog tab's `.` menu now offers
  **Reset to this entry** (soft/mixed/hard, with a confirm when the entry is off
  the current branch) and **Check out this entry…** (detached HEAD, or create a
  new branch at it and switch) — the "rescue lost work" half of the reflog,
  working even on dangling commits.
```

- [ ] **Step 3: Update README**

In `README.md`, the `ctrl+←/→` row's Reflog sentence currently ends after Copy SHA / Bookmark. Extend it:

```
… and the `.` menu offers **Copy SHA**, **Bookmark this commit**, **Reset to this entry** (soft/mixed/hard), and **Check out this entry** (detached or as a new branch you switch to) — recovery that works on dangling commits |
```

(Replace the existing trailing `… **Copy SHA** and **Bookmark this commit** |` fragment with the above.)

- [ ] **Step 4: Verify build + help test**

Run: `go build ./... && go test ./internal/tui/ -run Help`
Expected: clean / PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/help.go CHANGELOG.md README.md
git commit -m "docs: reflog recovery actions — help, changelog, readme

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Final verification (before merge)

- [ ] `./test.sh race` — all green.
- [ ] Live TTY eyeball: on the Reflog tab, `.` → *Reset to this entry* (walk the soft/mixed/hard modal; confirm the off-branch warning fires for an entry not on HEAD), and `.` → *Check out this entry…* → Detached and → Create branch… (name popup → switches). Confirm the reflog list refreshes and the cursor lands sensibly after each op.
- [ ] Use superpowers:finishing-a-development-branch to complete (merge to `main`).
