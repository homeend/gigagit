package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/hunkpick"
)

func pickerDoc() *hunkpick.Doc {
	d, _ := hunkpick.ParseConflict([]byte(
		"top\n<<<<<<< HEAD\nfoo\n=======\nbar\n>>>>>>> x\nmid\n<<<<<<< HEAD\nA\nB\n=======\nC\n>>>>>>> x\n"))
	return d
}

func TestConflictPickerTakeSides(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{stack: &viewStack{entries: []surface{e}}, width: 80, height: 24}
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
	m := Model{stack: &viewStack{entries: []surface{e}}, width: 80, height: 24}
	m, _ = e.update(m, key("I")) // take all incoming
	out, ok := e.doc.Resolved()
	if !ok || string(out) != "top\nbar\nmid\nC\n" {
		t.Fatalf("take-all-incoming = %q ok=%v", out, ok)
	}
}

func TestConflictPickerSpaceTogglesLineByLine(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{stack: &viewStack{entries: []surface{e}}, width: 80, height: 24}
	// focus region 0, current side, line 0; space picks it line-by-line
	m, _ = e.update(m, keyMsg("space"))
	b := e.doc.Blocks()[0]
	if b.Mode != hunkpick.LineByLine || !b.Picked(hunkpick.Current, 0) {
		t.Fatalf("space did not start line-by-line pick: mode=%v", b.Mode)
	}
}

func TestConflictPickerSideSwitchAndCursor(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{stack: &viewStack{entries: []surface{e}}, width: 80, height: 24}
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
	m := Model{stack: &viewStack{entries: []surface{e}}, width: 80, height: 24}
	// enter while pending: no apply, status set, surface still on top
	m, _ = e.update(m, keyMsg("enter"))
	if m.statusMsg == "" || m.stackTop() == nil {
		t.Fatal("enter with pending regions should warn and keep the surface")
	}
}

func TestConflictPickerRendersMarkers(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{stack: &viewStack{entries: []surface{e}}, width: 80, height: 24}
	out := e.render(m)
	if out == "" {
		t.Fatal("render produced nothing")
	}
}

func TestConflictFileLoadedPushesPicker(t *testing.T) {
	m := Model{width: 80, height: 24}
	content := []byte("<<<<<<< HEAD\na\n=======\nb\n>>>>>>> x\n")
	updated, _ := m.Update(conflictFileLoadedMsg{path: "f.txt", content: content})
	m = updated.(Model)
	if _, ok := m.stackTop().(*hunkPicker); !ok {
		t.Fatal("loaded conflict file should push the picker surface")
	}
}

func TestConflictFileLoadedBinaryNoOp(t *testing.T) {
	m := Model{width: 80, height: 24}
	updated, _ := m.Update(conflictFileLoadedMsg{path: "f.bin", content: []byte("\x00\x01\x02")})
	m = updated.(Model)
	if m.stackTop() != nil {
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
	if _, ok := m.stackTop().(*hunkPicker); !ok {
		t.Fatal("stageHunksLoadedMsg should push the hunk picker")
	}
}

func TestStageHunksLoadedNoChangeNoOp(t *testing.T) {
	m := Model{width: 80, height: 24}
	updated, _ := m.Update(stageHunksLoadedMsg{path: "f.txt", index: []byte("a\n"), work: []byte("a\n")})
	m = updated.(Model)
	if m.stackTop() != nil {
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
	m := Model{stack: &viewStack{entries: []surface{e}}, width: 80, height: 24}
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
	m := Model{stack: &viewStack{entries: []surface{e}}, width: 80, height: 24}
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
	m := Model{stack: &viewStack{entries: []surface{e}}, width: 80, height: 12}
	out := e.render(m)
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
