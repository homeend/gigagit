# Soft Reload for `r` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the `r` reload keep the existing panels on screen (with a per-panel ⏳ and a `reloading…` status line) instead of blanking to `gigagit (loading…)`, while a repo switch (`reRoot`) and initial startup keep their blank screen.

**Architecture:** Add a `softReload bool` to the TUI `Model`, set only by the `r` key handler. `View()` keeps blanking when `m.loading && !m.softReload` (startup + reRoot) and otherwise renders the still-valid old panel data. `panelLabel` appends the existing ⏳ glyph to every panel title while `m.softReload`, and the status line gains a `⏳ reloading…` prefix. Fresh data swaps in atomically at `dataLoadedMsg`, which clears both flags.

**Tech Stack:** Go 1.26, Bubble Tea (Elm-style value-receiver `Model`). Tests use a real `git` in `t.TempDir()` via the existing `newRepo`/`loadedModel` helpers and `keyMsg`.

## Global Constraints

- TUI-only change; do not touch `internal/domain`, `internal/git`, or any other package. (`internal/tui` reaches git only through `domain` — unchanged here.)
- `Model` is a **value receiver** with pointer fields; `softReload` is a plain `bool` value field (copied across the value, like `loading`), so no pointer handling.
- Reuse the existing `commitsLoadingGlyph` constant ("⏳", viewstate.go:513) — do not introduce a second glyph.
- Do not change the existing `!m.loading` write-op guards or any navigation key handling.
- Follow existing tui test style: `loadedModel(t)`, `keyMsg("…")`, drive through `m.Update(...)` / `m.View()`, set `m.width, m.height` before `View()`.

---

### Task 1: `softReload` flag — set on `r`, cleared on load completion

**Files:**
- Modify: `internal/tui/model.go:29` (add field), `internal/tui/model.go:732-737` (`r` handler), `internal/tui/model.go:447` (clear on current-gen path), `internal/tui/model.go:1953` (clear in `reRoot`)
- Test: `internal/tui/soft_reload_test.go` (create)

**Interfaces:**
- Produces: `Model.softReload bool` — true while a same-repo `r` reload is in flight; false during startup and `reRoot`, and cleared once the **current-generation** `dataLoadedMsg` has been applied. A superseded-generation `dataLoadedMsg` leaves it untouched (the newer in-flight load owns it). Later tasks read it in `View()` and `panelLabel`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/soft_reload_test.go`:

```go
package tui

import "testing"

// Pressing r on a loaded model starts a soft reload: loading + softReload set.
func TestRKeyStartsSoftReload(t *testing.T) {
	m := loadedModel(t)
	updated, _ := m.Update(keyMsg("r"))
	mm := updated.(Model)
	if !mm.loading {
		t.Fatal("r should set loading")
	}
	if !mm.softReload {
		t.Fatal("r should set softReload")
	}
}

// When the load completes, dataLoadedMsg clears softReload (and loading).
func TestDataLoadedClearsSoftReload(t *testing.T) {
	m := loadedModel(t)
	m.loading = true
	m.softReload = true
	updated, _ := m.Update(dataLoadedMsg{gen: m.loadGen})
	mm := updated.(Model)
	if mm.loading {
		t.Fatal("dataLoadedMsg should clear loading")
	}
	if mm.softReload {
		t.Fatal("dataLoadedMsg should clear softReload")
	}
}

// A superseded-generation dataLoadedMsg must NOT clear softReload: a newer r
// reload is still in flight (loadGen=5) and must keep soft-rendering until it
// completes. Clearing here would blank the screen mid double-reload.
func TestSupersededLoadKeepsSoftReload(t *testing.T) {
	m := loadedModel(t)
	m.loadGen = 5
	m.loading = true
	m.softReload = true
	updated, _ := m.Update(dataLoadedMsg{gen: 4}) // older generation, dropped
	mm := updated.(Model)
	if !mm.softReload {
		t.Fatal("a superseded dataLoadedMsg must leave softReload set for the newer in-flight load")
	}
	if !mm.loading {
		t.Fatal("a superseded dataLoadedMsg must leave loading set")
	}
}

