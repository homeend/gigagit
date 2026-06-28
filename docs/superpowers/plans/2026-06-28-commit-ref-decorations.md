# Commits-window ref decorations — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`).

**Goal:** In the Commits panel, render a `git log --decorate`-style group before the subject listing extra branch tips and tags (tags in yellow with a `⊙` glyph), plus a superscript count badge on the `■` marker when ≥2 branches tip a commit; collapse to `(+N)` when the panel is too narrow.

**Architecture:** A single pure helper `commitDecoGroup(id, budget)` owns the group layout (string + tag color spans + collapsed flag). The row builder (`commitIdentRowAt`) and the decorator builder (`commitDecoratorsRange`) both call it with the SAME budget so the rendered string and the yellow tag-color spans always agree (no inline ANSI; coloring stays in the single-pass `commitLineDecorator`). The Commits panel content width is threaded through `commitBody` to compute the budget.

**Tech Stack:** Go 1.26, Bubble Tea TUI, lipgloss. All changes in `internal/tui`.

## Global Constraints

- `internal/tui` MUST NOT import `internal/git` (archtest-guarded).
- The marker area stays EXACTLY 3 display cells (`commitMarkerW`); a `lipgloss.Width` assertion guards the count badge.
- No inline ANSI in commit row strings — all coloring goes through `commitLineDecorator` (single pass over plain runes, avoids rune-index drift). The row string the window truncates and the haystack searches must stay plain text.
- The decoration group renders before the subject whenever there is ≥1 extra branch OR ≥1 tag — including on non-tip lineage rows (tags show on their actual commit).
- Tags: yellow (`lipgloss.Color("220")`), `⊙` glyph prefix. Extra branches: default foreground. Count badge: superscript `²`–`⁹`, `⁺` for ≥10; shown only when ≥2 local tips.
- The identity column (primary branch / lineage) is UNCHANGED. The after-subject `pills()` path is REMOVED from row assembly.
- `commitDecoGroup` and `commitIdentRowAt` must produce identical group layout for the same `(id, budget)` — the decorator depends on it.
- TDD: failing test → see fail → implement → see pass → commit. The full `internal/tui` test run can take ~20-30s; allow it.

---

### Task 1: data + count-badge marker (commit_ident.go)

**Files:**
- Modify: `internal/tui/commit_ident.go` (`commitIdent` struct; `commitIdentOf`; a count-badge marker renderer; `token`/`fullToken`)
- Test: `internal/tui/commit_ident_decorations_test.go`

**Interfaces:**
- `commitIdent` gains `tags []string` and `count int` (number of local-branch tips at the commit).
- Produces `func (id commitIdent) markerField() string` — the 3-cell `markers()`+badge field (replaces the bare `markers() + " "` used by `token`/`fullToken`).

- [ ] **Step 1: Failing tests**

