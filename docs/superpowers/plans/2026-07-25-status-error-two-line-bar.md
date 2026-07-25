# Temporary Two-Line Status Bar for Truncated Errors — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When an error status message doesn't fit the status bar, temporarily (30s or until a newer message) render it across two bottom rows — the second row replacing the footer key hints — with a guaranteed-visible pointer to the Settings `,` → Session errors viewer.

**Architecture:** A single central timestamp (`Model.statusMsgAt`) stamped by a thin `Update` wrapper whenever `statusMsg` changes (zero changes at the ~110 call sites), plus a render-time decision in `renderInterface`: error-classified + overflows one line + fresh → wrap across the footer row and the status row. Collapse needs no timer: the perpetual 1s heartbeat re-renders, and the render condition expires on its own.

**Tech Stack:** Go 1.26, Bubble Tea, lipgloss (display-width math), the existing `i18n` English-text-as-key bundles.

**Spec:** `docs/superpowers/specs/2026-07-25-status-error-two-line-bar-design.md` (committed on this branch).

## Global Constraints

- Branch/worktree: `feat/error-bar-expand` at `/mnt/t/others/gigagit.worktrees/feat-error-bar-expand`. ALL file paths below are relative to that worktree root. Never touch the main checkout.
- TUI render layer only: `internal/tui` + `internal/i18n/lang/*.toml`. No engine/CLI/domain changes.
- The 30s window is a constant (`statusErrExpandFor`), NOT a config key.
- No friendly rewrite for the hostname-resolution error class (decided against in the spec).
- Every new user-visible string is an `i18n.T` string-literal key present in ALL FOUR bundles (ja/ko/zh/ru) — `internal/tui/i18n_scan_test.go` enforces this; adding the key and the bundle entries must land in the same task or the gate fails.
- Style AFTER truncation (existing rule: truncation slices runes and would corrupt ANSI codes).
- Width math in display columns via `lipgloss.Width`, never bytes/runes.
- Run tests from the worktree root: `go test ./internal/tui/ -run <Name> -v`.
- Commit messages: conventional style (`feat(tui): …`, `test(tui): …`), each task commits its own work.

---

### Task 1: Central `statusMsgAt` stamp via an `Update` wrapper

**Files:**
- Modify: `internal/tui/model.go` (Model struct: add field beside `statusMsg`; rename `Update` → `dispatch` at ~line 296; add new `Update` wrapper)
- Test: `internal/tui/status_error_test.go` (append)

**Interfaces:**
- Consumes: existing `Model.statusMsg string`, `opFinishedMsg`, `newTestModel(t)` (source_test.go:17).
- Produces: `Model.statusMsgAt time.Time` — stamped with `time.Now()` by `Update` whenever the dispatched message changed `statusMsg`. Task 2's render condition reads it. The private inner dispatcher is named `dispatch` (verified free; only `dispatchGenerate` exists).

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/status_error_test.go` (it already imports `errors`, `strings`, `testing`; add `time` and `tea "github.com/charmbracelet/bubbletea"` to its import block if not present):

```go
// Any message that changes statusMsg must stamp statusMsgAt (the central
// stamp lives in the Update wrapper, so no statusMsg call site needs to know
// about it); a message that leaves statusMsg alone must not re-stamp — the
// stamp is what bounds the two-line error expansion window at render time.
func TestStatusMsgChangeStampsStatusMsgAt(t *testing.T) {
	m := newTestModel(t)
	if !m.statusMsgAt.IsZero() {
		t.Fatal("precondition: a fresh model must have a zero stamp")
	}
	u, _ := m.Update(opFinishedMsg{err: errors.New("boom")})
	m = u.(Model)
	if m.statusMsg == "" {
		t.Fatal("precondition: a failed op must set statusMsg")
	}
	if m.statusMsgAt.IsZero() {
		t.Fatal("a statusMsg change must stamp statusMsgAt")
	}
	was := m.statusMsgAt
	u, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = u.(Model)
	if m.statusMsg == "" || !m.statusMsgAt.Equal(was) {
		t.Fatalf("a message that does not change statusMsg must not re-stamp (was %v, got %v)", was, m.statusMsgAt)
	}
}
```

(`m.Update(opFinishedMsg{err: errors.New("boom")})` is the established pattern — see `checkout_as_popup_test.go:117`.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestStatusMsgChangeStampsStatusMsgAt -v`
Expected: compile FAIL — `m.statusMsgAt undefined`.

