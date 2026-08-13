package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/hunkpick"
)

func pickerDoc() *hunkpick.Doc {
	d, _ := hunkpick.ParseConflict([]byte(
		"top\n<<<<<<< HEAD\nfoo\n=======\nbar\n>>>>>>> x\nmid\n<<<<<<< HEAD\nA\nB\n=======\nC\n>>>>>>> x\n"))
	return d
}

func TestConflictPickerTakeSides(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	// region 0 → current, region 1 → incoming
	m, _ = e.update(m, key("c"))
	m, _ = e.update(m, key("n")) // next region
	m, _ = e.update(m, key("i"))
	if e.doc.Pending() != 0 {
		t.Fatalf("Pending = %d, want 0", e.doc.Pending())
	}
	out, ok := e.doc.Resolved()
	if !ok || string(out) != "top\nfoo\nmid\nC\n" {
		t.Fatalf("resolved = %q ok=%v", out, ok)
	}
}

func TestConflictPickerTakeAll(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, key("I")) // take all incoming
	out, ok := e.doc.Resolved()
	if !ok || string(out) != "top\nbar\nmid\nC\n" {
		t.Fatalf("take-all-incoming = %q ok=%v", out, ok)
	}
}

func TestConflictPickerSpaceTogglesLineByLine(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	// focus region 0, current side, line 0; space picks it line-by-line
	m, _ = e.update(m, keyMsg("space"))
	b := e.doc.Blocks()[0]
	if b.Mode != hunkpick.LineByLine || !b.Picked(hunkpick.Current, 0) {
		t.Fatalf("space did not start line-by-line pick: mode=%v", b.Mode)
	}
}

func TestConflictPickerSideSwitchAndCursor(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyMsg("right")) // → incoming
	if e.side != hunkpick.Incoming {
		t.Fatal("→ should focus incoming side")
	}
	m, _ = e.update(m, keyMsg("left")) // ← current
	if e.side != hunkpick.Current {
		t.Fatal("← should focus current side")
	}
}

func TestConflictPickerEnterGateAndApply(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	// enter while pending: no apply, status set, surface still on top
	m, _ = e.update(m, keyMsg("enter"))
	if m.statusMsg == "" || m.topLayer() == nil {
		t.Fatal("enter with pending regions should warn and keep the surface")
	}
}

func TestConflictPickerRendersMarkers(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	out := e.render(m, "")
	if out == "" {
		t.Fatal("render produced nothing")
	}
}

// The picker must label which physical column is current and which is incoming,
// on a single header row spanning both columns (the only line carrying both
// labels and the column separator).
func TestConflictPickerShowsColumnLabels(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	out := e.render(m, "")
	var found bool
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "current") && strings.Contains(ln, "incoming") && strings.Contains(ln, "║") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no column-header row labelling current/incoming:\n%s", out)
	}
}

// The active side's label is highlighted so the cursor's column is obvious.
func TestConflictPickerActiveSideMarked(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	// Current side is active by default → its label carries the focus marker.
	if !strings.Contains(e.render(m, ""), "▶ current") {
		t.Fatalf("current side not marked active:\n%s", e.render(m, ""))
	}
	m, _ = e.update(m, keyMsg("right")) // → incoming
	if !strings.Contains(e.render(m, ""), "▶ incoming") {
		t.Fatalf("incoming side not marked active after →:\n%s", e.render(m, ""))
	}
}

// At a narrow width the action hint must wrap, not truncate — the last token
// stays visible.
func TestConflictPickerHintWrapsNotTruncated(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 40, height: 24}
	out := e.render(m, "")
	for _, tok := range []string{"[c] current", "[i] incoming", "[enter] apply", "[esc] cancel"} {
		if !strings.Contains(out, tok) {
			t.Fatalf("hint token %q missing (truncated?):\n%s", tok, out)
		}
	}
}

func TestConflictFileLoadedPushesPicker(t *testing.T) {
	m := Model{width: 80, height: 24}
	content := []byte("<<<<<<< HEAD\na\n=======\nb\n>>>>>>> x\n")
	updated, _ := m.Update(conflictFileLoadedMsg{path: "f.txt", content: content})
	m = updated.(Model)
	if _, ok := m.topLayer().(*hunkPicker); !ok {
		t.Fatal("loaded conflict file should push the picker surface")
	}
}

func TestConflictFileLoadedBinaryNoOp(t *testing.T) {
	m := Model{width: 80, height: 24}
	updated, _ := m.Update(conflictFileLoadedMsg{path: "f.bin", content: []byte("\x00\x01\x02")})
	m = updated.(Model)
	if m.topLayer() != nil {
		t.Fatal("binary file must not push a surface")
	}
}

func TestStagePickerNoGateAppliesImmediately(t *testing.T) {
	d := hunkpick.FromDiff([]byte("a\nb\n"), []byte("a\nB\n"))
	d.SetAll(hunkpick.TakeCurrent) // default: nothing staged
	e := newStagePicker("f.txt", d)
	if e.requireAll {
		t.Fatal("staging picker must not gate on Pending")
	}
	// Resolved with the default = the index content (a no-op stage).
	out, ok := d.Resolved()
	if !ok || string(out) != "a\nb\n" {
		t.Fatalf("default staging resolve = %q ok=%v, want the index", out, ok)
	}
	// Take the working side of the one hunk → staging it reproduces the work tree.
	e.doc.Blocks()[0].Mode = hunkpick.TakeIncoming
	out, _ = e.doc.Resolved()
	if string(out) != "a\nB\n" {
		t.Fatalf("staged = %q, want the working tree", out)
	}
}

