# Footer Overflow → Help Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The footer never cuts a shortcut label mid-word: on overflow it drops whole labels from the end, ends with a protected `… [?] help` tail, and the `?` help window lists the dropped keys as its first section.

**Architecture:** Refactor `footerLine()` (internal/tui/footer.go) into a parts list (`footerParts`) plus a hand-written-mode override helper, then add a width-aware `fitFooter(m, w)` used by the renderer (view.go) and the `?` handler (model.go). The help window gains a `helpWithHidden` wrapper that prepends the hidden bindings to the static `helpContent()` table.

**Tech Stack:** Go 1.26, Bubble Tea / lipgloss (already vendored). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-07-12-footer-overflow-help-design.md`

**Worktree:** all work happens in `/mnt/t/others/gigagit/.claude/worktrees/footer-overflow-help` on branch `feat/footer-overflow-help`. Use absolute worktree paths for every file operation — an absolute path into the main checkout would silently edit the wrong tree.

## Global Constraints

- `Model` is a **value receiver**; never add pointer methods to it.
- Width is measured in **display columns** via `lipgloss.Width` (labels carry wide glyphs like `⇧←→`), never `len()`.
- Existing behavior when everything fits must be **byte-identical** to today's `footerLine()` output (many tests assert substrings of it).
- `footerLine()` keeps its width-less signature — 38+ call sites in tests.
- Go source is LF-only (`.gitattributes` enforces it); do not introduce CRLF.
- Every task: `gofmt -l internal cmd` clean, `go vet ./internal/tui/` clean, TDD (write the failing test first).
- Commit messages end with:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_01MxgTsrNGbPzUjrSygfA68X`

---

### Task 1: `footerParts` refactor (pure restructuring, output unchanged)

**Files:**
- Modify: `internal/tui/footer.go` (the `footerLine` function, lines ~152–231)
- Test: `internal/tui/footer_test.go` (append)

**Interfaces:**
- Consumes: existing `footerBinding`, `contextBindings`, `globalBindings`, `bindingByID` (all in footer.go, unchanged).
- Produces (used by Task 2):
  - `type footerPart struct { label string; binding footerBinding; groupStart bool }`
  - `func (m Model) footerParts() []footerPart`
  - `func joinFooterParts(parts []footerPart) string`
  - `func (m Model) footerOverride() (string, bool)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/footer_test.go`:

```go
// TestFooterPartsRoundTrip pins the refactor: joining the parts must
// reproduce footerLine byte-for-byte, and exactly one part (the first live
// global after a non-empty context group) carries the bullet separator.
func TestFooterPartsRoundTrip(t *testing.T) {
	m := footerModel()
	if got := joinFooterParts(m.footerParts()); got != m.footerLine() {
		t.Errorf("joinFooterParts(footerParts()) = %q\nfooterLine() = %q", got, m.footerLine())
	}
	if !strings.Contains(m.footerLine(), "  •  ") {
		t.Fatalf("fixture footer must contain the group separator: %q", m.footerLine())
	}
	var starts int
	for _, p := range m.footerParts() {
		if p.groupStart {
			starts++
		}
	}
	if starts != 1 {
		t.Errorf("exactly one groupStart expected, got %d", starts)
	}
}

func TestFooterPartsAllowlistRoundTrip(t *testing.T) {
	m := footerModel()
	m.cfg.UI.FooterActions = []string{"repo", "pull"}
	if got := joinFooterParts(m.footerParts()); got != m.footerLine() {
		t.Errorf("allowlist parts = %q\nfooterLine() = %q", got, m.footerLine())
	}
	parts := m.footerParts()
	if len(parts) == 0 || parts[len(parts)-1].binding.id != "actions" {
		t.Errorf("allowlist parts must end with the actions binding: %+v", parts)
	}
}

// TestFooterOverrideModes pins which states bypass the registry footer.
func TestFooterOverrideModes(t *testing.T) {
	m := footerModel()
	if _, ok := m.footerOverride(); ok {
		t.Error("idle panels must use the registry footer")
	}
	m.filterTyping = true
	if s, ok := m.footerOverride(); !ok || !strings.Contains(s, "filter") {
		t.Errorf("filterTyping must override the footer, got %q ok=%v", s, ok)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/footer-overflow-help && go test ./internal/tui/ -run 'TestFooterParts|TestFooterOverride' -v`
