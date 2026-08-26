package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func pairModel() Model {
	m := Model{width: 100, height: 30}
	m = m.pushLayer(&pairOpPopup{marked: "feat/x", selected: "main", ops: pairOpsFor(panelBranches)})
	return m
}

func TestPairOpPopupZCyclesMode(t *testing.T) {
	t.Parallel()
	m := pairModel()
	p := layerOf[*pairOpPopup](m)
	if p.mode != modeCutoff {
		t.Fatalf("default mode = %v, want modeCutoff", p.mode)
	}
	u, _ := m.Update(keyMsg("ctrl+w"))
	mm := u.(Model)
	pp := layerOf[*pairOpPopup](mm)
	if pp.mode != modeWrap {
		t.Fatalf("after z, mode = %v, want modeWrap", pp.mode)
	}
}

func TestPairOpPopupRendersOps(t *testing.T) {
	t.Parallel()
	m := pairModel()
	p := layerOf[*pairOpPopup](m)
	out := ansi.Strip(p.box(m))
	if !strings.Contains(out, "feat/x + main") {
		t.Fatalf("header missing:\n%s", out)
	}
	// The first op is selected by default; its row carries the cursor prefix.
	if !strings.Contains(out, "> ") {
		t.Fatalf("selected-row cursor prefix missing:\n%s", out)
	}
	if !strings.Contains(out, "[ctrl+w] mode") {
		t.Fatalf("hint missing [ctrl+w] mode:\n%s", out)
	}
}

// When the marked/selected branch names make the rows wider than the default
// box, the pair-op popup opens maximized so the names — essential to knowing
// what the merge/rebase will do — are visible without pressing ctrl+t.
func TestPairOpPopupAutoMaximizesWhenCutOff(t *testing.T) {
	t.Parallel()
	ops := pairOpsFor(panelBranches)
	const marked = "fix/modal-wrap-content"
	const selected = "feat/external-tools-stage3-review"

	long := newPairOpPopup(120, marked, selected, ops)
	if !long.maximized {
		t.Fatalf("popup should auto-maximize when rows exceed the default width")
	}

	short := newPairOpPopup(120, "feat/x", "main", ops)
	if short.maximized {
		t.Fatalf("popup should NOT maximize when content fits the default width")
	}
}

// A maximized pair-op popup renders the full branch names, not a truncated
// "Merge fix/… into feat/…" row.
func TestPairOpPopupShowsFullNamesWhenMaximized(t *testing.T) {
	t.Parallel()
	ops := pairOpsFor(panelBranches)
	const marked = "fix/modal-wrap-content"
	const selected = "feat/external-tools-stage3-review"

	m := Model{width: 150, height: 40}
	p := newPairOpPopup(m.width, marked, selected, ops)
	out := ansi.Strip(p.box(m))

	want := "Merge " + marked + " into " + selected
	if !strings.Contains(out, want) {
		t.Fatalf("full merge label not shown; want substring %q in:\n%s", want, out)
	}
}