- [ ] **Step 3: Implement**

In `internal/tui/model.go`:

1. Find the `statusMsg string` field in the `Model` struct and add directly below it:

```go
	statusMsgAt time.Time // when statusMsg last changed; bounds the two-line error expansion (view.go)
```

2. Rename the existing top-level dispatcher (model.go:296) — signature only, body untouched:

```go
func (m Model) dispatch(msg tea.Msg) (tea.Model, tea.Cmd) {
```

3. Add the new wrapper immediately above it:

```go
// Update wraps the real dispatcher with the one piece of bookkeeping every
// message shares: whenever dispatch changed statusMsg — there are ~110 call
// sites, so this is the only place that can know — stamp statusMsgAt. The
// stamp bounds the temporary two-line error expansion in renderInterface;
// a newer message restarts the window by re-stamping. Recursive
// m.Update(synthKey(…)) self-calls pass through here too, which is correct:
// a synthesized key that changes statusMsg deserves a fresh stamp.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	before := m.statusMsg
	nm, cmd := m.dispatch(msg)
	if next, ok := nm.(Model); ok && next.statusMsg != before {
		next.statusMsgAt = time.Now()
		return next, cmd
	}
	return nm, cmd
}
```

(`time` is already imported by model.go. Do NOT rename any `m.Update(...)` call sites — routing self-calls through the wrapper is intended.)

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/tui/ -run TestStatusMsgChangeStampsStatusMsgAt -v`
Expected: PASS

- [ ] **Step 5: Run the whole tui package**

Run: `go test ./internal/tui/`
Expected: PASS (the wrapper must not disturb any existing Update-driven test).

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-error-bar-expand
gg add internal/tui/model.go internal/tui/status_error_test.go
gg commit -m "feat(tui): stamp statusMsgAt centrally when statusMsg changes"
```

---

### Task 2: Two-line expansion rendering + i18n key (all four bundles)

**Files:**
- Modify: `internal/tui/view.go` (constant near `statusErrStyle` ~line 85; `splitCols` helper beside `truncate` ~line 918; the truncate/style block in `renderInterface` ~lines 457–461; BOTH join sites ~lines 467 and 513)
- Modify: `internal/i18n/lang/ja.toml`, `ko.toml`, `ru.toml`, `zh.toml` (one key each, beside the existing `"Session errors"` entry at line 119 — bundles are NOT sorted; do not reorder)
- Test: `internal/tui/status_error_test.go` (append)

**Interfaces:**
- Consumes: `Model.statusMsgAt` (Task 1), existing `statusIsError`, `oneLine`, `truncate`, `statusErrStyle`, `fitFooter`.
- Produces: `splitCols(s string, n int) (head, tail string)` — display-width split, no ellipsis; `statusErrExpandFor = 30 * time.Second`; i18n key `"full: , → Session errors"`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/status_error_test.go` (imports additionally needed: `time`, `tea`, and `"github.com/charmbracelet/x/ansi"` — ansi is already imported by this file):

```go
// sizedModel returns a test model laid out at w×h with no layers open, so
// View() renders the base interface (header/panels/footer/status).
func sizedModel(t *testing.T, w, h int) Model {
	t.Helper()
	m := newTestModel(t)
	u, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return u.(Model)
}

// A fresh error too long for one line takes over the footer row: the message
// continues on a second row, the key hints vanish for the duration, and the
// bottom row always ends with the pointer to the full text in the Session
// errors viewer.
func TestLongFreshErrorExpandsOverFooter(t *testing.T) {
	m := sizedModel(t, 80, 30)
	tail := "the-very-end-of-the-error-text"
	m.statusMsg = "error: git push failed (exit 128): ssh: Could not resolve hostname " +
		strings.Repeat("x", 60) + " " + tail
	m.statusMsgAt = time.Now()
	out := ansi.Strip(m.View())
	if strings.Contains(out, "[b]ranch") {
		t.Fatalf("expanded error must replace the footer hints:\n%s", out)
	}
	if !strings.Contains(out, tail) {
		t.Fatalf("the second row must reveal the error tail:\n%s", out)
	}
	if !strings.Contains(out, "full: , → Session errors") {
		t.Fatalf("the bottom row must point at the Session errors viewer:\n%s", out)
	}
}

