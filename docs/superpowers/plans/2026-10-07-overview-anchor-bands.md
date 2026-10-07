# Overview Anchor Bands Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (this repo forbids implementer subagents — CLAUDE.md "NO IMPLEMENTER SUB AGENTS") to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A file opened from an overview anchor draws every line/range anchor that overview has in it — the opened one in one colour, the rest in another — and `n` / `p` walk them, in the TUI and in gg web.

**Architecture:** Bands are derived at draw time from the overview the file was opened from (`openFile.from` / web `view.from`), filtered to the file's path; a document remembers only which anchor is current (`anchorCur`, a destination). Pure helpers (`anchorBands`, `stepBand`) do the geometry in both frontends; the TUI paints through `previewRowMark` + the existing 2-column note gutter, the web through `vanchor` line classes. Two new theme roles colour the TUI bands.

**Tech Stack:** Go 1.26, Bubble Tea / lipgloss (TUI), vanilla JS + CSS (web, tested from Go under node), TOML i18n bundles.

**Spec:** `docs/superpowers/specs/2026-10-07-overview-anchor-bands-design.md`

## Global Constraints

- Work only in `/work/gigagit/.claude/worktrees/overview-anchor-bands` (branch `feat/overview-anchor-bands`); `cd <abs>` / `git -C <abs>` on every command — the shell cwd resets.
- Line and range anchors (`path:N`, `path:N-M`) are bands; note anchors (`note:t7`) are NOT (`}` / `{` keep them).
- `n` / `p` walk in LINE order and WRAP inside the file; they never leave it.
- The current anchor is a persistent BAND, not a selection: an anchor open no longer sets `lsel` / `view.range`. A steer landing (`gg session files focus <id>:<a>-<b>`) still selects.
- Every user-visible TUI string goes through `i18n.T` with a literal key present in ja/ko/zh/ru (`internal/i18n/lang/*.toml`). Web and engine prose stay English.
- New theme roles: `anchor_current_bg`, `anchor_bg`; `Terminal` leaves them "" (the gutter carries the bands there).
- Gutter marks: `┃` current band, `╎` other band, `│` under a note (precedence in that order).
- The web viewer ALWAYS claims `n` / `p` (inert without bands).
- Tests call `t.Parallel()`; TDD; `./test.sh race` must end in "all green" before merge (a `| tail` exit code lies).
- Commit with `gg add <paths>` then `git -C <wt> commit -F <msgfile>` (gg commit has no -F); message ends with the two attribution lines from the session. Never `git add -A` (a built `bin/gg` may sit in the tree).

## Review Focus

1. **The overview closed while its file is on screen** — the bands must vanish at once (no stale bands, no `n` landing on dead anchors). Pinned in Task 4 (`TestBandsGoWhenTheOverviewCloses`) and Task 7 (web `closed` list).
2. **`gg session overview set` while the file is open** — bands follow the new anchors; a current anchor whose destination left makes every band "other" and `n` steps from the cursor. Pinned in Task 4 (`TestBandsFollowAnOverviewSet`).
3. **An anchor past the file's end** (the file shrank, or the agent miscounted) — dropped or clamped, never a panic or an empty band. Pinned in Task 3 (`TestAnchorBandsDropAndClamp`).
4. **Several anchors on the SAME range, and overlapping ranges** — one band per range; stepping never stalls on a duplicate. Pinned in Task 3 (`TestAnchorBandsDedupeAndOrder`, `TestStepBandOverlap`).
5. **A file opened from a NOTE anchor** — it has a way back, so it shows the overview's bands, none current; `n` starts from the cursor. Pinned in Task 6 (`TestNoteAnchorOpenShowsBandsNoneCurrent`).

---

### Task 1: An anchor carries its label (agentdocs + web wire)

The status line says `anchor 2/5 in this file · <label>`; the label is the link text, which only the parser knows.

**Files:**
- Modify: `internal/agentdocs/overviews.go` (`Anchor` struct ~line 32; `ParseOverview` ~line 147)
- Modify: `internal/web/overview_http.go` (`overviewAnchor`, the loop building `wa`)
- Test: `internal/agentdocs/overviews_test.go` (add), `internal/web/agentdocs_overview_web_test.go` (add)

**Interfaces:**
- Produces: `agentdocs.Anchor.Label string` (link text, markup dropped, `flat(n.In)`); wire field `label` on `/api/overview` anchors.

- [ ] **Step 1: Write the failing tests**

In `internal/agentdocs/overviews_test.go`:

```go
func TestParseOverviewKeepsTheLabel(t *testing.T) {
	t.Parallel()
	_, as := ParseOverview("see [the **lock** order](a.go:5-8) and [x](note:t3)")
	if len(as) != 2 || as[0].Label != "the lock order" || as[1].Label != "x" {
		t.Fatalf("anchors = %+v", as)
	}
}
```

In `internal/web/agentdocs_overview_web_test.go`, beside the existing `body.Anchors[0].Ref` assertion (line ~105), add:

```go
	if body.Anchors[0].Label == "" {
		t.Fatalf("anchor label missing: %+v", body.Anchors[0])
	}
```

