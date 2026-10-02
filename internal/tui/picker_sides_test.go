package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/hunkpick"
)

// The output pane's first column says where each line came from: a picked
// line carries its side, an unchanged line and an untouched hunk carry none.
func TestPickerOutputMarksPickedSides(t *testing.T) {
	t.Parallel()
	e := newStagePicker("f.txt", stageDoc()) // a→A, b, c→C
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	e.ensureOutput()
	for i, s := range e.outSide {
		if s != outNone {
			t.Fatalf("untouched: output line %d must carry no side, got %v", i, s)
		}
	}
	m, _ = e.update(m, key("i")) // hunk 0: the working side
	m, _ = e.update(m, key("n"))
	m, _ = e.update(m, key("c")) // hunk 1: the index side, ticked
	e.ensureOutput()
	want := []outMark{outRight, outNone, outLeft}
	if len(e.outSide) != len(want) {
		t.Fatalf("one mark per output line: got %d marks for %d lines", len(e.outSide), len(e.outLines))
	}
	for i, w := range want {
		if e.outSide[i] != w {
			t.Fatalf("output line %d: mark %v, want %v", i, e.outSide[i], w)
		}
	}
	out := e.render(m, "")
	if n := strings.Count(out, pickerBar); n != 2 {
		t.Fatalf("two picked lines must draw two bars, got %d:\n%s", n, out)
	}
}

// The conflict resolver is the same window: current = left, incoming = right.
func TestConflictPickerOutputMarksSides(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 80, height: 24}
	m, _ = e.update(m, key("c"))
	_ = m
	e.ensureOutput()
	b := e.blocks[0]
	start := e.outStart[0]
	for i := range b.Current {
		if e.outSide[start+i] != outLeft {
			t.Fatalf("a current pick must mark left, line %d = %v", start+i, e.outSide[start+i])
		}
	}
	if len(e.outSide) != len(e.outLines) {
		t.Fatalf("marks %d != lines %d", len(e.outSide), len(e.outLines))
	}
}

// The bar takes one column from the text; no output line may outgrow the pane.
func TestPickerOutputBarKeepsWidth(t *testing.T) {
	t.Parallel()
	d := hunkpick.FromDiff([]byte("a\n"), []byte(strings.Repeat("x", 200)+"\n"))
	d.StartUntouched()
	e := newStagePicker("f.txt", d)
	e.blocks[0].ToggleSide(hunkpick.Incoming)
	e.pickRev++
	for _, mode := range []dispMode{modeScroll, modeWrap, modeCutoff} {
		e.mode = mode
		for _, l := range e.renderOutput(40, 5) {
			if w := lipgloss.Width(l); w != 40 {
				t.Fatalf("mode %v: output line is %d columns wide, want 40: %q", mode, w, l)
			}
		}
	}
}