Expected: FAIL — `undefined: joinFooterParts`, `m.footerParts undefined`, `m.footerOverride undefined`.

- [ ] **Step 3: Restructure footer.go**

Replace the whole `footerLine` function (from the `// footerLine builds ...` comment through its closing brace, currently lines ~152–231) with the following. Every hand-written string and comment is carried over verbatim from the old body — do not retype them, move them:

```go
// footerOverride returns the hand-written footer for the modes that own the
// keyboard, or ok=false when the registry-driven footer applies.
func (m Model) footerOverride() (string, bool) {
	// A process owns the keyboard; the panel footer would advertise keys that do
	// nothing, so show the process's own indicator instead.
	if m.proc != nil {
		return m.proc.indicator(m), true
	}
	if m.filterTyping {
		return "filter: type to search  [↑↓] move  [enter] keep  [esc] cancel", true
	}
	if m.highlightTyping {
		return "highlight: type to search  [↑↓] move  [ctrl+↑/↓] prev/next match  [enter] keep  [esc] clear", true
	}
	// The files view owns the keyboard while open, so the registry footer would
	// lie; show the view's own keys instead. The commit-list side mirrors the
	// Commits panel (. menu + graph keys); the tree side is file-scoped.
	if m.filesView != nil {
		if m.filesPreview != nil && !m.filesTreeFocused {
			return "file: [↑/↓] scroll  [z] view  [←/tab] back to tree  [esc] close preview", true
		}
		// i shows the displayed commit's message — only when canShowFilesViewMessage
		// holds (same gate as the handler, so the footer never advertises a dead i).
		msgHint := ""
		if m.canShowFilesViewMessage() {
			msgHint = "  [i] msg"
		}
		if m.filesTreeFocused {
			// [a] mirrors the handler's gate exactly (stash/compare/shelf have no
			// full tree to toggle to) so the footer never advertises a dead key.
			aHint := ""
			if m.stashView == nil && !m.inCompareMode() && m.filesHash != "" {
				aHint = "  [a] all files"
			}
			return "tree: [↑/↓] move  [enter] diff" + aHint + "  [.] view file/copy  [/] search  [h] hist  [b] blame  [z] view" + msgHint + "  [esc/l] close", true
		}
		return "commits: [enter/tab] tree  [↑/↓] move  [<>=] graph  [a] all files  [/] search  [.] actions" + msgHint + "  [esc/l] close", true
	}
	// The stash list owns the keyboard while it is the focused right column
	// (no file tree yet). When focus has moved to a left panel, fall through to
	// that panel's normal footer.
	if m.stashView != nil && m.focus == panelCommits {
		return "stash: [↑/↓] move  [l] files  [z] view  [←] panels  [enter] apply/pop/drop  [esc/S] close", true
	}
	return "", false
}

// footerPart is one renderable footer label plus the binding behind it.
// groupStart marks the context→global boundary ("  •  " separator).
type footerPart struct {
	label      string
	binding    footerBinding
	groupStart bool
}

// footerParts returns the registry-driven footer as ordered parts. A
// configured footer_actions allowlist replaces the default two-group layout:
// exactly those ids, in list order, among the available ones; [.] actions
// always stays so the menu remains discoverable.
func (m Model) footerParts() []footerPart {
	if ids := m.cfg.UI.FooterActions; len(ids) > 0 {
		var parts []footerPart
		for _, id := range ids {
			if b, ok := bindingByID(id); ok && b.when(m) {
				parts = append(parts, footerPart{label: b.label, binding: b})
			}
		}
		if b, ok := bindingByID("actions"); ok && b.when(m) {
			parts = append(parts, footerPart{label: b.label, binding: b})
		}
		return parts
	}
	var parts []footerPart
	for _, b := range contextBindings {
		if b.when(m) {
			parts = append(parts, footerPart{label: b.label, binding: b})
		}
	}
	nCtx := len(parts)
	for _, b := range globalBindings {
		if b.when(m) {
			p := footerPart{label: b.label, binding: b}
			if len(parts) == nCtx && nCtx > 0 {
				p.groupStart = true
			}
			parts = append(parts, p)
		}
	}
	return parts
}

// joinFooterParts renders parts with the standard separators: one space
// within a group, "  •  " at the groupStart boundary.
func joinFooterParts(parts []footerPart) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			if p.groupStart {
				b.WriteString("  •  ")
			} else {
				b.WriteString(" ")
			}
		}
		b.WriteString(p.label)
	}
	return b.String()
}

// footerLine builds the context-sensitive footer: panel/row-specific actions,
// a separator, then the predicated global tail. Mode footers (filter input,
// files view, …) override everything because those modes capture every key.
func (m Model) footerLine() string {
	if s, ok := m.footerOverride(); ok {
		return s
	}
	return joinFooterParts(m.footerParts())
}
```