// A short error keeps today's single-line bar and the footer hints.
func TestShortErrorStaysOneLine(t *testing.T) {
	m := sizedModel(t, 80, 30)
	m.statusMsg = "error: boom"
	m.statusMsgAt = time.Now()
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "[b]ranch") {
		t.Fatalf("a short error must not hide the footer:\n%s", out)
	}
	if strings.Contains(out, "full: , → Session errors") {
		t.Fatalf("a short error needs no viewer pointer:\n%s", out)
	}
}

// A long NON-error message never expands — the footer survives and the
// message truncates as before.
func TestLongNonErrorNeverExpands(t *testing.T) {
	m := sizedModel(t, 80, 30)
	m.statusMsg = "pulled and rebased onto origin/main " + strings.Repeat("y", 120)
	m.statusMsgAt = time.Now()
	if out := ansi.Strip(m.View()); !strings.Contains(out, "[b]ranch") {
		t.Fatalf("a non-error message must not take the footer row:\n%s", out)
	}
}

// The expansion is temporary: past the 30s window the bar collapses back to
// one truncated line and the footer returns.
func TestExpiredErrorCollapses(t *testing.T) {
	m := sizedModel(t, 80, 30)
	m.statusMsg = "error: git push failed (exit 128): " + strings.Repeat("x", 120)
	m.statusMsgAt = time.Now().Add(-statusErrExpandFor - time.Second)
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "[b]ranch") {
		t.Fatalf("an expired error must give the footer row back:\n%s", out)
	}
	if strings.Contains(out, "full: , → Session errors") {
		t.Fatalf("an expired error must not keep the pointer row:\n%s", out)
	}
}

// Even when two rows cannot hold the message, the viewer pointer survives at
// the bottom row's tail — truncation eats the message, never the pointer.
func TestHintSurvivesExtremeTruncation(t *testing.T) {
	m := sizedModel(t, 44, 30)
	m.statusMsg = "error: " + strings.Repeat("z", 400)
	m.statusMsgAt = time.Now()
	if out := ansi.Strip(m.View()); !strings.Contains(out, "full: , → Session errors") {
		t.Fatalf("the pointer must survive extreme truncation:\n%s", out)
	}
}

