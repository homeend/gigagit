package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/repos"
)

func slowFSPopupModel() (Model, string, string) {
	m := Model{width: 110, height: 32}
	fast := "/home/fake/fastrepo"
	slow := "/mnt/fake/slowrepo"
	m = m.pushLayer(&repoPopup{
		entries: []repos.Entry{
			{Path: fast, LastOpened: time.Now()},
			{Path: slow, LastOpened: time.Now()},
		},
		now: time.Now(),
	})
	u, _ := m.Update(repoFSMsg{foreign: map[string]bool{slow: true}})
	return u.(Model), fast, slow
}

// TestRepoPopupHeightStableAcrossSelection pins the fix for the reported
// flicker: moving the cursor between a local and a slow-fs row must NOT change
// the popup box height (the old in-body warning line made it flip).
func TestRepoPopupHeightStableAcrossSelection(t *testing.T) {
	m, _, _ := slowFSPopupModel()
	p := layerOf[*repoPopup](m)

	hLocal := len(strings.Split(p.box(m), "\n"))
	p.moveSel(1) // slow row
	hSlow := len(strings.Split(p.box(m), "\n"))
	if hLocal != hSlow {
		t.Fatalf("popup height flips with selection: local=%d slow=%d", hLocal, hSlow)
	}
	if strings.Contains(p.box(m), "switching may be very slow") {
		t.Fatal("warning must not be part of the box body (it changes height); it belongs to the overlay tooltip")
	}
}

// TestRepoPopupSlowFSTooltipOverlay pins the tooltip: rendered output carries
// the warning as an overlay one line above the popup box (under-row placement
// covered the neighbouring row — user report), and not at all while a local
// row is selected.
func TestRepoPopupSlowFSTooltipOverlay(t *testing.T) {
	m, _, _ := slowFSPopupModel()
	p := layerOf[*repoPopup](m)
	below := strings.TrimSuffix(strings.Repeat(strings.Repeat(" ", m.width)+"\n", m.height), "\n")

	out := p.render(m, below)
	if strings.Contains(out, "switching may be very slow") {
		t.Fatalf("tooltip shown for a local selection:\n%s", out)
	}

	p.moveSel(1)
	out = p.render(m, below)
	lines := strings.Split(out, "\n")
	boxTop, tipLine := -1, -1
	for i, l := range lines {
		if boxTop == -1 && strings.Contains(l, "╔") {
			boxTop = i // modalStyle's DoubleBorder top edge
		}
		if strings.Contains(l, "switching may be very slow") {
			tipLine = i
		}
	}
	if tipLine == -1 {
		t.Fatalf("tooltip missing for selected slow row:\n%s", out)
	}
	if boxTop == -1 || tipLine != boxTop-1 {
		t.Fatalf("tooltip at line %d, box top border at %d — want directly above the box", tipLine, boxTop)
	}

	// Centered horizontally on the box (left-anchoring read as lopsided — user
	// report): side gaps must match within the one-cell integer-division slack.
	// All runes involved are width-1, so rune columns are screen columns.
	stripped := strings.Split(ansi.Strip(out), "\n")
	boxLeft := runeCol(stripped[boxTop], '╔')
	boxRightExcl := runeCol(stripped[boxTop], '╗') + 1
	tipRunes := []rune(stripped[tipLine])
	tipLeft := runeCol(stripped[tipLine], '⚠') - 1 // strip's leading pad space
	tipRightExcl := len(tipRunes)
	for tipRightExcl > 0 && tipRunes[tipRightExcl-1] == ' ' {
		tipRightExcl--
	}
	tipRightExcl++ // strip's trailing pad space
	leftGap := tipLeft - boxLeft
	rightGap := boxRightExcl - tipRightExcl
	if d := leftGap - rightGap; d < -1 || d > 1 {
		t.Fatalf("tooltip gaps left=%d right=%d — want centered on the box:\n%s", leftGap, rightGap, out)
	}
}

// runeCol returns the rune column of the first occurrence of target, -1 if
// absent (valid as a column only when every rune on the line renders 1 cell).
func runeCol(line string, target rune) int {
	col := 0
	for _, r := range line {
		if r == target {
			return col
		}
		col++
	}
	return -1
}

// TestRepoPopupSlowFSEnterConfirms pins the confirm: enter on a slow-fs row
// raises a Yes/No modal (default No) instead of switching immediately; No is a
// no-op.
func TestRepoPopupSlowFSEnterConfirms(t *testing.T) {
	m, _, _ := slowFSPopupModel()
	p := layerOf[*repoPopup](m)
	p.moveSel(1)

	u, _ := m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.modal == nil {
		t.Fatal("enter on a slow-fs row must raise the confirm modal")
	}
	if got := m.modal.req.Options; len(got) != 2 || got[0] != "Yes" || got[1] != "No" {
		t.Fatalf("modal options = %v, want [Yes No]", got)
	}
	if m.modal.sel != 1 {
		t.Fatalf("default selection = %d, want 1 (No)", m.modal.sel)
	}
	if !strings.Contains(m.modal.req.Prompt, "very slow") {
		t.Fatalf("prompt = %q", m.modal.req.Prompt)
	}

	tm, cmd := m.modal.onResolve(m, "No")
	if cmd != nil {
		t.Fatal("No must be a no-op")
	}
	_ = tm
}

// TestRepoPopupLocalEnterNoConfirm pins that a local row still switches with
// no modal in the way.
func TestRepoPopupLocalEnterNoConfirm(t *testing.T) {
	m, _, _ := slowFSPopupModel()
	u, _ := m.Update(keyMsg("enter")) // sel 0 = local row
	m = u.(Model)
	if m.modal != nil {
		t.Fatal("local-row enter must not raise the slow-fs confirm")
	}
}

// TestRepoPopupSlowFSConfirmBypass pins the [ui] disable_slow_op_confirm
// bypass: with slow-op confirms disabled, enter switches without the modal —
// the same contract every other slow-op confirm honors.
func TestRepoPopupSlowFSConfirmBypass(t *testing.T) {
	m, _, _ := slowFSPopupModel()
	m.cfg.UI.DisableSlowOpConfirm = true
	p := layerOf[*repoPopup](m)
	p.moveSel(1)

	u, _ := m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.modal != nil {
		t.Fatal("confirm shown despite disable_slow_op_confirm")
	}
}
