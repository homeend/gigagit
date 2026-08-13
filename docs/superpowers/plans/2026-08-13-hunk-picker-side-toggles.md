# Hunk-Picker Side Toggles + Checkboxes + Output Pane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the picker's exclusive whole-side keys with tri-state toggles over one ordered line-pick model (left, right, or both sides selectable), render a GitKraken-style checkbox hierarchy, and swap the inline `result:` lines for a bottom live output pane.

**Architecture:** New pure helpers in `internal/hunkpick` (`EnsurePicks`, `SideState`, `LinePicked`, `ToggleSide`, `ToggleSideAll`, `SideStateAll`, `ResolvedLines`) translate side/master toggles into the existing `Mode=LineByLine` + ordered `Picks` representation; legacy whole-side modes are read as full picks so the web frontend and the staging picker's `SetAll(TakeCurrent)` default keep working unchanged. The TUI keys retarget onto the helpers; rendering derives checkboxes from `SideState`; an output pane windows the assembled result around the focused region.

**Tech Stack:** Go 1.26, Bubble Tea, lipgloss. Plain `go test` in `internal/hunkpick` and `internal/tui`.

**Spec:** `docs/superpowers/specs/2026-08-13-hunk-picker-side-toggles-design.md`

## Global Constraints

- Work happens in the worktree `/mnt/t/others/gigagit/.claude/worktrees/hunk-picker-side-toggles` on branch `feat/hunk-picker-side-toggles`. Prefix every Bash command with `cd /mnt/t/others/gigagit/.claude/worktrees/hunk-picker-side-toggles && ` and give Write/Edit that worktree's absolute paths.
- `internal/hunkpick`'s existing API (`Mode`, `TakeCurrent`, `TakeIncoming`, `LineByLine`, `Undecided`, `SetAll`, `ToggleLine`, `Picked`, `Resolved`, `Pending`) must remain — `internal/web` consumes it and is out of scope. `go build ./...` must stay green after every task.
- Toggle semantics: tri-state (all-of-side picked → clear that side; else complete it, appending missing lines top-to-bottom). Toggling one side never clears the other. A zero-line side never toggles and never reads as picked. Pick order = result order within a region; regions emit at document position.
- Untouched region (`Mode == Undecided`) blocks `enter` in the conflict flavor; a touched region with zero picks is decided-empty and `enter` accepts it.
- Every user-visible TUI string goes through `i18n.T` with a **literal** key present in ALL FOUR bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`); each task that adds/removes keys updates the bundles in the same task (the AST gates run with `go test ./internal/tui/`). Translation values given in a task are final — do not improve them.
- Checkbox glyphs are exactly `[x]` (all), `[~]` (some), `[ ]` (none).
- TDD: failing test → see it fail → implement → see it pass → commit, per task.

---

### Task 1: `hunkpick` pick-model helpers

**Files:**
- Modify: `internal/hunkpick/hunkpick.go`
- Test: `internal/hunkpick/hunkpick_test.go`

**Interfaces:**
- Consumes: the existing `Block`/`Doc`/`Pick`/`Mode` model (unchanged).
- Produces (later tasks call these exact signatures):
  - `func (b *Block) EnsurePicks()`
  - `func (b *Block) SideState(s Side) (all, any bool)`
  - `func (b *Block) LinePicked(s Side, line int) bool`
  - `func (b *Block) ToggleSide(s Side)`
  - `func (b *Block) ResolvedLines() ([]string, bool)`
  - `func (d *Doc) ToggleSideAll(s Side)`
  - `func (d *Doc) SideStateAll(s Side) (all, any bool)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/hunkpick/hunkpick_test.go` (the file already uses plain `package hunkpick` tests with small literal blocks — follow that style):

```go
func twoSideBlock() *Block {
	return &Block{Current: []string{"c1", "c2"}, Incoming: []string{"i1"}}
}

func TestEnsurePicksMaterializesLegacyModes(t *testing.T) {
	b := twoSideBlock()
	b.Mode = TakeCurrent
	b.EnsurePicks()
	if b.Mode != LineByLine || len(b.Picks) != 2 || !b.Picked(Current, 0) || !b.Picked(Current, 1) {
		t.Fatalf("TakeCurrent should materialize as full current picks: %+v", b.Picks)
	}
	b = twoSideBlock()
	b.Mode = TakeIncoming
	b.EnsurePicks()
	if len(b.Picks) != 1 || !b.Picked(Incoming, 0) {
		t.Fatalf("TakeIncoming should materialize as full incoming picks: %+v", b.Picks)
	}
	b = twoSideBlock() // Undecided
	b.EnsurePicks()
	if b.Mode != LineByLine || len(b.Picks) != 0 {
		t.Fatalf("Undecided should become touched-empty: mode=%v picks=%v", b.Mode, b.Picks)
	}
}

