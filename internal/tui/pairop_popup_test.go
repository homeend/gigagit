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
	m := pairModel()
	p := layerOf[*pairOpPopup](m)
	if p.mode != modeCutoff {
		t.Fatalf("default mode = %v, want modeCutoff", p.mode)
	}
	u, _ := m.Update(keyMsg("z"))
	mm := u.(Model)
	pp := layerOf[*pairOpPopup](mm)
	if pp.mode != modeWrap {
		t.Fatalf("after z, mode = %v, want modeWrap", pp.mode)
	}
}

func TestPairOpPopupRendersOps(t *testing.T) {
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
	if !strings.Contains(out, "[z] mode") {
		t.Fatalf("hint missing [z] mode:\n%s", out)
	}
}
