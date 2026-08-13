package tui

import (
	"fmt"
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
	if !strings.Contains(e.render(m, ""), "▶ [ ] current") {
		t.Fatalf("current side not marked active:\n%s", e.render(m, ""))
	}
	m, _ = e.update(m, keyMsg("right")) // → incoming
	if !strings.Contains(e.render(m, ""), "▶ [ ] incoming") {
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

func TestConflictPickerOutputAnchorEmptyTrailingRegion(t *testing.T) {
	// 30 distinct literal lines, then a final conflict block with no trailing
	// literal; deciding that block to empty must pin the pane to the END.
	var sb strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&sb, "line%02d\n", i)
	}
	sb.WriteString("<<<<<<< HEAD\nfoo\n=======\nbar\n>>>>>>> x\n")
	d, err := hunkpick.ParseConflict([]byte(sb.String()))
	if err != nil {
		t.Fatal(err)
	}
	e := newConflictPicker("f.txt", d)
	b := e.doc.Blocks()[0]
	b.ToggleSide(hunkpick.Current)
	b.ToggleSide(hunkpick.Current) // touched-empty
	out := e.renderOutput(80, 5)
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "line29") {
		t.Fatalf("pane must pin to the end for a trailing empty focused region:\n%s", joined)
	}
	if strings.Contains(joined, "line00") {
		t.Fatalf("pane must not window from the top:\n%s", joined)
	}
}