func TestStageHunksLoadedPushesPicker(t *testing.T) {
	m := Model{width: 80, height: 24}
	updated, _ := m.Update(stageHunksLoadedMsg{path: "f.txt", index: []byte("a\nb\n"), work: []byte("a\nB\n")})
	m = updated.(Model)
	if _, ok := m.topLayer().(*hunkPicker); !ok {
		t.Fatal("stageHunksLoadedMsg should push the hunk picker")
	}
}

func TestStageHunksLoadedNoChangeNoOp(t *testing.T) {
	m := Model{width: 80, height: 24}
	updated, _ := m.Update(stageHunksLoadedMsg{path: "f.txt", index: []byte("a\n"), work: []byte("a\n")})
	m = updated.(Model)
	if m.topLayer() != nil {
		t.Fatal("no changes → no surface")
	}
}

func TestHunkPickerDefaultsToScroll(t *testing.T) {
	e := newStagePicker("f.txt", pickerDoc())
	if e.mode != modeScroll {
		t.Fatalf("default mode = %v, want modeScroll", e.mode)
	}
}

func TestHunkPickerZCyclesScrollWrapCutoff(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	if e.mode != modeScroll {
		t.Fatalf("start = %v", e.mode)
	}
	m, _ = e.update(m, key("z"))
	if e.mode != modeWrap {
		t.Fatalf("after 1st z = %v, want wrap", e.mode)
	}
	m, _ = e.update(m, key("z"))
	if e.mode != modeCutoff {
		t.Fatalf("after 2nd z = %v, want cutoff", e.mode)
	}
	m, _ = e.update(m, key("z"))
	if e.mode != modeScroll {
		t.Fatalf("after 3rd z = %v, want scroll", e.mode)
	}
}

func TestHunkPickerShiftPansOnlyInScroll(t *testing.T) {
	e := newStagePicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyMsg("shift+right"))
	if e.hscroll != pickerHScrollStep {
		t.Fatalf("shift+right in scroll → hscroll=%d, want %d", e.hscroll, pickerHScrollStep)
	}
	m, _ = e.update(m, keyMsg("shift+left"))
	if e.hscroll != 0 {
		t.Fatalf("shift+left → hscroll=%d, want 0", e.hscroll)
	}
	m, _ = e.update(m, keyMsg("shift+right")) // hscroll = step
	m, _ = e.update(m, key("z"))              // → wrap, hscroll reset
	if e.hscroll != 0 {
		t.Fatalf("z must reset hscroll, got %d", e.hscroll)
	}
	m, _ = e.update(m, keyMsg("shift+right")) // wrap mode: no-op
	if e.hscroll != 0 {
		t.Fatalf("shift+right in wrap must not pan, got %d", e.hscroll)
	}
}

func TestHunkPickerRenderFitsHeight(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 12}
	out := e.render(m, "")
	if got := len(splitLinesTest(out)); got != 12 {
		t.Fatalf("render produced %d lines, want 12 (the overlay height)", got)
	}
}

func splitLinesTest(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func TestConflictPickerAltScrollMovesViewNotCursor(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	m, _ = e.update(m, tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	m, _ = e.update(m, tea.KeyMsg{Type: tea.KeyUp, Alt: true})
	if e.vshift != 1 {
		t.Fatalf("vshift = %d, want 1", e.vshift)
	}
	if e.bi != 0 || e.line != 0 || e.side != hunkpick.Current {
		t.Fatal("alt+scroll must not move the cursor")
	}
	if e.doc.Blocks()[0].Mode != hunkpick.Undecided {
		t.Fatal("alt+scroll must not touch picks")
	}
}

func TestConflictPickerPlainArrowSnapsBackConsumed(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	m, _ = e.update(m, keyMsg("down"))
	if e.vshift != 0 {
		t.Fatalf("plain ↓ must reset vshift, got %d", e.vshift)
	}
	if e.bi != 0 || e.line != 0 {
		t.Fatal("first plain ↓ after a free scroll must not move the cursor")
	}
	// region 0's current side has one line, so the next ↓ steps to block 1.
	m, _ = e.update(m, keyMsg("down"))
	if e.bi != 1 || e.line != 0 {
		t.Fatalf("second ↓ must move the cursor: bi=%d line=%d", e.bi, e.line)
	}
}

func TestConflictPickerOtherKeysResetViewScroll(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	m, _ = e.update(m, key("c")) // pick key resets AND acts
	if e.vshift != 0 {
		t.Fatalf("c must reset vshift, got %d", e.vshift)
	}
	if all, _ := e.doc.Blocks()[0].SideState(hunkpick.Current); !all {
		t.Fatal("c must still pick the whole current side")
	}
	m, _ = e.update(m, tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	m, _ = e.update(m, key("n"))
	if e.vshift != 0 || e.bi != 1 {
		t.Fatalf("n must reset vshift and move block: vshift=%d bi=%d", e.vshift, e.bi)
	}
}

func TestConflictPickerRenderStoresClampedShift(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	// The doc's display lines fit the 24-row overlay, so any shift clamps to 0.
	e.vshift = 9999
	_ = e.render(m, "")
	if e.vshift != 0 {
		t.Fatalf("render must store the clamped shift back, got %d", e.vshift)
	}
}

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