func TestToggleSideTriState(t *testing.T) {
	b := twoSideBlock()
	b.ToggleSide(Current) // complete
	if all, _ := b.SideState(Current); !all {
		t.Fatal("first toggle should pick all current lines")
	}
	b.ToggleSide(Incoming) // both on; incoming appended after current
	out, _ := b.ResolvedLines()
	if strings.Join(out, ",") != "c1,c2,i1" {
		t.Fatalf("both-on order = %v, want current then incoming", out)
	}
	b.ToggleSide(Current) // clear current, incoming order kept
	out, _ = b.ResolvedLines()
	if strings.Join(out, ",") != "i1" {
		t.Fatalf("after clearing current = %v, want just incoming", out)
	}
	// partial → toggle completes (does not clear)
	b = twoSideBlock()
	b.EnsurePicks()
	b.ToggleLine(Current, 1)
	b.ToggleSide(Current)
	out, _ = b.ResolvedLines()
	if strings.Join(out, ",") != "c2,c1" {
		t.Fatalf("completing a partial side = %v, want c2 first (pick order)", out)
	}
}

func TestToggleSideOrderReversed(t *testing.T) {
	b := twoSideBlock()
	b.ToggleSide(Incoming)
	b.ToggleSide(Current)
	out, _ := b.ResolvedLines()
	if strings.Join(out, ",") != "i1,c1,c2" {
		t.Fatalf("i-then-c order = %v, want incoming first", out)
	}
}

func TestToggleSideEmptySideNoOp(t *testing.T) {
	b := &Block{Current: nil, Incoming: []string{"i1"}}
	b.ToggleSide(Current)
	if b.Mode != Undecided {
		t.Fatal("toggling a zero-line side must not touch the block")
	}
	if all, any := b.SideState(Current); all || any {
		t.Fatal("a zero-line side never reads as picked")
	}
}

func TestSideStateLegacyAndLineByLine(t *testing.T) {
	b := twoSideBlock()
	b.Mode = TakeCurrent
	if all, any := b.SideState(Current); !all || !any {
		t.Fatal("TakeCurrent reads as all-current")
	}
	if all, any := b.SideState(Incoming); all || any {
		t.Fatal("TakeCurrent reads as no-incoming")
	}
	if !b.LinePicked(Current, 1) || b.LinePicked(Incoming, 0) {
		t.Fatal("LinePicked must read legacy modes")
	}
	b = twoSideBlock()
	b.EnsurePicks()
	b.ToggleLine(Current, 0)
	if all, any := b.SideState(Current); all || !any {
		t.Fatal("one of two picked = some")
	}
}

func TestToggleSideAllAndSideStateAll(t *testing.T) {
	d := &Doc{Items: []Item{
		{Block: &Block{Current: []string{"a"}, Incoming: []string{"x"}}},
		{Literal: []string{"mid"}},
		{Block: &Block{Current: []string{"b"}, Incoming: []string{"y"}}},
	}}
	d.ToggleSideAll(Current)
	if all, any := d.SideStateAll(Current); !all || !any {
		t.Fatal("master toggle should complete current everywhere")
	}
	if d.Pending() != 0 {
		t.Fatal("master toggle touches every block with that side")
	}
	// partial: clear one block, master completes instead of clearing
	d.Blocks()[0].ToggleSide(Current)
	if all, any := d.SideStateAll(Current); all || !any {
		t.Fatal("mixed state = some")
	}
	d.ToggleSideAll(Current)
	if all, _ := d.SideStateAll(Current); !all {
		t.Fatal("master on a partial state completes")
	}
	d.ToggleSideAll(Current) // now everything full → clears
	if _, any := d.SideStateAll(Current); any {
		t.Fatal("master on a full state clears")
	}
	// cleared blocks stay touched: decided-empty, Resolved drops them
	// (no FinalNewline on this hand-built doc, so no trailing \n)
	out, ok := d.Resolved()
	if !ok || string(out) != "mid" {
		t.Fatalf("decided-empty resolve = %q ok=%v, want just the literal", out, ok)
	}
}

func TestResolvedLinesUndecided(t *testing.T) {
	b := twoSideBlock()
	if _, ok := b.ResolvedLines(); ok {
		t.Fatal("undecided block must report ok=false")
	}
}
```

Add `"strings"` to the test file's imports if absent.

- [ ] **Step 2: Run the tests to verify they fail to compile**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/hunk-picker-side-toggles && go test ./internal/hunkpick/ -v`
Expected: compile errors — `EnsurePicks`, `SideState`, etc. undefined.

- [ ] **Step 3: Implement**

Append to `internal/hunkpick/hunkpick.go` (after `ToggleLine`, before `resolved`):