- [ ] **Step 4: Run the full package tests**

Run: `go test ./internal/tui/`
Expected: PASS (the new tests AND every pre-existing footer/space/discard/files-view test — the refactor must not change any output).

- [ ] **Step 5: gofmt + vet + commit**

```bash
gofmt -l internal && go vet ./internal/tui/
git add internal/tui/footer.go internal/tui/footer_test.go
git commit -m "refactor(tui): extract footerParts/footerOverride from footerLine

Pure restructuring — footerLine output is byte-identical. Prepares the
width-aware footer fitting.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01MxgTsrNGbPzUjrSygfA68X"
```

---

### Task 2: `fitFooter` — width-aware fitting with the protected tail

**Files:**
- Modify: `internal/tui/footer.go` (append; add lipgloss import)
- Test: `internal/tui/footer_test.go` (append)

**Interfaces:**
- Consumes (from Task 1): `footerPart`, `(Model).footerParts()`, `joinFooterParts`, `(Model).footerOverride()`; plus the existing `truncate(s string, n int) string` (view.go:890).
- Produces (used by Task 3):
  - `const footerOverflowTail = "… [?] help"`
  - `func fitFooter(m Model, w int) (string, []footerBinding)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/footer_test.go` (the file already imports `lipgloss` and `strings`):