// splitCols is the wrap primitive: head is the widest prefix that fits the
// column budget (no ellipsis), tail the remainder with leading spaces
// dropped; a wide glyph that would straddle the boundary moves wholly into
// tail so neither row can exceed its width.
func TestSplitCols(t *testing.T) {
	head, tail := splitCols("abcdef", 4)
	if head != "abcd" || tail != "ef" {
		t.Fatalf("plain split: got %q %q", head, tail)
	}
	head, tail = splitCols("ab cdef", 3)
	if head != "ab " && head != "ab" {
		t.Fatalf("split near a space: got head %q", head)
	}
	if strings.HasPrefix(tail, " ") {
		t.Fatalf("tail must not start with a space: %q", tail)
	}
	head, tail = splitCols("ab", 10)
	if head != "ab" || tail != "" {
		t.Fatalf("short input: got %q %q", head, tail)
	}
	// ⏳ is 2 columns wide; with 1 column left it must move to tail entirely.
	head, tail = splitCols("a⏳b", 2)
	if head != "a" || tail != "⏳b" {
		t.Fatalf("wide glyph must not straddle the boundary: got %q %q", head, tail)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestLongFreshErrorExpandsOverFooter|TestShortErrorStaysOneLine|TestLongNonErrorNeverExpands|TestExpiredErrorCollapses|TestHintSurvivesExtremeTruncation|TestSplitCols' -v`
Expected: compile FAIL — `splitCols` and `statusErrExpandFor` undefined.

- [ ] **Step 3: Implement in view.go**

1. Beside `statusErrStyle` (~line 85) add:

```go
// statusErrExpandFor bounds the temporary two-line error bar: an error too
// long for one line may take the footer row for this long (or until a newer
// status message re-stamps statusMsgAt). Constant on purpose — not config.
const statusErrExpandFor = 30 * time.Second
```

2. Beside `truncate` (~line 918) add:

```go
// splitCols splits s at n display columns: head is the widest prefix that
// fits (no ellipsis), tail the remainder with leading spaces dropped. The
// split is column-exact — a wide glyph that would straddle the boundary
// moves entirely into tail, so neither part can exceed its budget.
func splitCols(s string, n int) (head, tail string) {
	if n <= 0 {
		return "", s
	}
	if lipgloss.Width(s) <= n {
		return s, ""
	}
	r := []rune(s)
	i, w := 0, 0
	for i < len(r) {
		cw := lipgloss.Width(string(r[i]))
		if w+cw > n {
			break
		}
		w += cw
		i++
	}
	return string(r[:i]), strings.TrimLeft(string(r[i:]), " ")
}
```

3. In `renderInterface`, replace the current truncate/style block (lines ~457–461):

```go
	statusLine = truncate(oneLine(statusLine), g.w)
	// Style after truncation: truncate slices runes and would corrupt ANSI codes.
	if errMode {
		statusLine = statusErrStyle.Render(statusLine)
	}
```

with:

```go
	// Style after truncation: truncate slices runes and would corrupt ANSI codes.
	// An error too long for one line and still fresh temporarily takes the
	// footer row too (30s, or until a newer message re-stamps statusMsgAt —
	// the perpetual heartbeat re-render collapses it on expiry). The bottom
	// row's tail always keeps the pointer to the full text in the Session
	// errors viewer: truncation eats the message, never the pointer.
	full := oneLine(statusLine)
	footerRow := footer
	var statusRow string
	if errMode && lipgloss.Width(full) > g.w && time.Since(m.statusMsgAt) < statusErrExpandFor {
		hint := " · " + i18n.T("full: , → Session errors")
		head, tail := splitCols(full, g.w)
		room := g.w - lipgloss.Width(hint)
		if room < 1 {
			room = 1
		}
		footerRow = statusErrStyle.Render(head)
		statusRow = statusErrStyle.Render(truncate(tail, room) + hint)
	} else {
		statusRow = truncate(full, g.w)
		if errMode {
			statusRow = statusErrStyle.Render(statusRow)
		}
	}
```

4. Update BOTH join sites to use the new row variables (the `statusLine` variable is no longer joined):

Narrow branch (~line 467):
```go
		return strings.Join([]string{header, body, footerRow, statusRow}, "\n")
```

Normal return (~line 513):
```go
	return strings.Join([]string{header, body, footerRow, statusRow}, "\n")
```

5. Add the key to all four bundles, next to each file's existing `"Session errors"` entry at line 119 (do NOT sort or reorder the files):

`internal/i18n/lang/ja.toml`:
```toml
"full: , → Session errors" = "全文: , → セッションエラー"
```
`internal/i18n/lang/ko.toml`:
```toml
"full: , → Session errors" = "전체: , → 세션 오류"
```
`internal/i18n/lang/ru.toml`:
```toml
"full: , → Session errors" = "полный текст: , → Ошибки сеанса"
```
`internal/i18n/lang/zh.toml`:
```toml
"full: , → Session errors" = "完整信息: , → 会话错误"
```

(The `,` inside each translation is the literal Settings keybinding — keep it verbatim. The "Session errors" part reuses each bundle's existing translation of that Settings row so the pointer matches what the user will see in the menu. The hint renders in the same `statusErrStyle` as the rest of the row — a faint/dim style on the red error background would be unreadable; this deviates from the spec's "dim hint" wording deliberately.)

- [ ] **Step 4: Run the new tests**

Run: `go test ./internal/tui/ -run 'TestLongFreshErrorExpandsOverFooter|TestShortErrorStaysOneLine|TestLongNonErrorNeverExpands|TestExpiredErrorCollapses|TestHintSurvivesExtremeTruncation|TestSplitCols' -v`
Expected: PASS

- [ ] **Step 5: Run the i18n gates and the whole package**

Run: `go test ./internal/tui/ ./internal/i18n/`
Expected: PASS — in particular `i18n_scan_test.go` (key present ×4, orphan/verb checks) and every existing render test.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-error-bar-expand
gg add internal/tui/view.go internal/tui/status_error_test.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/ru.toml internal/i18n/lang/zh.toml
gg commit -m "feat(tui): two-line status bar (30s) for errors too long to read"
```

---

### Task 3: Full verification, visual sanity, docs

**Files:**
- Modify: `CHANGELOG.md` (new entry at top, matching existing entry style)
- Modify: `README.md` (one sentence where the status bar / error surfacing is described, if such a section exists — check with `grep -n "status bar\|Session errors" README.md`; skip if none fits)
- Modify: `CLAUDE.md` (the `tui` package-map row: append one sentence describing the expansion; keep the row's existing prose style)

**Interfaces:**
- Consumes: Tasks 1–2 complete on the branch.
- Produces: a built `gg` binary delivered to the user; docs updated; full gates green.

- [ ] **Step 1: Full test suite**

Run from the worktree root: `./test.sh`
Expected: vet+gofmt, unit, e2e all PASS.

- [ ] **Step 2: Race pass**

Run: `./test.sh race`
Expected: PASS (required before merging per CLAUDE.md).

- [ ] **Step 3: Visual sanity via tui-capture**

Build first: `go build -o ./gg ./cmd/gg` (in the worktree). Then use the `driving-tui-headless` skill: point `tui-capture.sh` at a scratch clone whose `origin` URL uses an unresolvable ssh host (e.g. `git remote set-url origin git@definitely-not-a-host-xyz:none/none.git`), press `P` (push), wait for the failure, and capture. Verify the screen shows the error across the two bottom rows with the `full: , → Session errors` tail and no footer hints; capture again after pressing a key that produces a new status message and verify the footer returned.

- [ ] **Step 4: Docs**

- `CHANGELOG.md`: add an entry like *"TUI: an error too long for the status bar now temporarily (30s) takes the footer row too, ending with a pointer to the full text in Settings `,` → Session errors."*
- `CLAUDE.md` `tui` row: append one sentence, e.g. *"**Two-line error bar** (`statusMsgAt` stamped centrally in the `Update` wrapper; `renderInterface`): an error status message wider than the terminal renders across the footer+status rows for 30s (`statusErrExpandFor`) or until a newer message, the bottom row's tail always keeping the `full: , → Session errors` pointer."*
- `README.md`: only if a fitting user-facing section exists.

- [ ] **Step 5: Commit docs**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-error-bar-expand
gg add CHANGELOG.md CLAUDE.md README.md
gg commit -m "docs: changelog + notes for the two-line error bar"
```

- [ ] **Step 6: Deliver the binary**

Send the built binary to the user via SendUserFile with the ABSOLUTE path `/mnt/t/others/gigagit.worktrees/feat-error-bar-expand/gg`. Do NOT merge — the user decides about merging (ask, never merge unprompted).

---

## Self-Review

- **Spec coverage:** expansion trigger (Task 2 step 3.3 condition), central stamp (Task 1), footer-row takeover both join sites (Task 2 step 3.4), reserved hint (room math + TestHintSurvivesExtremeTruncation), 30s constant + heartbeat collapse (verified: `heartbeatMsg` is perpetual, started in Init, model.go:1934–1942), i18n key ×4 (Task 2 step 3.5), test matrix (Task 2 step 1 — six spec cases; "newer non-error message collapses" is covered by the stamp test in Task 1 plus TestLongNonErrorNeverExpands: a replacement re-stamps and reclassifies, which are exactly the two render inputs), tui-capture pass (Task 3). No gaps.
- **Placeholder scan:** none — all code inline.
- **Type consistency:** `statusMsgAt time.Time` (Tasks 1→2), `splitCols(string, int) (string, string)`, `statusErrExpandFor` const — names match across tasks. `dispatch` verified unclaimed.
- **Deviation noted inline:** hint uses `statusErrStyle`, not a dim style (readability on the red background); recorded in Task 2 step 3.5.