```go
// EnsurePicks converts a block to the line-pick representation: a legacy
// whole-side mode materializes as that side's full ordered picks, Undecided
// becomes an empty (touched) pick list. Already-LineByLine blocks are
// untouched.
func (b *Block) EnsurePicks() {
	switch b.Mode {
	case TakeCurrent:
		b.Picks = fullPicks(Current, len(b.Current))
	case TakeIncoming:
		b.Picks = fullPicks(Incoming, len(b.Incoming))
	case LineByLine:
		return
	default:
		b.Picks = nil
	}
	b.Mode = LineByLine
}

func fullPicks(s Side, n int) []Pick {
	ps := make([]Pick, 0, n)
	for i := 0; i < n; i++ {
		ps = append(ps, Pick{Side: s, Line: i})
	}
	return ps
}

// SideState reports whether all/any of side s's lines are picked, reading
// legacy whole-side modes as full picks of that side. A zero-line side is
// never all (nor any) picked.
func (b *Block) SideState(s Side) (all, any bool) {
	n := len(b.lines(s))
	if n == 0 {
		return false, false
	}
	switch b.Mode {
	case TakeCurrent:
		return s == Current, s == Current
	case TakeIncoming:
		return s == Incoming, s == Incoming
	case LineByLine:
		cnt := 0
		for _, p := range b.Picks {
			if p.Side == s && p.Line >= 0 && p.Line < n {
				cnt++
			}
		}
		return cnt == n, cnt > 0
	default:
		return false, false
	}
}

// LinePicked reports whether side s's line is in the result, reading legacy
// whole-side modes as full picks of that side.
func (b *Block) LinePicked(s Side, line int) bool {
	switch b.Mode {
	case TakeCurrent:
		return s == Current
	case TakeIncoming:
		return s == Incoming
	}
	return b.Picked(s, line)
}

// ToggleSide is the tri-state whole-side toggle: with every line of s picked
// it removes s's picks (the other side keeps its order); otherwise it appends
// s's missing lines top-to-bottom. A zero-line side is a no-op (the block is
// not even touched).
func (b *Block) ToggleSide(s Side) {
	if len(b.lines(s)) == 0 {
		return
	}
	b.EnsurePicks()
	if all, _ := b.SideState(s); all {
		kept := b.Picks[:0]
		for _, p := range b.Picks {
			if p.Side != s {
				kept = append(kept, p)
			}
		}
		b.Picks = kept
		return
	}
	for i := range b.lines(s) {
		if !b.Picked(s, i) {
			b.Picks = append(b.Picks, Pick{Side: s, Line: i})
		}
	}
}

// ResolvedLines is the exported per-block view of resolved (for previews):
// the block's picked lines in order, ok=false while Undecided.
func (b *Block) ResolvedLines() ([]string, bool) {
	return b.resolved(nil)
}
```

Append to the `Doc` section (after `SetAll`):

```go
// ToggleSideAll is ToggleSide across the document: if every block that has
// s-lines is fully picked on s, it clears s from those blocks; otherwise it
// completes s on every block that has s-lines. Blocks without s-lines are
// left alone.
func (d *Doc) ToggleSideAll(s Side) {
	allFull, seen := true, false
	for _, b := range d.Blocks() {
		if len(b.lines(s)) == 0 {
			continue
		}
		seen = true
		if full, _ := b.SideState(s); !full {
			allFull = false
		}
	}
	if !seen {
		return
	}
	for _, b := range d.Blocks() {
		if len(b.lines(s)) == 0 {
			continue
		}
		full, _ := b.SideState(s)
		if allFull || !full {
			b.ToggleSide(s)
		}
	}
}

// SideStateAll aggregates SideState for the master checkbox: all = every
// block with s-lines has s fully picked (false when no block has s-lines);
// any = at least one s line picked anywhere.
func (d *Doc) SideStateAll(s Side) (all, any bool) {
	seen := false
	all = true
	for _, b := range d.Blocks() {
		if len(b.lines(s)) == 0 {
			continue
		}
		seen = true
		ba, bany := b.SideState(s)
		if !ba {
			all = false
		}
		if bany {
			any = true
		}
	}
	return all && seen, any
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/hunk-picker-side-toggles && go test ./internal/hunkpick/ -v && go build ./...`
Expected: all PASS (existing + new); whole tree builds.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/hunk-picker-side-toggles
git add internal/hunkpick/hunkpick.go internal/hunkpick/hunkpick_test.go
git commit -m "feat(hunkpick): tri-state side toggles over the ordered line-pick model"
```

---

### Task 2: Picker key rewiring — toggles replace exclusive modes

**Files:**
- Modify: `internal/tui/conflict_picker.go` (`update` only)
- Test: `internal/tui/conflict_picker_test.go`

**Interfaces:**
- Consumes: Task 1's `ToggleSide`, `ToggleSideAll`, `EnsurePicks`, `SideState`.
- Produces: the key behavior later rendering tasks assume; no new API.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/conflict_picker_test.go`:

```go
func TestConflictPickerSideTogglesBoth(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, key("c"))
	m, _ = e.update(m, key("i")) // both on, current first
	b := e.doc.Blocks()[0]
	if ca, _ := b.SideState(hunkpick.Current); !ca {
		t.Fatal("i must not clear current")
	}
	out, _ := b.ResolvedLines()
	if strings.Join(out, ",") != "foo,bar" {
		t.Fatalf("both = %v, want current then incoming", out)
	}
	m, _ = e.update(m, key("c")) // toggle current off
	out, _ = b.ResolvedLines()
	if strings.Join(out, ",") != "bar" {
		t.Fatalf("after c off = %v, want just incoming", out)
	}
}

func TestConflictPickerToggleOffIsDecidedEmpty(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, key("c"))
	m, _ = e.update(m, key("c")) // region 0 now touched-empty
	if e.doc.Pending() != 1 {
		t.Fatalf("Pending = %d, want 1 (only untouched region 1)", e.doc.Pending())
	}
	m, _ = e.update(m, key("n"))
	m, _ = e.update(m, key("i")) // region 1 decided
	// The gate is Pending==0; do NOT press enter here — the apply path calls
	// startOp, which needs a real domain service this test doesn't have.
	if e.doc.Pending() != 0 {
		t.Fatal("touched-empty region must count as decided")
	}
	out, ok := e.doc.Resolved()
	if !ok || string(out) != "top\nmid\nC\n" {
		t.Fatalf("resolved = %q ok=%v, want region 0 dropped entirely", out, ok)
	}
}

func TestConflictPickerMasterToggleTriState(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, key("C"))
	if all, _ := e.doc.SideStateAll(hunkpick.Current); !all {
		t.Fatal("C should complete current everywhere")
	}
	m, _ = e.update(m, key("C"))
	if _, any := e.doc.SideStateAll(hunkpick.Current); any {
		t.Fatal("C on a full state should clear everywhere")
	}
	if e.doc.Pending() != 0 {
		t.Fatal("cleared regions stay touched (decided empty)")
	}
}

func TestStagePickerSpaceMaterializesDefault(t *testing.T) {
	d := hunkpick.FromDiff([]byte("a\nb\n"), []byte("a\nB\n"))
	d.SetAll(hunkpick.TakeCurrent) // the H picker's nothing-staged default
	e := newStagePicker("f.txt", d)
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyMsg("right")) // working side
	m, _ = e.update(m, keyMsg("space"))
	out, ok := d.Resolved()
	if !ok || string(out) != "a\nb\nB\n" {
		t.Fatalf("space on the default must keep the index side and add the line: %q ok=%v", out, ok)
	}
}
```

Also update the stale assertion in `TestConflictPickerOtherKeysResetViewScroll` (around line 298): replace

```go
	if e.doc.Blocks()[0].Mode != hunkpick.TakeCurrent {
		t.Fatal("c must still take current")
	}
```

with

```go
	if all, _ := e.doc.Blocks()[0].SideState(hunkpick.Current); !all {
		t.Fatal("c must still pick the whole current side")
	}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/hunk-picker-side-toggles && go test ./internal/tui/ -run TestConflictPicker -v`
Expected: `TestConflictPickerSideTogglesBoth` fails (i clears current under the old exclusive modes); the others fail similarly.

- [ ] **Step 3: Implement**

In `internal/tui/conflict_picker.go`'s `update`, replace the four cases

```go
	case "c":
		if b != nil {
			b.Mode = hunkpick.TakeCurrent
		}
	case "i":
		if b != nil {
			b.Mode = hunkpick.TakeIncoming
		}
	case "C":
		e.doc.SetAll(hunkpick.TakeCurrent)
	case "I":
		e.doc.SetAll(hunkpick.TakeIncoming)
```

with

```go
	case "c":
		if b != nil {
			b.ToggleSide(hunkpick.Current)
		}
	case "i":
		if b != nil {
			b.ToggleSide(hunkpick.Incoming)
		}
	case "C":
		e.doc.ToggleSideAll(hunkpick.Current)
	case "I":
		e.doc.ToggleSideAll(hunkpick.Incoming)
```

and replace the space case's body

```go
	case " ":
		if b != nil && e.sideLen() > 0 {
			if b.Mode != hunkpick.LineByLine {
				b.Mode = hunkpick.LineByLine
				b.Picks = nil
			}
			b.ToggleLine(e.side, e.line)
		}
```

with

```go
	case " ":
		if b != nil && e.sideLen() > 0 {
			b.EnsurePicks()
			b.ToggleLine(e.side, e.line)
		}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/hunk-picker-side-toggles && go test ./internal/tui/ -v -run 'TestConflictPicker|TestStagePicker'`
Expected: all PASS, including the pre-existing tests (`TakeSides`, `TakeAll`, `SpaceTogglesLineByLine` still hold — a single toggle of an undecided region equals the old exclusive take).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/hunk-picker-side-toggles
git add internal/tui/conflict_picker.go internal/tui/conflict_picker_test.go
git commit -m "feat(tui): picker side keys become tri-state toggles (left/right/both)"
```

---

### Task 3: Checkbox hierarchy rendering

**Files:**
- Modify: `internal/tui/conflict_picker.go` (`badge` removed; `pickerCell`, `render`'s row loop, `columnLabels`; new `tickFor`, `stateSuffix`)
- Modify: `internal/i18n/lang/ja.toml`, `ko.toml`, `zh.toml`, `ru.toml`
- Test: `internal/tui/conflict_picker_test.go`

**Interfaces:**
- Consumes: Task 1's `SideState`, `SideStateAll`, `LinePicked`; Task 2's key behavior.
- Produces: the render shapes Task 4 slots the output pane under. New i18n keys `"region %d/%d"`, `"%s first"`, `"undecided"`, `"none"`; removed keys `"hunk %d/%d — %s"`, `"line-by-line"`, `"· undecided"`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/conflict_picker_test.go`:

```go
func TestConflictPickerCheckboxHierarchy(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	out := e.render(m, "")
	// master checkboxes in the column-label row, empty at start
	if !strings.Contains(out, "[ ] current") || !strings.Contains(out, "[ ] incoming") {
		t.Fatalf("column labels must carry master checkboxes:\n%s", out)
	}
	// paired group header: region counter on the left cell, undecided suffix right
	if !strings.Contains(out, "region 1/2") {
		t.Fatalf("group header must show the region counter:\n%s", out)
	}
	if !strings.Contains(out, "undecided") {
		t.Fatalf("untouched region must show the undecided suffix:\n%s", out)
	}
	// every selectable line carries a tick even while undecided
	if !strings.Contains(out, "[ ] foo") {
		t.Fatalf("line rows must always show ticks:\n%s", out)
	}
	m, _ = e.update(m, key("c"))
	m, _ = e.update(m, key("i"))
	out = e.render(m, "")
	if !strings.Contains(out, "[x] foo") {
		t.Fatalf("picked line must tick:\n%s", out)
	}
	if !strings.Contains(out, "current first") {
		t.Fatalf("both-on region must show the order suffix:\n%s", out)
	}
	m, _ = e.update(m, key("c"))
	m, _ = e.update(m, key("i"))
	out = e.render(m, "")
	if !strings.Contains(out, "none") {
		t.Fatalf("touched-empty region must show the none suffix:\n%s", out)
	}
}

func TestConflictPickerMasterCheckboxStates(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	m, _ = e.update(m, key("C"))
	if out := e.render(m, ""); !strings.Contains(out, "[x] current") {
		t.Fatalf("full master state must show [x]:\n%s", out)
	}
	m, _ = e.update(m, key("c")) // clear region 0's current → partial
	if out := e.render(m, ""); !strings.Contains(out, "[~] current") {
		t.Fatalf("partial master state must show [~]:\n%s", out)
	}
}
```

And update `TestConflictPickerActiveSideMarked`'s two assertions: `"▶ current"` → `"▶ [ ] current"` and `"▶ incoming"` → `"▶ [ ] incoming"` (initial state has nothing picked).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/hunk-picker-side-toggles && go test ./internal/tui/ -run TestConflictPicker -v`
Expected: the two new tests fail (no checkboxes rendered); `ActiveSideMarked` fails on the new expectation.

- [ ] **Step 3: Implement**

In `internal/tui/conflict_picker.go`:

1. Delete the `badge` method entirely. Add:

```go
// tickFor renders the three-state checkbox for an (all, any) side state.
func tickFor(all, any bool) string {
	switch {
	case all:
		return "[x]"
	case any:
		return "[~]"
	default:
		return "[ ]"
	}
}

// stateSuffix names what the checkboxes cannot show: an untouched region, a
// touched-empty one, or — with both sides on — which side's lines come first.
func (e *hunkPicker) stateSuffix(b *hunkpick.Block) string {
	if b.Mode == hunkpick.Undecided {
		return " — " + i18n.T("undecided")
	}
	if b.Mode == hunkpick.LineByLine && len(b.Picks) == 0 {
		return " — " + i18n.T("none")
	}
	ca, _ := b.SideState(hunkpick.Current)
	ia, _ := b.SideState(hunkpick.Incoming)
	if ca && ia && len(b.Picks) > 0 {
		lbl := e.leftLabel
		if b.Picks[0].Side == hunkpick.Incoming {
			lbl = e.rightLabel
		}
		return " — " + i18n.T("%s first", lbl)
	}
	return ""
}
```

2. In `pickerCell`, replace the tick block

```go
	tick := ""
	if blk.Mode == hunkpick.LineByLine {
		if blk.Picked(side, r) {
			tick = "[x] "
		} else {
			tick = "[ ] "
		}
	}
```

with

```go
	tick := "[ ] "
	if blk.LinePicked(side, r) {
		tick = "[x] "
	}
```

3. In `render`'s row loop, replace the full-width group header row

```go
		rows = append(rows, colRow{full: &winCell{
			body:  marker + i18n.T("hunk %d/%d — %s", blockNo+1, len(e.blocks), e.badge(blk)),
			style: hstyle,
		}})
```

with the paired row

```go
		lAll, lAny := blk.SideState(hunkpick.Current)
		rAll, rAny := blk.SideState(hunkpick.Incoming)
		rows = append(rows, colRow{
			left: &winCell{gutter: marker, style: hstyle,
				body: tickFor(lAll, lAny) + " " + e.leftLabel + " · " + i18n.T("region %d/%d", blockNo+1, len(e.blocks))},
			right: &winCell{style: hstyle,
				body: tickFor(rAll, rAny) + " " + e.rightLabel + e.stateSuffix(blk)},
		})
```

4. In `columnLabels`, change the cell body to include the master checkbox — replace

```go
	cell := func(label string, active bool) string {
		marker, style := "  ", pickerLabel
		if active {
			marker, style = "▶ ", selectedRow
		}
		return styleCell(style, marker+label, colW)
	}
	return cell(e.leftLabel, e.side == hunkpick.Current) +
		pickerColSep + cell(e.rightLabel, e.side == hunkpick.Incoming)
```

with

```go
	cell := func(label string, s hunkpick.Side) string {
		marker, style := "  ", pickerLabel
		if e.side == s {
			marker, style = "▶ ", selectedRow
		}
		return styleCell(style, marker+tickFor(e.doc.SideStateAll(s))+" "+label, colW)
	}
	return cell(e.leftLabel, hunkpick.Current) +
		pickerColSep + cell(e.rightLabel, hunkpick.Incoming)
```