// A CRLF file's lines reach the picker with their \r kept (ParseConflict
// preserves them so Resolved round-trips the endings). The DISPLAY must
// sanitize them — a raw \r in a padded cell makes the terminal jump to
// column 0 and corrupts the whole frame (tabs likewise desync widths).
func TestConflictPickerSanitizesCRLFDisplay(t *testing.T) {
	d, err := hunkpick.ParseConflict([]byte("top\ttabbed\r\n<<<<<<< HEAD\r\nfoo\r\n=======\r\nbar\r\n>>>>>>> x\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	e := newConflictPicker("f.txt", d)
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	out := e.render(m, "")
	if strings.Contains(out, "\r") {
		t.Fatal("rendered frame must not contain carriage returns")
	}
	if strings.Contains(out, "\t") {
		t.Fatal("rendered frame must not contain raw tabs")
	}
	// Display-only: the resolution keeps the file's CRLF endings.
	e.doc.Blocks()[0].ToggleSide(hunkpick.Current)
	res, ok := d.Resolved()
	if !ok || !strings.Contains(string(res), "foo\r\n") {
		t.Fatalf("Resolved must keep CRLF: %q ok=%v", res, ok)
	}
}

// Regenerated conflict text arrives with an explicit marker size; the loaded
// handler must parse with that size so content imitating 7-char markers (an
// old conflict committed unresolved) stays plain content.
func TestConflictFileLoadedSizedMarkers(t *testing.T) {
	m := Model{width: 80, height: 24}
	content := []byte(strings.Repeat("<", 31) + " current\n<<<<<<< HEAD\n=======\n" +
		strings.Repeat("=", 31) + "\nb\n" + strings.Repeat(">", 31) + " incoming\n")
	updated, _ := m.Update(conflictFileLoadedMsg{path: "f.txt", content: content, markerSize: 31})
	m = updated.(Model)
	if _, ok := m.topLayer().(*hunkPicker); !ok {
		t.Fatalf("sized conflict content should push the picker (status %q)", m.statusMsg)
	}
}

// The group-header row's right cell must carry the same 2-char gutter the
// line rows' cursor slot occupies, or its checkbox juts out two columns left
// of every other tick in the column.
func TestConflictPickerHeaderTicksAlign(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	out := plain(e.render(m, ""))
	found := false
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "region 1/2") {
			_, right, ok := strings.Cut(ln, "║")
			// sep is " ║ ", so the right cell starts after one space; the
			// cell itself must open with the 2-char gutter then the tick.
			if !ok || !strings.HasPrefix(right, "   [") {
				t.Fatalf("right header tick misaligned: %q", ln)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no group header row rendered")
	}
}

func TestPickerTabTogglesOutputFocus(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	m, _ = e.update(m, keyMsg("tab"))
	if !e.outFocused {
		t.Fatal("tab must focus the output")
	}
	m, _ = e.update(m, keyMsg("down"))
	m, _ = e.update(m, keyMsg("down"))
	m, _ = e.update(m, keyMsg("up"))
	if e.oshift != 1 || e.bi != 0 || e.line != 0 {
		t.Fatalf("output arrows must scroll the pane only: oshift=%d bi=%d line=%d", e.oshift, e.bi, e.line)
	}
	m, _ = e.update(m, tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	if e.vshift != 1 {
		t.Fatal("alt+↓ must keep free-scrolling the grid under output focus")
	}
	m, _ = e.update(m, keyMsg("tab"))
	if e.outFocused || e.oshift != 0 {
		t.Fatalf("tab back must return to the grid and resume follow: focused=%v oshift=%d", e.outFocused, e.oshift)
	}
}

func TestPickerOutputFocusInertSelectionKeys(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	m, _ = e.update(m, keyMsg("tab"))
	m, _ = e.update(m, key("c"))
	m, _ = e.update(m, keyMsg("space"))
	m, _ = e.update(m, key("n"))
	m, _ = e.update(m, keyMsg("right"))
	b := e.doc.Blocks()[0]
	if b.Mode != hunkpick.Undecided || e.bi != 0 || e.side != hunkpick.Current {
		t.Fatalf("selection keys must be inert under output focus: mode=%v bi=%d side=%v", b.Mode, e.bi, e.side)
	}
	m, _ = e.update(m, keyMsg("esc"))
	if m.topLayer() != nil {
		t.Fatal("esc must still cancel from output focus")
	}
}

func TestPickerTabExpandsCollapsedPane(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	m, _ = e.update(m, key("o")) // collapse under grid focus, as today
	if !e.outCollapsed {
		t.Fatal("o must collapse")
	}
	m, _ = e.update(m, keyMsg("tab"))
	if e.outCollapsed || !e.outFocused {
		t.Fatal("tab on a collapsed pane must expand AND focus it")
	}
	m, _ = e.update(m, keyMsg("down"))
	m, _ = e.update(m, key("o")) // collapse from output focus
	if !e.outCollapsed || e.outFocused || e.oshift != 0 {
		t.Fatalf("o under output focus must collapse, unfocus, and reset: collapsed=%v focused=%v oshift=%d",
			e.outCollapsed, e.outFocused, e.oshift)
	}
}

func TestPickerOutputScrollMovesPaneWindow(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&sb, "line%02d\n", i)
	}
	sb.WriteString("<<<<<<< HEAD\nfoo\n=======\nbar\n>>>>>>> x\n")
	d, err := hunkpick.ParseConflict([]byte(sb.String()))
	if err != nil {
		t.Fatal(err)
	}
	e := newConflictPicker("f.txt", d)
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	// Follow mode pins the pane near the focused (EOF) region: top not visible.
	if out := plain(e.render(m, "")); strings.Contains(out, "line00") {
		t.Fatalf("follow mode should sit at the focused region, not the top:\n%s", out)
	}
	m, _ = e.update(m, keyMsg("tab"))
	for i := 0; i < 50; i++ {
		m, _ = e.update(m, keyMsg("down"))
	}
	_ = e.render(m, "")
	if e.oshift < 0 || e.oshift > 10 {
		t.Fatalf("downward scroll past the end must clamp near 0, got %d", e.oshift)
	}
	for i := 0; i < 100; i++ {
		m, _ = e.update(m, keyMsg("up"))
	}
	out := plain(e.render(m, ""))
	if !strings.Contains(out, "line00") {
		t.Fatalf("scrolled-up pane must reach the top of the result:\n%s", out)
	}
	if e.oshift <= -100 || e.oshift >= 0 {
		t.Fatalf("render must store the clamped effective shift back, got %d", e.oshift)
	}
	m, _ = e.update(m, keyMsg("tab")) // back to grid: follow resumes
	if out := plain(e.render(m, "")); strings.Contains(out, "line00") {
		t.Fatalf("tab back must resume following the cursor:\n%s", out)
	}
}

func TestPickerOutputRuleShowsFocus(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	if strings.Contains(plain(e.render(m, "")), "▶ output") {
		t.Fatal("unfocused rule must not carry the focus marker")
	}
	m, _ = e.update(m, keyMsg("tab"))
	if !strings.Contains(plain(e.render(m, "")), "▶ output") {
		t.Fatal("focused rule must carry the focus marker")
	}
}

func TestPickerHintsSwapWithFocus(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	out := plain(e.render(m, ""))
	if !strings.Contains(out, "[tab] output") || strings.Contains(out, "[tab] grid") {
		t.Fatalf("grid-focus hints wrong:\n%s", out)
	}
	m, _ = e.update(m, keyMsg("tab"))
	out = plain(e.render(m, ""))
	if !strings.Contains(out, "[tab] grid") || !strings.Contains(out, "[↑/↓] scroll") || strings.Contains(out, "[space] pick") {
		t.Fatalf("output-focus hints wrong:\n%s", out)
	}
}

// Spec test #4's enter half: from output focus, the pending gate warns AND
// hands the arrows back to the grid (the anchor moved with the revealed
// region, so a retained manual scroll would land nowhere meaningful).
func TestPickerEnterGateReturnsGridFocus(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 30}
	m, _ = e.update(m, keyMsg("tab"))
	m, _ = e.update(m, keyMsg("down"))
	m, _ = e.update(m, keyMsg("enter"))
	if m.statusMsg == "" || m.topLayer() == nil {
		t.Fatal("enter with pending regions must warn and keep the surface")
	}
	if e.outFocused || e.oshift != 0 {
		t.Fatalf("the gate must return focus to the grid: focused=%v oshift=%d", e.outFocused, e.oshift)
	}
	// z and shift+→ keep falling through under output focus
	m, _ = e.update(m, keyMsg("tab"))
	m, _ = e.update(m, key("z"))
	if e.mode != modeWrap {
		t.Fatalf("z must keep cycling the display mode under output focus, got %v", e.mode)
	}
	m, _ = e.update(m, key("z"))
	m, _ = e.update(m, key("z")) // back to scroll so shift can pan
	m, _ = e.update(m, keyMsg("shift+right"))
	if e.hscroll == 0 {
		t.Fatal("shift+→ must keep panning under output focus")
	}
}