`internal/tui/commit_ident_decorations_test.go`:
```go
package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/homeend/gigagit/internal/model"
)

func TestCommitIdentOfCapturesTagsAndCount(t *testing.T) {
	c := model.Commit{Refs: []model.Ref{
		{Name: "main", Kind: model.RefLocal, Head: true},
		{Name: "branch1", Kind: model.RefLocal},
		{Name: "branch2", Kind: model.RefLocal},
		{Name: "v1.0.0", Kind: model.RefTag},
	}}
	id := commitIdentOf(c, nil)
	if id.name != "main" || !id.tip {
		t.Fatalf("primary should be HEAD branch main: %+v", id)
	}
	if id.count != 3 {
		t.Fatalf("count = %d, want 3 (main+branch1+branch2)", id.count)
	}
	if len(id.extra) != 2 { // branch1, branch2
		t.Fatalf("extra = %v, want 2 branches", id.extra)
	}
	if len(id.tags) != 1 || id.tags[0] != "v1.0.0" {
		t.Fatalf("tags = %v, want [v1.0.0]", id.tags)
	}
}

func TestMarkerFieldArrangement(t *testing.T) {
	// The 3 cells are [marker1][marker2-or-badge][separator]. The badge fills the
	// FILLER cell (cell 2) so a single local tip with count>=2 reads "■³ " (NOT
	// "■ ³" — badge must not displace the separator or glue to the name).
	cases := []struct {
		id   commitIdent
		want string
	}{
		{commitIdent{tip: true, count: 1}, markerLocal + "  "},                       // "■  " single tip, no badge
		{commitIdent{tip: true, count: 3}, markerLocal + "³ "},                        // "■³ " multi tip, badge in cell 2
		{commitIdent{tip: true, remoteTip: true, count: 3}, markerLocal + markerRemote + " "}, // "■▲ " both tips → badge DROPPED (no room)
		{commitIdent{remoteTip: true, count: 0}, markerRemote + "  "},                 // "▲  " remote only
		{commitIdent{count: 0}, "   "},                                               // "   " lineage
	}
	for _, tc := range cases {
		mf := tc.id.markerField()
		if mf != tc.want {
			t.Errorf("markerField(%+v) = %q, want %q", tc.id, mf, tc.want)
		}
		if w := lipgloss.Width(mf); w != commitMarkerW {
			t.Errorf("markerField(%+v) width %d, want %d", tc.id, w, commitMarkerW)
		}
	}
}
```

- [ ] **Step 2: Run, verify fail** — `go test ./internal/tui/ -run "CommitIdentOfCaptures|MarkerField"` → undefined fields/method.

- [ ] **Step 3: Implement**

In `commitIdent` (commit_ident.go:71):
```go
	tags  []string // tag names at this commit (RefTag), rendered in the deco group
	count int      // number of local-branch tips at this commit (for the count badge)
```
In `commitIdentOf`, collect tags and set count. After the existing `locals`/`remoteTipName` loop add a tag collection, and on both the no-locals and has-locals return paths set `tags`/`count`:
```go
	var tags []string
	for _, r := range c.Refs {
		if r.Kind == model.RefTag {
			tags = append(tags, r.Name)
		}
	}
```
- no-locals branch: `return commitIdent{name: remoteTipName, remoteTip: true, tags: tags}` and `return commitIdent{name: c.Source, tags: tags}` (count 0).
- has-locals branch: set `id.tags = tags` and `id.count = len(locals)` before returning.

Add the count-badge renderer + a `countBadge` helper:
```go
// countBadge is the superscript count shown on a multi-tip marker (≥2 local
// tips): ²–⁹, or ⁺ for ≥10. Empty for <2. Always one display cell when present.
func countBadge(n int) string {
	if n < 2 {
		return ""
	}
	const sup = "⁰¹²³⁴⁵⁶⁷⁸⁹"
	supRunes := []rune(sup)
	if n >= 10 {
		return "⁺"
	}
	return string(supRunes[n])
}

// markerField is the fixed 3-cell marker area, laid out as
// [marker1][marker2-or-badge][separator]. The count badge (≥2 local tips) fills
// the FILLER cell next to a lone ■ so it reads "■³ "; when BOTH a local and a
// remote marker are present there is no room, so the badge is dropped (the count
// still shows via the decoration group / (+N)). Always exactly commitMarkerW (3)
// display cells.
func (id commitIdent) markerField() string {
	badge := countBadge(id.count) // "" when <2
	switch {
	case id.tip && id.remoteTip:
		return markerLocal + markerRemote + " " // "■▲ " — no room for the badge
	case id.tip:
		if badge == "" {
			return markerLocal + "  " // "■  "
		}
		return markerLocal + badge + " " // "■³ " — badge in the filler cell
	case id.remoteTip:
		return markerRemote + "  " // "▲  "
	default:
		return "   " // lineage row
	}
}
```
Change `token` and `fullToken` to use `markerField()` instead of `markers() + " "`:
```go
func (id commitIdent) token(w int) (text string, trimmed bool) {
	name := id.label()
	var body string
	if lipgloss.Width(name) > w {
		body, trimmed = truncate(name, w), true
	} else {
		body = padRight(name, w)
	}
	return id.markerField() + body, trimmed
}

func (id commitIdent) fullToken(w int) string {
	return id.markerField() + padRight(id.label(), w)
}
```
(The `markerField()` is 3 cells = the old `markers()`(2) + " "(1), so `identStart += commitMarkerW` in view.go still points at the name column. Verify `commitMarkerW == 3` still holds.)