5. i18n bundles: in each of `ja.toml`, `ko.toml`, `zh.toml`, `ru.toml`, REMOVE the entries keyed `"hunk %d/%d — %s"`, `"line-by-line"`, and `"· undecided"` (grep the TUI first to confirm no other `i18n.T` call still uses them — if one does, leave that key and note it in your report), then ADD next to the other picker keys:

`ja.toml`:
```toml
"region %d/%d" = "領域 %d/%d"
"%s first" = "%s を先に"
"undecided" = "未決定"
"none" = "なし"
```

`ko.toml`:
```toml
"region %d/%d" = "영역 %d/%d"
"%s first" = "%s 먼저"
"undecided" = "미결정"
"none" = "없음"
```

`zh.toml`:
```toml
"region %d/%d" = "区域 %d/%d"
"%s first" = "%s 在前"
"undecided" = "未决定"
"none" = "无"
```

`ru.toml`:
```toml
"region %d/%d" = "область %d/%d"
"%s first" = "%s первым"
"undecided" = "не решено"
"none" = "пусто"
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/hunk-picker-side-toggles && go test ./internal/tui/ ./internal/i18n/`
Expected: PASS including the i18n AST gates (the removed keys are gone from code, the new keys are in all four bundles).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/hunk-picker-side-toggles
git add internal/tui/conflict_picker.go internal/tui/conflict_picker_test.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml
git commit -m "feat(tui): checkbox hierarchy in the hunk picker (master/region/line)"
```

---

### Task 4: Output pane

**Files:**
- Modify: `internal/tui/conflict_picker.go` (`hunkPicker` struct, `update`, `render`; new `outputLines`, `renderOutput`)
- Modify: the four i18n bundles
- Test: `internal/tui/conflict_picker_test.go`

**Interfaces:**
- Consumes: Task 1's `ResolvedLines`; Task 3's render layout.
- Produces: `hunkPicker.outCollapsed bool`; new i18n keys `"output"`, `"[o] output"`, `"‹region %d undecided›"`; removed key `"result:"`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/conflict_picker_test.go`:

```go
func TestConflictPickerOutputPane(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	out := e.render(m, "")
	if !strings.Contains(out, "output") || !strings.Contains(out, "─") {
		t.Fatalf("expanded pane needs its titled rule:\n%s", out)
	}
	if !strings.Contains(out, "‹region 1 undecided›") {
		t.Fatalf("undecided region must render its placeholder in the pane:\n%s", out)
	}
	m, _ = e.update(m, key("I")) // all incoming everywhere
	out = e.render(m, "")
	if !strings.Contains(out, "bar") || strings.Contains(out, "‹region 1 undecided›") {
		t.Fatalf("decided regions must show their picked lines in the pane:\n%s", out)
	}
	m, _ = e.update(m, key("o")) // collapse
	out = e.render(m, "")
	if strings.Contains(out, "‹region") || countRule(out) != 0 {
		t.Fatalf("collapsed pane must disappear:\n%s", out)
	}
}

// countRule counts lines that look like the output rule (contain the dashes).
func countRule(s string) int {
	n := 0
	for _, ln := range strings.Split(s, "\n") {
		if strings.Contains(ln, "──") {
			n++
		}
	}
	return n
}

func TestConflictPickerOutputAnchorFollowsFocus(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	lines, anchor := e.outputLines()
	if anchor != 1 { // "top" literal, then region 0's contribution
		t.Fatalf("anchor = %d (lines %v), want 1", anchor, lines)
	}
	e.bi = 1
	_, anchor = e.outputLines()
	if anchor != 3 { // top, ‹region 1›, mid, then region 1's contribution
		t.Fatalf("anchor for block 1 = %d, want 3", anchor)
	}
}
```

(Note: `key("I")` routes through `msg.String()` as `"I"` — same helper the existing `TestConflictPickerTakeAll` uses.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/hunk-picker-side-toggles && go test ./internal/tui/ -run TestConflictPickerOutput -v`
Expected: compile error — `outputLines` undefined, `"o"` unhandled.

- [ ] **Step 3: Implement**

In `internal/tui/conflict_picker.go`:

1. Add the field to `hunkPicker` (after `vshift int`):

```go
	outCollapsed bool // [o] hides the output pane; default shown
```

2. Add the `o` case to `update` (next to `"z"`):

```go
	case "o":
		e.outCollapsed = !e.outCollapsed
```

3. Add the pane builders:

```go
// outputLines assembles the live result — literals verbatim, each region's
// picked lines, a placeholder for an undecided region — and returns the index
// of the focused region's first line so the pane can follow the cursor.
func (e *hunkPicker) outputLines() ([]string, int) {
	var lines []string
	anchor, blockNo := 0, 0
	for _, it := range e.doc.Items {
		if it.Block == nil {
			lines = append(lines, it.Literal...)
			continue
		}
		if blockNo == e.bi {
			anchor = len(lines)
		}
		if ls, ok := it.Block.ResolvedLines(); ok {
			lines = append(lines, ls...)
		} else {
			lines = append(lines, i18n.T("‹region %d undecided›", blockNo+1))
		}
		blockNo++
	}
	return lines, anchor
}

// renderOutput windows the assembled result to h display lines of width w,
// keeping the focused region's first line in view; the picker's display mode
// applies per line (wrap expands, scroll pans with the shared hscroll).
func (e *hunkPicker) renderOutput(w, h int) []string {
	src, srcAnchor := e.outputLines()
	var dl []string
	anchor := 0
	for i, l := range src {
		if i == srcAnchor {
			anchor = len(dl)
		}
		switch e.mode {
		case modeWrap:
			ws := wrapWidth(l, w, 1<<20)
			if len(ws) == 0 {
				ws = []string{""}
			}
			dl = append(dl, ws...)
		case modeScroll:
			dl = append(dl, hslice(l, e.hscroll, w))
		default:
			dl = append(dl, truncate(l, w))
		}
	}
	start := windowStart(len(dl), h, anchor)
	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		if idx := start + i; idx < len(dl) {
			out = append(out, padRight(dl[idx], w))
		} else {
			out = append(out, padRight("", w))
		}
	}
	return out
}

