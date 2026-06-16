package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func pairModel() Model {
	m := Model{width: 100, height: 30}
	m.pairPopup = &pairOpPopup{marked: "feat/x", selected: "main", ops: pairOpsFor(panelBranches)}
	return m
}

func TestPairOpPopupZCyclesMode(t *testing.T) {
	m := pairModel()
	if m.pairPopup.mode != modeCutoff {
		t.Fatalf("default mode = %v, want modeCutoff", m.pairPopup.mode)
	}
	u, _ := m.updatePairPopupKey(keyMsg("z"))
	mm := u.(Model)
	if mm.pairPopup.mode != modeWrap {
		t.Fatalf("after z, mode = %v, want modeWrap", mm.pairPopup.mode)
	}
}

func TestPairOpPopupRendersOps(t *testing.T) {
	m := pairModel()
	out := ansi.Strip(m.renderPairOpPopup())
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