- [ ] **Step 4: Run, verify pass; whole package** — `go test ./internal/tui/ -run "CommitIdent|MarkerField" && go test ./internal/tui/`. Existing token/marker tests may need updating if they assumed `markers() + " "`; update them to `markerField()` (single-tip `markerField()` == old `markers() + " "`, so most should pass unchanged).

- [ ] **Step 5: Commit**
```bash
git add internal/tui/commit_ident.go internal/tui/commit_ident_decorations_test.go
git commit -m "feat(tui): commitIdent captures tags + tip count; ■N count badge in marker field"
```

---

### Task 2: pure `commitDecoGroup` helper (string + tag spans + collapse)

**Files:**
- Modify: `internal/tui/commit_ident.go` (add `decoSpan` type + `commitDecoGroup`)
- Test: `internal/tui/commit_deco_group_test.go`

**Interfaces:**
- Produces:
```go
type decoSpan struct{ Offset, Length int } // rune offsets within the group string

// commitDecoGroup renders the before-subject decoration group for an identity:
// " (branch1, branch2, ⊙v1.0.0)" with extra branches first then tags. Returns the
// group string (with a single leading space, "" when there are no extras/tags),
// the rune spans of each ⊙tag label (relative to the group string start) for the
// decorator to color yellow, and whether it collapsed to " (+N)". budget < 0
// means never collapse (full mode). When the natural group width exceeds budget
// it collapses to " (+N)" (N = extras+tags) and tagSpans is nil.
func commitDecoGroup(id commitIdent, budget int) (group string, tagSpans []decoSpan, collapsed bool)
```

- [ ] **Step 1: Failing tests**

`internal/tui/commit_deco_group_test.go`:
```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestDecoGroupFull(t *testing.T) {
	id := commitIdent{extra: []string{"branch1", "branch2"}, tags: []string{"v1.0.0"}}
	group, spans, collapsed := commitDecoGroup(id, -1)
	if collapsed {
		t.Fatal("budget<0 must never collapse")
	}
	want := " (branch1, branch2, " + tagGlyph + "v1.0.0)"
	if group != want {
		t.Fatalf("group=%q want %q", group, want)
	}
	if len(spans) != 1 {
		t.Fatalf("want 1 tag span, got %d", len(spans))
	}
	// the span must land exactly on "⊙v1.0.0"
	gr := []rune(group)
	got := string(gr[spans[0].Offset : spans[0].Offset+spans[0].Length])
	if got != tagGlyph+"v1.0.0" {
		t.Fatalf("span text=%q want %q", got, tagGlyph+"v1.0.0")
	}
}

func TestDecoGroupEmpty(t *testing.T) {
	if g, _, _ := commitDecoGroup(commitIdent{name: "main", tip: true}, -1); g != "" {
		t.Fatalf("no extras/tags must yield empty group, got %q", g)
	}
}

func TestDecoGroupCollapses(t *testing.T) {
	id := commitIdent{extra: []string{"feature-a", "feature-b"}, tags: []string{"release-2026"}}
	group, spans, collapsed := commitDecoGroup(id, 8) // tiny budget
	if !collapsed {
		t.Fatalf("over-budget group must collapse, got %q", group)
	}
	if group != " (+3)" {
		t.Fatalf("collapsed group=%q want \" (+3)\"", group)
	}
	if spans != nil {
		t.Fatal("collapsed group has no tag spans")
	}
	if lipgloss.Width(group) > 8 {
		// (+N) itself must always fit; it is tiny
		t.Logf("note: (+N) width %d", lipgloss.Width(group))
	}
}

func TestDecoGroupTagsOnlyOnLineage(t *testing.T) {
	// a tag on a non-tip commit (no extras): still renders
	id := commitIdent{name: "main", tags: []string{"v9"}} // tip=false
	g, spans, _ := commitDecoGroup(id, -1)
	if !strings.Contains(g, tagGlyph+"v9") || len(spans) != 1 {
		t.Fatalf("tag must render on a lineage row: %q spans=%v", g, spans)
	}
}
```