func TestPickerCtrlTTogglesZoom(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyMsg("ctrl+t"))
	if !e.zoomed {
		t.Fatalf("ctrl+t did not zoom")
	}
	m, _ = e.update(m, keyMsg("ctrl+t"))
	if e.zoomed {
		t.Fatalf("second ctrl+t did not restore the split")
	}
}

func TestPickerEscRestoresZoomFirst(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyMsg("ctrl+t"))
	m, _ = e.update(m, keyMsg("esc"))
	if e.zoomed {
		t.Fatalf("esc under zoom did not restore the split")
	}
	if len(m.layers.entries) != 1 {
		t.Fatalf("esc under zoom closed the picker (layers = %d, want 1)", len(m.layers.entries))
	}
	m, _ = e.update(m, keyMsg("esc"))
	if len(m.layers.entries) != 0 {
		t.Fatalf("second esc did not close the picker")
	}
}

func TestPickerEscRestoresZoomWhileOutputFocused(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyMsg("tab")) // focus output
	m, _ = e.update(m, keyMsg("ctrl+t"))
	m, _ = e.update(m, keyMsg("esc"))
	if e.zoomed || len(m.layers.entries) != 1 {
		t.Fatalf("esc under output-zoom: zoomed=%v layers=%d, want false/1", e.zoomed, len(m.layers.entries))
	}
}

func TestPickerODropsZoom(t *testing.T) {
	// Grid-focused: o unzooms AND collapses the pane.
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyMsg("ctrl+t"))
	m, _ = e.update(m, keyMsg("o"))
	if e.zoomed || !e.outCollapsed {
		t.Fatalf("grid o under zoom: zoomed=%v collapsed=%v, want false/true", e.zoomed, e.outCollapsed)
	}
	// Output-focused: o unzooms, collapses, and returns focus to the grid.
	e2 := newConflictPicker("f.txt", pickerDoc())
	m2 := Model{layers: &layerStack{entries: []layer{e2}}, width: 80, height: 24}
	m2, _ = e2.update(m2, keyMsg("tab"))
	m2, _ = e2.update(m2, keyMsg("ctrl+t"))
	m2, _ = e2.update(m2, keyMsg("o"))
	if e2.zoomed || !e2.outCollapsed || e2.outFocused {
		t.Fatalf("output o under zoom: zoomed=%v collapsed=%v focused=%v", e2.zoomed, e2.outCollapsed, e2.outFocused)
	}
}