(the decoded struct there must gain `Label string \`json:"label"\`` if it is a local type — check the test's decode target and add the field.)

- [ ] **Step 2: Run them to see them fail**

Run: `cd /work/gigagit/.claude/worktrees/overview-anchor-bands && go test ./internal/agentdocs/ -run TestParseOverviewKeepsTheLabel ./internal/web/ -run Overview`
Expected: FAIL — `as[0].Label undefined` (compile error).

- [ ] **Step 3: Implement**

`overviews.go`, in `Anchor`:

```go
	Note    string
	Label   string // the link text, markup dropped (ParseOverview)
	Missing bool   // its file or note was not found when last checked
```

In `ParseOverview`, after `a.Dest = n.URL`:

```go
				a.Label = flat(n.In)
```

`overview_http.go`: add `Label string \`json:"label,omitempty"\`` to `overviewAnchor` and `Label: a.Label,` to the `wa` literal.

- [ ] **Step 4: Run the packages**

Run: `go test ./internal/agentdocs/ ./internal/web/ 2>&1 | tail -20`
Expected: PASS. If a test compares whole `Anchor` values (`reflect.DeepEqual` / `!=`), add the label to its expected value rather than weakening the comparison.

- [ ] **Step 5: Commit**

```bash
gg add internal/agentdocs internal/web/overview_http.go internal/web/agentdocs_overview_web_test.go
git -C /work/gigagit/.claude/worktrees/overview-anchor-bands commit -F <msgfile>   # "feat(agentdocs): an overview anchor keeps its link text"
```

---

### Task 2: Two theme roles and their TUI styles

**Files:**
- Modify: `internal/theme/theme.go` (fields after `BlameRecentBg`; `roles()`; `Dark`; `Light`)
- Modify: `internal/theme/override.go` (`Override` fields; two `roleFields` rows after `blame_recent_bg`)
- Modify: `internal/tui/styles.go` (two fields + build lines beside `blameRecentBg`)
- Test: `internal/theme/override_test.go` (`TestRoleFieldsCoverEveryRole` already fails until rows exist), `internal/tui/anchor_bands_test.go` (new)

**Interfaces:**
- Produces: `theme.Theme.AnchorBg`, `theme.Theme.AnchorCurrentBg`; `styles.anchorBand`, `styles.anchorBandCur lipgloss.Style` (zero style when the role is "").

- [ ] **Step 1: Write the failing test** — `internal/tui/anchor_bands_test.go`:

```go
package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/theme"
)

func TestAnchorBandStylesFollowTheTheme(t *testing.T) {
	t.Parallel()
	s := buildStylesFor(theme.Dark)
	if s.anchorBand.GetBackground() == nil || s.anchorBandCur.GetBackground() == nil {
		t.Fatal("dark theme: anchor bands have no background")
	}
	s = buildStylesFor(theme.Terminal)
	if _, isNo := s.anchorBand.GetBackground().(lipgloss.NoColor); !isNo {
		t.Fatal("terminal theme: an anchor band paints a background")
	}
}
```

Before writing it, read `styles.go` for the real builder name (the function that turns a `theme.Theme` into `*styles`; the snippet above calls it `buildStylesFor` — use the real one) and import `lipgloss` if needed.

- [ ] **Step 2: Run** — `go test ./internal/theme/ ./internal/tui/ -run 'RoleFields|AnchorBandStyles'` → FAIL (fields undefined).

- [ ] **Step 3: Implement**

`theme.go`, after `BlameRecentBg string`:

```go
	// AnchorCurrentBg / AnchorBg are the backgrounds of an overview's
	// anchors in a file it opened: the anchor the reader is on, and the
	// others. "" (the Terminal theme) means none — the gutter marks them.
	AnchorCurrentBg, AnchorBg string
```

`roles()`: append `t.AnchorCurrentBg, t.AnchorBg,` after `t.BlameRecentBg,`.
`Dark`: `AnchorCurrentBg: "#4A3F12", AnchorBg: "#2A2614",`. `Light`: `AnchorCurrentBg: "#F3E2A6", AnchorBg: "#F7F0D6",`.

`override.go` `Override`, after `BlameRecentBg`:

```go
	AnchorCurrentBg string `toml:"anchor_current_bg"`
	AnchorBg        string `toml:"anchor_bg"`
```

`roleFields`, after the `blame_recent_bg` row:

```go
	{"anchor_current_bg", "background of the overview anchor you opened or stepped to (n/p) in a file an overview opened (empty = gutter mark only)", func(t *Theme) *string { return &t.AnchorCurrentBg }, func(o *Override) *string { return &o.AnchorCurrentBg }},
	{"anchor_bg", "background of the overview's other anchors in that file (empty = gutter mark only)", func(t *Theme) *string { return &t.AnchorBg }, func(o *Override) *string { return &o.AnchorBg }},
```

`styles.go`: fields `anchorBand, anchorBandCur lipgloss.Style // theme roles anchor_bg / anchor_current_bg; zero = none`; in the builder, next to `s.blameRecentBg = th.BlameRecentBg`:

```go
	s.anchorBand, s.anchorBandCur = ns(), ns()
	if th.AnchorBg != "" {
		s.anchorBand = ns().Background(lipgloss.Color(th.AnchorBg))
	}
	if th.AnchorCurrentBg != "" {
		s.anchorBandCur = ns().Background(lipgloss.Color(th.AnchorCurrentBg))
	}
```

- [ ] **Step 4: Run** — `go test ./internal/theme/ ./internal/tui/ -run 'Role|Theme|AnchorBandStyles' 2>&1 | tail` → PASS. Also `go test ./internal/config/ 2>&1 | tail` (populate/docs tests may enumerate roles; update their golden if one lists every role).

- [ ] **Step 5: Commit** — `feat(theme): anchor_bg and anchor_current_bg roles`.

---

### Task 3: Pure band geometry (TUI)

**Files:**
- Create: `internal/tui/anchor_bands.go`
- Test: `internal/tui/anchor_bands_test.go`

**Interfaces:**
- Produces:
  - `type anchorBand struct{ start, end, i int }` — 1-based lines; `i` = index of the first overview anchor with that range.
  - `func anchorBands(anchors []anchor, path string, nLines int) []anchorBand` — sorted by `(start, end)`, deduped, clipped.
  - `func bandOf(bands []anchorBand, anchors []anchor, dest string, nLines int) int` — index in `bands` of the band anchor `dest` makes (its range clamped exactly as `anchorBands` clamps it); -1 when none.
  - `func stepBand(bands []anchorBand, cur, line, dir int) (next int, wrapped bool)` — `cur` = current band index (-1 none), `line` = cursor line (1-based); -1 when there are no bands.

- [ ] **Step 1: Write the failing tests** (append to `anchor_bands_test.go`)

```go
func band(start, end, i int) anchorBand { return anchorBand{start, end, i} }

func anc(dest, path string, start, end int) anchor {
	return anchor{dest: dest, target: agentdocs.Anchor{Dest: dest, Path: path, Start: start, End: end}}
}

func TestAnchorBandsFilterAndKinds(t *testing.T) {
	t.Parallel()
	as := []anchor{
		anc("a.go", "a.go", 0, 0),           // the file: no line, no band
		anc("a.go:12", "a.go", 12, 0),       // a line: one-line band
		anc("a.go:5-8", "a.go", 5, 8),       // a range
		anc("b.go:3", "b.go", 3, 0),         // another file
		{dest: "note:t1", target: agentdocs.Anchor{Note: "t1", Path: "a.go", Start: 20, End: 20}}, // a note: never
	}
	got := anchorBands(as, "a.go", 100)
	want := []anchorBand{band(5, 8, 2), band(12, 12, 1)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestAnchorBandsDedupeAndOrder(t *testing.T) {
	t.Parallel()
	as := []anchor{anc("x:9-10", "x", 9, 10), anc("x:2", "x", 2, 0), anc("./x:9-10", "x", 9, 10), anc("x:2-4", "x", 2, 4)}
	got := anchorBands(as, "x", 50)
	want := []anchorBand{band(2, 2, 1), band(2, 4, 3), band(9, 10, 0)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestAnchorBandsDropAndClamp(t *testing.T) {
	t.Parallel()
	as := []anchor{anc("x:8-30", "x", 8, 30), anc("x:31", "x", 31, 0), anc("x:10", "x", 10, 0)}
	got := anchorBands(as, "x", 20)
	want := []anchorBand{band(8, 20, 0), band(10, 10, 2)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if anchorBands(as, "x", 0) != nil {
		t.Fatal("an empty file has bands")
	}
}

func TestBandOf(t *testing.T) {
	t.Parallel()
	as := []anchor{anc("x:9-10", "x", 9, 10), anc("x:2", "x", 2, 0), anc("./x:9-10", "x", 9, 10)}
	bs := anchorBands(as, "x", 50)
	if bandOf(bs, as, "./x:9-10", 50) != 1 || bandOf(bs, as, "x:2", 50) != 0 || bandOf(bs, as, "gone", 50) != -1 || bandOf(bs, as, "", 50) != -1 {
		t.Fatal("bandOf")
	}
	// same start, different end: the exact band, never the first sharing a start
	as = []anchor{anc("x:2", "x", 2, 0), anc("x:2-4", "x", 2, 4), anc("x:8-30", "x", 8, 30)}
	bs = anchorBands(as, "x", 20)
	if bandOf(bs, as, "x:2-4", 20) != 1 || bandOf(bs, as, "x:8-30", 20) != 2 {
		t.Fatalf("bandOf exact: %d %d", bandOf(bs, as, "x:2-4", 20), bandOf(bs, as, "x:8-30", 20))
	}
}

func TestStepBand(t *testing.T) {
	t.Parallel()
	bs := []anchorBand{band(5, 8, 0), band(12, 12, 1), band(30, 31, 2)}
	cases := []struct {
		cur, line, dir, want int
		wrapped              bool
	}{
		{1, 12, 1, 2, false},  // from the current band
		{2, 30, 1, 0, true},   // wraps forward
		{0, 6, -1, 2, true},   // wraps back from inside the current band
		{-1, 1, 1, 0, false},  // no current: from the cursor
		{-1, 40, 1, 0, true},  // past every band: wraps
		{-1, 20, -1, 1, false},
		{-1, 3, -1, 2, true},
		{1, 25, 1, 2, false},  // cursor walked off the current band: from the cursor
		{1, 25, -1, 1, false},
	}
	for _, c := range cases {
		got, w := stepBand(bs, c.cur, c.line, c.dir)
		if got != c.want || w != c.wrapped {
			t.Errorf("stepBand(cur=%d line=%d dir=%d) = %d,%v want %d,%v", c.cur, c.line, c.dir, got, w, c.want, c.wrapped)
		}
	}
	if got, w := stepBand(bs[:1], 0, 5, 1); got != 0 || w {
		t.Fatalf("one band: %d,%v want 0,false", got, w)
	}
	if got, _ := stepBand(nil, -1, 5, 1); got != -1 {
		t.Fatal("no bands")
	}
}

func TestStepBandOverlap(t *testing.T) {
	t.Parallel()
	bs := []anchorBand{band(2, 2, 0), band(2, 4, 1), band(3, 9, 2)}
	seen := []int{}
	cur := 0
	for range 3 {
		cur, _ = stepBand(bs, cur, bs[cur].start, 1)
		seen = append(seen, cur)
	}
	if !reflect.DeepEqual(seen, []int{1, 2, 0}) {
		t.Fatalf("walk = %v, want every band once", seen)
	}
}
```

Imports: `reflect`, `agentdocs`.

- [ ] **Step 2: Run** — `go test ./internal/tui/ -run 'AnchorBands|BandOf|StepBand'` → FAIL (undefined).

- [ ] **Step 3: Implement** `internal/tui/anchor_bands.go`:

```go
package tui

import "sort"

// Overview anchor bands (spec 2026-10-07-overview-anchor-bands): a file an
// overview's anchor opened draws every line / range anchor that overview
// has in it — the current one apart from the rest — and n / p walk them.

// anchorBand is one band: lines start..end (1-based) of the file, standing
// for overview anchor i (the first in document order with that range).
type anchorBand struct{ start, end, i int }

// anchorBands is the bands anchors make in path, a file of nLines lines: its
// line and range anchors (a file anchor has no line; a note anchor is the
// note's own mark), sorted by start then end, one per range, a band past the
// last line dropped and one running past it cut there.
func anchorBands(anchors []anchor, path string, nLines int) []anchorBand {
	var out []anchorBand
	seen := map[[2]int]bool{}
	for i, a := range anchors {
		t := a.target
		if t.Note != "" || t.Path != path || t.Start <= 0 || t.Start > nLines {
			continue
		}
		end := min(max(t.End, t.Start), nLines)
		k := [2]int{t.Start, end}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, anchorBand{t.Start, end, i})
	}
	sort.SliceStable(out, func(x, y int) bool {
		if out[x].start != out[y].start {
			return out[x].start < out[y].start
		}
		return out[x].end < out[y].end
	})
	return out
}

// bandOf is the index in bands of the band anchor dest makes in a file of
// nLines lines (its range clamped as anchorBands clamps it), or -1.
func bandOf(bands []anchorBand, anchors []anchor, dest string, nLines int) int {
	if dest == "" {
		return -1
	}
	for _, a := range anchors {
		t := a.target
		if a.dest != dest || t.Note != "" || t.Start <= 0 {
			continue
		}
		end := min(max(t.End, t.Start), nLines)
		for k, b := range bands {
			if b.start == t.Start && b.end == end {
				return k
			}
		}
	}
	return -1
}

// stepBand is n (dir 1) / p (-1): from band cur when the cursor line is in
// it, else from the cursor — the next band starting below it, the last
// starting above it — wrapping at the ends (wrapped says so; one band never
// "wraps"). -1 when there are none.
func stepBand(bands []anchorBand, cur, line, dir int) (int, bool) {
	n := len(bands)
	if n == 0 {
		return -1, false
	}
	if cur >= 0 && cur < n && bands[cur].start <= line && line <= bands[cur].end {
		next := cur + dir
		wrapped := next < 0 || next >= n
		return ((next % n) + n) % n, wrapped && n > 1
	}
	if dir > 0 {
		for k, b := range bands {
			if b.start > line {
				return k, false
			}
		}
		return 0, n > 1
	}
	for k := n - 1; k >= 0; k-- {
		if bands[k].start < line {
			return k, false
		}
	}
	return n - 1, n > 1
}
```

- [ ] **Step 4: Run** — `go test ./internal/tui/ -run 'AnchorBands|BandOf|StepBand' -v 2>&1 | tail -20` → PASS.

- [ ] **Step 5: Commit** — `feat(tui): pure overview anchor band geometry`.

---

### Task 4: A document's bands, its current anchor, and the open that no longer selects

**Files:**
- Modify: `internal/tui/open_file.go` (`openFile`: field `anchorCur string` beside `from`)
- Modify: `internal/tui/overview.go` (`overview`: field `closed bool`)
- Modify: `internal/tui/open_files.go` (`openFilesReg.remove`: mark a removed overview closed)
- Modify: `internal/tui/overview_keys.go` (`anchorStatted`: drop `pendingEnd`, set `anchorCur`; note branch of `openAnchor`: `anchorCur = ""`)
- Modify: `internal/tui/anchor_bands.go` (methods)
- Test: `internal/tui/overview_keys_test.go` (change one test, add four)

**Interfaces:**
- Consumes: `anchorBands`, `bandOf` (Task 3).
- Produces:
  - `func (d *openFile) bands() []anchorBand` — nil unless `d.from != nil && d.from.ov != nil && !d.from.ov.closed && docLoaded(d) && d.p.img == nil`.
  - `func (d *openFile) curBand(bands []anchorBand) int` — `bandOf(bands, d.from.ov.anchors, d.anchorCur, len(d.p.lines))`.
  - `type bandKind int` with `bandNone`, `bandOther`, `bandCurrent`; `func bandKindAt(bands []anchorBand, cur, line int) bandKind` (line 1-based; current wins on overlap).

- [ ] **Step 1: Write / change the tests** in `overview_keys_test.go`

Replace `TestEnterOnARangeSelectsTheLines` with:

```go
func TestEnterOnARangeLandsWithoutSelecting(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 2)
	f := topDoc(m)
	if f.p.cur != 4 || f.p.lsel.on {
		t.Fatalf("cur=%d lsel=%v, want line 5 and no selection", f.p.cur, f.p.lsel.on)
	}
	bs := f.bands()
	if len(bs) != 2 || f.curBand(bs) != 0 || bs[0].start != 5 || bs[0].end != 8 || bs[1].start != 12 {
		t.Fatalf("bands=%v cur=%d", bs, f.curBand(bs))
	}
}

func TestBandsGoWhenTheOverviewCloses(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 1)
	f := topDoc(m)
	m.openFiles.remove(m.currentWorktree, d)
	if f.bands() != nil {
		t.Fatal("a closed overview still bands its file")
	}
}

func TestBandsFollowAnOverviewSet(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 1) // a.txt:12 current
	f := topDoc(m)
	setOverviewText(t, m, d, "# Tour\n\n- [three](a.txt:3)\n- [nine to ten](a.txt:9-10)\n")
	m, _ = m.onAgentDocsChanged()
	bs := f.bands()
	if len(bs) != 2 || bs[0].start != 3 || bs[1].start != 9 || f.curBand(bs) != -1 {
		t.Fatalf("after set: bands=%v cur=%d", bs, f.curBand(bs))
	}
}

func TestFileOpenedWithoutAnOverviewHasNoBands(t *testing.T) {
	t.Parallel()
	m, _ := notedViewer(t)
	if d, _ := m.focusedDoc(); d.bands() != nil {
		t.Fatal("bands without an overview")
	}
}
```

`setOverviewText` — look for an existing helper that updates an overview in the store (grep `SetOverview\|UpdateOverview` in `internal/agentdocs` and the tui tests, e.g. `storeOverview`'s neighbour); if none exists, add to the test file:

```go
func setOverviewText(t *testing.T, m Model, d *openFile, text string) {
	t.Helper()
	if _, err := m.docs.SetOverview(d.id(), "Tour", text); err != nil { // use the store's real update method + signature
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run** — `go test ./internal/tui/ -run 'EnterOnARange|BandsGo|BandsFollow|WithoutAnOverviewHasNoBands'` → FAIL.

- [ ] **Step 3: Implement**

`open_file.go`, after `from *openFile`:

```go
	// anchorCur is the destination of the overview anchor this file is on —
	// the one opened, or the one n / p stepped to ("" = none): the band drawn
	// apart from the rest (anchor_bands.go).
	anchorCur string
```

`overview.go`, in `overview`: `closed bool // taken out of the open files: its files lose their bands`.

`open_files.go` `remove`, inside the loop before `if e != d`:

```go
		if e == d && d.ov != nil {
			d.ov.closed = true // a file it opened stops drawing its anchors
		}
```

`overview_keys.go` `openAnchor` note branch: after `d.pendingLine, d.pendingEnd = n.Start, 0` add `d.anchorCur = ""`. `anchorStatted`: replace

```go
	if d := topDoc(m); d != nil {
		if t.End > t.Start {
			d.pendingEnd = t.End
		}
		d.from, d.backgrounded = ov, true
	}
```

with

```go
	if d := topDoc(m); d != nil {
		// The range is drawn as the current band (anchor_bands.go), not
		// selected: the reader's own marking stays theirs.
		d.from, d.backgrounded, d.anchorCur = ov, true, msg.dest
	}
```

`anchor_bands.go`, append:

```go
// bands is the bands of the overview that opened d, while it is open; nil
// for any other document.
func (d *openFile) bands() []anchorBand {
	if d.from == nil || d.from.ov == nil || d.from.ov.closed || !docLoaded(d) || d.p.img != nil {
		return nil
	}
	return anchorBands(d.from.ov.anchors, d.path, len(d.p.lines))
}

// curBand is the index in bands of d's current anchor, or -1.
func (d *openFile) curBand(bands []anchorBand) int {
	if d.from == nil || d.from.ov == nil {
		return -1
	}
	return bandOf(bands, d.from.ov.anchors, d.anchorCur, len(d.p.lines))
}

// bandKind is how a line sits under the bands.
type bandKind int

const (
	bandNone bandKind = iota
	bandOther
	bandCurrent
)

// bandKindAt is line's kind (1-based): the current band wins an overlap.
func bandKindAt(bands []anchorBand, cur, line int) bandKind {
	k := bandNone
	for i, b := range bands {
		if b.start <= line && line <= b.end {
			if i == cur {
				return bandCurrent
			}
			k = bandOther
		}
	}
	return k
}
```

Check `len(d.p.lines)` is the file's line count for a loaded worktree document (wrapped display rows are NOT in `p.lines` — `renderPreviewBox` says lines are file lines; confirm, else count `src` lines).

- [ ] **Step 4: Run** — `go test ./internal/tui/ -run 'Overview|Anchor|Band|Backspace|Enter' 2>&1 | tail -20` → PASS (the old range test is replaced, not deleted silently — its new name is in the diff).

- [ ] **Step 5: Commit** — `feat(tui): a file keeps its overview's anchors as bands; an anchor open no longer selects`.

---

### Task 5: Paint the bands (TUI)

**Files:**
- Modify: `internal/tui/file_preview.go` (`previewRowMark`; `renderPreviewBox` gutter + mark loop)
- Modify: `internal/tui/open_file_notes.go` (`gutterW`)
- Modify: `internal/tui/binary_preview_test.go` (the three `previewRowMark` calls gain `bandNone`)
- Test: `internal/tui/anchor_bands_test.go`

**Interfaces:**
- Consumes: `bands()`, `curBand`, `bandKindAt`, `bandKind` (Task 4); `st().anchorBand`, `st().anchorBandCur` (Task 2).
- Produces: `func previewRowMark(p *contentPopup, row int, cursorOff bool, l contentLine, band bandKind) (lipgloss.Style, bool)`; `func gutterMark(k bandKind, noted bool) string`.

- [ ] **Step 1: Write the failing tests**

```go
func TestGutterMarkPrecedence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		k     bandKind
		noted bool
		want  string
	}{
		{bandCurrent, true, "┃ "}, {bandOther, true, "╎ "}, {bandNone, true, "│ "}, {bandNone, false, ""},
	}
	for _, c := range cases {
		if got := gutterMark(c.k, c.noted); got != c.want {
			t.Errorf("gutterMark(%d,%v) = %q want %q", c.k, c.noted, got, c.want)
		}
	}
}

func TestBandedFileDrawsItsGutter(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 1) // a.txt:12 current, 5-8 other
	f := topDoc(m)
	// Count, not Contains: a frame border may itself be drawn with ┃.
	withCur := strings.Count(m.View(), "┃ ")
	f.anchorCur = "" // line 12 becomes an "other" band
	v := m.View()
	if withCur-strings.Count(v, "┃ ") != 1 || strings.Count(v, "╎ ") < 5 {
		t.Fatalf("band gutter wrong (┃ diff %d):\n%s", withCur-strings.Count(v, "┃ "), v)
	}
}

func TestPreviewRowMarkLaysTheBandUnderTheCursor(t *testing.T) {
	t.Parallel()
	p := &contentPopup{lines: []contentLine{{text: "a"}, {text: "b"}}, cur: 0}
	if _, marked := previewRowMark(p, 1, false, p.lines[1], bandNone); marked {
		t.Fatal("a plain row is marked")
	}
	if _, marked := previewRowMark(p, 1, false, p.lines[1], bandOther); !marked {
		t.Fatal("a banded row is not marked")
	}
	cur, _ := previewRowMark(p, 0, false, p.lines[0], bandCurrent)
	if cur.GetBackground() != st().diffCursorRow.GetBackground() {
		t.Fatal("the cursor row lost to the band")
	}
}
```

Note: under the `Terminal` theme `anchorBand` has no background; `TestPreviewRowMarkLaysTheBandUnderTheCursor`'s second assertion only needs `marked == true` (the style may be empty). If `st()` in tests is the Terminal theme and `marked` for an empty style makes rendering differ, keep `marked` true only when the style has a background and change that assertion to check `marked == (st().anchorBand.GetBackground() != lipgloss.NoColor{})`.

- [ ] **Step 2: Run** — FAIL (signature / undefined).

- [ ] **Step 3: Implement**

`previewRowMark`:

```go
func previewRowMark(p *contentPopup, row int, cursorOff bool, l contentLine, band bandKind) (lipgloss.Style, bool) {
	var rowStyle lipgloss.Style
	if l.cells != nil {
		return rowStyle, false
	}
	marked := false
	switch band { // under the cursor and the selection: both stay readable on it
	case bandCurrent:
		rowStyle, marked = st().anchorBandCur, true
	case bandOther:
		rowStyle, marked = st().anchorBand, true
	}
	if row == p.cur && !cursorOff {
		rowStyle, marked = st().diffCursorRow, true
	}
	if p.lsel.contains(row, p.cur) {
		rowStyle, marked = st().selectionStyle(rowStyle), true
	}
	return rowStyle, marked
}
```

`gutterW` (`open_file_notes.go`):

```go
func (d *openFile) gutterW() int {
	if (len(d.notes) == 0 && d.bands() == nil) || !docLoaded(d) || d.p.img != nil {
		return 0
	}
	return noteGutterW
}
```

Also update its doc comment: the gutter carries a note's range mark and the anchor bands' marks.

`anchor_bands.go`:

```go
// gutterMark is a line's mark in the 2-column gutter: the current band,
// another band, a note's lines — in that order — or nothing.
func gutterMark(k bandKind, noted bool) string {
	switch {
	case k == bandCurrent:
		return "┃ "
	case k == bandOther:
		return "╎ "
	case noted:
		return "│ "
	}
	return ""
}
```

`renderPreviewBox`: where `notes, gut` are set, also compute bands:

```go
	var bands []anchorBand
	curB := -1
	if d := m.previewDoc(p); d != nil && d.gutterW() > 0 {
		boxH = noteBoxMaxRows(rowsCap)
		notes, gut = d.notes, d.gutterW()
		bands = d.bands()
		curB = d.curBand(bands)
		d.noteW, d.noteH = max(innerW-gut, 4), noteBoxMaxRows(rowsCap)
	}
```

In the row loop: `kind := bandKindAt(bands, curB, row+1)`; pass `kind` to `previewRowMark`; replace the note-prefix loop with:

```go
		noted := false
		for _, n := range notes {
			if n.Start <= row+1 && row+1 <= n.End {
				noted = true
				break
			}
		}
		r.prefix = gutterMark(kind, noted)
```

Fix the three `binary_preview_test.go` calls to pass `bandNone`.

- [ ] **Step 4: Run** — `go test ./internal/tui/ 2>&1 | tail -20` → PASS (whole package: the gutter change touches every noted-file test).

- [ ] **Step 5: Commit** — `feat(tui): draw an overview's anchors as bands with gutter marks`.

---

### Task 6: `n` / `p`, status, menu, hint, help (TUI) + translations

**Files:**
- Modify: `internal/tui/anchor_bands.go` (`bandKey`, `stepAnchorBand`, `bandRows`)
- Modify: `internal/tui/file_viewer.go` (`update`: call `bandKey` after `overviewKey`)
- Modify: `internal/tui/files_view.go` (~line 901: call `bandKey` after `previewNoteKey`)
- Modify: `internal/tui/overview_keys.go` (`overviewRows`: append `bandRows`)
- Modify: `internal/tui/file_preview.go` (hint: `[n/p] anchors` after `[bksp] back`)
- Modify: `internal/tui/help.go` (new `n / p` row; reword the `tab / enter` row's "a range selected")
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Test: `internal/tui/overview_keys_test.go`

**Interfaces:**
- Consumes: Tasks 3–5.
- Produces: `func (m Model) bandKey(msg tea.KeyMsg) (Model, tea.Cmd, bool)`; `func (m Model) stepAnchorBand(d *openFile, dir int) Model`; `func (m Model) bandRows(d *openFile) []actionRow`.

- [ ] **Step 1: Write the failing tests**

```go
func TestNStepsToTheNextBandAndWraps(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 2) // a.txt:5-8 current; bands 5-8, 12
	f := topDoc(m)
	m = fvKeys(t, m, keyMsg("n"))
	if f.p.cur != 11 || f.anchorCur != "a.txt:12" || d.ov.sel != 1 {
		t.Fatalf("n: cur=%d anchorCur=%q sel=%d", f.p.cur, f.anchorCur, d.ov.sel)
	}
	if !strings.Contains(m.statusMsg, "anchor 2/2 in this file · line twelve") {
		t.Fatalf("status = %q", m.statusMsg)
	}
	m = fvKeys(t, m, keyMsg("n"))
	if f.p.cur != 4 || !strings.HasSuffix(m.statusMsg, "· wrapped") {
		t.Fatalf("wrap: cur=%d status=%q", f.p.cur, m.statusMsg)
	}
}

func TestPStepsBackAndBackspaceReturnsOnIt(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 1) // a.txt:12
	m = fvKeys(t, m, keyMsg("p"))
	if f := topDoc(m); f.p.cur != 4 || f.anchorCur != "a.txt:5-8" {
		t.Fatalf("p: cur=%d cur anchor %q", f.p.cur, f.anchorCur)
	}
	m = fvKeys(t, m, keyMsg("backspace"))
	if topDoc(m) != d || d.ov.sel != 2 {
		t.Fatalf("back on sel %d, want 2 (the anchor stepped to)", d.ov.sel)
	}
}

func TestNAndPAreInertWithoutBands(t *testing.T) {
	t.Parallel()
	m, f := notedViewer(t)
	cur := f.p.cur
	m = fvKeys(t, m, keyMsg("n"))
	fvKeys(t, m, keyMsg("p"))
	if f.p.cur != cur {
		t.Fatal("n / p moved a file with no bands")
	}
}

func TestNoteAnchorOpenShowsBandsNoneCurrent(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 3) // the note on a.txt:20
	f := topDoc(m)
	bs := f.bands()
	if len(bs) != 2 || f.curBand(bs) != -1 {
		t.Fatalf("bands=%v cur=%d", bs, f.curBand(bs))
	}
	fvKeys(t, m, keyMsg("p")) // cursor on 20: the last band above is 12
	if f.p.cur != 11 {
		t.Fatalf("p from the note: cur=%d want 11", f.p.cur)
	}
}

func TestBandMenuRowsAndHint(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 1)
	ids := map[string]bool{}
	for _, r := range m.overviewRows() {
		ids[r.id] = true
	}
	if !ids["anchor-next"] || !ids["anchor-prev"] || !ids["overview-back"] {
		t.Fatalf("rows = %v", ids)
	}
	m.statusMsg = ""
	if !strings.Contains(m.View(), "[n/p] anchors") {
		t.Fatal("hint lacks [n/p] anchors")
	}
}
```

`notedViewer` returns `(Model, *openFile)` per its use at `TestBackspaceWithoutAnOriginDoesNothing` — check its real return shape and adapt.

- [ ] **Step 2: Run** — FAIL.

- [ ] **Step 3: Implement** (append to `anchor_bands.go`; add imports `tea`, `i18n`)

```go
// bandKey gives the focused document's anchor bands n / p. Declined (the
// key keeps its other meanings) when the document has none.
func (m Model) bandKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	s := msg.String()
	if s != "n" && s != "p" {
		return m, nil, false
	}
	d, ok := m.focusedDoc()
	if !ok || d.bands() == nil {
		return m, nil, false
	}
	dir := 1
	if s == "p" {
		dir = -1
	}
	return m.stepAnchorBand(d, dir), nil, true
}

// stepAnchorBand moves d to its next (dir 1) / previous band: the cursor on
// its first line, centred as an anchor open lands, the band current, and the
// overview's selection on its anchor so backspace comes back there.
func (m Model) stepAnchorBand(d *openFile, dir int) Model {
	bs := d.bands()
	k, wrapped := stepBand(bs, d.curBand(bs), d.p.cur+1, dir)
	if k < 0 {
		return m
	}
	b := bs[k]
	ov := d.from
	a := ov.ov.anchors[b.i]
	d.anchorCur = a.dest
	_, rows, _, _ := m.activePreview()
	d.pendingLine, d.pendingEnd = b.start, 0
	d.landPendingLine(rows)
	vr, _ := m.viewerGeom()
	ov.selectAnchor(b.i, vr)
	label := a.target.Label
	if label == "" {
		label = a.dest
	}
	if wrapped {
		m.statusMsg = i18n.T("anchor %d/%d in this file · %s · wrapped", k+1, len(bs), label)
	} else {
		m.statusMsg = i18n.T("anchor %d/%d in this file · %s", k+1, len(bs), label)
	}
	return m
}

// bandRows are the . menu's rows for n / p in a file with bands.
func (m Model) bandRows(d *openFile) []actionRow {
	if d.bands() == nil {
		return nil
	}
	return []actionRow{
		{id: "anchor-next", key: "n", label: i18n.T("Next anchor in this file"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.stepAnchorBand(d, 1), nil
		}},
		{id: "anchor-prev", key: "p", label: i18n.T("Previous anchor in this file"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.stepAnchorBand(d, -1), nil
		}},
	}
}
```

`overviewRows`: before the `if d.from != nil {` back row: `rows = append(rows, m.bandRows(d)...)`.

`file_viewer.go` `update`, after the `overviewKey` block:

```go
	if nm, cmd, ok := m.bandKey(msg); ok {
		return nm, cmd
	}
```

`files_view.go` ~901, after the `previewNoteKey` block, the same three lines (it must run before the review view's `case "n", "p"` at ~935).

`file_preview.go`, the `[bksp] back` lead:

```go
	if d := m.previewDoc(p); d != nil && d.from != nil {
		lead := i18n.T("[bksp] back")
		if d.bands() != nil {
			lead += "  " + i18n.T("[n/p] anchors")
		}
		hint = lead + "  " + hint // the way back to the overview leads
	}
```

`help.go`: after the `backspace` row (line ~101):

```go
		r("n / p", i18n.T("in a file an overview's anchor opened: the next / previous of that overview's line and range anchors in this file, in line order, wrapping at the ends; every one is drawn as a band, the current one in its own colour, and backspace returns to the overview on the anchor you stepped to")),
```

and in the `tab / enter` row change "the file at its line, a range selected, or the file a note sits on" → "the file at its line (that overview's anchors in it drawn as bands), or the file a note sits on" — a NEW key: remove the old key from all four bundles and add the new one.

i18n — add to each of `ja.toml`, `ko.toml`, `zh.toml`, `ru.toml` (keys verbatim; translate the values; keep `%d`/`%s` order and the bracketed key names `[n/p]` untranslated):

```
"anchor %d/%d in this file · %s"
"anchor %d/%d in this file · %s · wrapped"
"Next anchor in this file"
"Previous anchor in this file"
"[n/p] anchors"
"in a file an overview's anchor opened: the next / previous ..."   (the full help text above)
"in an agent's overview (gg session overview): ... (the reworded tab / enter text)"
```

Use the `adding-translations` skill for the bundle rules (verb agreement, `options_vocab_test`).

- [ ] **Step 4: Run** — `go test ./internal/tui/ ./internal/i18n/ 2>&1 | tail -20` → PASS, including the i18n AST gates (`i18n_scan_test.go`, `menu_labels_test.go`).

- [ ] **Step 5: Commit** — `feat(tui): n / p walk an overview's anchors in a file`.

---

### Task 7: gg web parity

**Files:**
- Modify: `internal/web/static/viewer.js` (overview model block: `anchorBands`, `bandOf`, `stepBand`, `bandKindAt`; `openAnchorAt`; `renderViewer`; `viewerKey`; `viewerAgentDocs`; `viewerFoot`; the `view file` help)
- Modify: `internal/web/static/style.css` (after `.vline.vrange.vcur`, line ~1696)
- Test: `internal/web/viewernotesjs_test.go` (new `TestViewerAnchorBandsJS`), plus a wiring test

**Interfaces:**
- Consumes: wire `label` (Task 1); the anchor wire shape `{dest, path, start, end, note, missing, ref, label}`.
- Produces (JS, pure, inside `// --- overview model …` … `// --- end overview model ---`):
  - `anchorBands(anchors, path, nLines)` → `[{start, end, i}]`
  - `bandOf(bands, anchors, dest, nLines)` → index | -1 (exact clamped range, as Go)
  - `stepBand(bands, cur, line, dir)` → `{i, wrapped}` (`i` -1 when none)
  - `bandKindAt(bands, cur, line)` → `""` | `"other"` | `"cur"`
  - `view.from` gains `anchors`, `stamp`, `cur` (dest), `closed`.

- [ ] **Step 1: Write the failing JS test** (in `viewernotesjs_test.go`)

```go
func TestViewerAnchorBandsJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", voPureStart, voPureEnd, `
const as = [
  {dest: "a.go", path: "a.go"},
  {dest: "a.go:12", path: "a.go", start: 12},
  {dest: "a.go:5-8", path: "a.go", start: 5, end: 8},
  {dest: "b.go:3", path: "b.go", start: 3},
  {dest: "note:t1", note: "t1", path: "a.go", start: 20, end: 20},
  {dest: "./a.go:5-8", path: "a.go", start: 5, end: 8},
  {dest: "a.go:40", path: "a.go", start: 40},
  {dest: "a.go:28-35", path: "a.go", start: 28, end: 35},
];
const bs = anchorBands(as, "a.go", 30);
const st = (c, l, d) => { const r = stepBand(bs, c, l, d); return r.i + (r.wrapped ? "w" : ""); };
console.log([
  JSON.stringify(bs),
  bandOf(bs, as, "a.go:12", 30), bandOf(bs, as, "./a.go:5-8", 30), bandOf(bs, as, "gone", 30), bandOf(bs, as, "", 30), bandOf(bs, as, "a.go:28-35", 30),
  st(1, 12, 1), st(2, 28, 1), st(0, 6, -1), st(-1, 1, 1), st(-1, 30, 1), st(-1, 20, -1), st(-1, 3, -1),
  stepBand([], -1, 5, 1).i,
  JSON.stringify(stepBand(bs.slice(0, 1), 0, 5, 1)),
  bandKindAt(bs, 1, 12), bandKindAt(bs, 1, 6), bandKindAt(bs, 1, 9),
].join("|"));
`)
	want := `[{"start":5,"end":8,"i":2},{"start":12,"end":12,"i":1},{"start":28,"end":30,"i":7}]|1|0|-1|-1|2|2|0w|2w|0|0w|1|2w|-1|{"i":0,"wrapped":false}|cur|other|`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}
```

- [ ] **Step 2: Run** — `go test ./internal/web/ -run TestViewerAnchorBandsJS` → FAIL (`anchorBands is not defined`).

- [ ] **Step 3: Implement the pure helpers** (inside the overview model block, before `// --- end overview model ---`), the JS twins of Task 3:

```js
// anchorBands is the bands an overview's anchors make in path (nLines
// lines): its line and range anchors — never a file's or a note's — sorted
// by start then end, one per range, past the end dropped, cut at it.
function anchorBands(anchors, path, nLines) {
  const out = [], seen = new Set();
  anchors.forEach((a, i) => {
    if (a.note || a.path !== path || !(a.start > 0) || a.start > nLines) return;
    const end = Math.min(Math.max(a.end || 0, a.start), nLines), k = a.start + ":" + end;
    if (seen.has(k)) return;
    seen.add(k);
    out.push({ start: a.start, end, i });
  });
  return out.sort((x, y) => x.start - y.start || x.end - y.end);
}

// bandOf is the index of the band anchor dest makes in nLines lines (its
// range clamped as anchorBands clamps it), or -1.
function bandOf(bands, anchors, dest, nLines) {
  if (!dest) return -1;
  for (const a of anchors) {
    if (a.dest !== dest || a.note || !(a.start > 0)) continue;
    const end = Math.min(Math.max(a.end || 0, a.start), nLines);
    const k = bands.findIndex((b) => b.start === a.start && b.end === end);
    if (k >= 0) return k;
  }
  return -1;
}

// stepBand is n (dir 1) / p (-1): from band cur when line is in it, else
// from line; wraps (one band never "wraps"). i is -1 with no bands.
function stepBand(bands, cur, line, dir) {
  const n = bands.length;
  if (!n) return { i: -1, wrapped: false };
  if (cur >= 0 && cur < n && bands[cur].start <= line && line <= bands[cur].end) {
    const next = cur + dir;
    return { i: ((next % n) + n) % n, wrapped: (next < 0 || next >= n) && n > 1 };
  }
  if (dir > 0) {
    const k = bands.findIndex((b) => b.start > line);
    return k >= 0 ? { i: k, wrapped: false } : { i: 0, wrapped: n > 1 };
  }
  for (let k = n - 1; k >= 0; k--) if (bands[k].start < line) return { i: k, wrapped: false };
  return { i: n - 1, wrapped: n > 1 };
}

// bandKindAt is line's band: "cur" (wins an overlap), "other" or "".
function bandKindAt(bands, cur, line) {
  let k = "";
  for (let i = 0; i < bands.length; i++) {
    if (bands[i].start <= line && line <= bands[i].end) {
      if (i === cur) return "cur";
      k = "other";
    }
  }
  return k;
}
```

Run the test → PASS. The Go and JS helpers must agree case for case (same fixtures in Task 3 and here).

- [ ] **Step 4: Wire the viewer**

`viewBands()` (outside the pure block, near `openAnchorAt`):

```js
// viewBands is the shown file's bands: the overview that opened it, while
// that overview is open; [] otherwise.
function viewBands() {
  const f = view.from;
  if (!f || f.closed || !f.anchors || view.ov || view.image || view.placeholder) return [];
  return anchorBands(f.anchors, view.path, view.lines.length);
}
```

`openAnchorAt`: change `const from = { id, sel: view.ov.sel, dest: a.dest };` to

```js
  const from = { id, sel: view.ov.sel, dest: a.dest, anchors: view.ov.anchors, stamp: view.ov.stamp, cur: t.note ? "" : a.dest };
```

and delete the two lines `view.range = t.end > t.line ? …` and `view.rangeOwn = false;` (the band replaces the tint). Update `openAnchorAt`'s comment ("its range tinted" → "its anchors in the file drawn as bands").

`renderViewer` — before `view.lines.forEach`: `const bands = viewBands(), curB = view.from ? bandOf(bands, view.from.anchors || [], view.from.cur, view.lines.length) : -1;` and in the row: 

```js
      const bk = bandKindAt(bands, curB, i + 1);
      const banded = bk ? " vanchor" + (bk === "cur" ? " acur" : "") : "";
```

appending `${banded}` after `${ranged}` in the class list.

`stepViewerAnchor(dir)`:

```js
// stepViewerAnchor is n / p: the next / previous band, the cursor on its
// first line centred, the band current, and the way back on its anchor.
function stepViewerAnchor(dir) {
  const bands = viewBands(), f = view.from;
  if (!bands.length) return;
  const r = stepBand(bands, bandOf(bands, f.anchors, f.cur, view.lines.length), view.cur, dir);
  const b = bands[r.i], a = f.anchors[b.i];
  f.cur = a.dest;
  f.sel = b.i;
  f.dest = a.dest; // backAnchor returns here
  view.cur = b.start;
  rerenderKeepingScroll();
  centerCursor();
  opLine("anchor " + (r.i + 1) + "/" + bands.length + " in this file · " + (a.label || a.dest) + (r.wrapped ? " · wrapped" : ""), false);
}
```

`viewerKey` switch, before `case "}"`:

```js
    case "n": stepViewerAnchor(1); break; // always ours: a viewer p is never pull
    case "p": stepViewerAnchor(-1); break;
```

`viewerAgentDocs`: after `viewerOpenFiles(files);` and before `if (!viewerFileId()) return;`:

```js
  const f = view.from;
  if (f && !f.closed && closed.includes(f.id)) {
    f.closed = true; // its anchors leave the file with it
    rerenderKeepingScroll();
    swapFoot(true);
  } else if (f && !f.closed && overviewStale(stamps, f.id, f.stamp)) {
    refreshFromAnchors(f);
  }
```

and

```js
// refreshFromAnchors re-reads the overview that opened the shown file, for
// its anchors; a failed read keeps the last ones.
async function refreshFromAnchors(f) {
  let ov;
  try {
    ov = await fetchOverview(f.id);
  } catch {
    return;
  }
  if (view.from !== f) return;
  f.anchors = ov.anchors || [];
  f.stamp = ov.stamp;
  rerenderKeepingScroll();
  swapFoot(true);
}
```

(A 404 from a closed overview lands in the `catch`; the `closed` list path above is what removes the bands.)

`viewerFoot`: `const bandsKey = view.from && viewBands().length ? "<span>n p anchors</span>" : "";` and put it right after `back`.

Help (`registerHelp` "view file"): append to the overview sentence: `"; in that file <b>n / p</b> step through the overview's line and range anchors in it (all drawn as bands, the current one brighter)"`.

`style.css` after line ~1696:

```css
.vline.vanchor { background: color-mix(in srgb, #d4a72c 10%, transparent); }
.vline.vanchor .vno { box-shadow: inset 3px 0 0 color-mix(in srgb, #d4a72c 45%, transparent); }
.vline.vanchor.acur { background: color-mix(in srgb, #d4a72c 24%, transparent); }
.vline.vanchor.acur .vno { box-shadow: inset 3px 0 0 #d4a72c; }
.vline.vanchor.vcur { background: var(--sel); }
```

(`.vnoted .vno` also uses an inset box-shadow; a noted line inside a band shows the band's — acceptable, the note box under it still marks it.)

- [ ] **Step 5: Wiring test** — add to `viewernotesjs_test.go` a source-text guard in the style of the repo's other wiring tests (grep `strings.Contains(src,` in `internal/web/*_test.go` for the precedent):

```go
func TestViewerAnchorBandsWiring(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("static/viewer.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{`case "n": stepViewerAnchor(1)`, `case "p": stepViewerAnchor(-1)`, `cur: t.note ? "" : a.dest`, "refreshFromAnchors(f)"} {
		if !strings.Contains(src, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	if strings.Contains(src, "view.range = t.end > t.line") {
		t.Error("an anchor open still sets the reader's range")
	}
}
```

- [ ] **Step 6: Run** — `go test ./internal/web/ 2>&1 | tail -20` → PASS.

- [ ] **Step 7: Manual browser check** (build `cd <wt> && go build -o bin/gg ./cmd/gg`, give the user the ABSOLUTE path `/work/gigagit/.claude/worktrees/overview-anchor-bands/bin/gg`): run `bin/gg web` in a test repo, `gg session overview add --title T` with anchors `a:3`, `a:10-12`, `a:20`; open `a:10-12` → three tinted bands, 10–12 brighter; `n` → 20 brighter, op line `anchor 3/3 in this file · …`; `n` → `· wrapped`; Back → overview on the `a:20` anchor; `p` in a file with no overview → nothing happens (no pull). Assert VISIBILITY (computed background differs between `.acur` and `.vanchor`), on the built binary (check its md5).

- [ ] **Step 8: Commit** — `feat(web): an overview's anchors as bands in the file, n / p walk them`.

---

### Task 8: Golden screen, docs, skill, race gate

**Files:**
- Create: `e2e/scenarios/tui_overview_anchor_bands.toml` (model on `e2e/scenarios/tui_reading_width.toml`; follow the `writing-e2e-scenarios` skill)
- Modify: `CHANGELOG.md`, `README.md` (~line 1472: "a range selected" → bands + `n`/`p`; the theme role list where `selection_bg` is listed), `internal/agentskill/using-gg.md` (line ~374: `(the range, selected)` → `(the range, drawn as a band; n / p walk a file's anchors)`), `internal/agentskill/agentskill.go` (`Version = 142`), `docs/CLAUDE-details.md` (overview section: bands, `anchorCur`, `overview.closed`, the dropped landing selection, the web `view.from.anchors` refresh)

- [ ] **Step 1: e2e scenario** — an overview with anchors on lines 3, 10-12, 20 of a fixture file; `[tui]` steps: open the overview, tab to the `10-12` anchor, enter, golden screen 1 (current band `┃`, others `╎`); key `n`, golden screen 2 (cursor on 20, status `anchor 3/3 in this file · …`). Generate with `go test ./e2e/ -run TestScenarios/tui_overview_anchor_bands -update`, read the golden files, confirm both marks and the status text are in them, then run without `-update` → PASS.

- [ ] **Step 2: Docs + skill** — edit the files above; run `go test ./internal/agentskill/` (the version marker test) → PASS; `./build.sh install` is for after the merge, not now.

- [ ] **Step 3: Full gate** — `cd /work/gigagit/.claude/worktrees/overview-anchor-bands && ./test.sh race 2>&1 | tee /tmp/claude-1000/-work-gigagit/bc5dbddc-991a-496c-824d-1d04a3e9a202/scratchpad/race.log; grep -c "all green" /tmp/claude-1000/-work-gigagit/bc5dbddc-991a-496c-824d-1d04a3e9a202/scratchpad/race.log` → the log must say "all green" (a non-zero count); anything else is a failure to fix.

- [ ] **Step 4: Commit** — `docs+e2e: overview anchor bands`.

- [ ] **Step 5: Final review** — one read-only review subagent (most capable model) over `main..feat/overview-anchor-bands` against the spec; fix findings; then ASK the user before `gg merge -F <msgfile> --into main feat/overview-anchor-bands`.