// reRoot (repo switch) clears softReload even if a soft reload was in flight, so
// the outgoing repo's panels stop soft-rendering immediately.
func TestReRootClearsInFlightSoftReload(t *testing.T) {
	m := loadedModel(t)
	m.softReload = true
	updated, _ := m.reRoot(m.currentWorktree)
	if updated.(Model).softReload {
		t.Fatal("reRoot must clear softReload")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'StartsSoftReload|DataLoadedClearsSoftReload|SupersededLoadKeepsSoftReload|ReRootClearsInFlight' -v`
Expected: COMPILE FAIL — `mm.softReload undefined (type Model has no field or method softReload)`.

- [ ] **Step 3: Add the field**

In `internal/tui/model.go`, add the field directly under `loading` (line 29):

```go
	loading    bool
	softReload bool // r reload in flight: render stale panels + ⏳ instead of blanking (reRoot/startup leave it false)
	err        error
```

(Re-align the adjacent `err`/`status`/… field block with gofmt; `go test` runs gofmt-sensitive code so just let `gofmt -w` handle spacing.)

- [ ] **Step 4: Set the flag in the `r` handler**

In `internal/tui/model.go`, the `r` case (currently lines 732-737):

```go
		case "r":
			if !m.running {
				m.loadGen++
				m.loading = true
				m.softReload = true
				return m, m.loadCmd()
			}
```

- [ ] **Step 5: Clear the flag on the current-generation `dataLoadedMsg` path**

In `internal/tui/model.go`, the `dataLoadedMsg` case. Do **not** touch the
superseded-gen early return (it must leave `softReload` set for the newer
in-flight load). Clear next to `m.loading = false` (line 447):

```go
	case dataLoadedMsg:
		if msg.gen != m.loadGen {
			return m, nil // superseded by a newer load (softReload left for the newer load)
		}
		m.loading = false
		m.softReload = false
		m.commitsLoading = false // the full load (which includes the feed) is done
```

- [ ] **Step 6: Clear the flag in `reRoot`**

In `internal/tui/model.go`, `reRoot` (around line 1953), add `m.softReload = false`
next to the existing `m.loading = true`:

```go
	m.svc = domain.OpenTUI(path)
	m.feed = m.svc.CommitFeed()
	m.switchTarget = path
	m.loading = true
	m.softReload = false // repo switch is a hard reload — never soft-render the outgoing repo
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'StartsSoftReload|DataLoadedClearsSoftReload|SupersededLoadKeepsSoftReload|ReRootClearsInFlight' -v`
Expected: PASS (all four).

- [ ] **Step 8: Commit**

```bash
git add internal/tui/model.go internal/tui/soft_reload_test.go
git commit -m "feat(tui): softReload flag — set on r, cleared on current-gen load + reRoot"
```

---

### Task 2: `View()` soft-renders the panels instead of blanking

**Files:**
- Modify: `internal/tui/model.go:1970-1981` (`View`)
- Test: `internal/tui/soft_reload_test.go` (append)

**Interfaces:**
- Consumes: `Model.softReload` (Task 1).
- Produces: same-repo soft reload now renders panels; `reRoot`/startup still blank.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/soft_reload_test.go`:

```go
import "strings" // add to the existing import block if not already present

// During a soft reload the panels stay on screen (no "gigagit (loading…)").
func TestViewSoftRendersDuringReload(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	m.focus = panelCommits // keep every panel label visible (see TestViewRendersPanelsWithoutPanic)
	m.loading = true
	m.softReload = true
	out := m.View()
	if strings.Contains(out, "gigagit (loading…)") {
		t.Fatalf("soft reload should not blank the screen:\n%s", out)
	}
	if !strings.Contains(out, "Branches") || !strings.Contains(out, "Commits") {
		t.Fatalf("soft reload should keep panels visible:\n%s", out)
	}
}

// A hard reload (loading without softReload — startup / reRoot) still blanks.
func TestViewBlanksForHardReload(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	m.loading = true
	m.softReload = false
	out := m.View()
	if !strings.Contains(out, "gigagit (loading…)") {
		t.Fatalf("hard reload should blank to the loading screen, got:\n%s", out)
	}
}

// reRoot (repo switch) sets loading WITHOUT softReload, so its View blanks.
func TestReRootIsHardReload(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	updated, _ := m.reRoot(m.currentWorktree)
	mm := updated.(Model)
	if mm.softReload {
		t.Fatal("reRoot must not set softReload")
	}
	if !strings.Contains(mm.View(), "gigagit (loading…)") {
		t.Fatal("reRoot should keep the blank loading screen")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'ViewSoftRenders|ViewBlanksForHard|ReRootIsHard' -v`
Expected: `TestViewSoftRendersDuringReload` FAILS (View still returns `"gigagit (loading…)"`). The other two PASS already (they assert the current behavior) — that is fine; they lock it in.

- [ ] **Step 3: Update `View()`**

In `internal/tui/model.go`, change the loading branch in `View` (lines 1974-1976):

```go
func (m Model) View() string {
	if m.modal != nil {
		return m.render()
	}
	if m.loading && !m.softReload {
		return "gigagit (loading…)\n" // startup + repo-switch keep the blank screen
	}
	if m.err != nil {
		return "error: " + m.err.Error() + "\n"
	}
	return m.render()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'ViewSoftRenders|ViewBlanksForHard|ReRootIsHard' -v`
Expected: PASS (all three).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/soft_reload_test.go
git commit -m "feat(tui): soft reload keeps panels visible; reRoot/startup still blank"
```

---

### Task 3: Per-panel ⏳ glyph + `reloading…` status line

**Files:**
- Modify: `internal/tui/viewstate.go:517-528` (`panelLabel`), `internal/tui/view.go:358-367` (status line)
- Test: `internal/tui/soft_reload_test.go` (append)

**Interfaces:**
- Consumes: `Model.softReload` (Task 1), `commitsLoadingGlyph` (viewstate.go:513).
- Produces: visual reload cues. No new exported symbols.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/soft_reload_test.go`:

```go
// Every panel title carries the ⏳ glyph during a soft reload, and the status
// line shows "reloading…".
func TestSoftReloadShowsGlyphAndStatus(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	m.focus = panelCommits
	m.loading = true
	m.softReload = true
	out := m.View()
	if !strings.Contains(out, commitsLoadingGlyph) {
		t.Fatalf("soft reload should show the %q glyph:\n%s", commitsLoadingGlyph, out)
	}
	if !strings.Contains(out, "reloading…") {
		t.Fatalf("soft reload should show a reloading status line:\n%s", out)
	}
}

// Direct panelLabel test: the per-panel glyph is proven in ISOLATION from the
// status line (which also emits ⏳), so a broken panelLabel edit can't pass on
// the status line alone.
func TestPanelLabelShowsGlyphDuringSoftReload(t *testing.T) {
	m := loadedModel(t)
	m.softReload = true
	got := m.panelLabel(panelBranches, "Branches")
	if !strings.Contains(got, commitsLoadingGlyph) {
		t.Fatalf("Branches label should carry the glyph during soft reload: %q", got)
	}
}

// Without a soft reload the Branches title carries no glyph (no false positive).
func TestNoGlyphWhenNotReloading(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	m.focus = panelCommits
	got := m.panelLabel(panelBranches, "Branches")
	if strings.Contains(got, commitsLoadingGlyph) {
		t.Fatalf("Branches label should have no glyph when idle: %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'SoftReloadShowsGlyph|PanelLabelShowsGlyph|NoGlyphWhenNot' -v`
Expected: `TestSoftReloadShowsGlyphAndStatus` FAILS (no per-panel glyph on Branches, no `reloading…` text). `TestNoGlyphWhenNotReloading` PASSES (locks the idle case).

- [ ] **Step 3: Append the glyph to every panel title during a soft reload**

In `internal/tui/viewstate.go`, in `panelLabel`, add a soft-reload glyph that applies to all panels. Place it just before the Commits-specific block so a Commits soft reload does not double-stamp. Current (lines 517-528):

```go
func (m Model) panelLabel(p panel, base string) string {
	if p == panelCommits {
		n := len(m.commits)
		if m.commitsExhausted {
			base += " " + strconv.Itoa(n)
		} else {
			base += " " + strconv.Itoa(n) + "+"
		}
		if m.commitsLoading {
			base += " " + commitsLoadingGlyph
		}
	}
```

Change to (move the Commits count out of the glyph concern, then stamp ONE glyph
from a unified condition — keeps ordering consistent `Commits 42+ ⏳` /
`Branches ⏳` and removes the double-glyph guard):

```go
func (m Model) panelLabel(p panel, base string) string {
	if p == panelCommits {
		n := len(m.commits)
		if m.commitsExhausted {
			base += " " + strconv.Itoa(n)
		} else {
			base += " " + strconv.Itoa(n) + "+"
		}
	}
	// Loading glyph: the Commits panel shows it during a feed reload/page
	// (commitsLoading); a soft reload (r) shows it on every panel.
	if m.softReload || (p == panelCommits && m.commitsLoading) {
		base += " " + commitsLoadingGlyph
	}
```

- [ ] **Step 4: Add the `reloading…` status-line prefix**

In `internal/tui/view.go`, after the `if m.running { … }` block (currently ends line 367), before `statusLine = truncate(...)` (line 368), add:

```go
	if m.softReload && !m.running {
		if statusLine == "" {
			statusLine = "⏳ reloading…"
		} else {
			statusLine = "⏳ reloading… · " + statusLine
		}
	}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'SoftReloadShowsGlyph|PanelLabelShowsGlyph|NoGlyphWhenNot' -v`
Expected: PASS (both).

- [ ] **Step 6: Run the full tui package + gofmt/vet**

Run: `gofmt -l internal/tui/ && go vet ./internal/tui/ && go test ./internal/tui/`
Expected: no gofmt output, no vet output, `ok` for the package (all existing tests still pass).

- [ ] **Step 7: Commit**

```bash
git add internal/tui/viewstate.go internal/tui/view.go internal/tui/soft_reload_test.go
git commit -m "feat(tui): per-panel ⏳ glyph + reloading… status line during soft reload"
```

---

### Task 4: Docs + full test sweep

**Files:**
- Modify: `CHANGELOG.md`, `internal/tui/help.go:49` (help text), `internal/tui/footer.go:112` (footer — optional, see step)

**Interfaces:** none (docs only).

- [ ] **Step 1: Update CHANGELOG.md**

Add an entry under the current unreleased/top section:

```markdown
- **Soft reload.** Pressing `r` no longer blanks the screen on large repos: the
  panels stay visible (showing the previous data) with a ⏳ in each panel title
  and a `reloading…` status line until the fresh data swaps in. Repo switches and
  initial startup keep the brief `loading…` screen.
```

- [ ] **Step 2: Refresh the `r` help text**

First confirm no test pins the current string:

Run: `grep -rn 'reload all panels' internal/tui/`
Expected: only `internal/tui/help.go:49`. If a `_test.go` file also matches, update that assertion in the same commit.

In `internal/tui/help.go:49`, update the description so it reflects the soft behavior:

```go
		r("r", "reload all panels (keeps them visible while refreshing)"),
```

(Leave `internal/tui/footer.go:112` `[r] reload` as-is — the footer is space-constrained and the label is still accurate.)

- [ ] **Step 3: Full race + e2e sweep**

Run: `./test.sh race`
Expected: vet+gofmt clean, all unit tests pass (including the new `soft_reload_test.go`), e2e green. No data races.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md internal/tui/help.go
git commit -m "docs: changelog + help for soft reload"
```

---

## Self-Review

**Spec coverage:**
- Soft render (no blank screen) for `r` → Task 2. ✓
- reRoot/startup keep blank screen → Task 2 (`TestReRootIsHardReload`, `TestViewBlanksForHardReload`). ✓
- `softReload` flag set by `r`, cleared on current-gen `dataLoadedMsg` + `reRoot`, left set on supersede (double-`r` keeps soft-rendering) → Task 1 (`TestSupersededLoadKeepsSoftReload`, `TestReRootClearsInFlightSoftReload`). ✓
- Per-panel ⏳ via generalized `commitsLoadingGlyph` → Task 3. ✓
- `reloading…` status line → Task 3. ✓
- Graceful "too little space" → covered by the existing title width clamp (no code; noted in spec). The glyph is appended pre-truncation, so a narrow panel drops it automatically — no explicit test needed. ✓
- Navigation stays live / write-ops gated → unchanged by design; no code touched, so no regression task needed. ✓
- Docs (CHANGELOG, help) → Task 4. No CLI surface change → no agentskill bump (per spec). ✓

**Placeholder scan:** No TBD/TODO; every code step shows the full edit.

**Type consistency:** `softReload bool` is referenced identically in Tasks 1–3 (`m.softReload`). `commitsLoadingGlyph` reused (not redefined). `m.View()`, `m.panelLabel`, `m.reRoot`, `dataLoadedMsg{gen:…}`, `keyMsg`, `loadedModel`, `panelBranches`/`panelCommits` all match existing symbols verified in the source.