- [ ] **Step 2: Run, verify fail** — undefined `commitDecoGroup`/`tagGlyph`/`decoSpan`.

- [ ] **Step 3: Implement** (in commit_ident.go)
```go
const tagGlyph = "⊙" // precedes a tag name in the commit-row decoration group

type decoSpan struct{ Offset, Length int }

func commitDecoGroup(id commitIdent, budget int) (string, []decoSpan, bool) {
	n := len(id.extra) + len(id.tags)
	if n == 0 {
		return "", nil, false
	}
	// Build the full group and record tag spans (rune offsets within the string).
	var b strings.Builder
	b.WriteString(" (")
	var spans []decoSpan
	first := true
	write := func(s string, isTag bool) {
		if !first {
			b.WriteString(", ")
		}
		first = false
		if isTag {
			start := len([]rune(b.String()))
			lbl := tagGlyph + s
			b.WriteString(lbl)
			spans = append(spans, decoSpan{Offset: start, Length: len([]rune(lbl))})
		} else {
			b.WriteString(s)
		}
	}
	for _, br := range id.extra {
		write(br, false)
	}
	for _, tg := range id.tags {
		write(tg, true)
	}
	b.WriteString(")")
	full := b.String()
	if budget < 0 || lipgloss.Width(full) <= budget {
		return full, spans, false
	}
	return fmt.Sprintf(" (+%d)", n), nil, true
}
```
(`fmt` is already imported in commit_ident.go.)

- [ ] **Step 4: Run, verify pass; package** — `go test ./internal/tui/ -run TestDecoGroup && go test ./internal/tui/`.

- [ ] **Step 5: Commit**
```bash
git add internal/tui/commit_ident.go internal/tui/commit_deco_group_test.go
git commit -m "feat(tui): pure commitDecoGroup — deco-group string + tag spans + (+N) collapse"
```

---

### Task 3: wire group into the row (view.go) + width budget + haystack

**Files:**
- Modify: `internal/tui/view.go` (`commitBody` threads width; `commitIdentRowAt` uses the group; `commitGroupBudget` helper; `commitHaystackAt` adds tags; `commitTextRevealAt` uses the group instead of pills)
- Test: `internal/tui/commit_deco_row_test.go`

**Interfaces:**
- `commitBody(boxW, boxH int)` (was `commitBody(boxH int)`) — call sites at view.go:412, 417, 445 pass `g.w` / `g.rightW` respectively.
- `commitIdentRowAt(i, w int, full bool, budget int)` — `budget` is the group width budget (-1 = unlimited; used by `commitIdentRows`/full paths).
- Produces `func (m Model) commitGroupBudget(boxW, identW int) int`.

- [ ] **Step 1: Failing tests**

`internal/tui/commit_deco_row_test.go` — build a Model with commits carrying multi-branch + tag refs (mirror how other view tests construct `m.commits` + call a row builder; grep existing `commitIdentRowAt`/`commitRows` tests). Assert:
```go
// group renders BEFORE the subject, after the identity column, for a multi-tip+tag commit.
func TestCommitRowHasDecoGroupBeforeSubject(t *testing.T) {
	// m with one commit: refs main(HEAD)+branch1 + tag v1, subject "Hello".
	// row := m.commitIdentRowAt(idx, w, false, -1)
	// assert strings.Contains(row, "(branch1, ⊙v1)") AND that "(branch1" appears
	// before "Hello" (strings.Index ordering).
}
// lineage row (no extra refs, no tags) is unchanged: no "(".
func TestCommitRowLineageUnchanged(t *testing.T) { /* row has no "(" group */ }
// haystack includes tag names.
func TestCommitHaystackIncludesTags(t *testing.T) {
	// m.commitHaystackAt(idx) contains "v1"
}
```
Follow the existing view-test construction patterns for the Model + commits.

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement**