```go
func TestFitFooterWideUnchanged(t *testing.T) {
	m := footerModel()
	line, hidden := fitFooter(m, 500)
	if line != m.footerLine() {
		t.Errorf("wide fit must be untrimmed:\n%q\n%q", line, m.footerLine())
	}
	if hidden != nil {
		t.Errorf("wide fit must hide nothing: %v", hidden)
	}
}

func TestFitFooterExactWidthUnchanged(t *testing.T) {
	m := footerModel()
	full := m.footerLine()
	line, hidden := fitFooter(m, lipgloss.Width(full))
	if line != full || hidden != nil {
		t.Errorf("exact-width fit must be unchanged: %q hidden=%v", line, hidden)
	}
}

// TestFitFooterNarrowDropsFromEndAndAppendsTail is the core contract: whole
// labels drop from the end, the line ends with the protected tail, fits the
// width, and hidden is exactly the contiguous dropped tail in footer order.
func TestFitFooterNarrowDropsFromEndAndAppendsTail(t *testing.T) {
	m := footerModel()
	full := m.footerLine()
	w := lipgloss.Width(full) - 1 // one column short: at least one label drops
	line, hidden := fitFooter(m, w)
	if lipgloss.Width(line) > w {
		t.Errorf("fitted line overflows: %d > %d (%q)", lipgloss.Width(line), w, line)
	}
	if !strings.HasSuffix(line, footerOverflowTail) {
		t.Errorf("trimmed footer must end with %q: %q", footerOverflowTail, line)
	}
	if len(hidden) == 0 {
		t.Fatal("at least one binding must be reported hidden")
	}
	var nonHelp []footerPart
	for _, p := range m.footerParts() {
		if p.binding.id != "help" {
			nonHelp = append(nonHelp, p)
		}
	}
	cut := len(nonHelp) - len(hidden)
	if cut <= 0 {
		t.Fatalf("expected a visible prefix, all %d parts hidden", len(nonHelp))
	}
	for i, b := range hidden {
		if nonHelp[cut+i].label != b.label {
			t.Fatalf("hidden[%d] = %q, want contiguous tail part %q", i, b.label, nonHelp[cut+i].label)
		}
	}
	want := joinFooterParts(nonHelp[:cut]) + " " + footerOverflowTail
	if line != want {
		t.Errorf("fitted line = %q, want %q", line, want)
	}
}

func TestFitFooterTinyWidthFallsBackToTruncate(t *testing.T) {
	m := footerModel()
	line, hidden := fitFooter(m, 8) // narrower than the tail itself (10 cols)
	if want := truncate(m.footerLine(), 8); line != want {
		t.Errorf("tiny width must fall back to truncation: %q want %q", line, want)
	}
	if hidden != nil {
		t.Errorf("tiny-width fallback must hide nothing: %v", hidden)
	}
}

func TestFitFooterPassesThroughModeFooters(t *testing.T) {
	m := footerModel()
	m.filterTyping = true
	line, hidden := fitFooter(m, 20)
	if want := truncate(m.footerLine(), 20); line != want {
		t.Errorf("mode footer must be truncated as before: %q want %q", line, want)
	}
	if hidden != nil {
		t.Errorf("mode footers hide nothing: %v", hidden)
	}
}

func TestFitFooterAllowlistOverflow(t *testing.T) {
	m := footerModel()
	m.cfg.UI.FooterActions = []string{"repo", "pull", "stashes", "undo", "bookmarks", "find", "order", "view", "settings"}
	full := m.footerLine()
	w := lipgloss.Width(full) - 1
	line, hidden := fitFooter(m, w)
	if !strings.HasSuffix(line, footerOverflowTail) {
		t.Errorf("allowlist overflow must end with the tail: %q", line)
	}
	if len(hidden) == 0 {
		t.Error("allowlist overflow must report hidden bindings")
	}
	if lipgloss.Width(line) > w {
		t.Errorf("allowlist fitted line overflows: %q", line)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestFitFooter -v`
Expected: FAIL — `undefined: fitFooter`, `undefined: footerOverflowTail`.

- [ ] **Step 3: Implement fitFooter**

Append to `internal/tui/footer.go`, and change its import block from `import "strings"` to:

```go
import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)
```

```go
// footerOverflowTail is the protected tail rendered when the footer had to
// drop labels: the ellipsis signals more keys exist, and ? (the help window)
// is where the dropped ones are listed.
const footerOverflowTail = "… [?] help"

// fitFooter renders the footer to at most w display columns without ever
// cutting a label mid-word. When the full line fits it is returned unchanged.
// Otherwise whole labels are dropped from the end (context bindings render
// first, so the stable global keys hide before the panel-specific ones), the
// line ends with footerOverflowTail, and the dropped bindings are returned
// for the ? help window to list. Hand-written mode footers (process, filter
// input, files view, stash list) are width-truncated as before and hide
// nothing; so does the degenerate width where not even the tail fits.
func fitFooter(m Model, w int) (string, []footerBinding) {
	if s, ok := m.footerOverride(); ok {
		return truncate(s, w), nil
	}
	parts := m.footerParts()
	full := joinFooterParts(parts)
	if lipgloss.Width(full) <= w {
		return full, nil
	}
	tailW := lipgloss.Width(footerOverflowTail)
	if tailW > w {
		return truncate(full, w), nil
	}
	cur := ""
	var hidden []footerBinding
	fitting := true
	for _, p := range parts {
		if p.binding.id == "help" {
			continue // always visible, inside the tail
		}
		if fitting {
			sep := ""
			if cur != "" {
				sep = " "
				if p.groupStart {
					sep = "  •  "
				}
			}
			cand := cur + sep + p.label
			if lipgloss.Width(cand)+1+tailW <= w {
				cur = cand
				continue
			}
			// First label that doesn't fit: stop taking — everything from
			// here on is hidden, so labels only ever drop from the end.
			fitting = false
		}
		hidden = append(hidden, p.binding)
	}
	if cur == "" {
		return footerOverflowTail, hidden
	}
	return cur + " " + footerOverflowTail, hidden
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'TestFitFooter|TestFooter' -v` then `go test ./internal/tui/`
Expected: PASS (all).