// outputRule is the pane's titled separator line.
func outputRule(w int) string {
	label := "── " + i18n.T("output") + " "
	fill := w - lipgloss.Width(label)
	if fill < 0 {
		fill = 0
	}
	return pickerDim.Render(padRight(label+strings.Repeat("─", fill), w))
}
```

4. In `render`: add `i18n.T("[o] output"),` to the hint list after `i18n.T("[n/p] hunk"),`. Then split the body height — replace

```go
	bodyH := H - 3 - len(hintLines)
	if bodyH < 1 {
		bodyH = 1
	}
```

with

```go
	bodyH := H - 3 - len(hintLines)
	if bodyH < 1 {
		bodyH = 1
	}
	gridH, outH := bodyH, 0
	if !e.outCollapsed {
		outH = bodyH / 3
		if outH < 3 {
			outH = 3
		}
		if outH > bodyH-4 {
			outH = bodyH - 4 // keep ≥3 grid lines + the rule
		}
		if outH < 1 {
			outH = 0 // too small to show a pane at all
		} else {
			gridH = bodyH - outH - 1
		}
	}
```

use `gridH` in the `renderTwoCol` options (`h: gridH`), remove the inline `result:` block from the row loop:

```go
		if blk.Mode == hunkpick.LineByLine {
			rows = append(rows, colRow{full: &winCell{body: "  " + i18n.T("result:"), style: pickerDim}})
			tmp := &hunkpick.Doc{Items: []hunkpick.Item{{Block: blk}}}
			if out, ok := tmp.Resolved(); ok {
				for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
					rows = append(rows, colRow{full: &winCell{body: "    " + l, style: pickerDim}})
				}
			}
		}
```

(delete the whole block), and assemble the pane after the grid — replace

```go
	lines := []string{header, colLabels}
	lines = append(lines, body...)
	lines = append(lines, "")
	lines = append(lines, hintLines...)
	return strings.Join(lines, "\n")
```

with

```go
	lines := []string{header, colLabels}
	lines = append(lines, body...)
	if outH > 0 {
		lines = append(lines, outputRule(w))
		lines = append(lines, e.renderOutput(w, outH)...)
	}
	lines = append(lines, "")
	lines = append(lines, hintLines...)
	return strings.Join(lines, "\n")
```

5. i18n bundles: REMOVE the `"result:"` entry from all four; ADD:

`ja.toml`:
```toml
"output" = "出力"
"[o] output" = "[o] 出力"
"‹region %d undecided›" = "‹領域 %d 未決定›"
```

`ko.toml`:
```toml
"output" = "출력"
"[o] output" = "[o] 출력"
"‹region %d undecided›" = "‹영역 %d 미결정›"
```

`zh.toml`:
```toml
"output" = "输出"
"[o] output" = "[o] 输出"
"‹region %d undecided›" = "‹区域 %d 未决定›"
```

`ru.toml`:
```toml
"output" = "результат"
"[o] output" = "[o] результат"
"‹region %d undecided›" = "‹область %d не решена›"
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/hunk-picker-side-toggles && go test ./internal/tui/ ./internal/i18n/`
Expected: PASS — including every pre-existing picker/free-scroll test (the grid shrank but all assertions are content-based, and height 24/30 leaves enough grid lines).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/hunk-picker-side-toggles
git add internal/tui/conflict_picker.go internal/tui/conflict_picker_test.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml
git commit -m "feat(tui): live output pane in the hunk picker ([o] collapses)"
```

---

### Task 5: Help, README, CHANGELOG, bundle cleanup, full suite

**Files:**
- Modify: `internal/tui/help.go` (hunk-picker section, ~line 129)
- Modify: the four i18n bundles
- Modify: `README.md` (the `H` row ~line 75 and the `x` row ~line 85), `CHANGELOG.md`

**Interfaces:**
- Consumes: everything shipped in Tasks 1–4. Nothing downstream.

- [ ] **Step 1: Update the help rows**

In `internal/tui/help.go`'s hunk-picker section, replace

```go
		r("c / i", i18n.T("take the whole region from current / incoming")),
		r("C / I", i18n.T("take all regions from current / incoming")),
```

with