In `commitIdentRowAt` (view.go:1128) replace the `row := tok + " " + id.pills() + c.Subject` line:
```go
	group, _, _ := commitDecoGroup(id, budget)
	row := tok + group + " " + c.Subject
```
(Note: the group already carries its own leading space `" (…)"`, and a single space separates it from the subject. For lineage rows `group == ""`, giving `tok + " " + subject` — unchanged from today minus pills.) Update the signature to `commitIdentRowAt(i, w int, full bool, budget int)` and propagate.

In `commitBody` change signature to `commitBody(boxW, boxH int)`; compute the budget once and pass it:
```go
	identW := m.commitIdentWidth()
	budget := m.commitGroupBudget(boxW, identW)
	// ... commitIdentRowAt(i, identW, false, budget) ...
	// ... m.commitDecoratorsRange(rows, idx, start, end, budget) ... (Task 4 adds the param)
```
For Task 3, `commitDecoratorsRange` does not yet take budget — leave its call as-is; Task 4 threads it. (So in Task 3, pass `budget` only to `commitIdentRowAt`.)

Add the budget helper:
```go
// commitGroupBudget is the max display width the before-subject decoration group
// may occupy before collapsing to (+N). It reserves the fixed left columns (the
// 2-col selection prefix renderPanel prepends, the list/graph prefix, the 3-cell
// marker, the identity column, and the separating space) plus a minimum subject
// width out of the panel content width (boxW minus the 2 border columns).
func (m Model) commitGroupBudget(boxW, identW int) int {
	const minSubjectW = 12
	content := boxW - 2 // renderPanel borders
	prefix := 2         // selection prefix renderPanel prepends
	if m.commitListMode {
		prefix += 2
	} else if !m.commitListMode && m.commitGraphOn() && len(m.commitGraphRows) == m.commitsTotal() {
		prefix += m.graphCols()*2 + 1
	}
	budget := content - prefix - commitMarkerW - identW - 1 - minSubjectW
	if budget < 0 {
		budget = 0
	}
	return budget
}
```
(Verify the prefix math against `commitDecoratorsRange`'s `identStart` computation at view.go:1214-1220 — they must agree. A budget of 0 collapses everything to `(+N)`, which is correct for an extremely narrow panel.)

Update `commitIdentRows` (view.go:1115) to pass `budget = -1` (these all-rows builders feed reveals/measurement, not the width-budgeted display): `m.commitIdentRowAt(i, w, full, -1)`.

In `commitHaystackAt` (view.go ~1255+), append tag names to the haystack so `/`+`@` find tags. (Read the function; add `id.tags` joined.)

In `commitTextRevealAt` (view.go:1071) replace the `id.pills()` usage with the full group (`commitDecoGroup(id, -1)` string) so the reveal tooltip shows the same decorations untrimmed.

REMOVE the now-unused `pills()` method (commit_ident.go:152) and any test referencing it (grep `pills(`), OR keep it only if still referenced — prefer removing dead code.

- [ ] **Step 4: Run, verify pass; build + package** — `go test ./internal/tui/ -run "CommitRow|CommitHaystack|DecoGroup|CommitIdent" && go test ./internal/tui/ && go build ./cmd/gg`.

- [ ] **Step 5: Commit**
```bash
git add internal/tui/view.go internal/tui/commit_ident.go internal/tui/commit_deco_row_test.go
git commit -m "feat(tui): render deco group before subject; width budget + (+N) collapse; tags in haystack"
```

---

### Task 4: color the tags (decorator extension)

**Files:**
- Modify: `internal/tui/commit_ident.go` (`commitLineDecorator` accepts color spans; `tagDecoStyle`)
- Modify: `internal/tui/view.go` (`commitDecoratorsRange` computes group tag spans and passes them; signature gains `budget`)
- Test: `internal/tui/commit_deco_color_test.go`

**Interfaces:**
- `commitLineDecorator(hasDot bool, dotCol int, dotColor lipgloss.Color, dim bool, identStart, identLen int, colorSpans []coloredSpan)` — new trailing param: absolute-column spans to recolor.
- `type coloredSpan struct{ Start, Length int; Style lipgloss.Style }`.

- [ ] **Step 1: Failing test**

`internal/tui/commit_deco_color_test.go` — force a color profile (mirror `commit_color_test.go`: `lipgloss.SetColorProfile(termenv.TrueColor)` with cleanup).

CRITICAL: the test MUST go end-to-end through the real column math — build a Model, call `m.commitBody(boxW, boxH)`, take the returned row + its decorator, and apply the decorator the way `renderPanel` does (decorator(visibleLine, hscroll=0, visualLine=0)). Do NOT hand-feed a span to `commitLineDecorator` directly — that would pass even if `groupBase` is off by one, which is the whole-feature failure mode. Assert the yellow fg-220 SGR wraps EXACTLY the `⊙tag` substring: the run immediately before `⊙` and immediately after the tag name are NOT yellow. Then add a **graph-mode variant** (set the model into graph mode so the prefix is `graphCols()*2+1`) and assert the same — graph-mode is where the column math most likely drifts.
```go
func TestCommitRowTagColoredEndToEnd(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	// m with one commit: refs main(HEAD) + tag "v1", subject "Hello".
	// rows, idx, decos := m.commitBody(boxW, boxH)
	// pick the row for the commit; decorated := decos[n](rows[n], 0, 0)
	// const yellow = "\x1b[38;5;220m"
	// assert decorated contains yellow immediately wrapping "⊙v1" and that the
	// "(" before it and ")" after are NOT inside a yellow span (split on yellow
	// SGR and verify the ⊙v1 token is the styled fragment).
	// Then repeat with m forced into graph mode and assert the same.
}
func TestCommitRowLineageStillDimmedNoWidthChange(t *testing.T) {
	// a lineage row's identity is still gray (dimIdentStyle) and the visible
	// width (ansi.Strip) is unchanged by the new coloring.
}
```
Investigate how existing decorator/color tests invoke a `rowDecorator` and build `commitBody` (grep `commitLineDecorator(`, `commit_color_test.go`, and `commitBody(`) and mirror that so the test exercises the real pipeline. Pick a `boxW` wide enough that the group does NOT collapse (so tag spans exist).

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement**

In `commit_ident.go`:
```go
var tagDecoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220")) // yellow tag labels

type coloredSpan struct {
	Start, Length int          // absolute content columns (pre-hscroll, like identStart)
	Style         lipgloss.Style
}
```
Extend `commitLineDecorator` to take `colorSpans []coloredSpan` and, in the single rune loop, when the current content `col` falls inside a span, emit that span's runes with its style (same range-walk technique as the dim branch). Keep dim + dot handling. Precedence: keep it simple — spans don't overlap the identity column (the group is after it) so order doesn't matter; handle spans with the same range-coalescing pattern as `dim`.

In `view.go` `commitDecoratorsRange`:
- add `budget int` param; update the `commitBody` call (line ~1053 / 1041) to pass the same `budget` computed in Task 3.
- after computing `id` for the row, compute the group + spans and convert to absolute columns:
```go
	group, tagSpans, _ := commitDecoGroup(id, budget)
	var colorSpans []coloredSpan
	if len(tagSpans) > 0 {
		groupBase := identStart + identW // group starts right after the name column...
		// ...but the row is tok + group + " " + subject, and tok = markerField + name(identW),
		// markerField is commitMarkerW and identStart already includes prefix+commitMarkerW,
		// so the name column occupies [identStart, identStart+identW) and the group string
		// (which begins with its own leading space "(") starts at identStart+identW.
		for _, s := range tagSpans {
			colorSpans = append(colorSpans, coloredSpan{Start: groupBase + s.Offset, Length: s.Length, Style: tagDecoStyle})
		}
	}
```
- pass `colorSpans` to `commitLineDecorator(...)`. Ensure the decorator is created even when only colorSpans exist (currently it early-`continue`s when `!dim && !hasDot`; change to also proceed when `len(colorSpans) > 0`).

CRITICAL alignment check: `commitDecoGroup` must be called with the SAME `budget` in both `commitIdentRowAt` (Task 3) and here, so the string and the spans match. Both now receive the budget computed once in `commitBody`. Add a test or assertion comment noting this invariant.

- [ ] **Step 4: Run, verify pass; build + race** — `go test ./internal/tui/ -run "CommitRowTag|Commit" && go test ./internal/tui/ && go build ./cmd/gg && go vet ./internal/tui/`.

- [ ] **Step 5: Commit**
```bash
git add internal/tui/commit_ident.go internal/tui/view.go internal/tui/commit_deco_color_test.go
git commit -m "feat(tui): color deco-group tags yellow via single-pass commitLineDecorator"
```

---

### Task 5: docs + memory

**Files:** `CHANGELOG.md`, `README.md`, `CLAUDE.md`; memory.

- [ ] **Step 1: CHANGELOG** — Commits panel now shows tags (yellow `⊙`) and all branch tips at a commit as a `git log --decorate`-style group before the subject, with a `■N` count badge and `(+N)` collapse when narrow.
- [ ] **Step 2: README** — in the Commits panel description, document the decoration group, the count badge, the tag glyph/color, and the `(+N)` collapse.
- [ ] **Step 3: CLAUDE.md** — in the `tui` row, update the Commits-panel rendering description: marker `markerField` count badge, `commitDecoGroup` (shared by row builder + decorator), tags rendered/colored, `commitGroupBudget`/`(+N)`.
- [ ] **Step 4: memory** — add `commit-ref-decorations-feature.md` (type project): deco group before subject (parens), `commitDecoGroup` is the single source both `commitIdentRowAt` and `commitDecoratorsRange` call (with the SAME budget — the sync invariant); `■N` superscript badge in the 3-cell marker field; tags yellow `⊙`; `(+N)` collapse via `commitGroupBudget`; pills removed. Link `[[local-remote-tip-markers-feature]]`, `[[commit-branch-column-feature]]`. Add a `MEMORY.md` index line.
- [ ] **Step 5: Build + test** — `go build ./cmd/gg && ./test.sh unit`.
- [ ] **Step 6: Commit**
```bash
git add CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: Commits-panel ref decorations (tags + multi-tip group)"
```

---

## Self-review notes

- **The sync invariant** is the highest risk: `commitDecoGroup` must be called with identical `(id, budget)` by both the row builder (Task 3) and the decorator builder (Task 4). The budget is computed ONCE in `commitBody` and passed to both. Any divergence colors the wrong columns. Task 4's test must exercise the real decorator over the real row to catch drift.
- **Width invariants:** marker field stays 3 cells (Task 1 test); group coloring adds no cells (decorator only restyles; Task 4 width assertion).
- **Lineage rows unchanged:** `commitDecoGroup` returns `""` with no extras/tags; the row reduces to `tok + " " + subject`.
- **Budget prefix math** must match `commitDecoratorsRange`'s `identStart` (list `+2`, graph `+graphCols*2+1`). Cross-check in Task 3.
- **Dead code:** remove `pills()` once unused.
- **No `internal/git` import**; all logic is in `internal/tui` over `model` types already present.
- **Count-semantics mismatch (accepted, by design):** the `■N` badge counts local branch tips *including* the primary in the identity column; `(+N)` counts the *group members* (extra branches + tags). They coincide in the canonical mock but diverge generally (e.g. `main`+`branch1`+`branch2`, no tag → `■³ … (+2)` when collapsed). Both are correct for what they measure; do not "fix" one to match the other.
- **Wrap mode + `(+N)`:** the collapse decision keys off panel width even though wrap mode could fit the full group on a continuation line. Accepted for v1 (one-line known-limitation note in docs).