- [ ] **Step 5: gofmt + vet + commit**

```bash
gofmt -l internal && go vet ./internal/tui/
git add internal/tui/footer.go internal/tui/footer_test.go
git commit -m "feat(tui): fitFooter — width-aware footer with protected '… [?] help' tail

Whole labels drop from the end on overflow; dropped bindings are
returned for the help window. Mode footers and degenerate widths keep
the old truncation.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01MxgTsrNGbPzUjrSygfA68X"
```

---

### Task 3: Wire the renderer and the `?` help window

**Files:**
- Modify: `internal/tui/view.go:363` (one line)
- Modify: `internal/tui/model.go:1630-1631` (the `case "?"` handler)
- Modify: `internal/tui/help.go` (append `helpWithHidden`)
- Test: `internal/tui/help_test.go` (append), `internal/tui/footer_test.go` (append render test)

**Interfaces:**
- Consumes (from Task 2): `fitFooter`, `footerOverflowTail`; existing `helpContent()`, `contentLine`, `newContentPopup`, `padRight(s string, n int) string` (view.go:932), `layerOf[*contentPopup]` (tests), `m.layout().w`.
- Produces: `func helpWithHidden(hidden []footerBinding) []contentLine`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/help_test.go` (no new imports needed — `strings` is already imported):

```go
// TestHelpListsHiddenFooterKeysAtNarrowWidth: at a width where the footer
// overflows, ? must open help with a leading "More keys" section listing
// exactly the bindings fitFooter dropped, in footer order.
func TestHelpListsHiddenFooterKeysAtNarrowWidth(t *testing.T) {
	m := footerModel()
	m.width = 40
	u, _ := m.Update(keyMsg("?"))
	m = u.(Model)
	p := layerOf[*contentPopup](m)
	if p == nil {
		t.Fatal("? must open the help popup")
	}
	_, hidden := fitFooter(m, m.layout().w)
	if len(hidden) == 0 {
		t.Fatal("fixture must overflow at width 40")
	}
	if !p.lines[0].heading || !strings.Contains(p.lines[0].text, "More keys") {
		t.Fatalf("first help line must be the hidden-keys heading, got %+v", p.lines[0])
	}
	for i, b := range hidden {
		if !strings.Contains(p.lines[1+i].text, b.label) {
			t.Errorf("help row %d must carry %q, got %q", i, b.label, p.lines[1+i].text)
		}
	}
	// The static table must follow, un-mangled.
	if !strings.Contains(p.lines[1+len(hidden)].text, "Global") {
		t.Errorf("static help must follow the hidden section, got %q", p.lines[1+len(hidden)].text)
	}
}