```go
		r("c / i", i18n.T("toggle this side of the region — all its lines; left, right, or both can be on, and toggle order = order in the result")),
		r("C / I", i18n.T("toggle this side across ALL regions (tri-state: complete everywhere, else clear everywhere)")),
		r("o", i18n.T("collapse / expand the output pane (the live assembled result)")),
```

- [ ] **Step 2: Update the bundles**

REMOVE from all four bundles the entries keyed `"take the whole region from current / incoming"` and `"take all regions from current / incoming"`. ADD:

`ja.toml`:
```toml
"toggle this side of the region — all its lines; left, right, or both can be on, and toggle order = order in the result" = "領域のこの側を全行まとめてトグル — 左・右・両方を選択でき、トグルした順が結果の順になる"
"toggle this side across ALL regions (tri-state: complete everywhere, else clear everywhere)" = "全領域でこの側をトグル（三状態: 未完なら全選択、全選択済みなら解除）"
"collapse / expand the output pane (the live assembled result)" = "出力ペインを折りたたむ / 展開する（組み立て結果のライブ表示）"
```

`ko.toml`:
```toml
"toggle this side of the region — all its lines; left, right, or both can be on, and toggle order = order in the result" = "영역의 해당 쪽 전체를 토글 — 왼쪽·오른쪽·양쪽 모두 선택 가능, 토글 순서가 결과 순서가 됨"
"toggle this side across ALL regions (tri-state: complete everywhere, else clear everywhere)" = "모든 영역에서 해당 쪽을 토글 (3단계: 미완이면 전체 선택, 전부 선택돼 있으면 해제)"
"collapse / expand the output pane (the live assembled result)" = "출력 패널 접기 / 펼치기 (실시간 조립 결과)"
```

`zh.toml`:
```toml
"toggle this side of the region — all its lines; left, right, or both can be on, and toggle order = order in the result" = "切换该区域这一侧的全部行 — 可选左侧、右侧或两侧，切换顺序即结果顺序"
"toggle this side across ALL regions (tri-state: complete everywhere, else clear everywhere)" = "在所有区域切换这一侧（三态：未全选则补全，已全选则清除）"
"collapse / expand the output pane (the live assembled result)" = "折叠 / 展开输出面板（实时组装结果）"
```

`ru.toml`:
```toml
"toggle this side of the region — all its lines; left, right, or both can be on, and toggle order = order in the result" = "переключает эту сторону области целиком — можно выбрать левую, правую или обе; порядок переключения задаёт порядок в результате"
"toggle this side across ALL regions (tri-state: complete everywhere, else clear everywhere)" = "переключает эту сторону во ВСЕХ областях (три состояния: дополнить всюду, иначе снять всюду)"
"collapse / expand the output pane (the live assembled result)" = "свернуть / развернуть панель результата (живая сборка итога)"
```

- [ ] **Step 3: README + CHANGELOG**

`README.md`, `H` row (~line 75): replace the fragment

```
`c`/`i` take the whole hunk from index/working, `C`/`I` all hunks,
```

with

```
`c`/`i` toggle a whole side of the hunk (index/working — left, right, or both can be on; toggle order = result order), `C`/`I` toggle a side across all hunks,
```

`README.md`, `x` row (~line 85): replace the fragment

```
`c`/`i` take the whole region, `C`/`I` take all regions,
```

with

```
`c`/`i` toggle a whole side of the region (left, right, or both — toggle order = result order; checkboxes at the side/region/line levels show the state), `C`/`I` toggle a side across all regions, a bottom **output** pane previews the assembled result live (`o` collapses it),
```

(Both fragments verified verbatim against the current README; keep each row a single line.)

`CHANGELOG.md` — first bullet under `## [Unreleased]`:

```markdown
- **TUI: hunk-picker sides are now toggles — keep left, right, or BOTH.**
  In the conflict resolver and the `H` staging picker, `c`/`i` no longer
  exclusively take one side: they toggle that side's lines on/off per
  region (tri-state — complete, else clear), `C`/`I` do the same across
  all regions, and `space` still toggles single lines. Everything is one
  ordered pick model, so toggle order = result order within a region, and
  a GitKraken-style checkbox hierarchy (`[x]`/`[~]`/`[ ]` at the
  side/region/line levels) shows the selection state at a glance. A region
  deliberately emptied resolves to nothing (drop both sides); untouched
  regions still gate enter. The inline `result:` preview is replaced by a
  bottom **output** pane with the live assembled result following the
  cursor (`o` collapses it).
```

- [ ] **Step 4: Full staged test run**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/hunk-picker-side-toggles && ./test.sh`
Expected: vet+gofmt clean, unit green (including `internal/web` — its `hunkpick` use is untouched), e2e green. Report any failure verbatim; do not fix unrelated breakage.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/hunk-picker-side-toggles
git add internal/tui/help.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml README.md CHANGELOG.md
git commit -m "docs(tui): help/README/CHANGELOG for picker side toggles + output pane"
```

---

## Out of scope (from the spec)

- The unstage picker (sub-project 2) and web parity (sub-project 3, `web-dev`).
- Mouse checkbox interaction; persistence of `o` across sessions.
- No `internal/agentskill` update (CLI surface unchanged). Merge into `main` is the human's call (run `./test.sh race` on a quiet machine first).