func TestHelpNoHiddenSectionAtWideWidth(t *testing.T) {
	m := footerModel()
	m.width = 500
	u, _ := m.Update(keyMsg("?"))
	m = u.(Model)
	p := layerOf[*contentPopup](m)
	if p == nil {
		t.Fatal("? must open the help popup")
	}
	if strings.Contains(p.lines[0].text, "More keys") {
		t.Fatal("no hidden-keys section expected at a width where everything fits")
	}
}
```

Append to `internal/tui/footer_test.go` (imports `ansi` already):

```go
// TestRenderFooterShowsTailWhenOverflowing pins the view.go wiring: the
// rendered frame's footer must end with the protected tail, never a
// mid-label hard cut.
func TestRenderFooterShowsTailWhenOverflowing(t *testing.T) {
	m := footerModel()
	m.width = 40
	out := ansi.Strip(m.render())
	if !strings.Contains(out, footerOverflowTail) {
		t.Fatalf("narrow render must show the overflow tail:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestHelpListsHidden|TestHelpNoHidden|TestRenderFooterShowsTail' -v`
Expected: FAIL — the render test shows a hard-truncated footer (no tail); the help tests fail on the missing heading (`helpWithHidden` undefined won't compile first — implement Step 3 in one go after seeing the compile error).

- [ ] **Step 3: Implement the wiring**

`internal/tui/view.go:363` — replace

```go
	footer := truncate(m.footerLine(), g.w)
```

with

```go
	footer, _ := fitFooter(m, g.w)
```

`internal/tui/model.go:1630-1631` — replace

```go
		case "?":
			m = m.pushLayer(newContentPopup("Help — keys", helpContent()))
```

with

```go
		case "?":
			_, hidden := fitFooter(m, m.layout().w)
			m = m.pushLayer(newContentPopup("Help — keys", helpWithHidden(hidden)))
```

`internal/tui/help.go` — append:

```go
// helpWithHidden prepends the footer's currently hidden bindings — dropped
// for width by fitFooter at this exact terminal size — as the first help
// section, so the footer's "… [?] help" tail always leads to a list of
// exactly what was cut. With nothing hidden the static table is returned
// as-is. layout().w is a pure function of the model, so the ? handler and
// the renderer can never disagree about what was hidden.
func helpWithHidden(hidden []footerBinding) []contentLine {
	if len(hidden) == 0 {
		return helpContent()
	}
	lines := make([]contentLine, 0, len(hidden)+1)
	lines = append(lines, contentLine{text: "More keys (not shown in the footer)", heading: true})
	for _, b := range hidden {
		lines = append(lines, contentLine{text: padRight(b.key, 16) + b.label})
	}
	return append(lines, helpContent()...)
}
```

- [ ] **Step 4: Run the full package tests**

Run: `go test ./internal/tui/`
Expected: PASS — including the untouched `TestHelpFooterCoverage` drift guard (it reads the static `helpContent()` directly) and `TestHelpOpensWithQuestionMark` (wide default width → no hidden section).

- [ ] **Step 5: gofmt + vet + commit**

```bash
gofmt -l internal && go vet ./internal/tui/
git add internal/tui/view.go internal/tui/model.go internal/tui/help.go internal/tui/help_test.go internal/tui/footer_test.go
git commit -m "feat(tui): footer overflow ends '… [?] help'; help lists the hidden keys

The renderer fits whole labels instead of hard-truncating; ? prepends a
'More keys (not shown in the footer)' section with exactly the dropped
bindings.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01MxgTsrNGbPzUjrSygfA68X"
```

---

### Task 4: Docs, full-suite verification

**Files:**
- Modify: `README.md:41-42`
- Modify: `CHANGELOG.md` (`[Unreleased]` → `### Added`)

**Interfaces:** none (docs only).

- [ ] **Step 1: README**

Replace lines 41–42:

```markdown
The footer is contextual: it lists only the keys that apply to the focused
panel and selected row right now; `?` opens the full searchable reference.
```

with:

```markdown
The footer is contextual: it lists only the keys that apply to the focused
panel and selected row right now. When the terminal is too narrow for all of
them, whole entries are dropped from the end and the line ends with
`… [?] help` — the dropped keys are listed at the top of the `?` help window
("More keys"), so nothing is ever silently hidden. `?` opens the full
searchable reference.
```

- [ ] **Step 2: CHANGELOG**

Under `## [Unreleased]`, in the `### Added` section (create the section if absent, directly under the `[Unreleased]` heading), add:

```markdown
- TUI: the footer no longer hard-truncates on narrow terminals — whole
  shortcut labels are dropped from the end, the line ends with a protected
  `… [?] help` tail, and the `?` help window lists the dropped keys in a
  leading "More keys (not shown in the footer)" section.
```

- [ ] **Step 3: Full verification**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/footer-overflow-help
go build ./cmd/gg && gofmt -l internal cmd && go vet ./... && go test ./...
```

Expected: build OK, no gofmt output, vet clean, all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add README.md CHANGELOG.md
git commit -m "docs: footer overflow behavior (… [?] help tail + More-keys help section)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01MxgTsrNGbPzUjrSygfA68X"
```
